// Package challenge implements bounded, authoritative solo guessing without rooms.
package challenge

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Keyring struct {
	Active string            `json:"active"`
	Keys   map[string]string `json:"keys"`
}

var keyIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

func randomID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw[:]), nil
}

// LoadKeys creates a private, persistent keyring atomically on first startup.
// Rotation: add a fresh 32-byte key and select its id as active; retain old keys
// until all their tickets and the subsequent 24-hour retry receipt retention
// have expired. No key is sent to the browser or logged.
func LoadKeys(path string) (*Keyring, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		id, err := randomID("k")
		if err != nil {
			return nil, err
		}
		id = id[:17]
		raw, _ := json.Marshal(Keyring{Active: id, Keys: map[string]string{id: base64.RawURLEncoding.EncodeToString(key)}})
		file, err := os.CreateTemp(filepath.Dir(path), ".verified-keys-*")
		if err != nil {
			return nil, err
		}
		temp := file.Name()
		defer os.Remove(temp)
		if _, err = file.Write(raw); err != nil {
			file.Close()
			return nil, err
		}
		if err = file.Sync(); err != nil {
			file.Close()
			return nil, err
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
		// Link is an atomic create-if-absent, unlike Rename which can overwrite keys.
		if err = os.Link(temp, path); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("verified keyring must be a private regular file (0600)")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) > 8192 {
		return nil, errors.New("invalid verified keyring size")
	}
	var keys Keyring
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&keys); err != nil {
		return nil, errors.New("invalid verified keyring")
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid verified keyring trailing content")
	}
	if len(keys.Keys) == 0 || len(keys.Keys) > 8 {
		return nil, errors.New("verified keyring needs 1..8 keys")
	}
	for id := range keys.Keys {
		if !keyIDPattern.MatchString(id) {
			return nil, errors.New("invalid verified key id")
		}
		if _, err := keys.aead(id); err != nil {
			return nil, err
		}
	}
	if _, err := keys.aead(keys.Active); err != nil {
		return nil, err
	}
	return &keys, nil
}
func (k *Keyring) aead(id string) (cipher.AEAD, error) {
	if k == nil {
		return nil, errors.New("verified keys unavailable")
	}
	encoded, ok := k.Keys[id]
	if !ok {
		return nil, errors.New("unknown verified key")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("verified keys must contain 32 bytes")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (k *Keyring) seal(claims ticketClaims) (string, error) {
	aead, err := k.aead(k.Active)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	prefix := "v2." + k.Active + "."
	sealed := aead.Seal(nonce, nonce, raw, []byte("umaguess:verified-classic:"+prefix))
	return prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}
func (k *Keyring) open(ticket string) (ticketClaims, error) {
	var claims ticketClaims
	if len(ticket) > 2048 {
		return claims, errors.New("invalid ticket")
	}
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 || parts[0] != "v2" || !keyIDPattern.MatchString(parts[1]) {
		return claims, errors.New("invalid ticket")
	}
	aead, err := k.aead(parts[1])
	if err != nil {
		return claims, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(raw) < aead.NonceSize()+aead.Overhead() {
		return claims, errors.New("invalid ticket")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("umaguess:verified-classic:v2."+parts[1]+"."))
	if err != nil {
		return claims, errors.New("invalid ticket authentication")
	}
	if err := json.Unmarshal(plain, &claims); err != nil {
		return claims, fmt.Errorf("invalid ticket claims")
	}
	return claims, nil
}

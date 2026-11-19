package staticserv

// Face aliases resolve opaque per-card tokens without disclosing pair relationships in URLs.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// memoryPairEntry is the subset of a pack pair record the resolver needs.
type memoryPairEntry struct {
	PairID    string `json:"pairId"`
	ImagePath string `json:"imagePath"`
}

// NewFaceAlias builds a token→stored-path resolver from the content pack directory.
//
// It returns nil when the pack has no memory catalogue: a nil resolver means "no
// aliases", and the media handler then serves only real file names.
func NewFaceAlias(contentDir string) func(token string) (string, bool) {
	if contentDir == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(contentDir, "memory_pairs.json"))
	if err != nil {
		return nil
	}
	var entries []memoryPairEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		// The pack may wrap the list in an object.
		var wrapped struct {
			Pairs []memoryPairEntry `json:"pairs"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil
		}
		entries = wrapped.Pairs
	}
	if len(entries) == 0 {
		return nil
	}

	// index maps token → stored path relative to the media root.
	index := make(map[string]string, len(entries)*2)
	for _, entry := range entries {
		if entry.PairID == "" || entry.ImagePath == "" {
			continue
		}
		// ImagePath is a public route like /assets/faces/mp-x.webp; the resolver wants
		// a path relative to the media root, i.e. faces/mp-x.webp.
		stored := strings.TrimPrefix(entry.ImagePath, "/assets/")
		stored = strings.TrimPrefix(stored, "/content-assets/")
		stored = strings.TrimPrefix(stored, "/")
		for copyIndex := 0; copyIndex < 2; copyIndex++ {
			index[faceToken(entry.PairID, entry.ImagePath, copyIndex)] = stored
		}
	}
	return func(token string) (string, bool) {
		rel, ok := index[token]
		return rel, ok
	}
}

// faceToken reproduces the mode's token derivation.
//
// It must stay byte-identical to the memory mode's cardFaceRoute: the mode publishes
// the token, this resolver looks it up. Two different derivations would mean the
// published token never matches anything — which is the bug this file fixes.
func faceToken(pairID, storedPath string, copyIndex int) string {
	sum := sha256.Sum256([]byte(pairID + "\x00" + storedPath + "\x00" + strconv.Itoa(copyIndex)))
	return "f" + hex.EncodeToString(sum[:10])
}

// faceExt keeps the extension handling in one place.
func faceExt(storedPath string) string { return path.Ext(storedPath) }

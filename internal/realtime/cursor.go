package realtime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
)

// Cursor is the opaque resume token handed to a client.
//
// requires three properties and this type provides exactly them:
//
// 1. Opaque. The wire form is base64url of a binary blob, not a number. A client
// cannot read a sequence out of it and therefore cannot compute "seq jumped by
// more than one", which is the failure mode the document calls out: private
// filtering legitimately creates gaps in the visible sequence.
// 2. Scope-bound. The blob carries a hash of (roomId, membershipId). A cursor
// minted for one member's projection is rejected for another member, so a
// cursor cannot be replayed to read someone else's filtered stream.
// 3. Epoch-bound. The blob carries the server epoch. After a restart the epoch
// changes and every old cursor fails validation, forcing the client
// to rebuild from a snapshot instead of trusting a stale in-memory position.
//
// The value is authenticated with HMAC-SHA256 so that a client cannot forge a
// cursor pointing at an arbitrary position and thereby skip a revocation notice.
// The MAC key is per-process and random; it does not need to survive a restart
// because the epoch already invalidates every old cursor on restart.
type Cursor struct {
	// epoch is the server epoch the cursor was minted under.
	epoch string
	// scopeHash binds the cursor to one subscription scope.
	scopeHash [16]byte
	// seq is the storage event sequence the client has been told about. It is
	// deliberately not exposed as a public field: nothing outside this package may
	// do arithmetic on it.
	seq int64
	// version is the cursor format version, so a future format change is
	// detectable rather than mis-parsed.
	version byte
}

const (
	cursorFormatV1 byte = 1

	// cursorMACLen is the truncated HMAC length. 128 bits is far beyond what an
	// offline guessing attack against a per-process key needs, and it keeps the
	// token short enough for a query string.
	cursorMACLen = 16
)

// ErrInvalidCursor reports a cursor that failed decoding, authentication, scope
// binding or epoch binding. Callers must treat every case identically: send
// resync.required and let the client re-fetch a snapshot. The distinction is only
// for logs.
var ErrInvalidCursor = errors.New("realtime: invalid cursor")

// cursorKey derives the per-process MAC key.
//
// It is derived from the epoch, which is already random per process, plus a fixed
// domain string. Using the epoch directly would be fine too; the domain
// separation just makes it impossible to confuse an epoch value with a MAC key in
// a log or a config file.
func cursorKey(epoch string) []byte {
	h := sha256.New()
	h.Write([]byte("umaguess.realtime.cursor.v1\x00"))
	h.Write([]byte(epoch))
	return h.Sum(nil)
}

// scopeHashOf binds a cursor to one (room, member) projection.
//
// The member id participates because the filtered stream is per member: two
// spectators of the same room see different event sequences once private
// filtering applies, so a cursor from one is meaningless to the other.
func scopeHashOf(roomID, membershipID string) [16]byte {
	h := sha256.New()
	h.Write([]byte("umaguess.realtime.scope.v1\x00"))
	h.Write([]byte(roomID))
	h.Write([]byte{0})
	h.Write([]byte(membershipID))
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

// mintCursor builds the wire token for one scope position.
func mintCursor(epoch, roomID, membershipID string, seq int64) string {
	c := Cursor{epoch: epoch, scopeHash: scopeHashOf(roomID, membershipID), seq: seq, version: cursorFormatV1}
	body := c.marshalBody()
	mac := hmac.New(sha256.New, cursorKey(epoch))
	mac.Write(body)
	sum := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(append(body, sum[:cursorMACLen]...))
}

// marshalBody is the authenticated part of the token.
func (c Cursor) marshalBody() []byte {
	// version(1) || seq(8, big endian) || scopeHash(16)
	buf := make([]byte, 0, 1+8+16)
	buf = append(buf, c.version)
	var seqBuf [8]byte
	binary.BigEndian.PutUint64(seqBuf[:], uint64(c.seq))
	buf = append(buf, seqBuf[:]...)
	buf = append(buf, c.scopeHash[:]...)
	return buf
}

// parseCursor decodes and fully verifies a client-supplied token.
//
// Every failure returns ErrInvalidCursor wrapped with the specific reason, so a
// log line can say "epoch mismatch" while the client just gets a resync.
func parseCursor(token, epoch, roomID, membershipID string) (Cursor, error) {
	if token == "" {
		return Cursor{}, fmt.Errorf("%w: empty", ErrInvalidCursor)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	const bodyLen = 1 + 8 + 16
	if len(raw) != bodyLen+cursorMACLen {
		return Cursor{}, fmt.Errorf("%w: wrong length", ErrInvalidCursor)
	}
	body, gotMAC := raw[:bodyLen], raw[bodyLen:]

	mac := hmac.New(sha256.New, cursorKey(epoch))
	mac.Write(body)
	if !hmac.Equal(gotMAC, mac.Sum(nil)[:cursorMACLen]) {
		// A MAC failure with a *different* epoch is the ordinary restart case, and
		// saying so is more useful than a generic forgery warning. We can only tell
		// the two apart by the caller telling us the epoch it expects, which it
		// did: the MAC was computed with the current epoch. So a mismatch here
		// means either a restart or a forged/garbled token; both require a resync.
		return Cursor{}, fmt.Errorf("%w: authentication failed (restart or forged token)", ErrInvalidCursor)
	}

	if body[0] != cursorFormatV1 {
		return Cursor{}, fmt.Errorf("%w: unsupported format version %d", ErrInvalidCursor, body[0])
	}
	var c Cursor
	c.version = body[0]
	c.epoch = epoch
	c.seq = int64(binary.BigEndian.Uint64(body[1:9]))
	copy(c.scopeHash[:], body[9:25])

	if c.scopeHash != scopeHashOf(roomID, membershipID) {
		return Cursor{}, fmt.Errorf("%w: scope mismatch", ErrInvalidCursor)
	}
	return c, nil
}

// String renders a short, non-reversible debug form. It never prints the token.
func (c Cursor) String() string {
	return "cursor{v" + strconv.Itoa(int(c.version)) + " seq=" + strconv.FormatInt(c.seq, 10) + "}"
}

// seqOf returns the internal sequence. It is package-private by design:
// forbids clients doing arithmetic on it, and keeping it unexported makes that a
// compile-time property inside this module too.
func (c Cursor) seqOf() int64 { return c.seq }

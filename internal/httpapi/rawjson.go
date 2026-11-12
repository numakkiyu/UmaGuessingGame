package httpapi

import (
	"bytes"
	"encoding/json"
)

// jsonRawMessage is a json.RawMessage that keeps an "absent" state distinct from
// an explicit null, so an optional body field can be omitted while a required one
// still fails validation.
//
// encoding/json cannot express that distinction for a bare json.RawMessage: an
// absent field and `null` both decode to nil. Optional object fields in the
// contract (RoomPatch.modeConfig, SegmentPatch.modeConfig) need it.
type jsonRawMessage struct {
	Raw json.RawMessage
	Set bool
}

// UnmarshalJSON records presence.
func (m *jsonRawMessage) UnmarshalJSON(b []byte) error {
	m.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		m.Raw = nil
		return nil
	}
	m.Raw = append(m.Raw[:0], b...)
	return nil
}

// MarshalJSON renders the raw bytes, or null when unset.
func (m jsonRawMessage) MarshalJSON() ([]byte, error) {
	if len(m.Raw) == 0 {
		return []byte("null"), nil
	}
	return m.Raw, nil
}

// String returns the raw JSON text.
func (m jsonRawMessage) String() string { return string(m.Raw) }

// Len reports the raw byte length.
func (m jsonRawMessage) Len() int { return len(m.Raw) }

// rawJSON is a byte slice that marshals verbatim as JSON.
//
// It is used for response fields whose content was already produced as JSON by the
// domain layer (a mode projection, a stored feedback blob). It never re-encodes,
// so nothing can be silently dropped or reshaped on the way out.
type rawJSON []byte

// MarshalJSON emits the bytes verbatim.
func (r rawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

// UnmarshalJSON stores the raw bytes.
func (r *rawJSON) UnmarshalJSON(b []byte) error {
	*r = append((*r)[:0], b...)
	return nil
}

// jsonOr returns v when non-empty, otherwise the fallback literal.
func jsonOr(v []byte, fallback string) rawJSON {
	if len(v) == 0 {
		return rawJSON(fallback)
	}
	return rawJSON(v)
}

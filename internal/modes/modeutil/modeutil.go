// Package modeutil holds the small set of helpers every game mode in
// internal/modes needs: a seeded deterministic random stream, content-section
// loading and the integer score-unit conversion.
//
// Determinism contract : given the same seed string the stream
// produces the same sequence on every platform, in every process and in every
// Go release. The implementation is splitmix64 over a FNV-1a 64-bit digest of
// the seed, so it never touches math/rand (whose sequence is not a documented
// stable contract) and never reads the clock, the locale or the environment.
package modeutil

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// UnitsPerPoint is how many integer scoreUnits make one display point .
const UnitsPerPoint = 100

// Units converts a whole display-point value into integer scoreUnits.
func Units(display int) int { return display * UnitsPerPoint }

// Stream is a deterministic pseudo-random stream keyed by a seed string.
//
// The zero value is not usable; construct with NewStream. A Stream is not safe
// for concurrent use, which is fine: every call site creates its own instance
// from the round scope seed.
type Stream struct {
	state uint64
}

// fnv64a is the FNV-1a 64-bit digest used to turn an arbitrary seed string into
// the initial stream state.
func fnv64a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// NewStream builds a stream from a seed string. An empty seed still yields a
// well-defined stream so a missing seed can never silently become "random".
func NewStream(seed string) *Stream {
	h := fnv64a(seed)
	if h == 0 {
		// Guard against the degenerate all-zero state of splitmix64.
		h = 0x9E3779B97F4A7C15
	}
	return &Stream{state: h}
}

// Next returns the next 64-bit value of the splitmix64 sequence.
func (s *Stream) Next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Intn returns a value in [0,n). n<=1 always returns 0.
//
// The modulo bias is irrelevant here (n is always a small pool size and the
// result is not used for anything security sensitive); using it keeps the
// mapping from stream to index fully deterministic across platforms.
func (s *Stream) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(s.Next() % uint64(n))
}

// Shuffle performs a Fisher-Yates shuffle in place using swap.
func (s *Stream) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		j := s.Intn(i + 1)
		swap(i, j)
	}
}

// ShuffleStrings returns a shuffled copy of items, leaving the input untouched.
func (s *Stream) ShuffleStrings(items []string) []string {
	out := make([]string, len(items))
	copy(out, items)
	s.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// PickIndex returns a deterministic index in [0,n) derived from seed and salt.
// Distinct salts give independent picks from the same seed.
func PickIndex(seed, salt string, n int) int {
	if n <= 1 {
		return 0
	}
	return NewStream(seed + "\x00" + salt).Intn(n)
}

// Section looks a content section up by trying each candidate name in order.
// Modes pass their canonical name first and a couple of tolerated aliases so a
// pack that names the section slightly differently still loads .
func Section(src modes.ContentSource, names ...string) (json.RawMessage, error) {
	if src == nil {
		return nil, fmt.Errorf("modeutil: no content source")
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if raw, ok := src.Section(name); ok && len(raw) > 0 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("modeutil: content section %v not found in pack %q", names, src.Version())
}

// DecodeSection loads the first matching section and unmarshals it into out.
func DecodeSection(src modes.ContentSource, out any, names ...string) error {
	raw, err := Section(src, names...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("modeutil: decode section %v: %w", names, err)
	}
	return nil
}

// Marshal is a helper that panics on programmer error only; every payload the
// modes produce is a plain struct that cannot fail to encode.
func Marshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("modeutil: marshal %T: %v", v, err))
	}
	return raw
}

// SortedKeys returns the keys of m in ascending order so callers can iterate a
// map deterministically.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DedupSorted removes empty strings and duplicates, returning a sorted slice.
// Two sets are equal exactly when their DedupSorted forms are identical, which
// is what the classic field comparison relies on.
func DedupSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// CeilDiv returns ceil(a/b) for non-negative a and positive b.
func CeilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// ClampInt bounds v into [lo,hi].
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// FieldMap is a decoded JSON object whose keys may be spelled in more than one
// convention. Compiled packs and the legacy question bank disagree on casing
// (g1_bracket vs g1Bracket), so every mode decodes its records through this and
// names the accepted spellings explicitly. A field that is absent, null or of
// the wrong type reads as "unknown", never as a zero value that would silently
// win a comparison .
type FieldMap map[string]json.RawMessage

// DecodeFieldMap unmarshals raw into a FieldMap.
func DecodeFieldMap(raw json.RawMessage) (FieldMap, error) {
	var m FieldMap
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// raw returns the first present, non-null value among keys.
func (m FieldMap) raw(keys ...string) (json.RawMessage, bool) {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || len(v) == 0 || string(v) == "null" {
			continue
		}
		return v, true
	}
	return nil, false
}

// Has reports whether any of keys carries a non-null value.
func (m FieldMap) Has(keys ...string) bool {
	_, ok := m.raw(keys...)
	return ok
}

// Str reads a string field, returning "" when absent or null.
func (m FieldMap) Str(keys ...string) string {
	v, ok := m.raw(keys...)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

// StrList reads an array of strings, tolerating a single bare string.
func (m FieldMap) StrList(keys ...string) []string {
	v, ok := m.raw(keys...)
	if !ok {
		return nil
	}
	var list []string
	if err := json.Unmarshal(v, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(v, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}

// Int reads an integer field. ok is false when absent, null or not an integer,
// which callers must treat as "unknown" rather than zero.
func (m FieldMap) Int(keys ...string) (int, bool) {
	v, ok := m.raw(keys...)
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(v, &n); err == nil {
		return n, true
	}
	var f float64
	if err := json.Unmarshal(v, &f); err == nil {
		// Only accept exact integers so 2.5 never silently becomes 2.
		if f == float64(int(f)) {
			return int(f), true
		}
	}
	return 0, false
}

// Bool reads a boolean field; absent or null reads as false.
func (m FieldMap) Bool(keys ...string) bool {
	v, ok := m.raw(keys...)
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return false
	}
	return b
}

// Object reads a nested object field.
func (m FieldMap) Object(keys ...string) (FieldMap, bool) {
	v, ok := m.raw(keys...)
	if !ok {
		return nil, false
	}
	nested, err := DecodeFieldMap(v)
	if err != nil {
		return nil, false
	}
	return nested, true
}

// List reads an array of objects.
func (m FieldMap) List(keys ...string) ([]FieldMap, bool) {
	v, ok := m.raw(keys...)
	if !ok {
		return nil, false
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(v, &raws); err != nil {
		return nil, false
	}
	out := make([]FieldMap, 0, len(raws))
	for _, r := range raws {
		fm, err := DecodeFieldMap(r)
		if err != nil {
			continue
		}
		out = append(out, fm)
	}
	return out, true
}

// Records decodes a section that may be either a bare JSON array of records or
// an object wrapping the array under one of wrapKeys. Packs are allowed to do
// either, so modes never hard-code one shape.
func Records(raw json.RawMessage, wrapKeys ...string) ([]FieldMap, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]FieldMap, 0, len(arr))
		for _, r := range arr {
			fm, err := DecodeFieldMap(r)
			if err != nil {
				return nil, err
			}
			out = append(out, fm)
		}
		return out, nil
	}
	obj, err := DecodeFieldMap(raw)
	if err != nil {
		return nil, fmt.Errorf("modeutil: section is neither an array nor an object: %w", err)
	}
	for _, key := range wrapKeys {
		if list, ok := obj.List(key); ok {
			return list, nil
		}
	}
	return nil, fmt.Errorf("modeutil: object section has none of the wrapper keys %v", wrapKeys)
}

// SharedStateMode is the optional extension a mode implements when it owns
// round-level shared state (an occupancy board, a shared deck).
//
// The base modes.Mode contract only lets Apply return the caller's own
// participant state, so a mode with a shared board exposes the fold step here.
// The room layer is expected to do, inside its write transaction:
//
//	dec, err := mode.Apply(in)
//	if dec.Accepted {
//
// pub, priv, err := mode.CommitShared(in, dec)
// // persist pub/priv with the submission, in the same transaction
//
//	}
//
// CommitShared is pure and deterministic: replaying it with the same input and
// decision yields byte-identical output, so a retried transaction cannot
// corrupt the board.
type SharedStateMode interface {
	modes.Mode
	CommitShared(in modes.ApplyInput, dec modes.Decision) (public, private json.RawMessage, err error)
}

// SecretSource is the optional extension a mode implements when it needs the
// pack's server-only answer material in addition to the public sections.
//
// The base modes.ContentSource deliberately exposes only public sections, so a
// mode whose answers live in the private tree (race-rebuild ranks, for example)
// cannot reach them through PrepareInput.Content. The room layer is expected to
// build such a mode with NewWithSecrets and hand it the SecretStore, keeping the
// public and private types separate at the call site .
//
// A SecretSource is itself a modes.ContentSource: it answers public sections
// through the embedded source and private sections through Secrets.
type SecretSource interface {
	modes.ContentSource
	// Secrets returns the private section store, or nil when none is loaded.
	Secrets() modes.ContentSource
}

// SecretStore adapts a public source plus a private source into one SecretSource.
// A nil secret source is allowed: the mode then reports the answers as missing
// rather than silently grading against nothing.
type SecretStore struct {
	public  modes.ContentSource
	private modes.ContentSource
}

// NewSecretStore combines a public and a private content source.
func NewSecretStore(public, private modes.ContentSource) *SecretStore {
	return &SecretStore{public: public, private: private}
}

// Section implements modes.ContentSource by delegating to the public source.
func (s *SecretStore) Section(name string) (json.RawMessage, bool) {
	if s == nil || s.public == nil {
		return nil, false
	}
	return s.public.Section(name)
}

// Version implements modes.ContentSource.
func (s *SecretStore) Version() string {
	if s == nil || s.public == nil {
		return ""
	}
	return s.public.Version()
}

// Secrets implements SecretSource.
func (s *SecretStore) Secrets() modes.ContentSource {
	if s == nil {
		return nil
	}
	return s.private
}

// PrivateSection looks a section up in the private store only. It returns an
// error that names the pack when the secret material is missing, which is what a
// mode needs to refuse a round instead of grading against an empty answer set.
func PrivateSection(src modes.ContentSource, name string) (json.RawMessage, error) {
	secrets, ok := src.(SecretSource)
	if !ok {
		return nil, fmt.Errorf("modeutil: content source carries no private sections, need %q", name)
	}
	private := secrets.Secrets()
	if private == nil {
		return nil, fmt.Errorf("modeutil: no private section store loaded, need %q", name)
	}
	raw, found := private.Section(name)
	if !found || len(raw) == 0 {
		return nil, fmt.Errorf("modeutil: private section %q not found in pack %q", name, private.Version())
	}
	return raw, nil
}

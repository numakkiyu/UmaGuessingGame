package modeutil

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// TestStreamGoldenSequence pins the exact splitmix64 output for a fixed seed.
//
// These constants are the determinism contract of the whole rule engine: every
// mode draws its question, layout and first mover from this stream. If anyone
// changes the algorithm, this test fails immediately, which is exactly the point
// .
func TestStreamGoldenSequence(t *testing.T) {
	want := []uint64{
		15743028121834684796,
		17739393220038367152,
		9230374159352187682,
		13903310618523326413,
		7884799184832374655,
	}
	stream := NewStream("golden-seed")
	for i, expected := range want {
		if got := stream.Next(); got != expected {
			t.Fatalf("Next() #%d = %d, want %d", i+1, got, expected)
		}
	}
}

// TestStreamGoldenShuffle pins a whole shuffle permutation, so a change to the
// Fisher-Yates direction or the modulo mapping is caught too.
func TestStreamGoldenShuffle(t *testing.T) {
	list := []string{"a", "b", "c", "d", "e", "f"}
	got := NewStream("shuffle-seed").ShuffleStrings(list)
	want := []string{"b", "e", "a", "c", "f", "d"}
	if len(got) != len(want) {
		t.Fatalf("shuffle length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shuffle = %v, want %v", got, want)
		}
	}
	// The input must not be mutated.
	wantOriginal := []string{"a", "b", "c", "d", "e", "f"}
	for i := range wantOriginal {
		if list[i] != wantOriginal[i] {
			t.Fatalf("ShuffleStrings mutated its input: %v", list)
		}
	}
}

// TestStreamDeterministicSameSeed asserts two streams from one seed agree.
func TestStreamDeterministicSameSeed(t *testing.T) {
	a := NewStream("same-seed")
	b := NewStream("same-seed")
	for i := 0; i < 64; i++ {
		if x, y := a.Next(), b.Next(); x != y {
			t.Fatalf("value #%d differs: %d vs %d", i+1, x, y)
		}
	}
}

// TestStreamDifferentSeedsDiffer asserts distinct seeds produce distinct streams,
// which is what makes a per-round seed meaningful.
func TestStreamDifferentSeedsDiffer(t *testing.T) {
	seeds := []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"}
	seen := make(map[uint64]struct{}, len(seeds))
	for _, seed := range seeds {
		v := NewStream(seed).Next()
		if _, dup := seen[v]; dup {
			t.Fatalf("seed %q collided with an earlier seed on its first value", seed)
		}
		seen[v] = struct{}{}
	}
}

// TestShuffleDifferentSeedsDiffer asserts shuffles vary across seeds, so a
// question pool is actually drawn from rather than always taking the same slice.
func TestShuffleDifferentSeedsDiffer(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	seen := make(map[string]struct{}, 16)
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		got := NewStream(seed).ShuffleStrings(base)
		key := ""
		for _, v := range got {
			key += v
		}
		seen[key] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("shuffle never varies across seeds: %v", seen)
	}
}

// TestIntnBounds asserts Intn stays in range and handles degenerate sizes.
func TestIntnBounds(t *testing.T) {
	stream := NewStream("intn-seed")
	for _, n := range []int{0, 1, 2, 3, 7, 100} {
		for i := 0; i < 200; i++ {
			got := stream.Intn(n)
			if n <= 1 {
				if got != 0 {
					t.Fatalf("Intn(%d) = %d, want 0", n, got)
				}
				continue
			}
			if got < 0 || got >= n {
				t.Fatalf("Intn(%d) = %d, out of range", n, got)
			}
		}
	}
}

// TestPickIndexGolden pins the seed+salt pick used to choose a round's answer.
func TestPickIndexGolden(t *testing.T) {
	if got := PickIndex("seed-alpha", "classic.answer", 6); got != 5 {
		t.Fatalf("PickIndex = %d, want 5", got)
	}
	// Distinct salts give independent picks.
	if PickIndex("seed-alpha", "classic.answer", 6) != PickIndex("seed-alpha", "classic.answer", 6) {
		t.Fatal("PickIndex is not deterministic")
	}
	if got := PickIndex("seed-alpha", "other.salt", 1); got != 0 {
		t.Fatalf("PickIndex with n=1 = %d, want 0", got)
	}
}

// TestEmptySeedIsDefined asserts an empty seed still yields a usable stream rather
// than silently becoming "unseeded".
func TestEmptySeedIsDefined(t *testing.T) {
	a := NewStream("")
	b := NewStream("")
	if a.Next() != b.Next() {
		t.Fatal("an empty seed is not deterministic")
	}
	if a.state == 0 {
		t.Fatal("an empty seed produced the degenerate zero state")
	}
}

// fakeContent is a minimal ContentSource for the section helpers.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack" }

// TestSectionPrefersCanonicalName asserts the canonical section name wins over an
// alias, so a pack that ships both cannot shadow the documented name.
func TestSectionPrefersCanonicalName(t *testing.T) {
	src := fakeContent{
		"classic.characters": json.RawMessage(`[{"id":"canonical"}]`),
		"characters":         json.RawMessage(`[{"id":"alias"}]`),
	}
	raw, err := Section(src, "classic.characters", "characters")
	if err != nil {
		t.Fatalf("Section: %v", err)
	}
	if string(raw) != `[{"id":"canonical"}]` {
		t.Fatalf("Section picked %s, want the canonical name", raw)
	}
}

// TestSectionFallsBackToAlias asserts an alias is used when the canonical name is
// absent.
func TestSectionFallsBackToAlias(t *testing.T) {
	src := fakeContent{"characters": json.RawMessage(`[{"id":"alias"}]`)}
	raw, err := Section(src, "classic.characters", "characters")
	if err != nil {
		t.Fatalf("Section: %v", err)
	}
	if string(raw) != `[{"id":"alias"}]` {
		t.Fatalf("Section = %s, want the alias payload", raw)
	}
}

// TestSectionMissing asserts a missing section is an error that names the pack.
func TestSectionMissing(t *testing.T) {
	if _, err := Section(fakeContent{}, "nope"); err == nil {
		t.Fatal("Section accepted a missing section")
	}
	if _, err := Section(nil, "nope"); err == nil {
		t.Fatal("Section accepted a nil source")
	}
}

// TestDecodeSection asserts the decode helper unmarshals a found section.
func TestDecodeSection(t *testing.T) {
	src := fakeContent{"sort.cards": json.RawMessage(`[{"characterId":"a"}]`)}
	var out []struct {
		CharacterID string `json:"characterId"`
	}
	if err := DecodeSection(src, &out, "sort.cards"); err != nil {
		t.Fatalf("DecodeSection: %v", err)
	}
	if len(out) != 1 || out[0].CharacterID != "a" {
		t.Fatalf("decoded %+v", out)
	}
}

// TestFieldMapReadsBothSpellings asserts the flexible decoder reads snake_case and
// camelCase, which is required because the compiled pack and the legacy question
// bank disagree .
func TestFieldMapReadsBothSpellings(t *testing.T) {
	raw := json.RawMessage(`{
		"id": "a",
		"star": 3,
		"surface_group": ["草地"],
		"g1Bracket": "3-4",
		"hasJpn": true,
		"nested": {"x": "y"}
	}`)
	fm, err := DecodeFieldMap(raw)
	if err != nil {
		t.Fatalf("DecodeFieldMap: %v", err)
	}
	if got := fm.Str("id"); got != "a" {
		t.Fatalf("Str(id) = %q", got)
	}
	if got, ok := fm.Int("star"); !ok || got != 3 {
		t.Fatalf("Int(star) = %d,%v", got, ok)
	}
	if got := fm.StrList("surfaceGroup", "surface_group"); len(got) != 1 || got[0] != "草地" {
		t.Fatalf("StrList = %v", got)
	}
	if got := fm.Str("g1Bracket", "g1_bracket"); got != "3-4" {
		t.Fatalf("Str(g1Bracket) = %q", got)
	}
	if !fm.Bool("hasJpn") {
		t.Fatal("Bool(hasJpn) = false")
	}
	if nested, ok := fm.Object("nested"); !ok || nested.Str("x") != "y" {
		t.Fatal("Object(nested) failed")
	}
}

// TestFieldMapUnknownIsNotZero asserts an absent or null field reports "unknown"
// rather than a zero value that could silently win a comparison .
func TestFieldMapUnknownIsNotZero(t *testing.T) {
	fm, err := DecodeFieldMap(json.RawMessage(`{"star": null, "height": 1.5}`))
	if err != nil {
		t.Fatalf("DecodeFieldMap: %v", err)
	}
	if _, ok := fm.Int("star"); ok {
		t.Fatal("a null int reported as known")
	}
	if _, ok := fm.Int("missing"); ok {
		t.Fatal("a missing int reported as known")
	}
	// A non-integral number must not silently truncate.
	if _, ok := fm.Int("height"); ok {
		t.Fatal("a fractional number was accepted as an int")
	}
	if fm.Has("star") {
		t.Fatal("Has(star) = true for a null value")
	}
}

// TestRecordsAcceptsArrayAndWrapper asserts a section may be a bare array or an
// object wrapping the array, which the packs do both ways.
func TestRecordsAcceptsArrayAndWrapper(t *testing.T) {
	array := json.RawMessage(`[{"id":"a"},{"id":"b"}]`)
	got, err := Records(array, "cards")
	if err != nil {
		t.Fatalf("Records(array): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("array records = %d, want 2", len(got))
	}

	wrapped := json.RawMessage(`{"cards":[{"id":"a"},{"id":"b"},{"id":"c"}]}`)
	got, err = Records(wrapped, "cards")
	if err != nil {
		t.Fatalf("Records(wrapped): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("wrapped records = %d, want 3", len(got))
	}

	if _, err := Records(json.RawMessage(`{"other":[]}`), "cards"); err == nil {
		t.Fatal("Records accepted an object with no matching wrapper key")
	}
	if _, err := Records(json.RawMessage(`"scalar"`), "cards"); err == nil {
		t.Fatal("Records accepted a scalar")
	}
}

// TestDedupSorted asserts set normalisation: order and duplicates are irrelevant.
func TestDedupSorted(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "already sorted", input: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "reordered", input: []string{"b", "a"}, want: []string{"a", "b"}},
		{name: "duplicates", input: []string{"a", "a", "b"}, want: []string{"a", "b"}},
		{name: "empties dropped", input: []string{"a", "", "b"}, want: []string{"a", "b"}},
		{name: "all empty", input: []string{"", ""}, want: nil},
		{name: "nil", input: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DedupSorted(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("DedupSorted = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("DedupSorted = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestCeilDiv asserts the ceiling helper used by the draft price formula.
func TestCeilDiv(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{0, 2, 0}, {1, 2, 1}, {2, 2, 1}, {3, 2, 2}, {4, 2, 2},
		{1, 1, 1}, {5, 3, 2}, {6, 3, 2}, {7, 3, 3}, {1, 0, 0}, {-1, 2, 0},
	}
	for _, tc := range cases {
		if got := CeilDiv(tc.a, tc.b); got != tc.want {
			t.Fatalf("CeilDiv(%d,%d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestUnits asserts the integer score-unit conversion .
func TestUnits(t *testing.T) {
	if UnitsPerPoint != 100 {
		t.Fatalf("UnitsPerPoint = %d, want 100", UnitsPerPoint)
	}
	for _, display := range []int{0, 1, 20, 44, 100} {
		if got := Units(display); got != display*100 {
			t.Fatalf("Units(%d) = %d", display, got)
		}
	}
}

// TestSortedKeys asserts map keys are returned in ascending order so callers can
// iterate deterministically instead of relying on map order .
func TestSortedKeys(t *testing.T) {
	m := map[string]int{"c": 3, "a": 1, "b": 2}
	got := SortedKeys(m)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedKeys = %v, want %v", got, want)
		}
	}
	if len(SortedKeys(map[string]int{})) != 0 {
		t.Fatal("SortedKeys of an empty map is not empty")
	}
}

// TestClampInt asserts the clamp helper.
func TestClampInt(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5}, {-1, 0, 10, 0}, {11, 0, 10, 10}, {0, 0, 0, 0},
	}
	for _, tc := range cases {
		if got := ClampInt(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Fatalf("ClampInt(%d,%d,%d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

// TestSharedStateModeContract asserts the optional extension is a superset of the
// base Mode interface, so the room layer can type-assert it safely.
func TestSharedStateModeContract(t *testing.T) {
	// A compile-time assertion: SharedStateMode embeds modes.Mode.
	var _ SharedStateMode = (*stubShared)(nil)
}

// stubShared is a minimal SharedStateMode implementation.
type stubShared struct{}

func (*stubShared) ID() string                                { return "stub" }
func (*stubShared) RulesVersion() string                      { return "v" }
func (*stubShared) Title() string                             { return "t" }
func (*stubShared) Summary() string                           { return "s" }
func (*stubShared) InformationStyles() []string               { return []string{"closed"} }
func (*stubShared) RoundWindow(json.RawMessage) time.Duration { return 0 }
func (*stubShared) TurnWindow(json.RawMessage) time.Duration  { return 0 }
func (*stubShared) MinSubjects() int                          { return 1 }
func (*stubShared) MaxSubjects() int                          { return 2 }
func (*stubShared) Requirement() modes.ContentRequirement     { return modes.ContentRequirement{} }
func (*stubShared) ValidateConfig(json.RawMessage) error      { return nil }
func (*stubShared) Prepare(context.Context, modes.PrepareInput) (modes.Prepared, error) {
	return modes.Prepared{}, nil
}
func (*stubShared) Apply(modes.ApplyInput) (modes.Decision, error) { return modes.Decision{}, nil }
func (*stubShared) Project(modes.ProjectInput) (json.RawMessage, error) {
	return nil, nil
}
func (*stubShared) Settle(modes.SettleInput) ([]modes.Score, error) { return nil, nil }
func (*stubShared) CommitShared(modes.ApplyInput, modes.Decision) (json.RawMessage, json.RawMessage, error) {
	return nil, nil, nil
}

// TestMarshalIsStable asserts Marshal produces the same bytes for the same value,
// which every determinism test in the mode packages relies on.
func TestMarshalIsStable(t *testing.T) {
	value := struct {
		B int `json:"b"`
		A int `json:"a"`
	}{B: 2, A: 1}
	if string(Marshal(value)) != string(Marshal(value)) {
		t.Fatal("Marshal is not stable")
	}
	if got := string(Marshal(value)); got != `{"b":2,"a":1}` {
		t.Fatalf("Marshal = %s", got)
	}
}

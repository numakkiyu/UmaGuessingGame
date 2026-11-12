package sortgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// cardSpec is the on-the-wire spelling of a sortable card.
type cardSpec struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	HeightCm    *int   `json:"heightCm,omitempty"`
	BirthMonth  *int   `json:"birthMonth,omitempty"`
	BirthDay    *int   `json:"birthDay,omitempty"`
	EventDate   any    `json:"eventDate,omitempty"`
}

// intPtr returns a pointer to v.
func intPtr(v int) *int { return &v }

// packOf builds a sort pack.
func packOf(cards ...cardSpec) fakeContent {
	raw, err := json.Marshal(cards)
	if err != nil {
		panic(err)
	}
	return fakeContent{"sort.cards": raw}
}

// heightPack builds a six-card pack with distinct heights.
func heightPack() fakeContent {
	return packOf(
		cardSpec{CharacterID: "a", Name: "甲", HeightCm: intPtr(150)},
		cardSpec{CharacterID: "b", Name: "乙", HeightCm: intPtr(155)},
		cardSpec{CharacterID: "c", Name: "丙", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "d", Name: "丁", HeightCm: intPtr(165)},
		cardSpec{CharacterID: "e", Name: "戊", HeightCm: intPtr(170)},
		cardSpec{CharacterID: "f", Name: "己", HeightCm: intPtr(175)},
	)
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-1",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// prepare runs Prepare and returns the decoded halves.
func prepare(t *testing.T, pack fakeContent, seed, cfg string, subjects ...modes.Subject) (modes.Prepared, publicPrepared, privatePrepared) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: subjects,
		Config:  json.RawMessage(cfg),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicPrepared
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privatePrepared
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return prep, pub, priv
}

// sortAction builds a contract-shaped SortOrder payload from a flat ascending id
// list, treating every id as its own value group.
func sortAction(order []string) modes.ModeAction {
	groups := make([][]string, 0, len(order))
	for _, id := range order {
		groups = append(groups, []string{id})
	}
	return sortActionGroups(groups)
}

// sortActionGroups builds a SortOrder payload from explicit value groups.
func sortActionGroups(groups [][]string) modes.ModeAction {
	raw, _ := json.Marshal(sortPayload{Kind: ActionSort, Groups: groups})
	return modes.ModeAction{Kind: ActionSort, Payload: raw}
}

// applyOne runs one Apply call.
func applyOne(t *testing.T, prep modes.Prepared, subj modes.Subject, prev json.RawMessage, action modes.ModeAction) (modes.Decision, state) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("apply"),
		Subject: subj,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   prev,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var st state
	if len(dec.State) > 0 {
		if err := json.Unmarshal(dec.State, &st); err != nil {
			t.Fatalf("decode state: %v", err)
		}
	}
	return dec, st
}

// ascendingIDs returns the card ids of the prepared question in true order.
func ascendingIDs(priv privatePrepared) []string {
	out := make([]string, len(priv.Order))
	copy(out, priv.Order)
	return out
}

// TestHeightOrdering asserts a strictly ascending height order scores 100 and a
// wrong order loses exactly the disagreeing pairs .
func TestHeightOrdering(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, heightPack(), "height-seed", `{"key":"height"}`, subjects...)
	if len(pub.Cards) != 6 {
		t.Fatalf("cards = %d, want 6", len(pub.Cards))
	}
	if pub.TotalPairs != 15 {
		t.Fatalf("totalPairs = %d, want 15", pub.TotalPairs)
	}

	order := ascendingIDs(priv)
	dec, st := applyOne(t, prep, subjects[0], nil, sortAction(order))
	if !dec.Accepted {
		t.Fatalf("ascending order rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.CorrectPairs != 15 || st.TotalPairs != 15 {
		t.Fatalf("correct = %d/%d, want 15/15", st.CorrectPairs, st.TotalPairs)
	}

	// Reversed order has no correct pair.
	reversed := make([]string, len(order))
	for i := range order {
		reversed[i] = order[len(order)-1-i]
	}
	_, stRev := applyOne(t, prep, subjects[0], nil, sortAction(reversed))
	if stRev.CorrectPairs != 0 {
		t.Fatalf("reversed order scored %d, want 0", stRev.CorrectPairs)
	}

	// Swapping the last two keeps 14 of 15.
	swapped := make([]string, len(order))
	copy(swapped, order)
	swapped[len(swapped)-1], swapped[len(swapped)-2] = swapped[len(swapped)-2], swapped[len(swapped)-1]
	_, stSwap := applyOne(t, prep, subjects[0], nil, sortAction(swapped))
	if stSwap.CorrectPairs != 14 {
		t.Fatalf("adjacent swap scored %d/15, want 14/15", stSwap.CorrectPairs)
	}
}

// TestBirthdaySameDayTie covers the documented "生日同日" case: two cards sharing a
// month/day are equal, and the pair only counts when the submission ties them
// .
func TestBirthdaySameDayTie(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// b and c share 03/15.
	pack := packOf(
		cardSpec{CharacterID: "a", Name: "甲", BirthMonth: intPtr(1), BirthDay: intPtr(5)},
		cardSpec{CharacterID: "b", Name: "乙", BirthMonth: intPtr(3), BirthDay: intPtr(15)},
		cardSpec{CharacterID: "c", Name: "丙", BirthMonth: intPtr(3), BirthDay: intPtr(15)},
		cardSpec{CharacterID: "d", Name: "丁", BirthMonth: intPtr(12), BirthDay: intPtr(31)},
	)
	prep, pub, priv := prepare(t, pack, "bday-seed", `{"key":"birthday","cardCount":4}`, subjects...)
	if pub.TotalPairs != 6 {
		t.Fatalf("totalPairs = %d, want 6", pub.TotalPairs)
	}

	// The tied pair must be stated as one group to score every pair.
	dec, st := applyOne(t, prep, subjects[0], nil, sortActionGroups([][]string{{"a"}, {"b", "c"}, {"d"}}))
	if !dec.Accepted {
		t.Fatalf("tie-group order rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.CorrectPairs != 6 {
		t.Fatalf("tie-group order scored %d/6, want 6/6", st.CorrectPairs)
	}

	// Splitting the tie into a strict order loses exactly that pair.
	_, stStrict := applyOne(t, prep, subjects[0], nil, sortAction([]string{"a", "b", "c", "d"}))
	if stStrict.CorrectPairs != 5 {
		t.Fatalf("strict order of a tied pair scored %d/6, want 5/6", stStrict.CorrectPairs)
	}

	// The private order keeps the tie as equal values.
	if priv.Values["b"] != priv.Values["c"] {
		t.Fatalf("tied birthdays have different values: %d vs %d",
			priv.Values["b"], priv.Values["c"])
	}
}

// TestCrossYearBirthdayOrdering covers the documented "跨年" case: birthdays are
// compared by month/day, so a January birthday precedes a December one regardless
// of any birth year .
func TestCrossYearBirthdayOrdering(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := packOf(
		cardSpec{CharacterID: "dec", Name: "十二月", BirthMonth: intPtr(12), BirthDay: intPtr(1)},
		cardSpec{CharacterID: "jan", Name: "一月", BirthMonth: intPtr(1), BirthDay: intPtr(31)},
		cardSpec{CharacterID: "jun", Name: "六月", BirthMonth: intPtr(6), BirthDay: intPtr(15)},
	)
	prep, pub, priv := prepare(t, pack, "cross-seed", `{"key":"birthday","cardCount":3}`, subjects...)
	if pub.TotalPairs != 3 {
		t.Fatalf("totalPairs = %d, want 3", pub.TotalPairs)
	}

	// January 31 sorts before June 15 and December 1: no year is involved.
	if priv.Values["jan"] >= priv.Values["jun"] {
		t.Fatalf("january (%d) did not precede june (%d)", priv.Values["jan"], priv.Values["jun"])
	}
	if priv.Values["jun"] >= priv.Values["dec"] {
		t.Fatalf("june (%d) did not precede december (%d)", priv.Values["jun"], priv.Values["dec"])
	}

	dec, st := applyOne(t, prep, subjects[0], nil, sortAction([]string{"jan", "jun", "dec"}))
	if !dec.Accepted {
		t.Fatalf("month/day order rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.CorrectPairs != 3 {
		t.Fatalf("month/day order scored %d/3, want 3/3", st.CorrectPairs)
	}

	// Sorting by the string form would put "12/01" before "01/31". Only the
	// jan<jun pair survives that order, so exactly one of three pairs is right:
	// the numeric comparison is what is used, not the lexical one.
	_, stWrong := applyOne(t, prep, subjects[0], nil, sortAction([]string{"dec", "jan", "jun"}))
	if stWrong.CorrectPairs != 1 {
		t.Fatalf("lexical order scored %d/3, want 1/3", stWrong.CorrectPairs)
	}
}

// TestSameHeightTie covers the documented "相同身高" case .
func TestSameHeightTie(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := packOf(
		cardSpec{CharacterID: "a", Name: "甲", HeightCm: intPtr(155)},
		cardSpec{CharacterID: "b", Name: "乙", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "c", Name: "丙", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "d", Name: "丁", HeightCm: intPtr(165)},
	)
	prep, _, priv := prepare(t, pack, "same-height", `{"key":"height","cardCount":4}`, subjects...)

	if priv.Values["b"] != priv.Values["c"] {
		t.Fatal("equal heights have different values")
	}

	// Tied heights grouped score everything.
	_, st := applyOne(t, prep, subjects[0], nil, sortActionGroups([][]string{{"a"}, {"b", "c"}, {"d"}}))
	if st.CorrectPairs != 6 {
		t.Fatalf("tie-group order scored %d/6, want 6/6", st.CorrectPairs)
	}
	// A strict order of the tied pair loses one pair.
	_, stStrict := applyOne(t, prep, subjects[0], nil, sortAction([]string{"a", "b", "c", "d"}))
	if stStrict.CorrectPairs != 5 {
		t.Fatalf("strict order scored %d/6, want 5/6", stStrict.CorrectPairs)
	}
}

// TestDateStringNotSortedLexically covers the documented "字符日期排序错误" case: a
// date must be compared by parsed components, never by string collation
// .
func TestDateStringNotSortedLexically(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// "2026-9-30" is not ISO-padded, so it must be rejected rather than sorted
	// by its string form.
	pack := packOf(
		cardSpec{CharacterID: "a", Name: "甲", EventDate: map[string]string{"date": "2026-01-05", "region": "JP"}},
		cardSpec{CharacterID: "b", Name: "乙", EventDate: map[string]string{"date": "2026-10-30", "region": "JP"}},
		cardSpec{CharacterID: "c", Name: "丙", EventDate: map[string]string{"date": "2026-09-30", "region": "JP"}},
		cardSpec{CharacterID: "bad", Name: "坏", EventDate: map[string]string{"date": "2026-9-30", "region": "JP"}},
	)
	prep, pub, priv := prepare(t, pack, "date-seed", `{"key":"eventDate","cardCount":3}`, subjects...)

	// The malformed date is not admitted to the question.
	for _, card := range pub.Cards {
		if card.CharacterID == "bad" {
			t.Fatal("a malformed date was admitted to the question")
		}
	}
	// Lexical order would be 01-05, 09-30, 10-30; numeric order is the same here
	// because the strings are zero-padded, so compare against the numeric truth.
	if !(priv.Values["a"] < priv.Values["c"] && priv.Values["c"] < priv.Values["b"]) {
		t.Fatalf("numeric date order wrong: a=%d c=%d b=%d",
			priv.Values["a"], priv.Values["c"], priv.Values["b"])
	}

	dec, st := applyOne(t, prep, subjects[0], nil, sortAction([]string{"a", "c", "b"}))
	if !dec.Accepted {
		t.Fatalf("date order rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.CorrectPairs != 3 {
		t.Fatalf("date order scored %d/3, want 3/3", st.CorrectPairs)
	}
}

// TestDateStringCollationDifference proves the comparison is numeric and not
// lexical, using dates whose lexical and numeric orders differ.
func TestDateStringCollationDifference(t *testing.T) {
	// With zero-padded ISO dates lexical and numeric order agree, so the real
	// risk is an unpadded or locale-formatted date. Those are rejected outright,
	// which this test asserts together with the parse helper itself.
	cases := []struct {
		input  string
		wantOK bool
		year   int
		month  int
		day    int
	}{
		{input: "2026-01-05", wantOK: true, year: 2026, month: 1, day: 5},
		{input: "2026-12-31", wantOK: true, year: 2026, month: 12, day: 31},
		{input: "2026-9-30", wantOK: false},
		{input: "2026/09/30", wantOK: false},
		{input: "30-09-2026", wantOK: false},
		{input: "2026-02-30", wantOK: false},
		{input: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			year, month, day, ok := parseISODate(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("parseISODate(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if year != tc.year || month != tc.month || day != tc.day {
				t.Fatalf("parseISODate(%q) = %d-%d-%d, want %d-%d-%d",
					tc.input, year, month, day, tc.year, tc.month, tc.day)
			}
		})
	}
}

// TestEventRegionMixingExcluded covers the documented "事件地区混用" case: a
// region-pinned question only admits cards from that region .
func TestEventRegionMixingExcluded(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := packOf(
		cardSpec{CharacterID: "jp1", Name: "日1", EventDate: map[string]string{"date": "2026-01-05", "region": "JP"}},
		cardSpec{CharacterID: "jp2", Name: "日2", EventDate: map[string]string{"date": "2026-02-05", "region": "JP"}},
		cardSpec{CharacterID: "jp3", Name: "日3", EventDate: map[string]string{"date": "2026-03-05", "region": "JP"}},
		cardSpec{CharacterID: "cn1", Name: "中1", EventDate: map[string]string{"date": "2025-01-05", "region": "CN"}},
		cardSpec{CharacterID: "cn2", Name: "中2", EventDate: map[string]string{"date": "2025-02-05", "region": "CN"}},
	)
	_, pub, _ := prepare(t, pack, "region-seed", `{"key":"eventDate","cardCount":3,"eventRegion":"JP"}`, subjects...)
	for _, card := range pub.Cards {
		if card.CharacterID == "cn1" || card.CharacterID == "cn2" {
			t.Fatalf("a card from another region (%s) was admitted", card.CharacterID)
		}
	}
	if len(pub.Cards) != 3 {
		t.Fatalf("cards = %d, want 3", len(pub.Cards))
	}
}

// TestAllEqualQuestionNotGenerated covers the documented "全部同值题不生成" case: a
// question whose cards all share one value is refused .
func TestAllEqualQuestionNotGenerated(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	allSameHeight := packOf(
		cardSpec{CharacterID: "a", Name: "甲", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "b", Name: "乙", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "c", Name: "丙", HeightCm: intPtr(160)},
	)
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("all-equal"),
		Subject: subjects,
		Config:  json.RawMessage(`{"key":"height","cardCount":3}`),
		Content: allSameHeight,
	})
	if err == nil {
		t.Fatal("an all-equal question was generated")
	}

	// One differing card makes it generatable again.
	oneDiffers := packOf(
		cardSpec{CharacterID: "a", Name: "甲", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "b", Name: "乙", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "c", Name: "丙", HeightCm: intPtr(165)},
	)
	if _, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("all-equal"),
		Subject: subjects,
		Config:  json.RawMessage(`{"key":"height","cardCount":3}`),
		Content: oneDiffers,
	}); err != nil {
		t.Fatalf("a question with one differing value was refused: %v", err)
	}
}

// TestUnknownValuesNotAdmitted covers the documented "未知字段不准入" rule
// .
func TestUnknownValuesNotAdmitted(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// Only two cards have a height; asking for three must fail.
	pack := packOf(
		cardSpec{CharacterID: "a", Name: "甲", HeightCm: intPtr(155)},
		cardSpec{CharacterID: "b", Name: "乙", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "x", Name: "未知"},
	)
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("unknown"),
		Subject: subjects,
		Config:  json.RawMessage(`{"key":"height","cardCount":3}`),
		Content: pack,
	})
	if err == nil {
		t.Fatal("a question was built with too few known values")
	}

	// A question of two known cards is fine, and the unknown card is absent.
	_, pub, _ := prepare(t, pack, "unknown", `{"key":"height","cardCount":2}`, subjects...)
	if len(pub.Cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(pub.Cards))
	}
	for _, card := range pub.Cards {
		if card.CharacterID == "x" {
			t.Fatal("a card with an unknown value was admitted")
		}
	}
}

// TestInvalidOrdersRejected covers malformed submissions.
func TestInvalidOrdersRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, heightPack(), "invalid-seed", `{"key":"height"}`, subjects...)

	ids := make([]string, 0, len(pub.Cards))
	for _, card := range pub.Cards {
		ids = append(ids, card.CharacterID)
	}
	cases := []struct {
		name  string
		order []string
		want  string
	}{
		{name: "too few", order: ids[:5], want: CodeIncomplete},
		{name: "too many", order: append(append([]string{}, ids...), "zzz"), want: CodeIncomplete},
		{name: "duplicate", order: []string{ids[0], ids[0], ids[2], ids[3], ids[4], ids[5]}, want: CodeDuplicateCard},
		{name: "unknown", order: []string{ids[0], ids[1], ids[2], ids[3], ids[4], "zzz"}, want: CodeUnknownCard},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := applyOne(t, prep, subjects[0], nil, sortAction(tc.order))
			if dec.Accepted {
				t.Fatalf("accepted an invalid order: %v", tc.order)
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q", dec.Code, tc.want)
			}
		})
	}
}

// TestRevisionsAllowed covers the documented "截止前可修订" rule .
func TestRevisionsAllowed(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, priv := prepare(t, heightPack(), "rev-seed", `{"key":"height"}`, subjects...)
	order := ascendingIDs(priv)
	reversed := make([]string, len(order))
	for i := range order {
		reversed[i] = order[len(order)-1-i]
	}

	_, st1 := applyOne(t, prep, subjects[0], nil, sortAction(reversed))
	if st1.CorrectPairs != 0 {
		t.Fatalf("reversed order scored %d, want 0", st1.CorrectPairs)
	}
	dec2, st2 := applyOne(t, prep, subjects[0], modeutil.Marshal(st1), sortAction(order))
	if !dec2.Accepted {
		t.Fatalf("revision rejected: %s", dec2.Code)
	}
	if st2.CorrectPairs != 15 {
		t.Fatalf("revised order scored %d, want 15", st2.CorrectPairs)
	}
	if st2.Revisions != 2 {
		t.Fatalf("revisions = %d, want 2", st2.Revisions)
	}
}

// TestSettleScoresAndRanks asserts settlement scores, the zero case and shared
// ranks .
func TestSettleScoresAndRanks(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	prep, _, priv := prepare(t, heightPack(), "settle-seed", `{"key":"height"}`, subjects...)
	order := ascendingIDs(priv)

	_, st1 := applyOne(t, prep, subjects[0], nil, sortAction(order))
	_, st2 := applyOne(t, prep, subjects[1], nil, sortAction(order))

	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("settle-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States: map[string]json.RawMessage{
			"p1": modeutil.Marshal(st1),
			"p2": modeutil.Marshal(st2),
		},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("scores = %d, want 3", len(scores))
	}
	if scores[0].ScoreUnits != 10000 || scores[1].ScoreUnits != 10000 {
		t.Fatalf("correct scores = %d,%d, want 10000,10000",
			scores[0].ScoreUnits, scores[1].ScoreUnits)
	}
	if scores[0].Rank != 1 || scores[1].Rank != 1 {
		t.Fatalf("tied ranks = %d,%d, want 1,1", scores[0].Rank, scores[1].Rank)
	}
	if !scores[0].SharedRank || !scores[1].SharedRank {
		t.Fatal("tied scores must report SharedRank")
	}
	if scores[2].ScoreUnits != 0 || scores[2].Rank != 3 {
		t.Fatalf("no-submission = %d units rank %d, want 0 rank 3",
			scores[2].ScoreUnits, scores[2].Rank)
	}
}

// TestScoreFloorArithmetic asserts floor(100×correct/total) never rounds up.
func TestScoreFloorArithmetic(t *testing.T) {
	cases := []struct {
		correct int
		total   int
		want    int
	}{
		{correct: 0, total: 15, want: 0},
		{correct: 1, total: 15, want: 600},   // 6.67 → 6
		{correct: 2, total: 15, want: 1300},  // 13.3 → 13
		{correct: 7, total: 15, want: 4600},  // 46.7 → 46
		{correct: 14, total: 15, want: 9300}, // 93.3 → 93
		{correct: 15, total: 15, want: 10000},
		{correct: 1, total: 3, want: 3300}, // 33.3 → 33
		{correct: 2, total: 3, want: 6600}, // 66.7 → 66
		{correct: 0, total: 0, want: 0},
	}
	for _, tc := range cases {
		if got := scoreUnits(tc.correct, tc.total); got != tc.want {
			t.Fatalf("scoreUnits(%d,%d) = %d, want %d", tc.correct, tc.total, got, tc.want)
		}
	}
}

// TestPrepareDeterministic asserts the determinism gate .
func TestPrepareDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// A pack larger than the question, so the draw is exercised.
	pack := packOf(
		cardSpec{CharacterID: "a", HeightCm: intPtr(150)},
		cardSpec{CharacterID: "b", HeightCm: intPtr(152)},
		cardSpec{CharacterID: "c", HeightCm: intPtr(154)},
		cardSpec{CharacterID: "d", HeightCm: intPtr(156)},
		cardSpec{CharacterID: "e", HeightCm: intPtr(158)},
		cardSpec{CharacterID: "f", HeightCm: intPtr(160)},
		cardSpec{CharacterID: "g", HeightCm: intPtr(162)},
		cardSpec{CharacterID: "h", HeightCm: intPtr(164)},
	)
	first, pubA, _ := prepare(t, pack, "det-seed", `{"key":"height","cardCount":4}`, subjects...)
	second, pubB, _ := prepare(t, pack, "det-seed", `{"key":"height","cardCount":4}`, subjects...)
	if string(first.Public) != string(second.Public) {
		t.Fatal("prepared public state differs between identical prepares")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("prepared private state differs between identical prepares")
	}
	if len(pubA.Cards) != 4 || len(pubB.Cards) != 4 {
		t.Fatalf("card counts = %d,%d, want 4,4", len(pubA.Cards), len(pubB.Cards))
	}
	// Across seeds the drawn set varies.
	seen := map[string]struct{}{}
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		_, pub, _ := prepare(t, pack, seed, `{"key":"height","cardCount":4}`, subjects...)
		key := ""
		for _, card := range pub.Cards {
			key += card.CharacterID
		}
		seen[key] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("the drawn set never varies across seeds: %v", seen)
	}
}

// TestSettleDeterministic asserts settlement is order-independent .
func TestSettleDeterministic(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, _, priv := prepare(t, heightPack(), "settle-det", `{"key":"height"}`, subjects...)
	order := ascendingIDs(priv)
	reversed := make([]string, len(order))
	for i := range order {
		reversed[i] = order[len(order)-1-i]
	}
	_, st1 := applyOne(t, prep, subjects[0], nil, sortAction(order))
	_, st2 := applyOne(t, prep, subjects[1], nil, sortAction(reversed))

	run := func() string {
		scores, err := New().Settle(modes.SettleInput{
			Scope:   scope("settle-det"),
			Phase:   modes.PhaseSettled,
			Public:  prep.Public,
			Private: prep.Private,
			States: map[string]json.RawMessage{
				"p2": modeutil.Marshal(st2),
				"p1": modeutil.Marshal(st1),
			},
		})
		if err != nil {
			t.Fatalf("Settle: %v", err)
		}
		return string(modeutil.Marshal(scores))
	}
	if a, b := run(), run(); a != b {
		t.Fatal("settle output depends on iteration order")
	}
}

// TestProjectHidesAnswerUntilReveal asserts the true ordering is not projected
// during play .
func TestProjectHidesAnswerUntilReveal(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, priv := prepare(t, heightPack(), "proj-seed", `{"key":"height"}`, subjects...)

	during, err := New().Project(modes.ProjectInput{
		Scope:   scope("proj-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var duringView projection
	if err := json.Unmarshal(during, &duringView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if duringView.Answer != nil {
		t.Fatal("the answer order was projected during play")
	}

	after, err := New().Project(modes.ProjectInput{
		Scope:     scope("proj-seed"),
		Viewer:    modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    map[string]json.RawMessage{},
		Phase:     modes.PhaseSettled,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("Project reveal: %v", err)
	}
	var afterView projection
	if err := json.Unmarshal(after, &afterView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if afterView.Answer == nil {
		t.Fatal("the reveal did not include the true order")
	}
	if len(afterView.Answer.Order) != len(priv.Order) {
		t.Fatalf("revealed order = %v, want %v", afterView.Answer.Order, priv.Order)
	}
}

// TestConfigValidation covers config bounds .
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
	}{
		{name: "empty uses defaults", cfg: ``},
		{name: "height valid", cfg: `{"key":"height"}`},
		{name: "birthday valid", cfg: `{"key":"birthday"}`},
		{name: "eventDate valid", cfg: `{"key":"eventDate"}`},
		{name: "unknown key rejected", cfg: `{"key":"weight"}`, wantErr: true},
		{name: "one card rejected", cfg: `{"cardCount":1}`, wantErr: true},
		{name: "seven cards rejected", cfg: `{"cardCount":7}`, wantErr: true},
		{name: "two cards valid", cfg: `{"cardCount":2}`},
		{name: "six cards valid", cfg: `{"cardCount":6}`},
		{name: "negative seconds rejected", cfg: `{"questionSeconds":-1}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error for %s", tc.cfg)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestPrepareRejectsBadPack asserts an unusable pack fails before the round opens
// .
func TestPrepareRejectsBadPack(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	cases := []struct {
		name string
		pack fakeContent
	}{
		{name: "no section", pack: fakeContent{}},
		{name: "empty array", pack: packOf()},
		{name: "one card only", pack: packOf(
			cardSpec{CharacterID: "a", HeightCm: intPtr(150)},
		)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bad-pack"),
				Subject: subjects,
				Config:  json.RawMessage(`{"key":"height","cardCount":2}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare accepted an unusable pack")
			}
		})
	}
}

// TestInformationStyleControlsCardValues pins the open/closed difference.
//
// defines two forms:
//
//	closed — 靠原作知识，题面明确不提供完整资料
//	open — 所有判定字段在局内卡面可查，考选择和规划
//
// The mode used to compute `values` and `displays` and then never write them
// into publicCard, so an "open" round published exactly what a closed one did:
// names and images, no comparable value. The host asked for a planning game and
// got an archive-recall game, with no error anywhere.
//
// This test reads the prepared public state and asserts the two forms differ in
// the one way that matters: whether the key value is on the card.
func TestInformationStyleControlsCardValues(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := packOf(
		cardSpec{CharacterID: "a", HeightCm: intPtr(150)},
		cardSpec{CharacterID: "b", HeightCm: intPtr(152)},
		cardSpec{CharacterID: "c", HeightCm: intPtr(154)},
		cardSpec{CharacterID: "d", HeightCm: intPtr(156)},
		cardSpec{CharacterID: "e", HeightCm: intPtr(158)},
		cardSpec{CharacterID: "f", HeightCm: intPtr(160)},
	)

	closed, closedPub, _ := prepare(t, pack, "style-seed",
		`{"key":"height","cardCount":4,"informationStyle":"closed"}`, subjects...)
	open, openPub, _ := prepare(t, pack, "style-seed",
		`{"key":"height","cardCount":4,"informationStyle":"open"}`, subjects...)

	// Same draw: only the published values may differ.
	if len(closedPub.Cards) != len(openPub.Cards) {
		t.Fatalf("card counts differ: closed=%d open=%d", len(closedPub.Cards), len(openPub.Cards))
	}
	for i := range closedPub.Cards {
		if closedPub.Cards[i].CharacterID != openPub.Cards[i].CharacterID {
			t.Fatalf("the draw differs at %d: closed=%q open=%q",
				i, closedPub.Cards[i].CharacterID, openPub.Cards[i].CharacterID)
		}
	}

	// Closed publishes identities only .
	for _, card := range closedPub.Cards {
		if card.Display != "" {
			t.Fatalf("closed published display %q for %q; a client could sort on it",
				card.Display, card.CharacterID)
		}
		if card.Raw != nil {
			t.Fatalf("closed published raw %d for %q; the round is auto-solvable",
				*card.Raw, card.CharacterID)
		}
	}

	// Open publishes both forms on every card .
	for _, card := range openPub.Cards {
		if card.Display == "" {
			t.Fatalf("open published no display for %q; the card cannot be read",
				card.CharacterID)
		}
		if card.Raw == nil {
			t.Fatalf("open published no raw for %q; the card cannot be sorted exactly",
				card.CharacterID)
		}
	}

	// Omitting informationStyle must preserve closed-book answer privacy
	defaulted, defaultPub, _ := prepare(t, pack, "style-seed",
		`{"key":"height","cardCount":4}`, subjects...)
	if len(defaulted.Public) == 0 {
		t.Fatal("defaulted prepare produced no public state")
	}
	for _, card := range defaultPub.Cards {
		if card.Raw != nil || card.Display != "" {
			t.Fatalf("the default must be closed, but %q published a value", card.CharacterID)
		}
	}

	// An unknown style must be refused, not silently treated as closed.
	if err := (&Mode{}).ValidateConfig(json.RawMessage(
		`{"key":"height","cardCount":4,"informationStyle":"half-open"}`)); err == nil {
		t.Fatal("an unknown informationStyle must be rejected")
	}
	_ = closed
	_ = open
}

// Closed-book resubmission must not expose a pairwise scoring oracle through
// receipts, one's own projection, or another participant's progress.
func TestClosedBookFeedbackAndProgressWaitForReveal(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}, {ID: "p2", Kind: modes.SubjectPlayer}}
	for _, style := range []string{StyleClosed, StyleOpen} {
		t.Run(style, func(t *testing.T) {
			prep, _, priv := prepare(t, heightPack(), "feedback", `{"key":"height","informationStyle":"`+style+`"}`, subjects...)
			dec, _ := applyOne(t, prep, subjects[0], nil, sortAction(ascendingIDs(priv)))
			var feedback map[string]any
			if err := json.Unmarshal(dec.Feedback, &feedback); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"correctPairs", "scoreUnits"} {
				_, exists := feedback[key]
				if exists != (style == StyleOpen) {
					t.Fatalf("%s receipt leaks or loses %s: %s", style, key, dec.Feedback)
				}
			}
			for _, reveal := range []bool{false, true} {
				for _, viewer := range subjects {
					phase := modes.PhaseCollecting
					if reveal {
						phase = modes.PhaseRevealed
					}
					raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: map[string]json.RawMessage{"p1": dec.State}, Viewer: modes.Viewer{Role: "player", Subject: viewer}, Phase: phase, RevealNow: reveal})
					if err != nil {
						t.Fatal(err)
					}
					var projected struct {
						Self     map[string]any   `json:"self"`
						Subjects []map[string]any `json:"subjects"`
						Answer   any              `json:"answer"`
					}
					if err := json.Unmarshal(raw, &projected); err != nil {
						t.Fatal(err)
					}
					for _, item := range append(projected.Subjects, projected.Self) {
						_, exists := item["correctPairs"]
						if exists != (reveal || style == StyleOpen) {
							t.Fatalf("%s projection exposes/loses score: %s", style, raw)
						}
					}
					if (projected.Answer != nil) != reveal {
						t.Fatalf("incorrect reveal gate: %s", raw)
					}
				}
			}
		})
	}
}

func TestProjectionPreservesSubmittedGroupsAcrossRevisions(t *testing.T) {
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, _, _ := prepare(t, heightPack(), "groups", `{"key":"height"}`, subject)
	var previous json.RawMessage
	for _, groups := range [][][]string{{{"f", "a"}, {"b"}, {"d", "c"}, {"e"}}, {{"a"}, {"b", "c", "d"}, {"e"}, {"f"}}} {
		dec, st := applyOne(t, prep, subject, previous, sortActionGroups(groups))
		if !dec.Accepted {
			t.Fatalf("rejected: %s", dec.Code)
		}
		previous = dec.State
		if string(modeutil.Marshal(st.Groups)) != string(modeutil.Marshal(groups)) {
			t.Fatal("lost submitted groups")
		}
		for _, reveal := range []bool{false, true} {
			phase := modes.PhaseCollecting
			if reveal {
				phase = modes.PhaseRevealed
			}
			raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: map[string]json.RawMessage{"p1": previous}, Viewer: modes.Viewer{Role: "player", Subject: subject}, Phase: phase, RevealNow: reveal})
			if err != nil {
				t.Fatal(err)
			}
			var projected projection
			if err := json.Unmarshal(raw, &projected); err != nil {
				t.Fatal(err)
			}
			if projected.Self == nil || string(modeutil.Marshal(projected.Self.Groups)) != string(modeutil.Marshal(groups)) {
				t.Fatalf("projection changed the participant's choice: %s", raw)
			}
		}
	}
}

func TestEmptySubmittedGroupsRejected(t *testing.T) {
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, _, _ := prepare(t, heightPack(), "empty-group", `{"key":"height"}`, subject)
	dec, _ := applyOne(t, prep, subject, nil, sortActionGroups([][]string{{"a"}, {"b"}, {}, {"c"}, {"d"}, {"e"}, {"f"}}))
	if dec.Accepted || dec.Code != CodeIncomplete {
		t.Fatalf("empty group accepted: %+v", dec)
	}
}

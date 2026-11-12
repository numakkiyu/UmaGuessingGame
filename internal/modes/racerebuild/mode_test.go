package racerebuild

import (
	"context"
	"encoding/json"
	"strings"
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

// starterSpec is the on-the-wire spelling of a published starter.
type starterSpec struct {
	CharacterID   string `json:"characterId"`
	CharacterName string `json:"characterName"`
}

// raceSpec is the on-the-wire spelling of a published race.
type raceSpec struct {
	RaceID   string        `json:"raceId"`
	Name     string        `json:"name"`
	Date     string        `json:"date"`
	Starters []starterSpec `json:"starters"`
}

// answerSpec is the on-the-wire spelling of a private settled answer.
type answerSpec struct {
	RaceID  string              `json:"raceId"`
	Order   []string            `json:"order"`
	Ranks   map[string]int      `json:"ranks,omitempty"`
	Ties    map[string][]string `json:"ties,omitempty"`
	Dropped []string            `json:"dropped,omitempty"`
}

// packOf builds a public section plus a private answer store.
//
// The published section carries identities only; the ranks live in the private
// section, which is exactly the split the real pack uses .
func packOf(races ...raceSpec) modes.ContentSource {
	return packWithAnswers(races, nil)
}

// packWithAnswers builds a public section plus a private answer store, wrapped in
// the SecretSource the mode requires.
func packWithAnswers(races []raceSpec, answers []answerSpec) modes.ContentSource {
	pubRaw, err := json.Marshal(races)
	if err != nil {
		panic(err)
	}
	privRaw, err := json.Marshal(struct {
		Mode  string       `json:"mode"`
		Races []answerSpec `json:"races"`
	}{Mode: ModeID, Races: answers})
	if err != nil {
		panic(err)
	}
	return modeutil.NewSecretStore(
		fakeContent{"race-rebuild.races": pubRaw},
		fakeContent{"race-rebuild.answers": privRaw},
	)
}

// publicOnly returns a source that carries the public section but no secrets, to
// assert the mode refuses to grade without answer material.
func publicOnly(races ...raceSpec) modes.ContentSource {
	raw, err := json.Marshal(races)
	if err != nil {
		panic(err)
	}
	return fakeContent{"race-rebuild.races": raw}
}

// secretSourceOf wraps a public and a private source into a SecretSource.
func secretSourceOf(public, private modes.ContentSource) modes.ContentSource {
	return modeutil.NewSecretStore(public, private)
}

// strictRace is a five-starter race whose settled answer is a strict 1..5 order.
func strictRace(id string) (raceSpec, answerSpec) {
	race := raceSpec{
		RaceID: id,
		Name:   "测试锦标",
		Date:   "2026-01-02",
		Starters: []starterSpec{
			{CharacterID: "a", CharacterName: "甲"},
			{CharacterID: "b", CharacterName: "乙"},
			{CharacterID: "c", CharacterName: "丙"},
			{CharacterID: "d", CharacterName: "丁"},
			{CharacterID: "e", CharacterName: "戊"},
		},
	}
	answer := answerSpec{RaceID: id, Order: []string{"a", "b", "c", "d", "e"}}
	return race, answer
}

// strictPack builds a pack with one strict five-starter race.
func strictPack(id string) modes.ContentSource {
	race, answer := strictRace(id)
	return packWithAnswers([]raceSpec{race}, []answerSpec{answer})
}

// strictPair bundles a strict race with its answer.
type strictPair struct {
	race   raceSpec
	answer answerSpec
}

// mustStrict returns a strict race/answer pair.
func mustStrict(id string) strictPair {
	race, answer := strictRace(id)
	return strictPair{race: race, answer: answer}
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
func prepare(t *testing.T, pack modes.ContentSource, seed, cfg string, subjects ...modes.Subject) (modes.Prepared, publicPrepared, privatePrepared) {
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

// orderAction builds a contract-shaped RaceOrder payload from a flat id list,
// treating every id as its own rank group.
func orderAction(order []string) modes.ModeAction {
	groups := make([][]string, 0, len(order))
	for _, id := range order {
		groups = append(groups, []string{id})
	}
	return orderActionGroups(groups)
}

// orderActionGroups builds a RaceOrder payload from explicit rank groups.
func orderActionGroups(groups [][]string) modes.ModeAction {
	raw, _ := json.Marshal(orderPayload{Kind: ActionOrder, Groups: groups})
	return modes.ModeAction{Kind: ActionOrder, Payload: raw}
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

// TestScoreCorrectPairs asserts floor(100×correctPairs/totalPairs) over all
// character pairs .
func TestScoreCorrectPairs(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, strictPack("r1"), "score-seed", `{}`, subjects...)

	// Five participants → 10 pairs.
	if pub.TotalPairs != 10 {
		t.Fatalf("totalPairs = %d, want 10", pub.TotalPairs)
	}

	cases := []struct {
		name      string
		order     []string
		wantUnits int
		wantPairs int
	}{
		{
			name:      "perfect order is 100",
			order:     []string{"a", "b", "c", "d", "e"},
			wantUnits: 10000, wantPairs: 10,
		},
		{
			name:      "fully reversed is 0",
			order:     []string{"e", "d", "c", "b", "a"},
			wantUnits: 0, wantPairs: 0,
		},
		{
			name:      "swapping the last two keeps 9 of 10",
			order:     []string{"a", "b", "c", "e", "d"},
			wantUnits: 9000, wantPairs: 9,
		},
		{
			name:      "swapping the first two keeps 9 of 10",
			order:     []string{"b", "a", "c", "d", "e"},
			wantUnits: 9000, wantPairs: 9,
		},
		{
			name:      "middle rotation keeps 7 of 10",
			order:     []string{"a", "c", "d", "e", "b"},
			wantUnits: 7000, wantPairs: 7,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, st := applyOne(t, prep, subjects[0], nil, orderAction(tc.order))
			if !dec.Accepted {
				t.Fatalf("order rejected: %s %s", dec.Code, dec.Detail)
			}
			if st.CorrectPairs != tc.wantPairs {
				t.Fatalf("correctPairs = %d, want %d", st.CorrectPairs, tc.wantPairs)
			}
			if st.TotalPairs != 10 {
				t.Fatalf("totalPairs = %d, want 10", st.TotalPairs)
			}
			// Verify the scored units through Settle.
			scores, err := New().Settle(modes.SettleInput{
				Scope:   scope("score-seed"),
				Phase:   modes.PhaseSettled,
				Public:  prep.Public,
				Private: prep.Private,
				States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st)},
			})
			if err != nil {
				t.Fatalf("Settle: %v", err)
			}
			if scores[0].ScoreUnits != tc.wantUnits {
				t.Fatalf("scoreUnits = %d, want %d", scores[0].ScoreUnits, tc.wantUnits)
			}
			if err := scores[0].Validate(); err != nil {
				t.Fatalf("invalid score: %v", err)
			}
		})
	}
}

// TestRankGapsPreserved covers the documented "真实名次空缺" case: recorded ranks
// with gaps (1,2,3,5,7) must stay as they are, not be renumbered to 1..n
// .
func TestRankGapsPreserved(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	gappedRace := raceSpec{
		RaceID: "gapped",
		Name:   "有缺号",
		Date:   "2026-02-03",
		Starters: []starterSpec{
			{CharacterID: "a", CharacterName: "甲"},
			{CharacterID: "b", CharacterName: "乙"},
			{CharacterID: "c", CharacterName: "丙"},
			{CharacterID: "d", CharacterName: "丁"},
			{CharacterID: "e", CharacterName: "戊"},
		},
	}
	// The recorded finishing ranks have gaps: 1,2,3,5,7. They must stay as they
	// are instead of being renumbered to 1..5 .
	gappedAnswer := answerSpec{
		RaceID: "gapped",
		Order:  []string{"a", "b", "c", "d", "e"},
		Ranks:  map[string]int{"a": 1, "b": 2, "c": 3, "d": 5, "e": 7},
	}
	gapped := packWithAnswers([]raceSpec{gappedRace}, []answerSpec{gappedAnswer})
	prep, _, priv := prepare(t, gapped, "gap-seed", `{}`, subjects...)

	// The truth keeps the real numbers.
	wantRanks := map[string]int{"a": 1, "b": 2, "c": 3, "d": 5, "e": 7}
	for id, want := range wantRanks {
		if got := priv.Truth[id]; got != want {
			t.Fatalf("truth[%s] = %d, want %d", id, got, want)
		}
	}

	// The reveal reports the gaps as recorded, in ascending order.
	out, err := New().Project(modes.ProjectInput{
		Scope:     scope("gap-seed"),
		Viewer:    modes.Viewer{MembershipID: "host", Role: "host", IsReferee: true},
		Public:    prep.Public,
		Private:   prep.Private,
		Phase:     modes.PhaseSettled,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var view projection
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if view.Answer == nil {
		t.Fatal("referee projection has no answer")
	}
	wantGaps := []int{1, 2, 3, 5, 7}
	if len(view.Answer.Gaps) != len(wantGaps) {
		t.Fatalf("rankGaps = %v, want %v", view.Answer.Gaps, wantGaps)
	}
	for i, want := range wantGaps {
		if view.Answer.Gaps[i] != want {
			t.Fatalf("rankGaps = %v, want %v", view.Answer.Gaps, wantGaps)
		}
	}
}

// TestUnknownRankExcluded covers the documented rule that an unknown rank is
// excluded rather than ranked last .
func TestUnknownRankExcluded(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// The published section lists five starters, but the settled answer ranks only
	// four: the fifth has no recorded rank (the pack marks it in dropped).
	withUnknown := raceSpec{
		RaceID: "with-unknown",
		Name:   "含未知",
		Date:   "2026-03-04",
		Starters: []starterSpec{
			{CharacterID: "a", CharacterName: "甲"},
			{CharacterID: "b", CharacterName: "乙"},
			{CharacterID: "c", CharacterName: "丙"},
			{CharacterID: "d", CharacterName: "丁"},
			{CharacterID: "x", CharacterName: "未知"},
		},
	}
	unknownPack := packWithAnswers(
		[]raceSpec{withUnknown},
		[]answerSpec{{
			RaceID:  "with-unknown",
			Order:   []string{"a", "b", "c", "d"},
			Dropped: []string{"x:no_recorded_rank"},
		}},
	)
	_, pub, priv := prepare(t, unknownPack, "unknown-seed", `{}`, subjects...)

	// The unranked participant is not in the question.
	for _, entry := range pub.Entries {
		if entry.CharacterID == "x" {
			t.Fatal("a participant with an unknown rank was included in the question")
		}
	}
	if _, ok := priv.Truth["x"]; ok {
		t.Fatal("an unknown rank was recorded as truth")
	}
	// Five recorded entries minus one unknown leaves four rankable ones.
	if len(pub.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(pub.Entries))
	}
	if pub.TotalPairs != 6 {
		t.Fatalf("totalPairs = %d, want 6 (four participants)", pub.TotalPairs)
	}
	// Submitting the unknown participant is refused as unknown, not scored.
	dec, _ := applyOne(t, modes.Prepared{Public: modeutil.Marshal(pub), Private: modeutil.Marshal(priv)},
		subjects[0], nil, orderAction([]string{"a", "b", "c", "x"}))
	if dec.Accepted {
		t.Fatal("an excluded participant was accepted in an order")
	}
	if dec.Code != CodeUnknownEntry {
		t.Fatalf("code = %q, want %q", dec.Code, CodeUnknownEntry)
	}
}

// TestGenuineTieRequiresSameGroup covers the documented "并列" case: a real tie
// only counts when the submission places the tied participants in the same group
// .
func TestGenuineTieRequiresSameGroup(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// b and c genuinely tied for 2nd place, declared by id list in the answer.
	tiedRace := raceSpec{
		RaceID: "tied",
		Name:   "并列赛",
		Date:   "2026-04-05",
		Starters: []starterSpec{
			{CharacterID: "a", CharacterName: "甲"},
			{CharacterID: "b", CharacterName: "乙"},
			{CharacterID: "c", CharacterName: "丙"},
			{CharacterID: "d", CharacterName: "丁"},
		},
	}
	tiedPack := packWithAnswers(
		[]raceSpec{tiedRace},
		[]answerSpec{{
			RaceID: "tied",
			Order:  []string{"a", "b", "c", "d"},
			Ties:   map[string][]string{"2": {"b", "c"}},
		}},
	)
	prep, pub, _ := prepare(t, tiedPack, "tie-seed", `{}`, subjects...)
	if pub.TotalPairs != 6 {
		t.Fatalf("totalPairs = %d, want 6", pub.TotalPairs)
	}

	// Placing b and c in one group expresses the tie: every pair is correct.
	dec, st := applyOne(t, prep, subjects[0], nil, orderActionGroups([][]string{{"a"}, {"b", "c"}, {"d"}}))
	if !dec.Accepted {
		t.Fatalf("tie-group order rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.CorrectPairs != 6 || st.TotalPairs != 6 {
		t.Fatalf("correct = %d/%d, want 6/6", st.CorrectPairs, st.TotalPairs)
	}

	// Splitting the tie into a strict order loses exactly that pair: b ahead of
	// c is not the true relation (they tied), so the pair is wrong.
	_, st2 := applyOne(t, prep, subjects[0], nil, orderAction([]string{"a", "b", "c", "d"}))
	if st2.CorrectPairs != 5 {
		t.Fatalf("strict order of a tied pair scored %d/6, want 5/6", st2.CorrectPairs)
	}

	// Reversing the tied pair is equally wrong.
	_, st3 := applyOne(t, prep, subjects[0], nil, orderAction([]string{"a", "c", "b", "d"}))
	if st3.CorrectPairs != 5 {
		t.Fatalf("reversed tied pair scored %d/6, want 5/6", st3.CorrectPairs)
	}
}

// TestSameNameDifferentYear covers the documented "同名不同年" case: two races with
// the same name but different years are distinct questions keyed by raceId
// .
func TestSameNameDifferentYear(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	yearARace, yearAAnswer := strictRace("race-2025")
	yearARace.Name = "秋季锦标"
	yearARace.Date = "2025-10-01"

	// The same name in a different year with a reversed ranking.
	yearBRace := raceSpec{
		RaceID: "race-2026",
		Name:   "秋季锦标",
		Date:   "2026-10-01",
		Starters: []starterSpec{
			{CharacterID: "a", CharacterName: "甲"},
			{CharacterID: "b", CharacterName: "乙"},
			{CharacterID: "c", CharacterName: "丙"},
			{CharacterID: "d", CharacterName: "丁"},
			{CharacterID: "e", CharacterName: "戊"},
		},
	}
	yearBAnswer := answerSpec{
		RaceID: "race-2026",
		Order:  []string{"e", "d", "c", "b", "a"},
	}
	pack := packWithAnswers(
		[]raceSpec{yearARace, yearBRace},
		[]answerSpec{yearAAnswer, yearBAnswer},
	)
	// Both races are separately selectable and their truths differ.
	seenRace := map[string]string{}
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		_, pub, priv := prepare(t, pack, seed, `{}`, subjects...)
		seenRace[pub.RaceID] = pub.RaceDate
		_ = priv
	}
	if len(seenRace) < 2 {
		t.Fatalf("only one race was ever selected: %v", seenRace)
	}
	// The two races must not be merged: each keeps its own date.
	if date, ok := seenRace["race-2025"]; ok && date != "2025-10-01" {
		t.Fatalf("race-2025 date = %q", date)
	}
	if date, ok := seenRace["race-2026"]; ok && date != "2026-10-01" {
		t.Fatalf("race-2026 date = %q", date)
	}

	// Selecting each race explicitly gives its own truth.
	_, _, privA := prepare(t, packWithAnswers([]raceSpec{yearARace}, []answerSpec{yearAAnswer}), "only-a", `{}`, subjects...)
	_, _, privB := prepare(t, packWithAnswers([]raceSpec{yearBRace}, []answerSpec{yearBAnswer}), "only-b", `{}`, subjects...)
	if privA.RaceID != "race-2025" || privB.RaceID != "race-2026" {
		t.Fatalf("race ids merged: %q and %q", privA.RaceID, privB.RaceID)
	}
	if privA.Truth["a"] != 1 || privB.Truth["a"] != 5 {
		t.Fatalf("truths merged: a=%d in 2025, a=%d in 2026", privA.Truth["a"], privB.Truth["a"])
	}
}

// TestWrongRaceIDNotMerged covers the documented "错误 raceId 合并" case: entries
// from a different race must never be folded into one question. The loader keys
// strictly by the section's raceId, so a race whose entries name another raceId
// cannot leak in.
func TestWrongRaceIDNotMerged(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// Two separate race records, each with its own participants.
	raceA, answerA := strictRace("race-a")
	raceB := raceSpec{
		RaceID: "race-b",
		Name:   "另一场",
		Date:   "2026-05-06",
		Starters: []starterSpec{
			{CharacterID: "p", CharacterName: "P"},
			{CharacterID: "q", CharacterName: "Q"},
			{CharacterID: "r", CharacterName: "R"},
			{CharacterID: "s", CharacterName: "S"},
		},
	}
	answerB := answerSpec{RaceID: "race-b", Order: []string{"p", "q", "r", "s"}}
	prep, pub, priv := prepare(t, packWithAnswers(
		[]raceSpec{raceA, raceB},
		[]answerSpec{answerA, answerB},
	), "merge-seed", `{}`, subjects...)

	// The question only ever contains participants of the selected race.
	allowed := map[string]struct{}{}
	switch priv.RaceID {
	case "race-a":
		for _, id := range []string{"a", "b", "c", "d", "e"} {
			allowed[id] = struct{}{}
		}
	case "race-b":
		for _, id := range []string{"p", "q", "r", "s"} {
			allowed[id] = struct{}{}
		}
	default:
		t.Fatalf("unexpected race %q", priv.RaceID)
	}
	for _, entry := range pub.Entries {
		if _, ok := allowed[entry.CharacterID]; !ok {
			t.Fatalf("entry %q from another race leaked into %q", entry.CharacterID, priv.RaceID)
		}
	}
	if len(priv.Truth) != len(pub.Entries) {
		t.Fatalf("truth has %d entries but the question has %d", len(priv.Truth), len(pub.Entries))
	}

	// Submitting a participant from the other race is refused.
	var foreign string
	for _, id := range []string{"a", "b", "c", "d", "e", "p", "q", "r", "s"} {
		if _, ok := allowed[id]; !ok {
			foreign = id
			break
		}
	}
	if foreign == "" {
		t.Fatal("no foreign participant available")
	}
	order := make([]string, 0, len(pub.Entries))
	for _, entry := range pub.Entries {
		order = append(order, entry.CharacterID)
	}
	order[len(order)-1] = foreign
	dec, _ := applyOne(t, prep, subjects[0], nil, orderAction(order))
	if dec.Accepted {
		t.Fatalf("order containing the foreign participant %q was accepted", foreign)
	}
	if dec.Code != CodeUnknownEntry {
		t.Fatalf("code = %q, want %q", dec.Code, CodeUnknownEntry)
	}
}

// TestRevisionsReplaceOrder covers the documented "允许提交修订" rule .
func TestRevisionsReplaceOrder(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, strictPack("r1"), "rev-seed", `{}`, subjects...)

	_, st1 := applyOne(t, prep, subjects[0], nil, orderAction([]string{"e", "d", "c", "b", "a"}))
	if st1.CorrectPairs != 0 {
		t.Fatalf("reversed order scored %d, want 0", st1.CorrectPairs)
	}
	dec2, st2 := applyOne(t, prep, subjects[0], modeutil.Marshal(st1), orderAction([]string{"a", "b", "c", "d", "e"}))
	if !dec2.Accepted {
		t.Fatalf("revision rejected: %s", dec2.Code)
	}
	if st2.CorrectPairs != 10 {
		t.Fatalf("revised order scored %d, want 10", st2.CorrectPairs)
	}
	if st2.Revisions != 2 {
		t.Fatalf("revisions = %d, want 2", st2.Revisions)
	}
	if len(st2.Order) != 5 || st2.Order[0] != "a" {
		t.Fatalf("order was not replaced: %v", st2.Order)
	}
}

// TestInvalidOrdersRejected covers malformed submissions.
func TestInvalidOrdersRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, strictPack("r1"), "invalid-seed", `{}`, subjects...)
	cases := []struct {
		name  string
		order []string
		want  string
	}{
		{name: "too few", order: []string{"a", "b", "c"}, want: CodeIncomplete},
		{name: "too many", order: []string{"a", "b", "c", "d", "e", "f"}, want: CodeIncomplete},
		{name: "duplicate participant", order: []string{"a", "a", "c", "d", "e"}, want: CodeDuplicateEntry},
		{name: "unknown participant", order: []string{"a", "b", "c", "d", "zzz"}, want: CodeUnknownEntry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := applyOne(t, prep, subjects[0], nil, orderAction(tc.order))
			if dec.Accepted {
				t.Fatalf("accepted an invalid order: %v", tc.order)
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q", dec.Code, tc.want)
			}
		})
	}
}

// TestPrepareRequiresRankableParticipants asserts a pack without any race of the
// documented size is refused before the round opens .
func TestPrepareRequiresRankableParticipants(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	cases := []struct {
		name string
		pack modes.ContentSource
	}{
		{name: "no section", pack: fakeContent{}},
		{name: "empty races", pack: packWithAnswers(nil, nil)},
		{
			// A public race with no private answer cannot be graded.
			name: "no private answers",
			pack: fakeContent{
				"race-rebuild.races": json.RawMessage(`[
					{"raceId":"no-answer","name":"无答案","date":"2026-01-01",
					 "starters":[{"characterId":"a"},{"characterId":"b"},
					             {"characterId":"c"},{"characterId":"d"}]}
				]`),
			},
		},
		{
			name: "too few participants",
			pack: packWithAnswers(
				[]raceSpec{{
					RaceID: "tiny", Name: "太小", Date: "2026-01-01",
					Starters: []starterSpec{
						{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"},
					},
				}},
				[]answerSpec{{RaceID: "tiny", Order: []string{"a", "b", "c"}}},
			),
		},
		{
			name: "too many participants",
			pack: packWithAnswers(
				[]raceSpec{{
					RaceID: "huge", Name: "太大", Date: "2026-01-01",
					Starters: []starterSpec{
						{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"},
						{CharacterID: "d"}, {CharacterID: "e"}, {CharacterID: "f"},
						{CharacterID: "g"}, {CharacterID: "h"},
					},
				}},
				[]answerSpec{{
					RaceID: "huge",
					Order:  []string{"a", "b", "c", "d", "e", "f", "g", "h"},
				}},
			),
		},
		{
			// Every starter is unranked, so nothing can be asked.
			name: "all ranks unknown",
			pack: packWithAnswers(
				[]raceSpec{{
					RaceID: "unknown", Name: "全未知", Date: "2026-01-01",
					Starters: []starterSpec{
						{CharacterID: "a"}, {CharacterID: "b"},
						{CharacterID: "c"}, {CharacterID: "d"},
					},
				}},
				[]answerSpec{{
					RaceID: "unknown",
					Order:  nil,
					Dropped: []string{"a:no_recorded_rank", "b:no_recorded_rank",
						"c:no_recorded_rank", "d:no_recorded_rank"},
				}},
			),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("req"),
				Subject: subjects,
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare accepted an unusable pack")
			}
		})
	}
}

// TestPrepareDeterministic asserts the determinism gate .
func TestPrepareDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := packWithAnswers(
		[]raceSpec{mustStrict("r1").race, mustStrict("r2").race},
		[]answerSpec{mustStrict("r1").answer, mustStrict("r2").answer},
	)
	first, pubA, _ := prepare(t, pack, "det-seed", `{}`, subjects...)
	second, pubB, _ := prepare(t, pack, "det-seed", `{}`, subjects...)
	if string(first.Public) != string(second.Public) {
		t.Fatal("prepared public state differs between identical prepares")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("prepared private state differs between identical prepares")
	}
	if pubA.RaceID != pubB.RaceID {
		t.Fatalf("race differs: %q vs %q", pubA.RaceID, pubB.RaceID)
	}
}

// TestPublicPreparedHidesTruth asserts the real ranking never appears in a public
// payload: the Entry type carries identity only, so there is no rank field to leak
// .
func TestPublicPreparedHidesTruth(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, strictPack("r1"), "hide-seed", `{}`, subjects...)

	// The published entries carry no rank and no tie group.
	for _, entry := range pub.Entries {
		if entry.CharacterID == "" {
			t.Fatal("an entry has no character id")
		}
	}
	// The serialised public payload must not contain the truth at all.
	serialized := string(prep.Public)
	for _, forbidden := range []string{"truth", "finishRank", "tieGroup", "ranks"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("public prepared state contains %q", forbidden)
		}
	}
	// Nor any concrete rank value keyed by a participant.
	for id := range priv.Truth {
		if strings.Contains(serialized, `"`+id+`":`) {
			t.Fatalf("public prepared state maps participant %q to a value", id)
		}
	}
	if !json.Valid(prep.Public) {
		t.Fatal("public prepared state is not valid JSON")
	}
}

// containsKey reports whether the JSON object has the given top-level key.
func containsKey(t *testing.T, raw json.RawMessage, key string) bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// TestProjectHidesTruthUntilReveal asserts a player does not see the ranking
// during play, and the answer appears on reveal .
func TestProjectHidesTruthUntilReveal(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, strictPack("r1"), "proj-seed", `{}`, subjects...)

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
		t.Fatal("the answer was projected during play")
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
	if afterView.Answer == nil || len(afterView.Answer.Ranked) != 5 {
		t.Fatalf("reveal did not include the full ranking")
	}
}

// TestBigScreenDoesNotLeakPrivateClues covers the documented "公开大屏不泄露私有
// 线索" case: a display viewer must not receive the answer during play, while a
// referee does .
func TestBigScreenDoesNotLeakPrivateClues(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, strictPack("r1"), "display-seed", `{"cooperative":true}`, subjects...)

	display, err := New().Project(modes.ProjectInput{
		Scope:   scope("display-seed"),
		Viewer:  modes.Viewer{MembershipID: "d1", Role: "display"},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project display: %v", err)
	}
	var displayView projection
	if err := json.Unmarshal(display, &displayView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if displayView.Answer != nil {
		t.Fatal("the display viewer received the answer during play")
	}

	referee, err := New().Project(modes.ProjectInput{
		Scope:   scope("display-seed"),
		Viewer:  modes.Viewer{MembershipID: "host", Role: "host", IsReferee: true},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project referee: %v", err)
	}
	var refView projection
	if err := json.Unmarshal(referee, &refView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if refView.Answer == nil {
		t.Fatal("the referee did not receive the answer")
	}
}

// TestCooperativeCluesSufficient covers the documented cooperative-variant rule:
// the clues must be enough to determine the answer, and they never hand over the
// order directly .
func TestCooperativeCluesSufficient(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, strictPack("r1"), "clue-seed", `{"cooperative":true}`, subjects...)
	if len(pub.Clues) == 0 {
		t.Fatal("the cooperative variant produced no clues")
	}
	// Five participants produce four adjacency clues, enough to rebuild the
	// strict order.
	if len(pub.Clues) != len(pub.Entries)-1 {
		t.Fatalf("clues = %d, want %d", len(pub.Clues), len(pub.Entries)-1)
	}
	if len(pub.ClueRelations) != len(pub.Clues) {
		t.Fatalf("structured public clues differ: %+v", pub.ClueRelations)
	}
	// The clues do not contain a serialized answer order.
	for _, clue := range pub.Clues {
		if len(clue) == 0 {
			t.Fatal("empty clue")
		}
	}
	// The non-cooperative variant emits no clues.
	_, plainPub, _ := prepare(t, strictPack("r1"), "clue-seed", `{}`, subjects...)
	if len(plainPub.Clues) != 0 {
		t.Fatalf("the closed variant emitted %d clues", len(plainPub.Clues))
	}
	if len(plainPub.ClueRelations) != 0 {
		t.Fatal("closed round leaked structured clue relations")
	}
	_ = prep
}

func TestPublicClueRelationsPreserveRecordedTieWithoutGroupLabel(t *testing.T) {
	entries := []Entry{{CharacterID: "a", Name: "甲"}, {CharacterID: "b", Name: "乙"}, {CharacterID: "c", Name: "丙"}}
	truth := map[string]int{"a": 3, "b": 3, "c": 7}
	got := buildClueRelations(entries, truth, nil, Config{Cooperative: true})
	if len(got) != 2 || got[0].FirstID != "a" || got[0].SecondID != "b" || got[0].Kind != "tied" || got[1].Kind != "before" {
		t.Fatalf("recorded same rank became a false before clue: %+v", got)
	}
	text := buildClues(entries, truth, nil, Config{Cooperative: true})
	if len(text) != 2 || text[0] != "甲 与 乙 并列" {
		t.Fatalf("legacy and semantic clues disagree: %+v", text)
	}
}

func TestRejectEmptyOrderGroup(t *testing.T) {
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, pub, _ := prepare(t, strictPack("r1"), "empty-group", `{}`, subject)
	groups := [][]string{{}}
	for _, entry := range pub.Entries {
		groups = append(groups, []string{entry.CharacterID})
	}
	decision, _ := applyOne(t, prep, subject, nil, orderActionGroups(groups))
	if decision.Accepted || decision.Code != CodeInvalidPayload || len(decision.State) != 0 {
		t.Fatalf("empty group accepted: %+v", decision)
	}
}

// TestSettleWithoutSubmissionScoresZero asserts a subject who never submits
// scores 0 .
func TestSettleWithoutSubmissionScoresZero(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, _, _ := prepare(t, strictPack("r1"), "zero-seed", `{}`, subjects...)
	_, st1 := applyOne(t, prep, subjects[0], nil, orderAction([]string{"a", "b", "c", "d", "e"}))

	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("zero-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st1)},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	byID := map[string]modes.Score{}
	for _, s := range scores {
		byID[s.Subject.ID] = s
	}
	if byID["p1"].ScoreUnits != 10000 {
		t.Fatalf("p1 score = %d, want 10000", byID["p1"].ScoreUnits)
	}
	if byID["p2"].ScoreUnits != 0 {
		t.Fatalf("p2 score = %d, want 0", byID["p2"].ScoreUnits)
	}
	if byID["p1"].Rank != 1 || byID["p2"].Rank != 2 {
		t.Fatalf("ranks = %d,%d, want 1,2", byID["p1"].Rank, byID["p2"].Rank)
	}
}

// TestSettleDeterministic asserts settlement is order-independent .
func TestSettleDeterministic(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, _, _ := prepare(t, strictPack("r1"), "settle-det", `{}`, subjects...)
	_, st1 := applyOne(t, prep, subjects[0], nil, orderAction([]string{"a", "b", "c", "d", "e"}))
	_, st2 := applyOne(t, prep, subjects[1], nil, orderAction([]string{"e", "d", "c", "b", "a"}))
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

// TestPairCount pins the denominator helper.
func TestPairCount(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{0, 0}, {1, 0}, {2, 1}, {3, 3}, {4, 6}, {5, 10}, {6, 15}, {7, 21},
	}
	for _, tc := range cases {
		if got := pairCount(tc.n); got != tc.want {
			t.Fatalf("pairCount(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// TestConfigValidation covers config bounds.
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
	}{
		{name: "empty uses defaults", cfg: ``},
		{name: "cooperative valid", cfg: `{"cooperative":true}`},
		{name: "open book valid", cfg: `{"openBook":true}`},
		{name: "negative seconds rejected", cfg: `{"questionSeconds":-5}`, wantErr: true},
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

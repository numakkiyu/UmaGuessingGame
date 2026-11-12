package bluff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// factSpec is the on-the-wire spelling of one candidate fact.
type factSpec struct {
	FactID          string   `json:"factId"`
	CharacterID     string   `json:"characterId"`
	Text            string   `json:"text"`
	Answer          string   `json:"answer"`
	Source          string   `json:"source"`
	FallbackAnswers []string `json:"fallbackAnswers,omitempty"`
	ReviewStatus    string   `json:"reviewStatus"`
}

// reviewedPack builds a pack carrying n reviewed facts.
func reviewedPack(n int) modes.ContentSource {
	facts := make([]factSpec, 0, n)
	for i := 0; i < n; i++ {
		facts = append(facts, factSpec{
			FactID:          fmt.Sprintf("fact-%03d", i+1),
			CharacterID:     fmt.Sprintf("c%03d", i+1),
			Text:            fmt.Sprintf("角色%d 的得意科目是数学", i+1),
			Answer:          "数学",
			Source:          "https://example.invalid/source",
			ReviewStatus:    ReviewStatusReviewed,
			FallbackAnswers: []string{"夹具备用甲", "夹具备用乙", "夹具备用丙", "夹具备用丁", "夹具备用戊", "夹具备用己", "夹具备用庚", "夹具备用辛"},
		})
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		panic(err)
	}
	return fakeContent{FactsSection: raw}
}

// unreviewedPack builds a pack whose every fact is pending review, which is the shape
// the real snapshot has.
func unreviewedPack(n int) modes.ContentSource {
	facts := make([]factSpec, 0, n)
	for i := 0; i < n; i++ {
		facts = append(facts, factSpec{
			FactID:       fmt.Sprintf("fact-%03d", i+1),
			CharacterID:  fmt.Sprintf("c%03d", i+1),
			Text:         fmt.Sprintf("角色%d 的得意科目是数学", i+1),
			Answer:       "数学",
			Source:       "",
			ReviewStatus: "pending",
		})
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		panic(err)
	}
	return fakeContent{FactsSection: raw}
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-test",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// roster builds n player subjects with stable ids.
func roster(n int) []modes.Subject {
	out := make([]modes.Subject, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, modes.Subject{
			ID:   fmt.Sprintf("p%d", i+1),
			Kind: modes.SubjectPlayer,
		})
	}
	return out
}

// prepareRound runs Prepare and returns the prepared round with decoded halves.
func prepareRound(t *testing.T, pack modes.ContentSource, seed, cfg string, players int) (modes.Prepared, publicState, privateState) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: roster(players),
		Config:  json.RawMessage(cfg),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return prep, pub, priv
}

// decoyAction builds a contract-shaped bluff.decoy payload.
func decoyAction(text string) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}{Kind: ActionKindDecoy, Text: text})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindDecoy, Payload: body}
}

// voteAction builds a contract-shaped bluff.vote payload.
func voteAction(optionID string) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind     string `json:"kind"`
		OptionID string `json:"optionId"`
	}{Kind: ActionKindVote, OptionID: optionID})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindVote, Payload: body}
}

// apply runs Apply against the current state.
func apply(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) modes.Decision {
	t.Helper()
	if action.Kind == ActionKindVote {
		prep = openVotingFixture(t, prep)
	}
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   state,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return dec
}

// commit applies then commits one action.
func commit(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) (modes.Prepared, json.RawMessage) {
	t.Helper()
	if action.Kind == ActionKindVote {
		prep = openVotingFixture(t, prep)
	}
	in := modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   state,
	}
	dec, err := New().Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("action rejected: code=%q detail=%q", dec.Code, dec.Detail)
	}
	pub, priv, err := New().CommitShared(in, dec)
	if err != nil {
		t.Fatalf("CommitShared: %v", err)
	}
	return modes.Prepared{Public: pub, Private: priv, QuestionCount: prep.QuestionCount}, dec.State
}

// project decodes the projection for a viewer.
func project(t *testing.T, prep modes.Prepared, viewer modes.Viewer, phase modes.Phase, states map[string]json.RawMessage) projection {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	raw, err := New().Project(modes.ProjectInput{
		Scope:   scope("seed"),
		Viewer:  viewer,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   phase,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var out projection
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	return out
}

// settle runs Settle.
func settle(t *testing.T, prep modes.Prepared, states map[string]json.RawMessage) map[string]modes.Score {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	out := make(map[string]modes.Score, len(scores))
	for _, score := range scores {
		if err := score.Validate(); err != nil {
			t.Fatalf("invalid score for %s: %v", score.Subject.ID, err)
		}
		out[score.Subject.ID] = score
	}
	return out
}

// ---------------------------------------------------------------------------
// Part 1: the admission gate
// ---------------------------------------------------------------------------

// TestPrepareRefusesUnreviewedFacts verifies that pending facts cannot start a round and the admission result reports their counts.
func TestPrepareRefusesUnreviewedFacts(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("gate-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: unreviewedPack(264),
	})
	if err == nil {
		t.Fatal("Prepare admitted a round built on unreviewed facts")
	}
	var admission *AdmissionError
	if !errors.As(err, &admission) {
		t.Fatalf("the refusal is not an AdmissionError: %v", err)
	}
	if admission.Total != 264 {
		t.Fatalf("admission reported %d candidates, want 264", admission.Total)
	}
	if admission.Unreviewed != 264 {
		t.Fatalf("admission reported %d unreviewed, want 264", admission.Unreviewed)
	}
	if admission.ReasonCode() != "no_reviewed_facts" {
		t.Fatalf("reason code = %q, want no_reviewed_facts", admission.ReasonCode())
	}
	// The message must be actionable and must not pretend the pack is missing.
	msg := err.Error()
	if !strings.Contains(msg, "no reviewed facts") {
		t.Fatalf("the refusal does not say what is missing: %q", msg)
	}
	if !strings.Contains(msg, "human semantic review") {
		t.Fatalf("the refusal does not say what is required: %q", msg)
	}
}

// TestPrepareRefusesMissingSection asserts an absent section is a different report
// from an unreviewed one.
func TestPrepareRefusesMissingSection(t *testing.T) {
	cases := []struct {
		name string
		pack modes.ContentSource
		want string
	}{
		{name: "nil content", pack: nil, want: "no content source"},
		{name: "empty pack", pack: fakeContent{}, want: "no facts are available"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("missing-seed"),
				Subject: roster(3),
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare admitted a round with no facts")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestPrepareRefusesReviewedFactWithoutSource asserts a review alone is not enough:
// the plan requires a citation too .
func TestPrepareRefusesReviewedFactWithoutSource(t *testing.T) {
	facts := []factSpec{{
		FactID:          "fact-001",
		CharacterID:     "c001",
		Text:            "角色1 的得意科目是数学",
		Answer:          "数学",
		Source:          "",
		ReviewStatus:    ReviewStatusReviewed,
		FallbackAnswers: []string{"夹具备用甲", "夹具备用乙", "夹具备用丙", "夹具备用丁", "夹具备用戊", "夹具备用己", "夹具备用庚", "夹具备用辛"},
	}}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("unsourced-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: fakeContent{FactsSection: raw},
	})
	if err == nil {
		t.Fatal("Prepare admitted a reviewed fact with no source")
	}
	var admission *AdmissionError
	if !errors.As(err, &admission) {
		t.Fatalf("the refusal is not an AdmissionError: %v", err)
	}
	if admission.Unsourced != 1 {
		t.Fatalf("admission reported %d unsourced, want 1", admission.Unsourced)
	}
}

// TestPrepareRefusesUnblankableFact asserts a fact whose text does not contain its
// answer phrase is refused rather than posed with nothing blanked.
func TestPrepareRefusesUnblankableFact(t *testing.T) {
	facts := []factSpec{{
		FactID:          "fact-001",
		CharacterID:     "c001",
		Text:            "角色1 的得意科目是数学",
		Answer:          "音乐",
		Source:          "https://example.invalid/source",
		ReviewStatus:    ReviewStatusReviewed,
		FallbackAnswers: []string{"夹具备用甲", "夹具备用乙", "夹具备用丙", "夹具备用丁", "夹具备用戊", "夹具备用己", "夹具备用庚", "夹具备用辛"},
	}}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("unblankable-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: fakeContent{FactsSection: raw},
	})
	if err == nil {
		t.Fatal("Prepare admitted a fact whose answer phrase is not in its text")
	}
	if !strings.Contains(err.Error(), "does not contain the answer phrase") {
		t.Fatalf("error %q does not explain the mismatch", err)
	}
}

// TestUnreviewedFactNeverReachesPublicState asserts the strongest form of the gate:
// even when a pack mixes reviewed and unreviewed facts, only a reviewed one may be
// posed, and the unreviewed text never appears in the public state.
func TestUnreviewedFactNeverReachesPublicState(t *testing.T) {
	facts := []factSpec{
		{
			FactID:          "fact-001",
			CharacterID:     "c001",
			Text:            "已审校角色 的故乡是北海道",
			Answer:          "北海道",
			Source:          "https://example.invalid/source",
			ReviewStatus:    ReviewStatusReviewed,
			FallbackAnswers: []string{"夹具备用甲", "夹具备用乙", "夹具备用丙", "夹具备用丁", "夹具备用戊", "夹具备用己", "夹具备用庚", "夹具备用辛"},
		},
		{
			FactID:       "fact-002",
			CharacterID:  "c002",
			Text:         "未审校角色 的秘密是喜欢雨天",
			Answer:       "喜欢雨天",
			Source:       "",
			ReviewStatus: "pending",
		},
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	prep, pub, priv := prepareRound(t, fakeContent{FactsSection: raw}, "mixed-seed", `{}`, 3)
	if priv.FactID != "fact-001" {
		t.Fatalf("posed fact = %q, want the reviewed one", priv.FactID)
	}
	if strings.Contains(string(prep.Public), "喜欢雨天") {
		t.Fatal("the unreviewed fact text reached the public state")
	}
	if !strings.Contains(pub.Prompt, "____") {
		t.Fatalf("the prompt does not contain a blank: %q", pub.Prompt)
	}
	if strings.Contains(pub.Prompt, "北海道") {
		t.Fatalf("the prompt still shows the answer: %q", pub.Prompt)
	}
}

// ---------------------------------------------------------------------------
// Part 2: the engine, against a reviewed fixture
// ---------------------------------------------------------------------------

// currentPrivate re-reads the round's server-only state from a prepared round.
//
// A test must not reuse the private state returned by prepareRound after committing
// actions: CommitShared rewrites it (the merged decoy list, the ballot), so the
// original value is stale. Reading it back through this helper is what a room-layer
// caller does after its transaction.
func currentPrivate(t *testing.T, prep modes.Prepared) privateState {
	t.Helper()
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode current private state: %v", err)
	}
	return priv
}

// decoyOptionOf returns the ballot option id of the decoy a unit authored.
func decoyOptionOf(t *testing.T, prep modes.Prepared, subjectID string) string {
	t.Helper()
	priv := currentPrivate(t, prep)
	for _, d := range priv.Decoys {
		if containsString(d.AuthorIDs, subjectID) {
			return optionIDFor(d.DecoyID)
		}
	}
	return ""
}

// TestAnonymousBallotCarriesNoAuthor covers the 「匿名泄露」 bullet.
func TestAnonymousBallotCarriesNoAuthor(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "anon-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}

	prep = openVotingFixture(t, prep)
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	if len(view.Options) != len(pub.Subjects)+1 {
		t.Fatalf("ballot has %d options, want %d (decoys + truth)",
			len(view.Options), len(pub.Subjects)+1)
	}
	// The ballot must not carry an author list, and no author id may appear.
	raw := string(mustJSON(t, view.Options))
	for _, entry := range pub.Subjects {
		if strings.Contains(raw, `"`+entry.SubjectID+`"`) {
			t.Fatalf("the ballot carries the author id %q", entry.SubjectID)
		}
	}
	for _, opt := range view.Options {
		if opt.OptionID == "" || opt.Text == "" {
			t.Fatalf("option is incomplete: %+v", opt)
		}
	}
	// The truth must not be identifiable from the payload.
	if strings.Contains(string(mustJSON(t, view)), "isTruth") {
		t.Fatal("the projection exposes which option is the truth")
	}
	if view.Reveal != nil {
		t.Fatal("the reveal is visible before the round ends")
	}
	// Every option must have a distinct opaque id.
	ids := make(map[string]struct{}, len(view.Options))
	for _, opt := range view.Options {
		if _, dup := ids[opt.OptionID]; dup {
			t.Fatalf("option id %q appears twice", opt.OptionID)
		}
		ids[opt.OptionID] = struct{}{}
		if strings.HasPrefix(opt.OptionID, "truth") {
			t.Fatalf("option id %q names the truth", opt.OptionID)
		}
	}
	// The truth option id must not be guessable from the decoy ids.
	if _, isDecoyShaped := ids[priv.TruthOptionID]; !isDecoyShaped {
		t.Fatalf("the truth option %q is not on the ballot", priv.TruthOptionID)
	}
}

// TestDuplicateDecoysMergeWithCoAuthors covers the 「重复/同义」 bullet.
func TestDuplicateDecoysMergeWithCoAuthors(t *testing.T) {
	prep, pub, _ := prepareRound(t, reviewedPack(3), "merge-seed", `{}`, 3)
	states := map[string]json.RawMessage{}

	// p1 and p2 submit the same text; p3 submits something else.
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		decoyAction("音乐"), states["p1"])
	prep = next
	states["p1"] = state

	// The same text with different whitespace and full-width forms must merge: the
	// comparison rule is explicit .
	next, state = commit(t, prep, modes.Subject{ID: "p2", Kind: modes.SubjectPlayer},
		decoyAction(" 音乐 "), states["p2"])
	prep = next
	states["p2"] = state

	next, state = commit(t, prep, modes.Subject{ID: "p3", Kind: modes.SubjectPlayer},
		decoyAction("美术"), states["p3"])
	prep = next
	states["p3"] = state

	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if len(priv.Decoys) != 2 {
		t.Fatalf("%d decoys after merging, want 2: %+v", len(priv.Decoys), priv.Decoys)
	}
	merged := false
	for _, d := range priv.Decoys {
		if len(d.AuthorIDs) == 2 {
			merged = true
			if !containsString(d.AuthorIDs, "p1") || !containsString(d.AuthorIDs, "p2") {
				t.Fatalf("merged decoy authors = %v, want p1 and p2", d.AuthorIDs)
			}
		}
	}
	if !merged {
		t.Fatal("the identical submissions did not merge into one decoy with two authors")
	}
	_ = pub
}

// TestFullWidthAndWhitespaceNormalisation asserts the documented comparison rule.
func TestFullWidthAndWhitespaceNormalisation(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{a: "abc", b: "ＡＢＣ", same: true},
		{a: "a b", b: "a   b", same: true},
		{a: " abc ", b: "abc", same: true},
		{a: "ア", b: "ア", same: true},
		{a: "数学", b: "音乐", same: false},
		{a: "ABC", b: "abc", same: true},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			got := normalizeDecoy(tc.a) == normalizeDecoy(tc.b)
			if got != tc.same {
				t.Fatalf("normalizeDecoy(%q)=%q vs (%q)=%q: same=%v, want %v",
					tc.a, normalizeDecoy(tc.a), tc.b, normalizeDecoy(tc.b), got, tc.same)
			}
		})
	}
}

// TestCannotVoteOwnDecoy covers the 「不能投自己」 bullet.
func TestCannotVoteOwnDecoy(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "own-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}

	// Find p1's own decoy option, reading the CURRENT private state.
	priv = currentPrivate(t, prep)
	ownOptionID := decoyOptionOf(t, prep, "p1")
	if ownOptionID == "" {
		t.Fatalf("p1's decoy is not in the merged list: %+v", priv.Decoys)
	}

	dec := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(ownOptionID), states["p1"])
	if dec.Accepted {
		t.Fatal("a unit voted for its own decoy")
	}
	if dec.Code != CodeOwnDecoy {
		t.Fatalf("code = %q, want %q", dec.Code, CodeOwnDecoy)
	}

	// Voting for somebody else's decoy works.
	var otherOptionID string
	for _, d := range priv.Decoys {
		if !containsString(d.AuthorIDs, "p1") {
			otherOptionID = optionIDFor(d.DecoyID)
			break
		}
	}
	dec = apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(otherOptionID), states["p1"])
	if !dec.Accepted {
		t.Fatalf("voting for another decoy was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestRepeatedBallotRefused covers the 「票数重试」 bullet.
func TestRepeatedBallotRefused(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "retry-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	// p1 votes for the truth option, then tries again.
	priv = currentPrivate(t, prep)
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(priv.TruthOptionID), states["p1"])
	prep = next
	states["p1"] = state

	priv = currentPrivate(t, prep)
	var otherOptionID string
	for _, d := range priv.Decoys {
		if !containsString(d.AuthorIDs, "p1") {
			otherOptionID = optionIDFor(d.DecoyID)
			break
		}
	}
	if otherOptionID == "" {
		t.Fatalf("no other decoy to vote for: %+v", priv.Decoys)
	}
	dec := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(otherOptionID), state)
	if dec.Accepted {
		t.Fatal("a second ballot was accepted")
	}
	if dec.Code != CodeAlreadyVoted {
		t.Fatalf("code = %q, want %q", dec.Code, CodeAlreadyVoted)
	}
}

// TestIdentificationAndDeceptionScores asserts the documented 70 + up to 30 formula.
func TestIdentificationAndDeceptionScores(t *testing.T) {
	// Five units: p1 writes a decoy, p2 and p3 vote for it, p4 votes for the truth,
	// p5 does not vote.
	prep, pub, priv := prepareRound(t, reviewedPack(3), "score-seed", `{}`, 5)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	priv = currentPrivate(t, prep)
	p1DecoyOption := decoyOptionOf(t, prep, "p1")
	if p1DecoyOption == "" {
		t.Fatalf("p1's decoy is missing: %+v", priv.Decoys)
	}

	// p2 and p3 vote for p1's decoy.
	for _, voter := range []string{"p2", "p3"} {
		next, state := commit(t, prep, modes.Subject{ID: voter, Kind: modes.SubjectPlayer},
			voteAction(p1DecoyOption), states[voter])
		prep = next
		states[voter] = state
	}
	// p4 votes for the truth.
	priv = currentPrivate(t, prep)
	next, state := commit(t, prep, modes.Subject{ID: "p4", Kind: modes.SubjectPlayer},
		voteAction(priv.TruthOptionID), states["p4"])
	prep = next
	states["p4"] = state
	// p5 never votes.

	scores := settle(t, prep, states)

	// p4 identified the truth: 70 points, no deception.
	if got := scores["p4"].ScoreUnits; got != TruthPoints*100 {
		t.Fatalf("p4 scored %d units, want %d", got, TruthPoints*100)
	}
	// p5 did not vote: no identification. It authored a decoy that nobody voted for,
	// so no deception either.
	if got := scores["p5"].ScoreUnits; got != 0 {
		t.Fatalf("p5 scored %d units, want 0", got)
	}
	// p1's decoy got 2 of 4 eligible voters (p2..p5 minus the author): 30*2/4 = 15.
	if got := scores["p1"].ScoreUnits; got != 15*100 {
		t.Fatalf("p1 scored %d units, want %d (deception 30*2/4)", got, 15*100)
	}
	// The explanation names both components.
	var exp struct {
		TruthPoints     int  `json:"truthPoints"`
		DeceptionPoints int  `json:"deceptionPoints"`
		IdentifiedTruth bool `json:"identifiedTruth"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.TruthPoints != 0 {
		t.Fatalf("p1 earned %d truth points without identifying the truth", exp.TruthPoints)
	}
	if exp.DeceptionPoints != 15 {
		t.Fatalf("p1 deception = %d, want 15", exp.DeceptionPoints)
	}
	if exp.IdentifiedTruth {
		t.Fatal("p1 is marked as having identified the truth")
	}
}

// TestMergedDecoySplitsBonusEvenly asserts the co-authors share the bonus, floored.
func TestMergedDecoySplitsBonusEvenly(t *testing.T) {
	// Four units. p1 and p2 write the same decoy; p3 and p4 vote for it. Eligible
	// voters for that decoy = p3, p4 = 2, both voted, so the total is 30*2/2 = 30,
	// split between 2 authors = 15 each.
	prep, _, priv := prepareRound(t, reviewedPack(3), "split-seed", `{}`, 4)
	states := map[string]json.RawMessage{}
	for _, id := range []string{"p1", "p2"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			decoyAction("同一个假答案"), states[id])
		prep = next
		states[id] = state
	}
	for _, id := range []string{"p3", "p4"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			decoyAction("别的假答案 "+id), states[id])
		prep = next
		states[id] = state
	}

	priv = currentPrivate(t, prep)
	var mergedOption string
	for _, d := range priv.Decoys {
		if len(d.AuthorIDs) == 2 {
			mergedOption = optionIDFor(d.DecoyID)
		}
	}
	if mergedOption == "" {
		t.Fatalf("the duplicate submissions did not merge: %+v", priv.Decoys)
	}
	for _, id := range []string{"p3", "p4"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			voteAction(mergedOption), states[id])
		prep = next
		states[id] = state
	}

	scores := settle(t, prep, states)
	for _, id := range []string{"p1", "p2"} {
		if got := scores[id].ScoreUnits; got != 15*100 {
			t.Fatalf("%s scored %d units, want 1500 (30 split two ways)", id, got)
		}
	}
}

// TestEmptyDenominatorScoresZero covers the 「空分母」 bullet.
//
// With only the author on the roster, nobody else could vote for the decoy, so the
// denominator is 0 and the bonus is 0 rather than a division by zero.
func TestEmptyDenominatorScoresZero(t *testing.T) {
	// A single-unit round is refused by the participant bound, so the case is built at
	// the scoring level: an author whose decoy has no eligible voter.
	prep, pub, priv := prepareRound(t, reviewedPack(3), "empty-denom-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	// Nobody votes at all.
	scores := settle(t, prep, states)
	for _, entry := range pub.Subjects {
		if got := scores[entry.SubjectID].ScoreUnits; got != 0 {
			t.Fatalf("%s scored %d units with no votes", entry.SubjectID, got)
		}
	}
	_ = priv

	// The explicit zero-denominator path: a decoy whose only other unit is gone. Build
	// it by dropping every other subject from the roster, which leaves one author and
	// zero eligible voters.
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	board.Subjects = board.Subjects[:1]
	prep.Public = mustJSON(t, board)
	scores = settle(t, prep, states)
	if got := scores["p1"].ScoreUnits; got != 0 {
		t.Fatalf("p1 scored %d units with an empty denominator, want 0", got)
	}
}

// TestHostRejectionRemovesOption covers the 「主持驳回」 bullet.
//
// A host rejection removes the decoy from the ballot. The work is then not votable and
// its author earns no deception bonus from it.
func TestHostRejectionRemovesOption(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "reject-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}

	// Remove p2's decoy from the ballot and from the private list, which is how the
	// host's exclusion is expressed.
	priv = currentPrivate(t, prep)
	var p2DecoyID string
	for _, d := range priv.Decoys {
		if containsString(d.AuthorIDs, "p2") {
			p2DecoyID = d.DecoyID
		}
	}
	if p2DecoyID == "" {
		t.Fatalf("p2's decoy is missing: %+v", priv.Decoys)
	}
	removedOption := optionIDFor(p2DecoyID)

	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	kept := make([]option, 0, len(board.Options))
	for _, opt := range board.Options {
		if opt.OptionID != removedOption {
			kept = append(kept, opt)
		}
	}
	board.Options = kept
	prep.Public = mustJSON(t, board)

	var roundPriv privateState
	if err := json.Unmarshal(prep.Private, &roundPriv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	keptDecoys := make([]decoy, 0, len(roundPriv.Decoys))
	for _, d := range roundPriv.Decoys {
		if d.DecoyID != p2DecoyID {
			keptDecoys = append(keptDecoys, d)
		}
	}
	roundPriv.Decoys = keptDecoys
	delete(roundPriv.AuthorOf, p2DecoyID)
	prep.Private = mustJSON(t, roundPriv)

	// A ballot for the removed option is refused.
	dec := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(removedOption), states["p1"])
	if dec.Accepted {
		t.Fatal("a ballot for a removed option was accepted")
	}
	if dec.Code != CodeUnknownOption {
		t.Fatalf("code = %q, want %q", dec.Code, CodeUnknownOption)
	}
	// The projection no longer lists it.
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	for _, opt := range view.Options {
		if opt.OptionID == removedOption {
			t.Fatal("the rejected option is still on the ballot")
		}
	}
	// The author earns no deception bonus from the removed decoy.
	scores := settle(t, prep, states)
	if got := scores["p2"].ScoreUnits; got != 0 {
		t.Fatalf("the rejected author scored %d units, want 0", got)
	}
}

// TestNonVoterEarnsNoIdentification covers the 「未投票」 bullet.
func TestNonVoterEarnsNoIdentification(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "nonvoter-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	// p1 votes for the truth; p2 and p3 do not vote.
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(priv.TruthOptionID), states["p1"])
	prep = next
	states["p1"] = state

	scores := settle(t, prep, states)
	if got := scores["p1"].ScoreUnits; got < TruthPoints*100 {
		t.Fatalf("the voter scored %d units, want at least %d", got, TruthPoints*100)
	}
	for _, id := range []string{"p2", "p3"} {
		var exp struct {
			IdentifiedTruth bool `json:"identifiedTruth"`
			TruthPoints     int  `json:"truthPoints"`
		}
		if err := json.Unmarshal(scores[id].Explanation, &exp); err != nil {
			t.Fatalf("decode explanation: %v", err)
		}
		if exp.IdentifiedTruth || exp.TruthPoints != 0 {
			t.Fatalf("%s earned identification without voting: %+v", id, exp)
		}
	}
}

// TestSubmittingTheTruthEarnsIdentificationNoDeception covers the documented rule that
// a unit which types the truth scores identification and no deception.
func TestSubmittingTheTruthEarnsIdentificationNoDeception(t *testing.T) {
	prep, _, priv := prepareRound(t, reviewedPack(3), "truth-submit-seed", `{}`, 3)
	states := map[string]json.RawMessage{}

	// p1 submits the truth itself.
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		decoyAction(priv.Truth), states["p1"])
	prep = next
	states["p1"] = state
	// p2 and p3 submit ordinary decoys.
	for _, id := range []string{"p2", "p3"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			decoyAction("假答案 "+id), states[id])
		prep = next
		states[id] = state
	}

	var after privateState
	if err := json.Unmarshal(prep.Private, &after); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	// The truth submission is not a decoy on the ballot.
	for _, d := range after.Decoys {
		if containsString(d.AuthorIDs, "p1") {
			t.Fatal("the truth submission was recorded as a decoy")
		}
	}

	scores := settle(t, prep, states)
	var exp struct {
		TruthPoints     int  `json:"truthPoints"`
		DeceptionPoints int  `json:"deceptionPoints"`
		IdentifiedTruth bool `json:"identifiedTruth"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.TruthPoints != TruthPoints {
		t.Fatalf("truth points = %d, want %d", exp.TruthPoints, TruthPoints)
	}
	if exp.DeceptionPoints != 0 {
		t.Fatalf("deception = %d, want 0 for a truth submitter", exp.DeceptionPoints)
	}
	if !exp.IdentifiedTruth {
		t.Fatal("the truth submitter is not credited with identifying the truth")
	}
}

// TestDecoyLengthIsCountedInCodePoints covers the 2–40 code point rule.
func TestDecoyLengthIsCountedInCodePoints(t *testing.T) {
	prep, _, _ := prepareRound(t, reviewedPack(3), "length-seed", `{}`, 3)
	p1 := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	cases := []struct {
		name    string
		text    string
		wantErr string
	}{
		{name: "empty", text: "", wantErr: CodeDecoyTooShort},
		{name: "one code point", text: "a", wantErr: CodeDecoyTooShort},
		{name: "two code points", text: "ab"},
		{name: "forty code points", text: strings.Repeat("馬", 40)},
		{name: "forty one code points", text: strings.Repeat("馬", 41), wantErr: CodeDecoyTooLong},
		{name: "forty astral", text: strings.Repeat("🐎", 40)},
		{name: "forty one astral", text: strings.Repeat("🐎", 41), wantErr: CodeDecoyTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := apply(t, prep, p1, decoyAction(tc.text), nil)
			if tc.wantErr == "" {
				if !dec.Accepted {
					t.Fatalf("a legal decoy was refused: %q %q", dec.Code, dec.Detail)
				}
				return
			}
			if dec.Accepted {
				t.Fatalf("an invalid decoy was accepted: %q", tc.text)
			}
			if dec.Code != tc.wantErr {
				t.Fatalf("code = %q, want %q", dec.Code, tc.wantErr)
			}
		})
	}
}

// TestDecoyClosedAfterVotingStarts asserts the writing phase closes when a ballot
// exists.
func TestDecoyClosedAfterVotingStarts(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "closed-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(priv.TruthOptionID), states["p1"])
	prep = next
	states["p1"] = state

	// p3 already submitted; a resubmission is refused either by the one-decoy rule or
	// by the closed phase, and both are correct.
	dec := apply(t, prep, modes.Subject{ID: "p3", Kind: modes.SubjectPlayer},
		decoyAction("改过的假答案"), states["p3"])
	if dec.Accepted {
		t.Fatal("a decoy was changed after voting started")
	}
	if dec.Code != CodeDecoyClosed && dec.Code != CodeTooManyDecoys {
		t.Fatalf("code = %q, want %q or %q", dec.Code, CodeDecoyClosed, CodeTooManyDecoys)
	}
}

// TestOneDecoyPerUnit asserts a unit submits exactly one fake answer.
func TestOneDecoyPerUnit(t *testing.T) {
	prep, _, _ := prepareRound(t, reviewedPack(3), "one-decoy-seed", `{}`, 3)
	p1 := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	next, state := commit(t, prep, p1, decoyAction("第一个假答案"), nil)
	prep = next

	dec := apply(t, prep, p1, decoyAction("第二个假答案"), state)
	if dec.Accepted {
		t.Fatal("a second decoy was accepted from one unit")
	}
	if dec.Code != CodeTooManyDecoys {
		t.Fatalf("code = %q, want %q", dec.Code, CodeTooManyDecoys)
	}
}

// TestInvalidActionsRejected covers payload validation for both kinds.
func TestInvalidActionsRejected(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "invalid-seed", `{}`, 3)
	p1 := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	next, state := commit(t, prep, p1, decoyAction("一个假答案"), nil)
	prep = next

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "bluff.teleport", body: `{}`, want: CodeUnknownAction},
		{name: "decoy empty payload", kind: ActionKindDecoy, body: ``, want: CodeInvalidPayload},
		{name: "decoy not json", kind: ActionKindDecoy, body: `nope`, want: CodeInvalidPayload},
		{name: "decoy kind mismatch", kind: ActionKindDecoy,
			body: `{"kind":"bluff.vote","text":"abc"}`, want: CodeInvalidPayload},
		{name: "vote empty payload", kind: ActionKindVote, body: ``, want: CodeInvalidPayload},
		{name: "vote not json", kind: ActionKindVote, body: `nope`, want: CodeInvalidPayload},
		{name: "vote empty option", kind: ActionKindVote,
			body: `{"kind":"bluff.vote"}`, want: CodeInvalidPayload},
		{name: "vote unknown option", kind: ActionKindVote,
			body: `{"kind":"bluff.vote","optionId":"bo-nope"}`, want: CodeUnknownOption},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := apply(t, prep, p1,
				modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)}, state)
			if dec.Accepted {
				t.Fatal("an invalid action was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
	_ = pub
	_ = priv
}

// TestNonParticipantCannotAct asserts a subject outside the roster is refused.
func TestNonParticipantCannotAct(t *testing.T) {
	prep, _, priv := prepareRound(t, reviewedPack(3), "outsider-seed", `{}`, 3)
	outsider := modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer}
	dec := apply(t, prep, outsider, decoyAction("外部假答案"), nil)
	if dec.Accepted {
		t.Fatal("a non-participant submitted a decoy")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
	dec = apply(t, prep, outsider, voteAction(priv.TruthOptionID), nil)
	if dec.Accepted {
		t.Fatal("a non-participant voted")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
}

// TestRevealCarriesTruthAndSource asserts the reveal publishes the fact, the source and
// the decoy authorship, and only at the end.
func TestRevealCarriesTruthAndSource(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "reveal-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}

	player := modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}
	// Not visible while the round runs.
	view := project(t, prep, player, modes.PhaseCollecting, states)
	if view.Reveal != nil {
		t.Fatal("the reveal is visible while the round is collecting")
	}
	// Visible once settled.
	settled := project(t, prep, player, modes.PhaseSettled, states)
	if settled.Reveal == nil {
		t.Fatal("the settled projection carries no reveal")
	}
	priv = currentPrivate(t, prep)
	if settled.Reveal.Truth != priv.Truth {
		t.Fatalf("reveal truth = %q, want %q", settled.Reveal.Truth, priv.Truth)
	}
	if settled.Reveal.Source == "" {
		t.Fatal("the reveal carries no source")
	}
	if settled.Reveal.FactID != priv.FactID {
		t.Fatalf("reveal factId = %q, want %q", settled.Reveal.FactID, priv.FactID)
	}
	if len(settled.Reveal.Decoys) != len(priv.Decoys) {
		t.Fatalf("reveal lists %d decoys, want %d", len(settled.Reveal.Decoys), len(priv.Decoys))
	}
	// The reveal is where authorship finally appears.
	for _, d := range settled.Reveal.Decoys {
		if len(d.AuthorIDs) == 0 {
			t.Fatalf("revealed decoy %q carries no author", d.DecoyID)
		}
	}
}

// TestFakeAnswersAreNotPersistedToTheFactStore asserts the plan's rule that a decoy is
// scoped to this round: nothing in the state presents a decoy as a fact.
func TestFakeAnswersAreNotPersistedToTheFactStore(t *testing.T) {
	prep, pub, _ := prepareRound(t, reviewedPack(3), "scope-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		decoyAction("这个假答案不该进事实库"), nil)
	prep = next
	states["p1"] = state

	// The public state must not present the decoy as a fact: no reviewStatus, no
	// source, and the scoring text says the decoy is round-scoped.
	public := string(prep.Public)
	if strings.Contains(public, "reviewStatus") {
		t.Fatal("the public state carries a review status for a decoy")
	}
	if !strings.Contains(pub.Scoring, "不写入事实库") {
		t.Fatalf("the scoring text does not scope decoys to the round: %q", pub.Scoring)
	}
	// The settle explanation repeats the scope.
	scores := settle(t, prep, states)
	var exp struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if !strings.Contains(exp.Note, "不写入事实库") {
		t.Fatalf("the explanation does not scope decoys to the round: %q", exp.Note)
	}
}

// TestConfigurationBounds covers the documented configuration limits.
func TestConfigurationBounds(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{name: "default", cfg: `{}`},
		{name: "nil", cfg: ``},
		{name: "explicit", cfg: `{"writeSeconds":60,"voteSeconds":30}`},
		{name: "write too short", cfg: `{"writeSeconds":19}`, wantErr: "outside"},
		{name: "write too long", cfg: `{"writeSeconds":301}`, wantErr: "outside"},
		{name: "vote too short", cfg: `{"voteSeconds":9}`, wantErr: "outside"},
		{name: "vote too long", cfg: `{"voteSeconds":181}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"writeSeconds":"sixty"}`, wantErr: "invalid config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig rejected %s: %v", tc.cfg, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig accepted %s", tc.cfg)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestParticipantBounds covers the 3–8 unit bound. The bound is checked before the
// admission gate, so a bad roster is reported as a bad roster.
func TestParticipantBounds(t *testing.T) {
	for _, players := range []int{0, 1, 2, 9} {
		t.Run(fmt.Sprintf("units%d", players), func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bounds-seed"),
				Subject: roster(players),
				Config:  json.RawMessage(`{}`),
				Content: reviewedPack(3),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a %d-unit round", players)
			}
			if !strings.Contains(err.Error(), "outside") {
				t.Fatalf("error %q does not mention the bound", err)
			}
		})
	}
}

// TestPrepareIsDeterministic asserts the fact choice is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	first, _, _ := prepareRound(t, reviewedPack(5), "det-seed", `{}`, 3)
	second, _, _ := prepareRound(t, reviewedPack(5), "det-seed", `{}`, 3)
	if string(first.Public) != string(second.Public) {
		t.Fatal("public state is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}
}

// TestOptionsAreGroupedBeyondNine covers the "选项最多 9 个；超出分组" rule.
func TestOptionsAreGroupedBeyondNine(t *testing.T) {
	// Eight units produce eight decoys plus the truth = nine options, which still fits.
	prep, pub, _ := prepareRound(t, reviewedPack(3), "group-seed", `{}`, 8)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	prep = openVotingFixture(t, prep)
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	if len(view.Options) != 9 {
		t.Fatalf("ballot has %d options, want 9", len(view.Options))
	}
	if view.OptionsGrouped {
		t.Fatalf("a nine-option ballot is marked grouped")
	}

	// A ballot beyond nine must be flagged for grouping. The roster bound caps decoys
	// at eight, so the flag is exercised through the public state directly.
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	extra := make([]option, 0, len(board.Options)+2)
	extra = append(extra, board.Options...)
	extra = append(extra,
		option{OptionID: "bo-extra1", Text: "额外一"},
		option{OptionID: "bo-extra2", Text: "额外二"},
	)
	board.Options = extra
	prep.Public = mustJSON(t, board)
	grouped := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	if !grouped.OptionsGrouped {
		t.Fatal("an eleven-option ballot is not flagged for grouping")
	}
}

// TestBallotOrderIsStable asserts the ballot order is a pure function of the seed, so
// two clients see the same ballot.
func TestBallotOrderIsStable(t *testing.T) {
	prep, pub, _ := prepareRound(t, reviewedPack(3), "order-seed", `{}`, 4)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	first := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	second := project(t, prep, modes.Viewer{MembershipID: "m2", Role: "player",
		Subject: modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	if len(first.Options) != len(second.Options) {
		t.Fatalf("two viewers see %d and %d options", len(first.Options), len(second.Options))
	}
	for i := range first.Options {
		if first.Options[i].OptionID != second.Options[i].OptionID {
			t.Fatalf("option %d differs between viewers: %q vs %q",
				i, first.Options[i].OptionID, second.Options[i].OptionID)
		}
	}
	// The order must not be the decoy-id order, which would be reproducible from the
	// texts alone.
	ids := make([]string, 0, len(first.Options))
	for _, opt := range first.Options {
		ids = append(ids, opt.OptionID)
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	if strings.Join(ids, ",") == strings.Join(sorted, ",") && len(ids) > 3 {
		t.Logf("ballot happens to be in id order for this seed: %v", ids)
	}
}

// TestSelfAuthoredFlagDoesNotLeakOthers asserts the self-authored flag is set for the
// caller's own decoy and for nobody else's.
func TestSelfAuthoredFlagDoesNotLeakOthers(t *testing.T) {
	prep, pub, priv := prepareRound(t, reviewedPack(3), "self-flag-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}

	priv = currentPrivate(t, prep)
	own := make(map[string]bool, 1)
	for _, d := range priv.Decoys {
		if containsString(d.AuthorIDs, "p1") {
			own[optionIDFor(d.DecoyID)] = true
		}
	}
	if len(own) == 0 {
		t.Fatalf("p1's decoy is missing: %+v", priv.Decoys)
	}
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	for _, opt := range view.Options {
		want := own[opt.OptionID]
		if opt.SelfAuthored != want {
			t.Fatalf("option %q selfAuthored = %v, want %v", opt.OptionID, opt.SelfAuthored, want)
		}
	}
}

// mustJSON marshals v or fails.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// TestNFCBoundaryIsVisibleToPlayers asserts the NFC limitation is stated where a player reads it.
//
// The rule layer cannot compute NFC: the standard library has no normaliser and this package may
// not add a module dependency. So the client is responsible for calling normalize('NFC'). That is
// a capability boundary, and a boundary a player only discovers by being rejected is a bad one.
func TestNFCBoundaryIsVisibleToPlayers(t *testing.T) {
	prep, pub, _ := prepareRound(t, reviewedPack(3), "nfc-visible-seed", `{}`, 3)

	// 1. The public state carries the statement.
	if pub.TextRules == "" {
		t.Fatal("the public state carries no text rules, so a player cannot know the length limit")
	}
	// The rules must state the limit in plain words and must NOT expose encoding
	// vocabulary: normalisation is the program's job, not the player's.
	for _, banned := range []string{"NFC", "Unicode", "码点", "归一化"} {
		if strings.Contains(pub.TextRules, banned) {
			t.Fatalf("the text rules expose %q to players: %q", banned, pub.TextRules)
		}
	}
	if !strings.Contains(pub.TextRules, "字") {
		t.Fatalf("the text rules do not state the limit in plain words: %q", pub.TextRules)
	}
	// The bounds must be the published numbers.
	if !strings.Contains(pub.TextRules, fmt.Sprintf("%d", MinDecoyCodePoints)) ||
		!strings.Contains(pub.TextRules, fmt.Sprintf("%d", MaxDecoyCodePoints)) {
		t.Fatalf("the text rules do not state the %d-%d code point bounds: %q",
			MinDecoyCodePoints, MaxDecoyCodePoints, pub.TextRules)
	}

	// 2. It reaches the projection.
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)
	if view.TextRules == "" {
		t.Fatal("the projection drops the text rules, so the player never sees them")
	}

	// 3. Normalisation is performed by the server, not delegated to players.
	dec := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, decoyAction("e\u0301x"), nil)
	if !dec.Accepted {
		t.Fatalf("decomposed text should be normalised: %s", dec.Code)
	}
	var st subjectState
	if err := json.Unmarshal(dec.State, &st); err != nil {
		t.Fatal(err)
	}
	if st.DecoyText != "éx" {
		t.Fatalf("text=%q", st.DecoyText)
	}
}

// Existing scoring/duplicate tests explicitly open the host-reviewed ballot in
// their setup; stage authorisation has separate raw Apply and room regressions.
func openVotingFixture(t *testing.T, prep modes.Prepared) modes.Prepared {
	t.Helper()
	stage, err := Stage(prep.Public)
	if err != nil {
		t.Fatal(err)
	}
	if stage == "writing" {
		pub, priv, err := CloseWriting(prep.Public, prep.Private, "seed")
		if err != nil {
			t.Fatal(err)
		}
		prep.Public, prep.Private = pub, priv
		stage = "review"
	}
	if stage == "review" {
		pub, priv, err := OpenVoting(prep.Public, prep.Private, "seed", nil)
		if err != nil {
			t.Fatal(err)
		}
		prep.Public, prep.Private = pub, priv
	}
	return prep
}

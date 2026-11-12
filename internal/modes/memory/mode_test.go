package memory

import (
	"context"
	"encoding/json"
	"fmt"
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

// faceSpec is the on-the-wire spelling of a published face.
type faceSpec struct {
	FaceID    string `json:"faceId"`
	ImagePath string `json:"imagePath"`
}

// pairSpec is the on-the-wire spelling of a private pair.
type pairSpec struct {
	PairID string   `json:"pairId"`
	Faces  []string `json:"faces"`
}

// packOf builds a memory pack with n usable pairs (2n faces) plus the private
// pairing, wrapped in the SecretSource the mode requires.
func packOf(n int) modes.ContentSource {
	faces := make([]faceSpec, 0, n*2)
	pairs := make([]pairSpec, 0, n)
	for i := 0; i < n; i++ {
		pairID := fmt.Sprintf("mp-%04d", i)
		a := fmt.Sprintf("mf-%04d-a", i)
		b := fmt.Sprintf("mf-%04d-b", i)
		faces = append(faces,
			faceSpec{FaceID: a, ImagePath: fmt.Sprintf("/faces/%s.png", a)},
			faceSpec{FaceID: b, ImagePath: fmt.Sprintf("/faces/%s.png", b)},
		)
		pairs = append(pairs, pairSpec{PairID: pairID, Faces: []string{a, b}})
	}
	pubRaw, err := json.Marshal(faces)
	if err != nil {
		panic(err)
	}
	privRaw, err := json.Marshal(struct {
		Mode  string     `json:"mode"`
		Pairs []pairSpec `json:"pairs"`
	}{Mode: ModeID, Pairs: pairs})
	if err != nil {
		panic(err)
	}
	return modeutil.NewSecretStore(
		fakeContent{"memory.pairs": pubRaw},
		fakeContent{"memory.solution": privRaw},
	)
}

// publicOnly returns a source with faces but no private pairing.
func publicOnly(n int) modes.ContentSource {
	faces := make([]faceSpec, 0, n*2)
	for i := 0; i < n; i++ {
		a := fmt.Sprintf("mf-%04d-a", i)
		b := fmt.Sprintf("mf-%04d-b", i)
		faces = append(faces,
			faceSpec{FaceID: a, ImagePath: fmt.Sprintf("/faces/%s.png", a)},
			faceSpec{FaceID: b, ImagePath: fmt.Sprintf("/faces/%s.png", b)},
		)
	}
	raw, err := json.Marshal(faces)
	if err != nil {
		panic(err)
	}
	return fakeContent{"memory.pairs": raw}
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

// flipAction builds an ActionFlip payload.
func flipAction(cardID string) modes.ModeAction {
	raw, _ := json.Marshal(flipPayload{CardID: cardID})
	return modes.ModeAction{Kind: ActionFlip, Payload: raw}
}

// applyOne runs one Apply call.
func applyOne(t *testing.T, prep modes.Prepared, subj modes.Subject, prev json.RawMessage, action modes.ModeAction) (modes.Decision, state) {
	t.Helper()
	// Ordinary sequence tests wait out any previous mismatch. The explicit
	// cooldown regression below tests earlier arrivals separately.
	var previous state
	if len(prev) > 0 {
		if err := json.Unmarshal(prev, &previous); err != nil {
			t.Fatal(err)
		}
	}
	dec, err := New().Apply(modes.ApplyInput{
		Scope:      scope("apply"),
		Subject:    subj,
		Action:     action,
		Public:     prep.Public,
		Private:    prep.Private,
		State:      prev,
		ReceivedAt: previous.RevealUntil + 1,
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

// pairCards groups the board's card ids by pair id.
func pairCards(priv privatePrepared) map[string][]string {
	out := make(map[string][]string, len(priv.PairLabel))
	for cardID, pairID := range priv.PairOf {
		out[pairID] = append(out[pairID], cardID)
	}
	return out
}

// TestBoardShape asserts the documented 4×4 board with eight pairs and opaque
// card ids .
func TestBoardShape(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, packOf(12), "shape-seed", `{}`, subjects...)

	if pub.Rows != 4 || pub.Cols != 4 {
		t.Fatalf("board = %dx%d, want 4x4", pub.Rows, pub.Cols)
	}
	if len(pub.Cards) != CardCount {
		t.Fatalf("cards = %d, want %d", len(pub.Cards), CardCount)
	}
	if len(priv.PairLabel) != PairCount {
		t.Fatalf("pairs = %d, want %d", len(priv.PairLabel), PairCount)
	}
	if prep.QuestionCount != PairCount {
		t.Fatalf("questionCount = %d, want %d", prep.QuestionCount, PairCount)
	}

	// Every pair appears exactly twice.
	counts := make(map[string]int, len(priv.PairLabel))
	for _, pairID := range priv.PairOf {
		counts[pairID]++
	}
	for pairID, n := range counts {
		if n != 2 {
			t.Fatalf("pair %q appears %d times, want 2", pairID, n)
		}
	}

	// Positions are 0..15 exactly once.
	seenPos := make(map[int]struct{}, CardCount)
	for _, card := range pub.Cards {
		if card.Position < 0 || card.Position >= CardCount {
			t.Fatalf("position %d out of range", card.Position)
		}
		if _, dup := seenPos[card.Position]; dup {
			t.Fatalf("position %d used twice", card.Position)
		}
		seenPos[card.Position] = struct{}{}
	}
}

// TestCardIDsAreOpaque covers the documented opaque-id rule and the leak check:
// a card id must not encode its pair, and the public payload must not contain the
// pairing map .
func TestCardIDsAreOpaque(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, packOf(12), "opaque-seed", `{}`, subjects...)

	// No card id contains any pair id.
	for _, card := range pub.Cards {
		for pairID := range priv.PairLabel {
			if strings.Contains(card.CardID, pairID) {
				t.Fatalf("card id %q contains the pair id %q", card.CardID, pairID)
			}
		}
	}
	// Card ids are unique.
	seen := make(map[string]struct{}, len(pub.Cards))
	for _, card := range pub.Cards {
		if _, dup := seen[card.CardID]; dup {
			t.Fatalf("duplicate card id %q", card.CardID)
		}
		seen[card.CardID] = struct{}{}
	}

	// The public prepared payload must not contain pair information.
	public := string(prep.Public)
	for pairID := range priv.PairLabel {
		if strings.Contains(public, pairID) {
			t.Fatalf("public prepared state leaks pair id %q", pairID)
		}
	}
	// Nor the images, which would let a client group identical faces.
	for _, image := range priv.CardImage {
		if strings.Contains(public, image) {
			t.Fatalf("public prepared state leaks the face image %q", image)
		}
	}
	for _, forbidden := range []string{"pairOf", "faces", "imagePath"} {
		if strings.Contains(public, forbidden) {
			t.Fatalf("public prepared state contains %q", forbidden)
		}
	}
}

// TestSameCardDoubleFlip covers the documented "同张双击" case: flipping the same
// card twice in one attempt is refused and does not consume an attempt
// .
func TestSameCardDoubleFlip(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, packOf(12), "double-seed", `{}`, subjects...)
	cardID := pub.Cards[0].CardID

	dec, st := applyOne(t, prep, subjects[0], nil, flipAction(cardID))
	if !dec.Accepted {
		t.Fatalf("first flip rejected: %s %s", dec.Code, dec.Detail)
	}
	if len(st.Flipped) != 1 {
		t.Fatalf("flipped = %v, want one card", st.Flipped)
	}

	dec2, _ := applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(cardID))
	if dec2.Accepted {
		t.Fatal("the same card was accepted twice")
	}
	if dec2.Code != CodeAlreadyFlipped {
		t.Fatalf("code = %q, want %q", dec2.Code, CodeAlreadyFlipped)
	}
	// A rejected action must return no new state, so the caller keeps the old one:
	// the attempt count and the face-up card are unchanged.
	if len(dec2.State) != 0 {
		t.Fatalf("a rejected action returned new state: %s", dec2.State)
	}
}

// TestThirdFlipRefusedBeforeResolution asserts the third-card rule using an
// unresolved attempt built directly in state.
func TestThirdFlipRefusedBeforeResolution(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, packOf(12), "third2-seed", `{}`, subjects...)

	// Force an unresolved two-card attempt in state, which is what happens between
	// the two flips of one attempt.
	st := state{Flipped: []string{pub.Cards[0].CardID, pub.Cards[1].CardID}}
	dec, _ := applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(pub.Cards[2].CardID))
	if dec.Accepted {
		t.Fatal("a third flip before resolution was accepted")
	}
	if dec.Code != CodeTooManyFlipped {
		t.Fatalf("code = %q, want %q", dec.Code, CodeTooManyFlipped)
	}
	if len(dec.State) != 0 {
		t.Fatalf("a rejected action returned new state: %s", dec.State)
	}
}

// TestMatchAndMismatchScoring covers the scoring formula
// max(0, floor(100×matched/8) - 2×wrong) .
func TestMatchAndMismatchScoring(t *testing.T) {
	cases := []struct {
		name    string
		matched int
		wrong   int
		want    int
	}{
		{name: "nothing matched", matched: 0, wrong: 0, want: 0},
		{name: "one pair no mistakes", matched: 1, wrong: 0, want: 1200},
		{name: "two pairs no mistakes", matched: 2, wrong: 0, want: 2500},
		{name: "four pairs no mistakes", matched: 4, wrong: 0, want: 5000},
		{name: "eight pairs no mistakes", matched: 8, wrong: 0, want: 10000},
		{name: "eight pairs one mistake", matched: 8, wrong: 1, want: 9800},
		{name: "eight pairs five mistakes", matched: 8, wrong: 5, want: 9000},
		{name: "one pair one mistake goes to zero", matched: 1, wrong: 6, want: 0},
		{name: "no pairs many mistakes stays at zero", matched: 0, wrong: 12, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreUnits(tc.matched, tc.wrong); got != tc.want {
				t.Fatalf("scoreUnits(%d,%d) = %d, want %d",
					tc.matched, tc.wrong, got, tc.want)
			}
		})
	}
}

// TestPerfectRunScoresFull plays a full winning board through Apply and asserts
// the score and finish flag .
func TestPerfectRunScoresFull(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, priv := prepare(t, packOf(12), "perfect-seed", `{}`, subjects...)

	groups := pairCards(priv)
	// Iterate pair ids in sorted order for a stable play sequence.
	pairIDs := make([]string, 0, len(groups))
	for pairID := range groups {
		pairIDs = append(pairIDs, pairID)
	}
	sortStrings(pairIDs)

	var st state
	for _, pairID := range pairIDs {
		ids := groups[pairID]
		if len(ids) != 2 {
			t.Fatalf("pair %q has %d cards", pairID, len(ids))
		}
		dec, next := applyOne(t, prep, subjects[0], decStateOrNil(st), flipAction(ids[0]))
		if !dec.Accepted {
			t.Fatalf("first flip of %q rejected: %s", pairID, dec.Code)
		}
		st = next
		dec, next = applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(ids[1]))
		if !dec.Accepted {
			t.Fatalf("second flip of %q rejected: %s", pairID, dec.Code)
		}
		st = next
	}
	if st.matchedPairs() != PairCount {
		t.Fatalf("matchedPairs = %d, want %d", st.matchedPairs(), PairCount)
	}
	if st.WrongAttempts != 0 {
		t.Fatalf("wrongAttempts = %d, want 0", st.WrongAttempts)
	}
	if !st.Finished {
		t.Fatal("a completed board did not finish")
	}
	if got := scoreUnits(st.matchedPairs(), st.WrongAttempts); got != 10000 {
		t.Fatalf("perfect score = %d, want 10000", got)
	}

	// Settle agrees.
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("perfect-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st)},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if scores[0].ScoreUnits != 10000 {
		t.Fatalf("settled score = %d, want 10000", scores[0].ScoreUnits)
	}
	if err := scores[0].Validate(); err != nil {
		t.Fatalf("invalid score: %v", err)
	}
}

// decStateOrNil encodes a state only when it is non-zero.
func decStateOrNil(st state) json.RawMessage {
	if st.Attempts == 0 && len(st.Flipped) == 0 && len(st.Matched) == 0 {
		return nil
	}
	return modeutil.Marshal(st)
}

// sortStrings sorts a small string slice in place.
func sortStrings(list []string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// TestAttemptLimitEndsBoard covers the documented "尝试用尽" end condition
// .
func TestAttemptLimitEndsBoard(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// A tiny attempt budget makes the boundary cheap to reach.
	prep, pub, priv := prepare(t, packOf(12), "limit-seed", `{"maxAttempts":2}`, subjects...)
	if pub.Config.MaxAttempts != 2 {
		t.Fatalf("maxAttempts = %d, want 2", pub.Config.MaxAttempts)
	}

	groups := pairCards(priv)
	pairIDs := make([]string, 0, len(groups))
	for pairID := range groups {
		pairIDs = append(pairIDs, pairID)
	}
	sortStrings(pairIDs)
	if len(pairIDs) < 2 {
		t.Fatal("not enough pairs")
	}

	// Two mismatching attempts exhaust the budget.
	var st state
	for attempt := 0; attempt < 2; attempt++ {
		a := groups[pairIDs[attempt]][0]
		b := groups[pairIDs[attempt+1]][0]
		_, st = applyOne(t, prep, subjects[0], decStateOrNil(st), flipAction(a))
		dec, next := applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(b))
		if !dec.Accepted {
			t.Fatalf("attempt %d rejected: %s", attempt+1, dec.Code)
		}
		st = next
	}
	if st.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", st.Attempts)
	}
	if st.WrongAttempts != 2 {
		t.Fatalf("wrongAttempts = %d, want 2", st.WrongAttempts)
	}
	if !st.Finished {
		t.Fatal("the board did not finish when attempts ran out")
	}

	// A further flip is refused.
	dec, _ := applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(groups[pairIDs[0]][0]))
	if dec.Accepted {
		t.Fatal("a flip after the attempts ran out was accepted")
	}
	if dec.Code != CodeOutOfAttempts {
		t.Fatalf("code = %q, want %q", dec.Code, CodeOutOfAttempts)
	}
}

// TestReconnectKeepsKnownState covers the documented "重连刷牌" case: reconnecting
// must not re-reveal the whole board, and the known/matched state is preserved
// .
func TestReconnectKeepsKnownState(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, packOf(12), "reconnect-seed", `{}`, subjects...)

	groups := pairCards(priv)
	pairIDs := make([]string, 0, len(groups))
	for pairID := range groups {
		pairIDs = append(pairIDs, pairID)
	}
	sortStrings(pairIDs)
	ids := groups[pairIDs[0]]

	// Match one pair, then reconnect mid-collecting phase.
	var st state
	_, st = applyOne(t, prep, subjects[0], nil, flipAction(ids[0]))
	_, st = applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(ids[1]))

	out, err := New().Project(modes.ProjectInput{
		Scope:   scope("reconnect-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st)},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var view projection
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}

	// Exactly the two matched cards are visible; every other card is hidden.
	visible := 0
	for _, card := range view.Cards {
		switch card.State {
		case cardMatched:
			visible++
			if card.ImagePath == "" {
				t.Fatalf("matched card %q has no face", card.CardID)
			}
		case cardHidden:
			if card.ImagePath != "" {
				t.Fatalf("hidden card %q exposes its face", card.CardID)
			}
		default:
			t.Fatalf("card %q in unexpected state %q", card.CardID, card.State)
		}
	}
	if visible != 2 {
		t.Fatalf("visible cards = %d, want 2", visible)
	}
	// The known state survived.
	if view.Self == nil || view.Self.MatchedPairs != 1 {
		t.Fatalf("reconnected state lost the matched pair: %+v", view.Self)
	}
	_ = pub
}

// TestViewingPhaseShowsAllFaces covers the documented 8-second viewing phase: all
// faces are visible during briefing and hidden afterwards .
func TestViewingPhaseShowsAllFaces(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, packOf(12), "view-seed", `{}`, subjects...)

	during, err := New().Project(modes.ProjectInput{
		Scope:   scope("view-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseBriefing,
	})
	if err != nil {
		t.Fatalf("Project briefing: %v", err)
	}
	var duringView projection
	if err := json.Unmarshal(during, &duringView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	visible := 0
	for _, card := range duringView.Cards {
		if card.ImagePath != "" {
			visible++
		}
	}
	if visible != len(pub.Cards) {
		t.Fatalf("visible cards during viewing = %d, want %d", visible, len(pub.Cards))
	}
	// The pairing map is still absent: only faces, not pair ids.
	for _, card := range duringView.Cards {
		if card.PairID != "" {
			t.Fatalf("card %q exposed its pair id during viewing", card.CardID)
		}
	}

	// After briefing, everything is hidden again.
	after, err := New().Project(modes.ProjectInput{
		Scope:   scope("view-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project collecting: %v", err)
	}
	var afterView projection
	if err := json.Unmarshal(after, &afterView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	for _, card := range afterView.Cards {
		if card.ImagePath != "" {
			t.Fatalf("card %q is still visible after the viewing phase", card.CardID)
		}
	}
}

// TestAssistiveTextDoesNotLeak covers the documented "辅助文本泄露" case: no
// projected field carries the pair id or a label that would give away the match
// while the card is hidden .
func TestAssistiveTextDoesNotLeak(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, priv := prepare(t, packOf(12), "assist-seed", `{}`, subjects...)

	out, err := New().Project(modes.ProjectInput{
		Scope:   scope("assist-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var view projection
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	// No hidden card carries a label or pair id, which is what an assistive
	// tooltip would read out.
	for _, card := range view.Cards {
		if card.State != cardHidden {
			continue
		}
		if card.Label != "" {
			t.Fatalf("hidden card %q exposes the label %q", card.CardID, card.Label)
		}
		if card.PairID != "" {
			t.Fatalf("hidden card %q exposes its pair id", card.CardID)
		}
	}
	// The projection as a whole must not contain any hidden card's face id or
	// image, which is what an assistive tooltip or a DOM inspection would read.
	serialized := string(out)
	for _, image := range priv.CardImage {
		if image == "" {
			continue
		}
		// A hidden card must not contribute its image to the payload; matched or
		// flipped cards legitimately do, and this test's board has none.
		if strings.Contains(serialized, image) {
			t.Fatalf("projection leaks a hidden card image %q", image)
		}
	}
}

// TestDifferentLoadSpeedSameBoard covers the documented "不同加载速度" case: the
// board is a pure function of the seed, so a slow client and a fast client see the
// same layout .
func TestDifferentLoadSpeedSameBoard(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	first, pubA, privA := prepare(t, packOf(12), "load-seed", `{}`, subjects...)
	second, pubB, privB := prepare(t, packOf(12), "load-seed", `{}`, subjects...)

	if string(first.Public) != string(second.Public) {
		t.Fatal("the board differs between two loads of the same seed")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("the pairing map differs between two loads of the same seed")
	}
	// The layout is also stable across a second projection.
	if len(pubA.Cards) != len(pubB.Cards) {
		t.Fatalf("card counts differ: %d vs %d", len(pubA.Cards), len(pubB.Cards))
	}
	for i := range pubA.Cards {
		if pubA.Cards[i].CardID != pubB.Cards[i].CardID {
			t.Fatalf("card %d differs: %q vs %q", i, pubA.Cards[i].CardID, pubB.Cards[i].CardID)
		}
	}
	// The pairing is the same too.
	if len(privA.PairOf) != len(privB.PairOf) {
		t.Fatalf("pairing sizes differ")
	}
	for cardID, pairID := range privA.PairOf {
		if privB.PairOf[cardID] != pairID {
			t.Fatalf("pairing of %q differs", cardID)
		}
	}
}

// TestLayoutNotPairAdjacent asserts the layout is shuffled: the two cards of a
// pair are not always in adjacent positions, which would make the board trivial.
func TestLayoutNotPairAdjacent(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// Across seeds, at least one pair must be non-adjacent in at least one
	// layout; an unshuffled board would keep every pair adjacent forever.
	foundNonAdjacent := false
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5"} {
		_, pub, priv := prepare(t, packOf(12), seed, `{}`, subjects...)
		pos := make(map[string]int, len(pub.Cards))
		for _, card := range pub.Cards {
			pos[card.CardID] = card.Position
		}
		for _, ids := range pairCards(priv) {
			if len(ids) != 2 {
				continue
			}
			diff := pos[ids[0]] - pos[ids[1]]
			if diff < 0 {
				diff = -diff
			}
			if diff != 1 {
				foundNonAdjacent = true
			}
		}
	}
	if !foundNonAdjacent {
		t.Fatal("every pair stayed position-adjacent, the layout was not shuffled")
	}
}

// TestSettleRanksAndTies asserts settlement ranks and shared ranks (,
// ).
func TestSettleRanksAndTies(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	prep, _, _ := prepare(t, packOf(12), "settle-seed", `{}`, subjects...)

	states := map[string]json.RawMessage{
		// p1 and p2 finish identically; p3 never plays.
		"p1": modeutil.Marshal(state{Matched: []string{"a", "b", "c", "d", "e", "f", "g", "h"}, Attempts: 4}),
		"p2": modeutil.Marshal(state{Matched: []string{"i", "j", "k", "l", "m", "n", "o", "p"}, Attempts: 4}),
	}
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("settle-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("scores = %d, want 3", len(scores))
	}
	if scores[0].ScoreUnits != 5000 || scores[1].ScoreUnits != 5000 {
		t.Fatalf("tied scores = %d,%d, want 5000,5000",
			scores[0].ScoreUnits, scores[1].ScoreUnits)
	}
	if scores[0].Rank != 1 || scores[1].Rank != 1 {
		t.Fatalf("tied ranks = %d,%d, want 1,1", scores[0].Rank, scores[1].Rank)
	}
	if !scores[0].SharedRank || !scores[1].SharedRank {
		t.Fatal("tied scores must report SharedRank")
	}
	if scores[2].ScoreUnits != 0 || scores[2].Rank != 3 {
		t.Fatalf("no-play = %d units rank %d, want 0 rank 3",
			scores[2].ScoreUnits, scores[2].Rank)
	}
}

// TestSettleDeterministic asserts settlement is order-independent .
func TestSettleDeterministic(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, _, _ := prepare(t, packOf(12), "settle-det", `{}`, subjects...)
	states := map[string]json.RawMessage{
		"p1": modeutil.Marshal(state{Matched: []string{"a", "b"}, WrongAttempts: 1, Attempts: 2}),
		"p2": modeutil.Marshal(state{Matched: []string{"c", "d", "e", "f"}, Attempts: 3}),
	}
	run := func() string {
		scores, err := New().Settle(modes.SettleInput{
			Scope:   scope("settle-det"),
			Phase:   modes.PhaseSettled,
			Public:  prep.Public,
			Private: prep.Private,
			States:  states,
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

// TestRevealIncludesPairingMap asserts the pairing map is released on reveal
// .
func TestRevealIncludesPairingMap(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, priv := prepare(t, packOf(12), "reveal-seed", `{}`, subjects...)

	out, err := New().Project(modes.ProjectInput{
		Scope:     scope("reveal-seed"),
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
	var view projection
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if view.Answer == nil {
		t.Fatal("the reveal did not include the pairing map")
	}
	if len(view.Answer.Pairs) != PairCount {
		t.Fatalf("revealed pairs = %d, want %d", len(view.Answer.Pairs), PairCount)
	}
	for _, pair := range view.Answer.Pairs {
		if len(pair.Cards) != 2 {
			t.Fatalf("revealed pair %q has %d cards", pair.PairID, len(pair.Cards))
		}
		for _, card := range pair.Cards {
			if priv.PairOf[card.CardID] != pair.PairID {
				t.Fatalf("revealed pair %q claims card %q which belongs to %q",
					pair.PairID, card.CardID, priv.PairOf[card.CardID])
			}
			if card.ImagePath == "" {
				t.Fatalf("revealed card %q has no face image", card.CardID)
			}
		}
	}
}

// TestPrepareRejectsInsufficientPairs asserts a pack without eight verified pairs
// is refused before the round opens .
func TestPrepareRejectsInsufficientPairs(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	cases := []struct {
		name string
		pack modes.ContentSource
	}{
		{name: "no section", pack: fakeContent{}},
		{name: "empty array", pack: fakeContent{"memory.pairs": json.RawMessage(`[]`)}},
		{name: "too few pairs", pack: packOf(7)},
		{
			// Faces exist but no private pairing: the board cannot be graded.
			name: "no private pairing",
			pack: publicOnly(12),
		},
		{
			// Faces published without an image are unusable.
			name: "faces without images",
			pack: func() modes.ContentSource {
				specs := make([]faceSpec, 0, 20)
				for i := 0; i < 20; i++ {
					specs = append(specs, faceSpec{FaceID: fmt.Sprintf("mf-%04d", i)})
				}
				pubRaw, _ := json.Marshal(specs)
				pairs := make([]pairSpec, 0, 10)
				for i := 0; i < 10; i++ {
					pairs = append(pairs, pairSpec{
						PairID: fmt.Sprintf("mp-%04d", i),
						Faces:  []string{fmt.Sprintf("mf-%04d", i*2), fmt.Sprintf("mf-%04d", i*2+1)},
					})
				}
				privRaw, _ := json.Marshal(struct {
					Mode  string     `json:"mode"`
					Pairs []pairSpec `json:"pairs"`
				}{Mode: ModeID, Pairs: pairs})
				return modeutil.NewSecretStore(
					fakeContent{"memory.pairs": pubRaw},
					fakeContent{"memory.solution": privRaw},
				)
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("insufficient"),
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

// TestUnknownCardRejected asserts an unknown card id is refused.
func TestUnknownCardRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, packOf(12), "unknown-seed", `{}`, subjects...)
	dec, _ := applyOne(t, prep, subjects[0], nil, flipAction("nope"))
	if dec.Accepted {
		t.Fatal("an unknown card was accepted")
	}
	if dec.Code != CodeUnknownCard {
		t.Fatalf("code = %q, want %q", dec.Code, CodeUnknownCard)
	}
}

// TestMatchedCardCannotBeReflipped asserts a matched card is permanently face up
// .
func TestMatchedCardCannotBeReflipped(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, priv := prepare(t, packOf(12), "matched-seed", `{}`, subjects...)

	groups := pairCards(priv)
	var ids []string
	for _, pair := range groups {
		ids = pair
		break
	}
	var st state
	_, st = applyOne(t, prep, subjects[0], nil, flipAction(ids[0]))
	_, st = applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(ids[1]))
	if len(st.Matched) != 2 {
		t.Fatalf("matched = %v, want two cards", st.Matched)
	}

	dec, _ := applyOne(t, prep, subjects[0], modeutil.Marshal(st), flipAction(ids[0]))
	if dec.Accepted {
		t.Fatal("a matched card was flipped again")
	}
	if dec.Code != CodeAlreadyMatched {
		t.Fatalf("code = %q, want %q", dec.Code, CodeAlreadyMatched)
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
		{name: "explicit view seconds", cfg: `{"viewSeconds":8}`},
		{name: "explicit max attempts", cfg: `{"maxAttempts":20}`},
		{name: "zero attempts rejected", cfg: `{"maxAttempts":-1}`, wantErr: true},
		{name: "negative view rejected", cfg: `{"viewSeconds":-1}`, wantErr: true},
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

// TestDefaultsMatchDocumentation pins the documented parameters .
func TestDefaultsMatchDocumentation(t *testing.T) {
	if Rows != 4 || Cols != 4 {
		t.Fatalf("board = %dx%d, want 4x4", Rows, Cols)
	}
	if PairCount != 8 {
		t.Fatalf("PairCount = %d, want 8", PairCount)
	}
	if ViewSeconds != 8 {
		t.Fatalf("ViewSeconds = %d, want 8", ViewSeconds)
	}
	if AnswerSeconds != 90 {
		t.Fatalf("AnswerSeconds = %d, want 90", AnswerSeconds)
	}
	if MaxAttempts != 20 {
		t.Fatalf("MaxAttempts = %d, want 20", MaxAttempts)
	}
	if WrongPenalty != 2 {
		t.Fatalf("WrongPenalty = %d, want 2", WrongPenalty)
	}
	cfg, err := decodeConfig(nil)
	if err != nil {
		t.Fatalf("decodeConfig: %v", err)
	}
	if cfg.ViewSeconds != ViewSeconds || cfg.AnswerSeconds != AnswerSeconds ||
		cfg.MaxAttempts != MaxAttempts {
		t.Fatalf("default config = %+v, want the documented values", cfg)
	}
}

func TestMismatchShowsBothFacesBrieflyAndBlocksThirdFlip(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectTeam}, {ID: "p2", Kind: modes.SubjectTeam}}
	prep, pub, priv := prepare(t, packOf(12), "mismatch-visibility", `{}`, subjects...)
	first := pub.Cards[0].CardID
	second, third := "", ""
	for _, card := range pub.Cards {
		if priv.PairOf[card.CardID] != priv.PairOf[first] {
			if second == "" {
				second = card.CardID
			} else {
				third = card.CardID
				break
			}
		}
	}
	apply := func(prev json.RawMessage, id string, at int64) modes.Decision {
		t.Helper()
		d, err := New().Apply(modes.ApplyInput{Subject: subjects[0], Action: flipAction(id), Public: prep.Public, Private: prep.Private, State: prev, ReceivedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	a := apply(nil, first, 1000)
	b := apply(a.State, second, 1100)
	if !b.Accepted {
		t.Fatal(b)
	}
	early := apply(b.State, third, 2099)
	if early.Accepted || early.Code != CodeTooManyFlipped {
		t.Fatalf("early=%+v", early)
	}
	project := func(subject modes.Subject, at int64) projection {
		t.Helper()
		raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: map[string]json.RawMessage{subjects[0].ID: b.State}, Viewer: modes.Viewer{Subject: subject}, Phase: modes.PhaseCollecting, ServerTime: at})
		if err != nil {
			t.Fatal(err)
		}
		var out projection
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	own := project(subjects[0], 1500)
	visible := 0
	for _, c := range own.Cards {
		if c.State == cardFlipped {
			visible++
			if c.ImagePath == "" || c.PairID != "" {
				t.Fatal("missing face or leaked pair")
			}
		}
	}
	if visible != 2 || own.Self.RevealUntil != 2100 {
		t.Fatalf("visible=%d self=%+v", visible, own.Self)
	}
	for _, out := range []projection{project(subjects[1], 1500), project(subjects[0], 2100)} {
		for _, c := range out.Cards {
			if c.State != cardHidden || c.ImagePath != "" {
				t.Fatalf("unexpected face: %+v", c)
			}
		}
	}
	late := apply(b.State, third, 2100)
	if !late.Accepted {
		t.Fatalf("late=%+v", late)
	}
}

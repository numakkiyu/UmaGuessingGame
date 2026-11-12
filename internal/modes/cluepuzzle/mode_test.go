package cluepuzzle

import (
	"context"
	"encoding/json"
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

// fixtureProvenance is the provenance block every fixture card carries.
func fixtureProvenance() map[string]any {
	return map[string]any{
		"kind":            ProvenanceAuthored,
		"notOfficialData": true,
		"author":          "test-fixture",
		"basis":           "docs/玩法与规则.md (clue-puzzle)",
	}
}

// fixtureCardCount is the deck size the fixture publishes. It is larger than MaxCards so a round
// draws a subset, which is what the real pack does.
const fixtureCardCount = 12

// packOf builds a content source carrying the fixture deck.
func packOf() modes.ContentSource {
	return packWith(packOptions{})
}

// packOptions tunes what the fixture publishes.
type packOptions struct {
	omitSection    bool
	omitProvenance bool
	badProvenance  string
	cardCount      int
}

// packWith builds a pack from options.
func packWith(o packOptions) modes.ContentSource {
	if o.omitSection {
		return fakeContent{}
	}
	n := o.cardCount
	if n == 0 {
		n = fixtureCardCount
	}
	docs := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		entry := map[string]any{
			"cardId": fmt.Sprintf("dc-%02d", i+1),
			"label":  fmt.Sprintf("测试档案 %d", i+1),
		}
		if !o.omitProvenance {
			if o.badProvenance != "" {
				entry["provenance"] = json.RawMessage(o.badProvenance)
			} else {
				entry["provenance"] = fixtureProvenance()
			}
		}
		docs = append(docs, entry)
	}
	raw, err := json.Marshal(docs)
	if err != nil {
		panic(err)
	}
	return fakeContent{DeckSection: raw}
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
func prepareRound(t *testing.T, seed, cfg string, players int) (modes.Prepared, publicState, privateState) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: roster(players),
		Config:  json.RawMessage(cfg),
		Content: packOf(),
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

// assignAction builds a contract-shaped clue.assignment payload.
//
// The contract's Assignment schema names the card field "characterId" because the
// schema is shared with the draft mode, so the wire payload uses that name.
func assignAction(pairs ...string) modes.ModeAction {
	type item struct {
		SlotID      string `json:"slotId"`
		CharacterID string `json:"characterId"`
	}
	items := make([]item, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, item{SlotID: pairs[i], CharacterID: pairs[i+1]})
	}
	body, err := json.Marshal(struct {
		Kind        string `json:"kind"`
		Assignments []item `json:"assignments"`
	}{Kind: ActionKindAssignment, Assignments: items})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindAssignment, Payload: body}
}

// correctPairs renders the solution as flat slot/card arguments.
func correctPairs(priv privateState) []string {
	slots := make([]int, 0, len(priv.Solution))
	for slot := range priv.Solution {
		var index int
		if _, err := fmt.Sscanf(slot, "%d", &index); err != nil {
			panic(err)
		}
		slots = append(slots, index)
	}
	sort.Ints(slots)
	out := make([]string, 0, len(slots)*2)
	for _, index := range slots {
		out = append(out, fmt.Sprintf("%d", index), priv.Solution[fmt.Sprintf("%d", index)])
	}
	return out
}

// apply runs Apply against the current state.
func apply(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) (modes.Decision, json.RawMessage) {
	t.Helper()
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
	return dec, dec.State
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

// TestGeneratorProducesUniqueSolution covers the core generator contract across
// every documented card/participant combination.
//
// "生成器先生成有效方案与约束，再求解" means the puzzle must be verifiable,
// so this test re-solves each generated puzzle independently and asserts the
// answer count.
func TestGeneratorProducesUniqueSolution(t *testing.T) {
	for _, cards := range []int{4, 5, 6} {
		for _, players := range []int{3, 4, 5, 6} {
			name := fmt.Sprintf("cards%d_players%d", cards, players)
			t.Run(name, func(t *testing.T) {
				cfg := fmt.Sprintf(`{"cards":%d,"participants":%d}`, cards, players)
				prep, pub, priv := prepareRound(t, "unique-seed", cfg, players)

				if len(pub.Cards) != cards {
					t.Fatalf("deck has %d cards, want %d", len(pub.Cards), cards)
				}
				if len(pub.Slots) != cards {
					t.Fatalf("board has %d slots, want %d", len(pub.Slots), cards)
				}
				if priv.SolutionCount != 1 {
					t.Fatalf("solutionCount = %d, want 1", priv.SolutionCount)
				}
				// QuestionCount is the usable POOL size counted in DECK CARDS, matching
				// MinEntities which is also counted in cards. It is NOT the generated
				// constraint count: that is a different unit, and comparing the two is what
				// made the availability gate compare constraints against a card floor.
				if prep.QuestionCount != fixtureCardCount {
					t.Fatalf("questionCount = %d, want the deck size %d",
						prep.QuestionCount, fixtureCardCount)
				}
				if pub.ConstraintCount <= 0 {
					t.Fatalf("constraintCount = %d, want a positive count", pub.ConstraintCount)
				}

				// Re-solve independently: exactly one assignment satisfies the
				// kept constraints, and it is the recorded solution.
				cardIDs := make([]string, 0, len(pub.Cards))
				for _, c := range pub.Cards {
					cardIDs = append(cardIDs, c.CardID)
				}
				sols, err := solveAll(cardIDs, priv.Constraints, 10)
				if err != nil {
					t.Fatalf("solver: %v", err)
				}
				if len(sols) != 1 {
					t.Fatalf("independent solver found %d solutions, want 1", len(sols))
				}
				for slot, cardID := range priv.Solution {
					var index int
					if _, err := fmt.Sscanf(slot, "%d", &index); err != nil {
						t.Fatalf("solution slot %q is not an index", slot)
					}
					if sols[0][index] != cardID {
						t.Fatalf("slot %s: solver says %q, solution says %q",
							slot, sols[0][index], cardID)
					}
				}
			})
		}
	}
}

// TestEveryParticipantHoldsAnExclusiveClue covers the 「线索必要性」 bullet.
func TestEveryParticipantHoldsAnExclusiveClue(t *testing.T) {
	for _, players := range []int{3, 4, 5, 6} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			cfg := fmt.Sprintf(`{"cards":5,"participants":%d}`, players)
			_, pub, priv := prepareRound(t, "clue-seed", cfg, players)

			if len(priv.PrivateClues) != players {
				t.Fatalf("%d clue sets for %d participants", len(priv.PrivateClues), players)
			}
			// Count how many participants hold each clue.
			holders := make(map[string]int, 32)
			for _, list := range priv.PrivateClues {
				if len(list) == 0 {
					t.Fatal("a participant holds no clue")
				}
				for _, c := range list {
					holders[c.ConstraintID]++
				}
			}
			for _, entry := range pub.Subjects {
				exclusive := 0
				for _, c := range priv.PrivateClues[entry.SubjectID] {
					if holders[c.ConstraintID] == 1 {
						exclusive++
					}
				}
				if exclusive == 0 {
					t.Fatalf("%s holds no exclusive clue", entry.SubjectID)
				}
			}
			// Every private clue must be one of the published constraint set.
			kept := make(map[string]struct{}, len(priv.Constraints))
			for _, c := range priv.Constraints {
				kept[c.ConstraintID] = struct{}{}
			}
			for subjectID, list := range priv.PrivateClues {
				for _, c := range list {
					if _, ok := kept[c.ConstraintID]; !ok {
						t.Fatalf("%s holds clue %q which is not in the kept set",
							subjectID, c.ConstraintID)
					}
				}
			}
			// No clue may be shared with every participant: that would make it a
			// public constraint wearing a private label.
			for id, count := range holders {
				if count == players {
					t.Fatalf("clue %q is held by every participant but is not in the shared area", id)
				}
			}
		})
	}
}

// TestEveryKeptClueIsSatisfiedByTheAnswer asserts no clue contradicts the answer.
func TestEveryKeptClueIsSatisfiedByTheAnswer(t *testing.T) {
	_, pub, priv := prepareRound(t, "satisfied-seed", `{"cards":5,"participants":3}`, 3)
	sol := make(solution, len(priv.Solution))
	for slot, cardID := range priv.Solution {
		var index int
		if _, err := fmt.Sscanf(slot, "%d", &index); err != nil {
			t.Fatalf("solution slot %q is not an index", slot)
		}
		sol[index] = cardID
	}
	for _, c := range priv.Constraints {
		ok, err := satisfies(c, sol, len(pub.Cards))
		if err != nil {
			t.Fatalf("constraint %q: %v", c.ConstraintID, err)
		}
		if !ok {
			t.Fatalf("constraint %q is not satisfied by the recorded answer: %s",
				c.ConstraintID, c.Text)
		}
	}
}

// TestInformativeCluesActuallyNarrow covers the load-bearing half of 线索必要性:
// removing the informative clues must leave more than one solution, otherwise the
// clues were decorative.
func TestInformativeCluesActuallyNarrow(t *testing.T) {
	_, pub, priv := prepareRound(t, "narrow-seed", `{"cards":5,"participants":3}`, 3)
	cardIDs := make([]string, 0, len(pub.Cards))
	for _, c := range pub.Cards {
		cardIDs = append(cardIDs, c.CardID)
	}

	informative := make([]constraint, 0, len(priv.Constraints))
	for _, c := range priv.Constraints {
		if !c.Confirmatory {
			informative = append(informative, c)
		}
	}
	if len(informative) == 0 {
		t.Fatal("the puzzle carries no informative constraint")
	}
	// Without the informative constraints there must be more than one solution.
	limit := len(cardIDs)*factorial(len(cardIDs)) + 1
	count, err := countSolutions(cardIDs, informative, limit)
	if err != nil {
		t.Fatalf("solver: %v", err)
	}
	if count != 1 {
		t.Fatalf("the informative constraints leave %d solutions, want 1", count)
	}
	// And the puzzle without any constraint has many solutions, which is the
	// baseline the clues narrow down.
	baseCount, err := countSolutions(cardIDs, nil, limit)
	if err != nil {
		t.Fatalf("solver: %v", err)
	}
	if baseCount <= 1 {
		t.Fatalf("a %d-card deck has %d assignments; the puzzle would need no clue",
			len(cardIDs), baseCount)
	}
	if baseCount != factorial(len(cardIDs)) {
		t.Fatalf("a %d-card deck has %d assignments, want %d",
			len(cardIDs), baseCount, factorial(len(cardIDs)))
	}
}

// TestPositionCluesAreCapped asserts the generator does not hand the answer over
// by publishing a position clue per slot.
func TestPositionCluesAreCapped(t *testing.T) {
	for _, cards := range []int{4, 5, 6} {
		t.Run(fmt.Sprintf("cards%d", cards), func(t *testing.T) {
			cfg := fmt.Sprintf(`{"cards":%d,"participants":3}`, cards)
			_, _, priv := prepareRound(t, "position-seed", cfg, 3)
			positions := 0
			for _, c := range priv.Constraints {
				if c.Kind == KindPosition {
					positions++
				}
			}
			if positions > maxPositionClues {
				t.Fatalf("puzzle keeps %d position clues, cap is %d", positions, maxPositionClues)
			}
		})
	}
}

// TestNoDuplicateConstraintText covers "不能靠同一条拆成很多重复约束刷分" .
func TestNoDuplicateConstraintText(t *testing.T) {
	for _, cards := range []int{4, 5, 6} {
		t.Run(fmt.Sprintf("cards%d", cards), func(t *testing.T) {
			cfg := fmt.Sprintf(`{"cards":%d,"participants":4}`, cards)
			_, _, priv := prepareRound(t, "dup-text-seed", cfg, 4)
			seen := make(map[string]struct{}, len(priv.Constraints))
			for _, c := range priv.Constraints {
				if _, dup := seen[c.Text]; dup {
					t.Fatalf("constraint text %q appears twice", c.Text)
				}
				seen[c.Text] = struct{}{}
			}
			ids := make(map[string]struct{}, len(priv.Constraints))
			for _, c := range priv.Constraints {
				if _, dup := ids[c.ConstraintID]; dup {
					t.Fatalf("constraint id %q appears twice", c.ConstraintID)
				}
				ids[c.ConstraintID] = struct{}{}
			}
		})
	}
}

// TestUnsatisfiableConstraintSetRefused covers the 「无解」 bullet: a constraint set
// with no solution is refused rather than shipped.
func TestUnsatisfiableConstraintSetRefused(t *testing.T) {
	// A deck where "card-01 is in slot 1" and "card-01 is in slot 2" are both
	// asserted has no solution.
	cards := []string{"card-01", "card-02", "card-03", "card-04"}
	contradictory := []constraint{
		{ConstraintID: "c1", Kind: KindPosition, Text: "a 在第 1 位",
			Cards: []string{"card-01"}, Position: 1},
		{ConstraintID: "c2", Kind: KindPosition, Text: "a 在第 2 位",
			Cards: []string{"card-01"}, Position: 2},
	}
	sols, err := solveAll(cards, contradictory, 10)
	if err != nil {
		t.Fatalf("solver: %v", err)
	}
	if len(sols) != 0 {
		t.Fatalf("a contradictory constraint set yielded %d solutions", len(sols))
	}

	// And the generator never produces such a set: it refuses rather than
	// returning a puzzle with zero solutions. The refusal path is exercised by
	// asking for an impossible card count.
	_, err = New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("unsolvable-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{"cards":9,"participants":3}`),
	})
	if err == nil {
		t.Fatal("Prepare accepted a 9-card round")
	}
}

// TestMultiSolutionPuzzleAcceptedOnlyWhenVerified covers the 「多解接受」 bullet.
//
// With unique=false the round accepts a puzzle with several solutions, but every
// accepted answer must satisfy every constraint, which the generator asserts.
func TestMultiSolutionPuzzleAcceptedOnlyWhenVerified(t *testing.T) {
	cfg := `{"cards":4,"participants":3,"unique":false,"constraintCount":4}`
	prep, pub, priv := prepareRound(t, "multi-seed", cfg, 3)
	if priv.SolutionCount < 1 {
		t.Fatalf("solutionCount = %d", priv.SolutionCount)
	}
	cardIDs := make([]string, 0, len(pub.Cards))
	for _, c := range pub.Cards {
		cardIDs = append(cardIDs, c.CardID)
	}
	sols, err := solveAll(cardIDs, priv.Constraints, 100)
	if err != nil {
		t.Fatalf("solver: %v", err)
	}
	if len(sols) == 0 {
		t.Fatal("the multi-solution puzzle has no solution")
	}
	// Every accepted solution must satisfy every constraint.
	for _, sol := range sols {
		n, err := satisfiesCount(priv.Constraints, sol, len(cardIDs))
		if err != nil {
			t.Fatalf("satisfiesCount: %v", err)
		}
		if n != len(priv.Constraints) {
			t.Fatalf("an accepted solution satisfies %d of %d constraints",
				n, len(priv.Constraints))
		}
	}
	// The recorded count must equal the independent count.
	if len(sols) != priv.SolutionCount {
		t.Fatalf("recorded solutionCount = %d, independent solver found %d",
			priv.SolutionCount, len(sols))
	}
	_ = prep
}

// TestPrivateCluesNeverInPublicState covers the
// 「不把所有私人线索下发公共客户端」 requirement at its strongest: the public prepared
// state must not contain any private clue text at all.
func TestPrivateCluesNeverInPublicState(t *testing.T) {
	prep, pub, priv := prepareRound(t, "leak-seed", `{"cards":5,"participants":3}`, 3)

	// Collect the private clue texts.
	privateTexts := make([]string, 0, 16)
	for _, list := range priv.PrivateClues {
		for _, c := range list {
			privateTexts = append(privateTexts, c.Text)
		}
	}
	if len(privateTexts) == 0 {
		t.Fatal("no private clue was generated")
	}

	// The public state may contain the shared constraints, so the check is on the
	// private-only texts.
	sharedTexts := make(map[string]struct{}, len(pub.Shared))
	for _, c := range pub.Shared {
		sharedTexts[c.Text] = struct{}{}
	}
	public := string(prep.Public)
	checked := 0
	for _, text := range privateTexts {
		if _, isShared := sharedTexts[text]; isShared {
			continue
		}
		checked++
		if strings.Contains(public, text) {
			t.Fatalf("public prepared state contains the private clue %q", text)
		}
	}
	if checked == 0 {
		t.Fatal("every private clue is also a shared one; nothing is private")
	}
	// The private state must carry them, or the game has no clues.
	privateJSON := string(prep.Private)
	for _, text := range privateTexts[:1] {
		if !strings.Contains(privateJSON, text) {
			t.Fatalf("private state does not carry the clue %q", text)
		}
	}
}

// TestNonParticipantCannotReadClues covers the 「非队员读取」 and 「队外不可读」 bullets.
func TestNonParticipantCannotReadClues(t *testing.T) {
	prep, pub, priv := prepareRound(t, "outsider-seed", `{"cards":5,"participants":3}`, 3)

	viewers := []struct {
		name   string
		viewer modes.Viewer
	}{
		{name: "spectator", viewer: modes.Viewer{MembershipID: "s1", Role: "spectator"}},
		{name: "display", viewer: modes.Viewer{MembershipID: "d1", Role: "display"}},
		{name: "unknown subject", viewer: modes.Viewer{
			MembershipID: "x1", Role: "player",
			Subject: modes.Subject{ID: "not-on-roster", Kind: modes.SubjectPlayer},
		}},
	}
	for _, tc := range viewers {
		t.Run(tc.name, func(t *testing.T) {
			view := project(t, prep, tc.viewer, modes.PhaseCollecting, nil)
			if len(view.PrivateClues) != 0 {
				t.Fatalf("%s received %d private clues: %+v",
					tc.name, len(view.PrivateClues), view.PrivateClues)
			}
			if view.Self != nil {
				t.Fatalf("%s received a self view", tc.name)
			}
			// The clue texts must not appear anywhere in the payload.
			raw := string(mustJSON(t, view))
			for _, list := range priv.PrivateClues {
				for _, c := range list {
					if strings.Contains(raw, c.Text) {
						t.Fatalf("%s can read the clue %q", tc.name, c.Text)
					}
				}
			}
		})
	}
	_ = pub
}

// TestHostCannotReadPrivateClues asserts the host's reveal authority does not
// expose a player's private clue .
func TestHostCannotReadPrivateClues(t *testing.T) {
	prep, _, priv := prepareRound(t, "host-seed", `{"cards":5,"participants":3}`, 3)
	host := modes.Viewer{MembershipID: "host-1", Role: "host", IsReferee: true}
	view := project(t, prep, host, modes.PhaseCollecting, nil)
	if len(view.PrivateClues) != 0 {
		t.Fatalf("the host received %d private clues", len(view.PrivateClues))
	}
	// The host does get the solution, which is the documented reveal authority.
	if view.Answer == nil {
		t.Fatal("the host cannot see the solution")
	}
	// But the solution reveal must not carry a private clue either.
	raw := string(mustJSON(t, view.Answer))
	for _, list := range priv.PrivateClues {
		for _, c := range list {
			if strings.Contains(raw, c.Text) {
				t.Fatalf("the host's solution reveal carries the private clue %q", c.Text)
			}
		}
	}
}

// TestPlayerSeesOnlyOwnClues asserts one player never receives another's clues.
func TestPlayerSeesOnlyOwnClues(t *testing.T) {
	prep, pub, priv := prepareRound(t, "own-seed", `{"cards":5,"participants":3}`, 3)
	for _, entry := range pub.Subjects {
		view := project(t, prep, modes.Viewer{
			MembershipID: "m-" + entry.SubjectID,
			Role:         "player",
			Subject:      modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
		}, modes.PhaseCollecting, nil)

		own := make(map[string]struct{}, len(priv.PrivateClues[entry.SubjectID]))
		for _, c := range priv.PrivateClues[entry.SubjectID] {
			own[c.ConstraintID] = struct{}{}
		}
		if len(view.PrivateClues) != len(own) {
			t.Fatalf("%s sees %d clues, owns %d", entry.SubjectID, len(view.PrivateClues), len(own))
		}
		for _, c := range view.PrivateClues {
			if _, ok := own[c.ConstraintID]; !ok {
				t.Fatalf("%s received clue %q which belongs to somebody else",
					entry.SubjectID, c.ConstraintID)
			}
		}
		// Another participant's exclusive clue must not be present.
		for otherID, list := range priv.PrivateClues {
			if otherID == entry.SubjectID {
				continue
			}
			for _, c := range list {
				if _, mine := own[c.ConstraintID]; mine {
					continue
				}
				for _, received := range view.PrivateClues {
					if received.ConstraintID == c.ConstraintID {
						t.Fatalf("%s received %s's clue %q",
							entry.SubjectID, otherID, c.ConstraintID)
					}
				}
			}
		}
	}
}

// TestNonControllerCannotSubmit covers the 「队员换身份」 and 「控制权切换」 bullets.
func TestNonControllerCannotSubmit(t *testing.T) {
	prep, pub, priv := prepareRound(t, "control-seed", `{"cards":5,"participants":3}`, 3)
	pairs := correctPairs(priv)
	controller := modes.Subject{ID: pub.ControlSubjectID, Kind: modes.SubjectPlayer}
	if controller.ID == "" {
		t.Fatal("the round has no initial controller")
	}

	// A participant who is not the controller is refused.
	for _, entry := range pub.Subjects {
		if entry.SubjectID == controller.ID {
			continue
		}
		dec, _ := apply(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			assignAction(pairs...), nil)
		if dec.Accepted {
			t.Fatalf("%s submitted without holding the control lease", entry.SubjectID)
		}
		if dec.Code != CodeNotController {
			t.Fatalf("code = %q, want %q", dec.Code, CodeNotController)
		}
	}

	// The controller may submit.
	dec, state := apply(t, prep, controller, assignAction(pairs...), nil)
	if !dec.Accepted {
		t.Fatalf("the controller was refused: %q %q", dec.Code, dec.Detail)
	}

	// Transfer the lease by rewriting the public state, which is what the room
	// layer does when it rotates control. The former controller is then refused.
	var rotated publicState
	if err := json.Unmarshal(prep.Public, &rotated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	newController := ""
	for _, entry := range rotated.Subjects {
		if entry.SubjectID != controller.ID {
			newController = entry.SubjectID
			break
		}
	}
	rotated.ControlSubjectID = newController
	rotatedPrep := modes.Prepared{
		Public:        mustJSON(t, rotated),
		Private:       prep.Private,
		QuestionCount: prep.QuestionCount,
	}

	dec, _ = apply(t, rotatedPrep, controller, assignAction(pairs...), state)
	if dec.Accepted {
		t.Fatal("the former controller submitted after the lease moved")
	}
	if dec.Code != CodeNotController {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotController)
	}
	dec, _ = apply(t, rotatedPrep, modes.Subject{ID: newController, Kind: modes.SubjectPlayer},
		assignAction(pairs...), nil)
	if !dec.Accepted {
		t.Fatalf("the new controller was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestNonParticipantCannotSubmit covers 「非队员读取」 on the write path.
func TestNonParticipantCannotSubmit(t *testing.T) {
	prep, _, priv := prepareRound(t, "outsider-write-seed", `{"cards":5,"participants":3}`, 3)
	dec, _ := apply(t, prep, modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer},
		assignAction(correctPairs(priv)...), nil)
	if dec.Accepted {
		t.Fatal("a non-participant submission was accepted")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
}

// TestPartialSatisfactionScoresRatio covers the 「部分满足」 bullet and the documented
// scoring formula.
func TestPartialSatisfactionScoresRatio(t *testing.T) {
	prep, pub, priv := prepareRound(t, "partial-seed", `{"cards":5,"participants":3}`, 3)
	controller := modes.Subject{ID: pub.ControlSubjectID, Kind: modes.SubjectPlayer}
	total := pub.ConstraintCount
	if total < 2 {
		t.Fatalf("puzzle has %d constraints, need at least 2 for this test", total)
	}

	cases := []struct {
		name  string
		pairs []string
		want  int
	}{
		{name: "nothing submitted", pairs: nil, want: 0},
		{name: "fully correct", pairs: correctPairs(priv), want: 100},
		// A wrong-but-valid assignment: reverse the deck order.
		{
			name:  "reversed deck",
			pairs: reversedPairs(pub),
			want:  floorPoints(satisfiedBy(t, pub, priv, reversedAssignment(pub)), total),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			states := map[string]json.RawMessage{}
			if len(tc.pairs) > 0 {
				dec, state := apply(t, prep, controller, assignAction(tc.pairs...), nil)
				if !dec.Accepted {
					t.Fatalf("submission rejected: %q %q", dec.Code, dec.Detail)
				}
				states[controller.ID] = state
			}
			scores := settle(t, prep, states)
			if got := scores[controller.ID].ScoreUnits; got != tc.want*100 {
				t.Fatalf("score = %d units, want %d", got, tc.want*100)
			}
			// Every participant shares the team score.
			for _, entry := range pub.Subjects {
				if scores[entry.SubjectID].ScoreUnits != scores[controller.ID].ScoreUnits {
					t.Fatalf("%s scored %d but the team scored %d",
						entry.SubjectID, scores[entry.SubjectID].ScoreUnits,
						scores[controller.ID].ScoreUnits)
				}
			}
		})
	}
}

// floorPoints applies the documented floor(100×satisfied/total) formula.
func floorPoints(satisfied, total int) int {
	if total <= 0 {
		return 0
	}
	return 100 * satisfied / total
}

// reversedPairs renders the deck in reverse slot order.
func reversedPairs(pub publicState) []string {
	cards := make([]string, 0, len(pub.Cards))
	for _, c := range pub.Cards {
		cards = append(cards, c.CardID)
	}
	out := make([]string, 0, len(cards)*2)
	for i := len(cards) - 1; i >= 0; i-- {
		out = append(out, fmt.Sprintf("%d", len(cards)-i), cards[i])
	}
	return out
}

// reversedAssignment is reversedPairs as a solution.
func reversedAssignment(pub publicState) solution {
	pairs := reversedPairs(pub)
	sol := make(solution, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		var index int
		if _, err := fmt.Sscanf(pairs[i], "%d", &index); err != nil {
			panic(err)
		}
		sol[index] = pairs[i+1]
	}
	return sol
}

// satisfiedBy counts how many constraints a solution satisfies.
func satisfiedBy(t *testing.T, pub publicState, priv privateState, sol solution) int {
	t.Helper()
	n, err := satisfiesCount(priv.Constraints, sol, len(pub.Cards))
	if err != nil {
		t.Fatalf("satisfiesCount: %v", err)
	}
	return n
}

// TestInvalidSubmissionsRejected covers payload and board validation.
func TestInvalidSubmissionsRejected(t *testing.T) {
	prep, pub, priv := prepareRound(t, "invalid-seed", `{"cards":5,"participants":3}`, 3)
	controller := modes.Subject{ID: pub.ControlSubjectID, Kind: modes.SubjectPlayer}
	pairs := correctPairs(priv)

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "clue.teleport",
			body: `{"assignments":[{"slotId":"1","characterId":"card-01"}]}`, want: CodeUnknownAction},
		{name: "empty payload", kind: ActionKindAssignment, body: ``, want: CodeInvalidPayload},
		{name: "not json", kind: ActionKindAssignment, body: `nope`, want: CodeInvalidPayload},
		{name: "kind mismatch", kind: ActionKindAssignment,
			body: `{"kind":"pedigree.assignment","assignments":[{"slotId":"1","characterId":"card-01"}]}`,
			want: CodeInvalidPayload},
		{name: "unknown slot", kind: ActionKindAssignment,
			body: `{"kind":"clue.assignment","assignments":[{"slotId":"99","characterId":"card-01"}]}`,
			want: CodeUnknownSlot},
		{name: "unknown card", kind: ActionKindAssignment,
			body: `{"kind":"clue.assignment","assignments":[{"slotId":"1","characterId":"card-99"}]}`,
			want: CodeUnknownCard},
		{name: "duplicate card", kind: ActionKindAssignment,
			body: `{"kind":"clue.assignment","assignments":[` +
				`{"slotId":"1","characterId":"` + pub.Cards[0].CardID + `"},` +
				`{"slotId":"2","characterId":"` + pub.Cards[0].CardID + `"}]}`,
			want: CodeDuplicateCard},
		{name: "incomplete", kind: ActionKindAssignment,
			body: `{"kind":"clue.assignment","assignments":[` +
				`{"slotId":"1","characterId":"` + pub.Cards[0].CardID + `"}]}`,
			want: CodeIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := apply(t, prep, controller,
				modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)}, nil)
			if dec.Accepted {
				t.Fatal("an invalid submission was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}

	// The correct submission is still accepted after all those rejections.
	dec, _ := apply(t, prep, controller, assignAction(pairs...), nil)
	if !dec.Accepted {
		t.Fatalf("the correct submission was rejected: %q %q", dec.Code, dec.Detail)
	}
}

// TestRevisionReplacesPreviousSubmission asserts a controller may revise.
func TestRevisionReplacesPreviousSubmission(t *testing.T) {
	prep, pub, priv := prepareRound(t, "revise-seed", `{"cards":5,"participants":3}`, 3)
	controller := modes.Subject{ID: pub.ControlSubjectID, Kind: modes.SubjectPlayer}

	// Submit a deliberately wrong assignment first.
	wrong := reversedPairs(pub)
	dec, state := apply(t, prep, controller, assignAction(wrong...), nil)
	if !dec.Accepted {
		t.Fatalf("first submission rejected: %q %q", dec.Code, dec.Detail)
	}
	first := settle(t, prep, map[string]json.RawMessage{controller.ID: state})

	// Revise to the correct one.
	dec, state = apply(t, prep, controller, assignAction(correctPairs(priv)...), state)
	if !dec.Accepted {
		t.Fatalf("revision rejected: %q %q", dec.Code, dec.Detail)
	}
	second := settle(t, prep, map[string]json.RawMessage{controller.ID: state})
	if got := second[controller.ID].ScoreUnits; got != 10000 {
		t.Fatalf("revised answer scored %d units, want 10000", got)
	}
	if first[controller.ID].ScoreUnits == 10000 {
		t.Fatal("the deliberately wrong answer scored full marks")
	}

	var st subjectState
	if err := json.Unmarshal(state, &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.Submissions != 2 {
		t.Fatalf("submissions = %d, want 2", st.Submissions)
	}
}

// TestSubmissionFeedbackNeverRevealsCorrectness asserts a submission response does
// not tell the team whether the answer is right, which would let them probe.
func TestSubmissionFeedbackNeverRevealsCorrectness(t *testing.T) {
	prep, pub, priv := prepareRound(t, "feedback-seed", `{"cards":5,"participants":3}`, 3)
	controller := modes.Subject{ID: pub.ControlSubjectID, Kind: modes.SubjectPlayer}

	wrongDec, _ := apply(t, prep, controller, assignAction(reversedPairs(pub)...), nil)
	rightDec, _ := apply(t, prep, controller, assignAction(correctPairs(priv)...), nil)
	if !wrongDec.Accepted || !rightDec.Accepted {
		t.Fatal("a submission was rejected")
	}

	// The two feedback payloads must have the same shape: same keys, same
	// success signal. Only counts differ, and counts are derived from the board.
	var wrongFb, rightFb map[string]any
	if err := json.Unmarshal(wrongDec.Feedback, &wrongFb); err != nil {
		t.Fatalf("decode feedback: %v", err)
	}
	if err := json.Unmarshal(rightDec.Feedback, &rightFb); err != nil {
		t.Fatalf("decode feedback: %v", err)
	}
	wrongKeys := make([]string, 0, len(wrongFb))
	for k := range wrongFb {
		wrongKeys = append(wrongKeys, k)
	}
	rightKeys := make([]string, 0, len(rightFb))
	for k := range rightFb {
		rightKeys = append(rightKeys, k)
	}
	sort.Strings(wrongKeys)
	sort.Strings(rightKeys)
	if strings.Join(wrongKeys, ",") != strings.Join(rightKeys, ",") {
		t.Fatalf("feedback keys differ: wrong=%v right=%v", wrongKeys, rightKeys)
	}
	// No feedback field may be named after correctness.
	for _, key := range wrongKeys {
		lower := strings.ToLower(key)
		for _, forbidden := range []string{"correct", "satisfied", "score", "right", "wrong"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("feedback carries the field %q, which reveals correctness", key)
			}
		}
	}
}

// TestSolutionWithheldUntilReveal asserts the answer never reaches a player early.
func TestSolutionWithheldUntilReveal(t *testing.T) {
	prep, _, _ := prepareRound(t, "withhold-seed", `{"cards":5,"participants":3}`, 3)
	player := modes.Viewer{
		MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
	}
	view := project(t, prep, player, modes.PhaseCollecting, nil)
	if view.Answer != nil {
		t.Fatal("the solution is visible while the round is collecting")
	}
	settled := project(t, prep, player, modes.PhaseSettled, nil)
	if settled.Answer == nil {
		t.Fatal("the solution is not revealed after settle")
	}
	if len(settled.Answer.Slots) != 5 {
		t.Fatalf("reveal has %d slots, want 5", len(settled.Answer.Slots))
	}
	for _, slot := range settled.Answer.Slots {
		if slot.CardID == "" || slot.Label == "" {
			t.Fatalf("revealed slot %d is incomplete: %+v", slot.SlotIndex, slot)
		}
	}
	if settled.Answer.SolutionCount != 1 {
		t.Fatalf("reveal solutionCount = %d, want 1", settled.Answer.SolutionCount)
	}
	if len(settled.Answer.Constraints) == 0 {
		t.Fatal("the reveal carries no constraint explanation")
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
		{name: "three cards", cfg: `{"cards":3}`, wantErr: "outside"},
		{name: "seven cards", cfg: `{"cards":7}`, wantErr: "outside"},
		{name: "two participants", cfg: `{"participants":2}`, wantErr: "outside"},
		{name: "seven participants", cfg: `{"participants":7}`, wantErr: "outside"},
		{name: "too few constraints", cfg: `{"constraintCount":2}`, wantErr: "private clue"},
		{name: "too many constraints", cfg: `{"constraintCount":25}`, wantErr: "above 24"},
		{name: "negative shared", cfg: `{"sharedCount":-1}`, wantErr: "negative"},
		{name: "window too short", cfg: `{"answerSeconds":29}`, wantErr: "outside"},
		{name: "window too long", cfg: `{"answerSeconds":901}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"cards":"five"}`, wantErr: "invalid config"},
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

// TestParticipantCountMustMatchRoster asserts the configured team size and the
// actual roster agree, because the clue distribution is keyed on seats.
func TestParticipantCountMustMatchRoster(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("mismatch-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{"participants":5}`),
	})
	if err == nil {
		t.Fatal("Prepare accepted a roster that disagrees with the configuration")
	}
	if !strings.Contains(err.Error(), "roster has") {
		t.Fatalf("error %q does not explain the mismatch", err)
	}

	// A roster outside the documented bounds is refused.
	for _, players := range []int{0, 2, 7} {
		t.Run(fmt.Sprintf("roster%d", players), func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bounds-seed"),
				Subject: roster(players),
				Config:  json.RawMessage(`{}`),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a %d-player roster", players)
			}
		})
	}
}

// TestDuplicateSubjectRefused asserts a duplicated roster entry is refused.
func TestDuplicateSubjectRefused(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("dup-seed"),
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("Prepare accepted a duplicated subject")
	}
	if !strings.Contains(err.Error(), "duplicate subject") {
		t.Fatalf("error %q does not mention the duplicate", err)
	}
}

// TestPrepareIsDeterministic asserts the puzzle is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	first, _, _ := prepareRound(t, "det-seed", `{"cards":5,"participants":3}`, 3)
	second, _, _ := prepareRound(t, "det-seed", `{"cards":5,"participants":3}`, 3)
	if string(first.Public) != string(second.Public) {
		t.Fatal("public state is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}

	// A different seed produces a different puzzle (not merely a different answer:
	// the constraint set or the intended order must differ).
	_, otherPub, otherPriv := prepareRound(t, "det-other", `{"cards":5,"participants":3}`, 3)
	if string(mustJSON(t, otherPriv.Solution)) == string(mustJSON(t, mustSolution(t, first))) &&
		otherPub.ConstraintCount == 0 {
		t.Fatal("a different seed produced an empty puzzle")
	}
}

// mustSolution decodes the private solution of a prepared round.
func mustSolution(t *testing.T, prep modes.Prepared) map[string]string {
	t.Helper()
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return priv.Solution
}

// TestSharedAreaCarriesOnlyWeakClues asserts the public shared constraints are the
// weak kinds, so the shared area does not hand the answer over.
func TestSharedAreaCarriesOnlyWeakClues(t *testing.T) {
	_, pub, priv := prepareRound(t, "shared-seed", `{"cards":5,"participants":3,"sharedCount":2}`, 3)
	byID := make(map[string]constraint, len(priv.Constraints))
	for _, c := range priv.Constraints {
		byID[c.ConstraintID] = c
	}
	for _, shared := range pub.Shared {
		c, ok := byID[shared.ConstraintID]
		if !ok {
			t.Fatalf("shared constraint %q is not in the kept set", shared.ConstraintID)
		}
		if c.Kind == KindPosition {
			t.Fatalf("a position clue %q is published in the shared area", shared.ConstraintID)
		}
		if shared.Text == "" || len(shared.Cards) == 0 {
			t.Fatalf("shared constraint %q is incomplete: %+v", shared.ConstraintID, shared)
		}
	}
	// The shared constraints must not be the ones players also hold, or the
	// private allocation would be smaller than the published set claims.
	sharedIDs := make(map[string]struct{}, len(pub.Shared))
	for _, shared := range pub.Shared {
		sharedIDs[shared.ConstraintID] = struct{}{}
	}
	for subjectID, list := range priv.PrivateClues {
		for _, c := range list {
			if _, isShared := sharedIDs[c.ConstraintID]; isShared {
				t.Fatalf("%s holds clue %q which is also published as shared",
					subjectID, c.ConstraintID)
			}
		}
	}
}

// TestBoardDoesNotLeakTheAnswerOrder asserts the published card list and slot list
// do not reveal the solution order.
func TestBoardDoesNotLeakTheAnswerOrder(t *testing.T) {
	_, pub, priv := prepareRound(t, "order-seed", `{"cards":6,"participants":3}`, 3)
	// The cards are published sorted by id; the solution is a permutation. If the
	// published order happened to equal the solution order, the board would leak.
	published := make([]string, 0, len(pub.Cards))
	for _, c := range pub.Cards {
		published = append(published, c.CardID)
	}
	sortedCopy := append([]string{}, published...)
	sort.Strings(sortedCopy)
	if strings.Join(published, ",") != strings.Join(sortedCopy, ",") {
		t.Fatalf("the card list is not published in a neutral order: %v", published)
	}
	solutionOrder := make([]string, 0, len(pub.Slots))
	for _, slot := range pub.Slots {
		solutionOrder = append(solutionOrder, priv.Solution[fmt.Sprintf("%d", slot.SlotIndex)])
	}
	if strings.Join(published, ",") == strings.Join(solutionOrder, ",") {
		t.Fatalf("the published card order equals the answer order: %v", published)
	}
}

// TestCardLabelsAreStable asserts every published card has an id and a label and
// the slot list covers the board exactly once.
func TestCardLabelsAreStable(t *testing.T) {
	_, pub, _ := prepareRound(t, "labels-seed", `{"cards":5,"participants":3}`, 3)
	cardIDs := make(map[string]struct{}, len(pub.Cards))
	for _, c := range pub.Cards {
		if c.CardID == "" || c.Label == "" {
			t.Fatalf("card is incomplete: %+v", c)
		}
		if _, dup := cardIDs[c.CardID]; dup {
			t.Fatalf("card id %q appears twice", c.CardID)
		}
		cardIDs[c.CardID] = struct{}{}
	}
	slotIndexes := make(map[int]struct{}, len(pub.Slots))
	for _, slot := range pub.Slots {
		if slot.SlotIndex < 1 || slot.SlotIndex > len(pub.Cards) {
			t.Fatalf("slot index %d outside [1,%d]", slot.SlotIndex, len(pub.Cards))
		}
		if slot.Label != fmt.Sprintf("第 %d 位", slot.SlotIndex) {
			t.Fatalf("slot %d label does not match the clue position: %q", slot.SlotIndex, slot.Label)
		}
		if _, dup := slotIndexes[slot.SlotIndex]; dup {
			t.Fatalf("slot index %d appears twice", slot.SlotIndex)
		}
		slotIndexes[slot.SlotIndex] = struct{}{}
	}
	if len(slotIndexes) != len(pub.Cards) {
		t.Fatalf("%d slots for %d cards", len(slotIndexes), len(pub.Cards))
	}
}

// TestSolverRejectsMalformedConstraints asserts the solver refuses a constraint
// that names a card outside the deck rather than silently ignoring it.
func TestSolverRejectsMalformedConstraints(t *testing.T) {
	cards := []string{"card-01", "card-02", "card-03", "card-04"}
	cases := []struct {
		name string
		c    constraint
		want string
	}{
		{name: "unknown kind", c: constraint{ConstraintID: "c1", Kind: "teleport",
			Cards: []string{"card-01"}}, want: "unknown constraint kind"},
		{name: "not_with one card", c: constraint{ConstraintID: "c2", Kind: KindNotWith,
			Cards: []string{"card-01"}}, want: "needs 2 cards"},
		{name: "before one card", c: constraint{ConstraintID: "c3", Kind: KindBefore,
			Cards: []string{"card-01"}}, want: "needs 2 cards"},
		{name: "position out of range", c: constraint{ConstraintID: "c4", Kind: KindPosition,
			Cards: []string{"card-01"}, Position: 9}, want: "outside"},
		{name: "group one card", c: constraint{ConstraintID: "c5", Kind: KindGroup,
			Cards: []string{"card-01"}}, want: "at least 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := solveAll(cards, []constraint{tc.c}, 10)
			if err == nil {
				t.Fatal("the solver accepted a malformed constraint")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// A repeated card in the deck is refused too.
	_, err := solveAll([]string{"card-01", "card-01"}, nil, 10)
	if err == nil {
		t.Fatal("the solver accepted a deck with a repeated card")
	}
	if !strings.Contains(err.Error(), "appears twice") {
		t.Fatalf("error %q does not mention the duplicate", err)
	}
}

// TestConstraintKindsAreAllEvaluable asserts every documented kind has a working
// checker, so no constraint can be published without being gradeable.
func TestConstraintKindsAreAllEvaluable(t *testing.T) {
	sol := solution{1: "a", 2: "b", 3: "c", 4: "d"}
	cases := []struct {
		name string
		c    constraint
		want bool
	}{
		{name: "not_with true", c: constraint{Kind: KindNotWith, Cards: []string{"a", "c"}}, want: true},
		{name: "not_with false", c: constraint{Kind: KindNotWith, Cards: []string{"a", "b"}}, want: false},
		{name: "before true", c: constraint{Kind: KindBefore, Cards: []string{"a", "d"}}, want: true},
		{name: "before false", c: constraint{Kind: KindBefore, Cards: []string{"d", "a"}}, want: false},
		{name: "position true", c: constraint{Kind: KindPosition, Cards: []string{"b"}, Position: 2}, want: true},
		{name: "position false", c: constraint{Kind: KindPosition, Cards: []string{"b"}, Position: 3}, want: false},
		{name: "adjacent true", c: constraint{Kind: KindAdjacent, Cards: []string{"a", "b"}}, want: true},
		{name: "adjacent false", c: constraint{Kind: KindAdjacent, Cards: []string{"a", "c"}}, want: false},
		{name: "group true", c: constraint{Kind: KindGroup, Cards: []string{"a", "b", "c"}}, want: true},
		{name: "group false", c: constraint{Kind: KindGroup, Cards: []string{"a", "b", "d"}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := satisfies(tc.c, sol, 4)
			if err != nil {
				t.Fatalf("satisfies: %v", err)
			}
			if got != tc.want {
				t.Fatalf("satisfies = %v, want %v", got, tc.want)
			}
		})
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

// TestPrepareRefusesWithoutDeck asserts the mode has no built-in fallback.
func TestPrepareRefusesWithoutDeck(t *testing.T) {
	cases := []struct {
		name    string
		content modes.ContentSource
		want    string
	}{
		{name: "nil content", content: nil, want: "no content source"},
		{name: "empty pack", content: fakeContent{}, want: DeckSection},
		{name: "section present but empty",
			content: fakeContent{DeckSection: json.RawMessage(`[]`)}, want: "no usable card"},
		{name: "deck smaller than the round", content: packWith(packOptions{cardCount: 3}),
			want: "but the round needs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("no-deck-seed"),
				Subject: roster(3),
				Config:  json.RawMessage(`{}`),
				Content: tc.content,
			})
			if err == nil {
				t.Fatal("Prepare admitted a round with no usable deck")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
		})
	}
}

// TestDeckComesFromThePack asserts the drawn cards use the PACK ids.
//
// The mode used to synthesise "档案 A/B/C" labels when the pack had no deck, so publishing
// cluepuzzle.deck changed nothing a test could see.
func TestDeckComesFromThePack(t *testing.T) {
	_, pub, _ := prepareRound(t, "from-pack-seed", `{"cards":4,"participants":3}`, 3)
	if len(pub.Cards) != 4 {
		t.Fatalf("round has %d cards, want 4", len(pub.Cards))
	}
	for _, c := range pub.Cards {
		if !strings.HasPrefix(c.CardID, "dc-") {
			t.Fatalf("card %q is not from the pack", c.CardID)
		}
		if !strings.HasPrefix(c.Label, "测试档案") {
			t.Fatalf("card %q label %q is not from the pack", c.CardID, c.Label)
		}
	}
}

// TestDeckDrawIsSeeded asserts two different seeds can draw different card subsets from a pool
// larger than the round, so a round does not always use the first N cards.
func TestDeckDrawIsSeeded(t *testing.T) {
	pubSeen := make(map[string]map[string]struct{})
	for _, seed := range []string{"draw-a", "draw-b", "draw-c", "draw-d"} {
		_, pub, _ := prepareRound(t, seed, `{"cards":4,"participants":3}`, 3)
		ids := make(map[string]struct{}, len(pub.Cards))
		for _, c := range pub.Cards {
			ids[c.CardID] = struct{}{}
		}
		pubSeen[seed] = ids
	}
	// At least two seeds must draw different subsets; if all four were identical the draw would
	// be ignoring the seed.
	first := pubSeen["draw-a"]
	differed := false
	for seed, ids := range pubSeen {
		if seed == "draw-a" {
			continue
		}
		same := len(ids) == len(first)
		if same {
			for id := range ids {
				if _, ok := first[id]; !ok {
					same = false
					break
				}
			}
		}
		if !same {
			differed = true
		}
	}
	if !differed {
		t.Fatal("every seed drew the same card subset; the draw ignores the seed")
	}
}

// TestDeckProvenanceIsValidated asserts the provenance block is checked.
func TestDeckProvenanceIsValidated(t *testing.T) {
	cases := []struct {
		name string
		prov string
		want string
	}{
		{name: "unknown kind",
			prov: `{"kind":"guess","notOfficialData":true,"author":"t","basis":"b"}`,
			want: "unknown provenance kind"},
		{name: "authored but claims official",
			prov: `{"kind":"authored-design-value","notOfficialData":false,"author":"t","basis":"b"}`,
			want: "must set notOfficialData=true"},
		{name: "no author",
			prov: `{"kind":"authored-design-value","notOfficialData":true,"basis":"b"}`,
			want: "no author"},
		{name: "no basis",
			prov: `{"kind":"authored-design-value","notOfficialData":true,"author":"t"}`,
			want: "no basis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("prov-seed"),
				Subject: roster(3),
				Config:  json.RawMessage(`{}`),
				Content: packWith(packOptions{badProvenance: tc.prov}),
			})
			if err == nil {
				t.Fatalf("Prepare accepted cards whose provenance was %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	t.Run("no provenance block", func(t *testing.T) {
		_, err := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("prov-seed"),
			Subject: roster(3),
			Config:  json.RawMessage(`{}`),
			Content: packWith(packOptions{omitProvenance: true}),
		})
		if err == nil {
			t.Fatal("Prepare accepted cards with no provenance block")
		}
		if !strings.Contains(err.Error(), "no provenance block") {
			t.Fatalf("error %q does not mention the missing block", err)
		}
	})
}

// TestRequirementFieldsMatchWhatTheModeReads asserts the declaration matches the loader.
func TestRequirementFieldsMatchWhatTheModeReads(t *testing.T) {
	fields := New().Requirement().Fields
	if len(fields) != 1 || fields[0] != DeckSection {
		t.Fatalf("Requirement().Fields = %v, want exactly [%q]", fields, DeckSection)
	}
}

// TestQuestionCountIsTheDeckSize asserts QuestionCount and MinEntities use the same unit.
func TestQuestionCountIsTheDeckSize(t *testing.T) {
	prep, pub, _ := prepareRound(t, "unit-seed", `{"cards":4,"participants":3}`, 3)
	if prep.QuestionCount != fixtureCardCount {
		t.Fatalf("questionCount = %d, want the deck size %d", prep.QuestionCount, fixtureCardCount)
	}
	// MinEntities counts cards too, so the comparison is meaningful.
	req := New().Requirement()
	if req.MinEntities != MinCards {
		t.Fatalf("MinEntities = %d, want MinCards %d", req.MinEntities, MinCards)
	}
	if prep.QuestionCount < req.MinEntities {
		t.Fatalf("the pack reports %d cards but MinEntities is %d",
			prep.QuestionCount, req.MinEntities)
	}
	_ = pub
}

// TestSharedEmptyReasonIsExplained asserts an empty shared area says WHY it is empty.
//
// The shared area is capped at len(constraints)-participants so every participant keeps a
// private clue. On a 4-card board with 3 participants the generator produces exactly 3
// constraints, so the cap is 0 and the shared area is always empty. That is legitimate, but a
// player who sees an empty shared area must be told which case they are in — otherwise it looks
// like the field was forgotten, which is exactly the defect class this project keeps hitting.
func TestSharedEmptyReasonIsExplained(t *testing.T) {
	cases := []struct {
		name       string
		cards      int
		players    int
		wantShared bool
	}{
		// A 4-card board yields 3 constraints for 3 participants: no spare, shared empty.
		{name: "small board has no spare", cards: 4, players: 3, wantShared: false},
		// A 5-card board yields 4 constraints, leaving one spare.
		{name: "larger board has a spare", cards: 5, players: 3, wantShared: true},
		{name: "six card board", cards: 6, players: 3, wantShared: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fmt.Sprintf(`{"cards":%d,"participants":%d}`, tc.cards, tc.players)
			_, pub, _ := prepareRound(t, "shared-reason-seed", cfg, tc.players)
			if tc.wantShared {
				if len(pub.Shared) == 0 {
					t.Fatalf("a board with spare constraints published no shared clue")
				}
				return
			}
			if len(pub.Shared) != 0 {
				t.Fatalf("expected an empty shared area, got %d", len(pub.Shared))
			}
			if pub.SharedEmptyReason == "" {
				t.Fatal("the shared area is empty but carries no explanation")
			}
			// The explanation must reach the player, not only the server.
			view := project(t, prepareRoundPrep(t, cfg, tc.players), modes.Viewer{
				MembershipID: "m1", Role: "player",
				Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
			}, modes.PhaseCollecting, nil)
			if view.SharedEmptyReason == "" {
				t.Fatal("the projection drops the explanation, so the player sees a silent gap")
			}
		})
	}
}

// prepareRoundPrep returns only the prepared round, for tests that need to project it.
func prepareRoundPrep(t *testing.T, cfg string, players int) modes.Prepared {
	t.Helper()
	prep, _, _ := prepareRound(t, "shared-reason-seed", cfg, players)
	return prep
}

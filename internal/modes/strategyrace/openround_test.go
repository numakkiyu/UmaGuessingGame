package strategyrace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// deliveredPack loads the delivered rules.track file as a content source.
func deliveredPack(t *testing.T) modes.ContentSource {
	t.Helper()
	path := filepath.Join("..", "..", "..", "data", "manual", "rules-track.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("delivered content not present at %s: %v", path, err)
	}
	return fakeContent{TrackSection: json.RawMessage(raw)}
}

// TestOpenRoundWithDeliveredBaseline drives a full round with the delivered baseline.
func TestOpenRoundWithDeliveredBaseline(t *testing.T) {
	pack := deliveredPack(t)
	subjects := []modes.Subject{
		{ID: "t1", Kind: modes.SubjectPlayer},
		{ID: "t2", Kind: modes.SubjectPlayer},
		{ID: "t3", Kind: modes.SubjectPlayer},
	}
	scope := modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "delivered-baseline",
		ContentPackVersion: "delivered-v1",
		Seed:               "open-round-seed",
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare refused the delivered baseline: %v", err)
	}

	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}

	// 1. QuestionCount is the turn count from the loaded baseline.
	if prep.QuestionCount != pub.Track.TotalTurns {
		t.Fatalf("questionCount = %d, baseline has %d turns",
			prep.QuestionCount, pub.Track.TotalTurns)
	}
	if prep.QuestionCount != 8 {
		t.Fatalf("questionCount = %d, want 8", prep.QuestionCount)
	}
	// 2. The baseline is published and marked as a simulation.
	if !pub.Provenance.Simulation {
		t.Fatal("the delivered baseline is not marked as an entertainment simulation")
	}
	if !pub.Provenance.NotOfficialData {
		t.Fatal("the delivered baseline claims to be official data")
	}
	// 3. All four actions are published with their numbers.
	if len(pub.Track.Actions) != 4 {
		t.Fatalf("published %d actions, want 4", len(pub.Track.Actions))
	}
	byAction := make(map[Action]actionRule, 4)
	for _, r := range pub.Track.Actions {
		byAction[r.Action] = r
	}
	for _, want := range []struct {
		action Action
		gain   int
	}{
		{ActionSteady, 3}, {ActionSprint, 5}, {ActionRecover, 1}, {ActionFollow, 4},
	} {
		rule, ok := byAction[want.action]
		if !ok {
			t.Fatalf("the baseline omits action %q", want.action)
		}
		if rule.PositionGain != want.gain {
			t.Fatalf("action %q gain = %d, want %d", want.action, rule.PositionGain, want.gain)
		}
	}

	// 4. Every team can act, and the race resolves.
	// The action plan must be LEGAL, not merely varied: sprinting every turn drains stamina
	// to zero and the mode correctly refuses the next sprint. Each team alternates so its
	// stamina stays inside the published budget.
	plan := map[string][]Action{
		"t1": {ActionSprint, ActionRecover, ActionSprint, ActionRecover,
			ActionSprint, ActionRecover, ActionSprint, ActionSteady},
		"t2": {ActionSteady, ActionSteady, ActionFollow, ActionFollow,
			ActionSteady, ActionSteady, ActionFollow, ActionFollow},
		"t3": {ActionFollow, ActionRecover, ActionSteady, ActionSprint,
			ActionRecover, ActionFollow, ActionSteady, ActionSteady},
	}
	for turn := 1; turn <= pub.Track.TotalTurns; turn++ {
		for _, subject := range subjects {
			seq := plan[subject.ID]
			if turn-1 >= len(seq) {
				t.Fatalf("%s has no planned action for turn %d", subject.ID, turn)
			}
			prep = commitAndPrepare(t, prep, subject, decisionAction(turn, seq[turn-1]))
		}
	}

	// 5. Projection carries standings and the baseline.
	view := project(t, prep, modes.Viewer{Subject: subjects[0], Role: "player"},
		modes.PhaseCollecting)
	if len(view.Standings) != len(subjects) {
		t.Fatalf("standings list %d teams, want %d", len(view.Standings), len(subjects))
	}
	if !view.Provenance.Simulation {
		t.Fatal("the projection does not carry the simulation marker")
	}

	// 6. Settle produces a valid score per team, ranked.
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope,
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != len(subjects) {
		t.Fatalf("settled %d scores, want %d", len(scores), len(subjects))
	}
	for _, s := range scores {
		if err := s.Validate(); err != nil {
			t.Fatalf("invalid score: %v", err)
		}
	}
	t.Logf("opened a %d-team, %d-turn race from the delivered baseline; top score %d units",
		len(subjects), prep.QuestionCount, scores[0].ScoreUnits)
}

// TestOpenRoundPracticeForm asserts the single-team practice form works from the delivered
// baseline and produces no competitive score.
func TestOpenRoundPracticeForm(t *testing.T) {
	pack := deliveredPack(t)
	scope := modes.Scope{
		ModeID: ModeID, RulesVersion: RulesVersion,
		DatasetVersion: "delivered", ContentPackVersion: "delivered-v1", Seed: "practice-seed",
	}
	subjects := []modes.Subject{{ID: "t1", Kind: modes.SubjectPlayer}}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{"practice":true}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare refused the practice form: %v", err)
	}
	prep = expireRemaining(t, prep)
	scores, err := New().Settle(modes.SettleInput{
		Scope: scope, Phase: modes.PhaseSettled,
		Public: prep.Public, Private: prep.Private,
		States: map[string]json.RawMessage{},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != 1 {
		t.Fatalf("settled %d scores, want 1", len(scores))
	}
	if scores[0].ScoreUnits != 0 {
		t.Fatalf("the practice form scored %d units, want 0", scores[0].ScoreUnits)
	}
	t.Logf("practice form settled with 0 competitive units, as documented")
}

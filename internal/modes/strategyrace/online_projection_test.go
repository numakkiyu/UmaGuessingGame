package strategyrace

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

func TestOnlineProjectionNeverInventsTurnsOrCancelledResults(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "projection", `{}`, subjects)
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseRevealed, modes.PhaseSettled, modes.PhaseCancelled} {
		view := project(t, prep, modes.Viewer{}, phase)
		if len(view.Final) != 0 || len(view.History) != 0 || view.ResolvedTurns != 0 || view.Standings[0].Position != 0 {
			t.Fatalf("%s invented results: %+v", phase, view)
		}
	}
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(1, ActionRecover))
	view := project(t, prep, modes.Viewer{}, modes.PhaseCancelled)
	if len(view.History) != 1 || len(view.Final) != 0 || view.Standings[0].Position != 5 || view.Standings[1].Position != 1 {
		t.Fatalf("cancellation lost completed history or added turns: %+v", view)
	}
	for turn := 2; turn <= TotalTurns; turn++ {
		var err error
		prep.Public, _, err = New().ExpireTurn(prep.Public)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, phase := range []modes.Phase{modes.PhaseLocked, modes.PhaseCancelled, modes.PhaseCollecting} {
		if v := project(t, prep, modes.Viewer{}, phase); len(v.Final) != 0 {
			t.Fatalf("%s exposed final", phase)
		}
	}
	for _, phase := range []modes.Phase{modes.PhaseRevealed, modes.PhaseSettled} {
		v := project(t, prep, modes.Viewer{}, phase)
		if len(v.Final) != 2 || len(v.History) != 8 || !v.History[7].Applied[0].Defaulted || v.History[0].Applied[0].Defaulted {
			t.Fatalf("final/history default status: %+v", v)
		}
	}
}

func TestOnlineHistoryHidesOpenChoicesAndUsesPublishedDefault(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRace(t, "history", `{}`, subjects)
	pub.Track.DefaultAction = ActionRecover
	prep.Public = mustJSON(t, pub)
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))
	view := project(t, prep, modes.Viewer{Subject: subjects[1]}, modes.PhaseCollecting)
	if len(view.History) != 0 || len(view.Self.Decisions) != 0 {
		t.Fatal("open rival choice exposed")
	}
	var err error
	prep.Public, _, err = New().ExpireTurn(prep.Public)
	if err != nil {
		t.Fatal(err)
	}
	view = project(t, prep, modes.Viewer{Subject: subjects[1]}, modes.PhaseCollecting)
	h := view.History[0]
	if h.Applied[0].Defaulted || h.Applied[1].Action != ActionRecover || !h.Applied[1].Defaulted || h.Applied[1].Stamina != 8 || !view.Self.Decisions[0].Defaulted {
		t.Fatalf("timeout marker/default lost: %+v", view)
	}
	// Round-trip persistence cannot turn a timeout into a human choice.
	raw := mustJSON(t, view)
	var restored projection
	if err := json.Unmarshal(raw, &restored); err != nil || !restored.History[0].Applied[1].Defaulted {
		t.Fatal("lost persisted marker", err)
	}
}

func TestOnlinePublishedClockAndSprintGuard(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{Scope: scope("clock"), Subject: roster(2), Config: json.RawMessage(`{"decisionSeconds":30}`), Content: packOf()})
	if err == nil {
		t.Fatal("accepted a clock that differs from published track")
	}
	prep, pub := prepareRace(t, "stamina", `{}`, roster(2))
	pub.Track.StartStamina = 3
	for i := range pub.Track.Actions {
		if pub.Track.Actions[i].Action == ActionSprint {
			pub.Track.Actions[i].MinStamina = 4
		}
	}
	prep.Public = mustJSON(t, pub)
	dec := apply(t, prep, roster(2)[0], decisionAction(1, ActionSprint))
	if dec.Accepted || dec.Code != CodeInsufficientStamina {
		t.Fatalf("published sprint guard: %+v", dec)
	}
	for _, a := range []Action{ActionSprint, ActionSteady} {
		tr := fixtureTrack(t)
		tr.DefaultAction = a
		if a == ActionSteady {
			tr.Actions[0].StaminaDelta = -1
		}
		if tr.validate() == nil {
			t.Fatalf("unsafe default %s accepted", a)
		}
	}
}

func TestOnlineSettleRefusesUnfinishedRace(t *testing.T) {
	prep, _ := prepareRace(t, "no-shortcut", `{}`, roster(2))
	scores, err := New().Settle(modes.SettleInput{Public: prep.Public, Private: prep.Private, Phase: modes.PhaseSettled})
	if err == nil || len(scores) != 0 {
		t.Fatal("unfinished race was scored", scores, err)
	}
}

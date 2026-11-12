package trade

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

func finishTrading(t *testing.T, prep modes.Prepared) modes.Prepared {
	t.Helper()
	for i := 0; i < 7; i++ {
		more, err := Unfinished(prep.Public)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			return prep
		}
		prep.Public, _, err = New().ExpireTurn(prep.Public)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("negotiation did not finish")
	return prep
}

func TestTradeCancelledOrEarlyLockedNeverRevealOrSettle(t *testing.T) {
	prep, _, _ := prepareRound(t, "safety", `{}`, 3)
	for _, p := range []modes.Prepared{prep, finishTrading(t, prep)} {
		for _, phase := range []modes.Phase{modes.PhaseCancelled, modes.PhaseLocked} {
			raw, err := New().Project(modes.ProjectInput{Public: p.Public, Private: p.Private, Phase: phase, RevealNow: true})
			if err != nil {
				t.Fatal(err)
			}
			var v projection
			if err = json.Unmarshal(raw, &v); err != nil {
				t.Fatal(err)
			}
			if v.Result != nil {
				t.Fatalf("revealed at %s", phase)
			}
		}
	}
	for _, in := range []modes.SettleInput{{Public: prep.Public, Private: prep.Private, Phase: modes.PhaseSettled}, {Public: finishTrading(t, prep).Public, Private: prep.Private, Phase: modes.PhaseCancelled}} {
		if _, err := New().Settle(in); err == nil {
			t.Fatal("settled cancelled or unfinished round")
		}
	}
}

func TestTradeObjectiveAllocationDoesNotReuseInstance(t *testing.T) {
	pub := publicState{Instances: []instance{{InstanceID: "s", CardID: "speed"}, {InstanceID: "s2", CardID: "speed"}, {InstanceID: "t", CardID: "stamina"}, {InstanceID: "w", CardID: "wit"}}}
	objectives := []objective{{ObjectiveID: "a", Kinds: []string{"speed", "stamina"}, Points: 50}, {ObjectiveID: "b", Kinds: []string{"speed", "wit"}, Points: 50}}
	for _, test := range []struct {
		hand []string
		want int
	}{{[]string{"s", "t", "w"}, 1}, {[]string{"s", "s", "t", "w"}, 1}, {[]string{"s", "s2", "t", "w"}, 2}} {
		got := satisfiedObjectives(pub, test.hand, objectives)
		if len(got) != test.want {
			t.Fatalf("hand %v: %v", test.hand, got)
		}
	}
	if combinationFeasible(objectives, map[string]int{"speed": 1, "stamina": 1, "wit": 1}) {
		t.Fatal("unsatisfiable pair accepted")
	}
	if !combinationFeasible(objectives, map[string]int{"speed": 2, "stamina": 1, "wit": 1}) {
		t.Fatal("feasible pair refused")
	}
	// A tie is independent of the source array order.
	if !satisfiedObjectives(pub, []string{"s", "t", "w"}, []objective{objectives[1], objectives[0]})["a"] {
		t.Fatal("unstable tie")
	}
}

func TestTradeAllSeatCountsReceiveFeasibleDistinctCombinations(t *testing.T) {
	for seats := MinSubjects; seats <= MaxSubjects; seats++ {
		_, pub, priv := prepareRound(t, "feasible-seats", `{}`, seats)
		counts := map[string]int{}
		for _, inst := range pub.Instances {
			counts[inst.CardID]++
		}
		seen := map[string]bool{}
		for _, own := range priv.Objectives {
			if !combinationFeasible(own, counts) {
				t.Fatalf("%d seats infeasible", seats)
			}
			for _, obj := range own {
				if seen[obj.ObjectiveID] {
					t.Fatal("reused objective")
				}
				seen[obj.ObjectiveID] = true
			}
		}
	}
}

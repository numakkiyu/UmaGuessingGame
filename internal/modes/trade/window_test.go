package trade

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"testing"
	"time"
)

func TestTradingRoundsExpirePendingAndFinishAtConfiguredCount(t *testing.T) {
	prep, _, _ := prepareRound(t, "three-windows", `{"rounds":3,"roundSeconds":60}`, 3)
	action := proposeAction("p2", firstHand(t, prep.Public, "p1"), firstHand(t, prep.Public, "p2"))
	prep, _ = commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, action, prep.SubjectPrivate["p1"])
	if window := New().TurnWindow(json.RawMessage(`{"roundSeconds":60}`)); window != time.Minute {
		t.Fatalf("window=%s", window)
	}
	raw := prep.Public
	for i := 1; i <= 3; i++ {
		next, more, err := New().ExpireTurn(raw)
		if err != nil {
			t.Fatal(err)
		}
		var board publicState
		if err = json.Unmarshal(next, &board); err != nil {
			t.Fatal(err)
		}
		if board.CurrentRound != i+1 || more != (i < 3) {
			t.Fatalf("round=%d more=%v", board.CurrentRound, more)
		}
		if board.Trades[0].Status != StatusExpired {
			t.Fatal("pending proposal survived round boundary")
		}
		raw = next
	}
	again, more, err := New().ExpireTurn(raw)
	if err != nil || more || string(again) != string(raw) {
		t.Fatal("final expiration was not stable")
	}
	projection, err := New().Project(modes.ProjectInput{Public: raw, Private: prep.Private, Phase: modes.PhaseLocked})
	if err != nil {
		t.Fatal(err)
	}
	var shown struct {
		CurrentRound int `json:"currentRound"`
	}
	_ = json.Unmarshal(projection, &shown)
	if shown.CurrentRound != 3 {
		t.Fatal("finished game displayed a fourth trading round")
	}
}
func firstHand(t *testing.T, raw json.RawMessage, subject string) string {
	t.Helper()
	var board publicState
	if err := json.Unmarshal(raw, &board); err != nil {
		t.Fatal(err)
	}
	return board.Hands[subject][0]
}

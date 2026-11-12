package pedigree

import (
	"github.com/bhcn/umaguessinggame/internal/modes"
	"testing"
)

func TestCancelledRoundDoesNotPublishPedigreeSolution(t *testing.T) {
	prep, _, _ := prepareRound(t, packOf(12), "cancelled-boundary", `{}`)
	player := modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Role: "player"}
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseCancelled} {
		if got := project(t, prep, player, phase, nil); got.Answer != nil {
			t.Fatalf("solution published in %s", phase)
		}
	}
	for _, phase := range []modes.Phase{modes.PhaseRevealed, modes.PhaseSettled} {
		if got := project(t, prep, player, phase, nil); got.Answer == nil {
			t.Fatalf("solution absent in %s", phase)
		}
	}
}

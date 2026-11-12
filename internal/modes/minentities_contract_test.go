package modes_test

// MinEntities measures the content-pool floor, independently of participant counts.

import (
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/modes/audio"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/modes/cluepuzzle"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
	"github.com/bhcn/umaguessinggame/internal/modes/draft"
	"github.com/bhcn/umaguessinggame/internal/modes/memory"
	"github.com/bhcn/umaguessinggame/internal/modes/pedigree"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
	"github.com/bhcn/umaguessinggame/internal/modes/rapport"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
)

// TestMinEntitiesIsAPoolFloor asserts the requirement is usable at all.
//
// A zero or negative floor would make the comparison meaningless (everything passes), and
// an absurdly large one would silently disable a mode whose content is fine. The upper
// bound is deliberately loose: it only catches a value that is obviously not a pool size.
func TestMinEntitiesIsAPoolFloor(t *testing.T) {
	all := []modes.Mode{
		classic.New(), aptitudegrid.New(), draft.New(), racerebuild.New(),
		coracepath.New(), sortgame.New(), memory.New(), bluff.New(),
		cluepuzzle.New(), strategyrace.New(), pedigree.New(), audio.New(),
		trade.New(), creative.New(), rapport.New(),
	}
	if len(all) == 0 {
		t.Fatal("no modes registered; the test would pass vacuously")
	}
	for _, m := range all {
		req := m.Requirement()
		if req.MinEntities < 1 {
			t.Errorf("%s: MinEntities=%d; a content-pool floor must be at least 1", m.ID(), req.MinEntities)
		}
		// A pool floor larger than a few hundred cannot be a real minimum: it would mean
		// the mode needs hundreds of compiled items before it may open, which no mode in
		// this project requires.
		if req.MinEntities > 500 {
			t.Errorf("%s: MinEntities=%d looks like a total rather than a floor", m.ID(), req.MinEntities)
		}
	}
}

// TestEveryModeDeclaresContentFields guards the other half of the requirement.
//
// A mode that declares no fields is claiming it needs nothing from the pack, which would
// make its admission check vacuous. The scraped modes all declare at least one.
func TestEveryModeDeclaresContentFields(t *testing.T) {
	all := []modes.Mode{
		classic.New(), aptitudegrid.New(), draft.New(), racerebuild.New(),
		coracepath.New(), sortgame.New(), memory.New(), bluff.New(),
		cluepuzzle.New(), strategyrace.New(), pedigree.New(), audio.New(),
		trade.New(), creative.New(), rapport.New(),
	}
	for _, m := range all {
		req := m.Requirement()
		if len(req.Fields) == 0 {
			t.Errorf("%s: declares no content fields, so its admission check cannot fail", m.ID())
		}
	}
}

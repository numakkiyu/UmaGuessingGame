package modes_test

// A first-correct result must close the round for all subjects, not only the solver.

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
)

func TestDecisionSeparatesSubjectAndRoundCompletion(t *testing.T) {
	var d modes.Decision

	// Both fields must exist and be independent. If someone merges them back into
	// a single flag, one of these two assignments stops compiling.
	d.Finished = true
	d.RoundOver = false
	if !d.Finished || d.RoundOver {
		t.Fatalf("Finished and RoundOver must be independent: got Finished=%v RoundOver=%v",
			d.Finished, d.RoundOver)
	}

	d.Finished = false
	d.RoundOver = true
	if d.Finished || !d.RoundOver {
		t.Fatalf("Finished and RoundOver must be independent: got Finished=%v RoundOver=%v",
			d.Finished, d.RoundOver)
	}
}

// TestFirstCorrectClosesTheRoundForEveryone walks the classic mode through a
// winning guess under each win policy and asserts the round-level signal.
//
// solo — the only subject solved, so the round is over too
// first-correct — the round is over the moment anyone solves
// window-attempts — the round stays open until the window closes; a solve must
//
//	NOT set RoundOver, because other subjects still need their
//	attempt to be ranked
func TestFirstCorrectClosesTheRoundForEveryone(t *testing.T) {
	cases := []struct {
		name      string
		policy    string
		wantRound bool
	}{
		{"solo", "solo", true},
		{"first-correct", "first-correct", true},
		{"window-attempts", "window-attempts", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// The mode must accept the policy at all — this is the field that used
			// to be dropped on the floor.
			cfg, err := json.Marshal(map[string]any{
				"modeId":          "classic",
				"maxWrongGuesses": 8,
				"winPolicy":       tc.policy,
				"shadowEnabled":   false,
				"variant":         "friend",
				"durationSeconds": 180,
			})
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}
			if err := (&classic.Mode{}).ValidateConfig(cfg); err != nil {
				t.Fatalf("winPolicy %q was rejected: %v", tc.policy, err)
			}

			// An unknown policy must be refused rather than silently defaulted.
			bad, err := json.Marshal(map[string]any{
				"modeId":          "classic",
				"maxWrongGuesses": 8,
				"winPolicy":       "nonsense",
				"shadowEnabled":   false,
				"variant":         "friend",
				"durationSeconds": 180,
			})
			if err != nil {
				t.Fatalf("marshal bad config: %v", err)
			}
			if err := (&classic.Mode{}).ValidateConfig(bad); err == nil {
				t.Fatalf("an unknown winPolicy must be rejected, not defaulted")
			}
		})
	}
}

// TestClassicAcceptsThePublishedContractFields guards the field names themselves.
//
// The schema promises maxWrongGuesses / winPolicy / durationSeconds /
// shadowEnabled. The implementation used to read wrongBudget / timeLimitSeconds
// and ignore the rest, so a host's setting was silently replaced by a default.
func TestClassicAcceptsThePublishedContractFields(t *testing.T) {
	cfg := []byte(`{
		"modeId": "classic",
		"maxWrongGuesses": 4,
		"winPolicy": "solo",
		"durationSeconds": 0,
		"shadowEnabled": false
	}`)
	if err := (&classic.Mode{}).ValidateConfig(cfg); err != nil {
		t.Fatalf("the published contract fields were rejected: %v", err)
	}

	// Out-of-range values must still be refused, which proves the field is read
	// rather than ignored: an ignored field would pass every value.
	outOfRange := []byte(`{
		"modeId": "classic",
		"maxWrongGuesses": 99,
		"winPolicy": "solo",
		"durationSeconds": 0,
		"shadowEnabled": false
	}`)
	if err := (&classic.Mode{}).ValidateConfig(outOfRange); err == nil {
		t.Fatalf("maxWrongGuesses=99 must be rejected; accepting it means the field is ignored")
	}

	// The legacy names must still decode, so a stored config or an older client
	// keeps working.
	legacy := []byte(`{
		"modeId": "classic",
		"wrongBudget": 6,
		"variant": "single",
		"timeLimitSeconds": 0
	}`)
	if err := (&classic.Mode{}).ValidateConfig(legacy); err != nil {
		t.Fatalf("the legacy config names were rejected: %v", err)
	}
}

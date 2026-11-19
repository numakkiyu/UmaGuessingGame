package trade

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// TestProjectionCarriesSelfObjectivesToPublicState proves the mode's Project output really
// contains the per-player objective assignment.
//
// The room layer's privateProjection is a separate concern (it currently returns nil), so the
// question this test answers is narrow and important: does the MODE put the assignment in its
// projection? If yes, the fix belongs in the room layer; if no, it belongs here.
func TestProjectionCarriesSelfObjectivesToPublicState(t *testing.T) {
	prep, pub, _ := prepareRound(t, "proj-seed", `{}`, 3)
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)

	if len(view.SelfObjectives) != ObjectiveCount {
		t.Fatalf("the mode's projection carries %d self objectives, want %d",
			len(view.SelfObjectives), ObjectiveCount)
	}
	for _, obj := range view.SelfObjectives {
		if obj.ObjectiveID == "" || obj.Text == "" {
			t.Fatalf("self objective is incomplete: %+v", obj)
		}
	}
	if view.Self == nil || len(view.Self.Hand) != HandSize {
		t.Fatalf("the mode's projection carries no usable self view: %+v", view.Self)
	}
	// The raw JSON must contain them, because that is what the room layer stores.
	raw, err := New().Project(modes.ProjectInput{
		Scope: scope("proj-seed"),
		Viewer: modes.Viewer{MembershipID: "m1", Role: "player",
			Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}},
		Public: prep.Public, Private: prep.Private,
		States: map[string]json.RawMessage{}, Phase: modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := generic["selfObjectives"]; !ok {
		t.Fatalf("the projection JSON has no selfObjectives key; keys are %v", keysOfRaw(generic))
	}
	t.Logf("mode projection carries selfObjectives for p1: %d entries", len(view.SelfObjectives))
	_ = pub
}

func keysOfRaw(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

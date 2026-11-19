package coracepath

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

func TestOwnConfirmedRouteEvidenceOnlyAppearsAtReveal(t *testing.T) {
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, pub, priv := prepare(t, fixedPairPack(), "evidence", "{}", subject, modes.Subject{ID: "p2", Kind: modes.SubjectPlayer})
	pub.Start = Node{CharacterID: "a", Name: "甲"}
	pub.Goal = Node{CharacterID: "d", Name: "丁"}
	pub.ShortestEdges = 2
	priv.ShortestPath = []string{"a", "b", "d"}
	prep.Public = modeutil.Marshal(pub)
	prep.Private = modeutil.Marshal(priv)
	accepted, _ := applyOne(t, prep, subject, nil, pathAction([]string{"a", "c", "b", "d"}))
	if !accepted.Accepted {
		t.Fatal(accepted.Detail)
	}
	for _, reveal := range []bool{false, true} {
		phase := modes.PhaseCollecting
		if reveal {
			phase = modes.PhaseRevealed
		}
		raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: map[string]json.RawMessage{"p1": accepted.State}, Viewer: modes.Viewer{Role: "player", Subject: subject}, Phase: phase, RevealNow: reveal})
		if err != nil {
			t.Fatal(err)
		}
		var out projection
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if out.Self == nil {
			t.Fatal("own route missing")
		}
		if !reveal {
			if len(out.Self.Evidence) != 0 || out.Answer != nil {
				t.Fatal("closed route evidence leaked before reveal")
			}
			continue
		}
		if len(out.Self.Evidence) != 3 || len(out.Answer.Evidence) != 2 {
			t.Fatal("submitted route and reference evidence conflated")
		}
		if out.Self.Evidence[0].RaceID != "r3" || out.Self.Evidence[1].RaceID != "r4" || out.Self.Evidence[2].RaceID != "r2" {
			t.Fatal("wrong evidence for actual detour", out.Self.Evidence)
		}
		if out.Self.ScoreUnits != 9000 {
			t.Fatal("detour score changed")
		}
	}
	// A later accepted revision supplies its own evidence; no old draft is used.
	next, _ := applyOne(t, prep, subject, accepted.State, pathAction([]string{"a", "b"}))
	var st state
	_ = json.Unmarshal(next.State, &st)
	if st.Revisions != 2 || st.Arrived || !reflect.DeepEqual(st.Path, []string{"a", "b"}) {
		t.Fatal("incomplete revision did not replace confirmed route")
	}
}

func TestMissingEdgeProvidesOnlyAttemptedStep(t *testing.T) {
	pub := publicPrepared{Config: Config{MaxEdges: 6}, Start: Node{CharacterID: "a"}, Goal: Node{CharacterID: "d"}, Nodes: []Node{{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"}, {CharacterID: "d"}}}
	prep := modes.Prepared{Public: modeutil.Marshal(pub), Private: modeutil.Marshal(privatePrepared{ShortestPath: []string{"a", "b", "d"}, Edges: []Edge{{From: "a", To: "b", RaceID: "r1"}, {From: "b", To: "d", RaceID: "r2"}}})}
	decision, _ := applyOne(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, nil, pathAction([]string{"a", "b", "c", "d"}))
	if decision.Accepted || decision.Code != CodeNoEdge || len(decision.State) != 0 || len(decision.Feedback) != 0 || len(decision.Issues) != 1 || decision.Issues[0].Field != "modeAction.characterIds[2]" || decision.Issues[0].Code != CodeNoEdge {
		t.Fatal("missing-edge rejection does not identify attempted second step", decision)
	}
}

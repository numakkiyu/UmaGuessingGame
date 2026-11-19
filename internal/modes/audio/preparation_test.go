package audio

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"testing"
)

func TestPreparationAndCancellationBoundary(t *testing.T) {
	prep, pub, _ := prepareRound(t, verifiedPack(4), "prepare-boundary", `{}`, 2)
	states := map[string]json.RawMessage{}
	apply := func(subject string, ready, failed []string) modes.Decision {
		body, _ := json.Marshal(map[string]any{"kind": ActionKindReady, "readyClipIds": ready, "playbackFailures": failed})
		d, err := New().Apply(modes.ApplyInput{Public: prep.Public, Private: prep.Private, State: states[subject], Subject: modes.Subject{ID: subject, Kind: modes.SubjectPlayer}, Action: modes.ModeAction{Kind: ActionKindReady, Payload: body}})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	ids := []string{}
	for _, c := range pub.Clips {
		ids = append(ids, c.ClipID)
	}
	assertReady := func(want bool) {
		t.Helper()
		got, err := AllPrepared(prep.Public, states)
		if err != nil || got != want {
			t.Fatalf("ready=%v err=%v want=%v", got, err, want)
		}
	}
	assertReady(false)
	if apply("outsider", ids, nil).Accepted {
		t.Fatal("outsider accepted")
	}
	if apply("p1", ids[:1], nil).Accepted {
		t.Fatal("partial preparation accepted")
	}
	if apply("p1", ids, ids[:1]).Accepted {
		t.Fatal("overlap accepted")
	}
	for _, id := range []string{"p1", "p2"} {
		d := apply(id, ids, []string{})
		if !d.Accepted {
			t.Fatal(d)
		}
		states[id] = d.State
		assertReady(id == "p2")
	}
	player := modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Role: "player"}
	got := project(t, prep, player, modes.PhaseBriefing, states)
	if got.Self == nil || !got.Self.Ready || got.Self.Submissions != 0 || len(got.Self.Matches) != 0 {
		t.Fatal("preparation became a guess")
	}
	d := apply("p2", ids[1:], ids[:1])
	states["p2"] = d.State
	assertReady(false)
	d = apply("p2", ids, []string{})
	states["p2"] = d.State
	assertReady(false)
	if _, err := New().Settle(modes.SettleInput{Public: prep.Public, Private: prep.Private, States: states}); err == nil {
		t.Fatal("fault should block scoring")
	}
	for _, phase := range []modes.Phase{modes.PhaseBriefing, modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseCancelled} {
		if project(t, prep, player, phase, states).Answer != nil {
			t.Fatalf("answer leaked in %s", phase)
		}
	}
	for _, phase := range []modes.Phase{modes.PhaseRevealed, modes.PhaseSettled} {
		if project(t, prep, player, phase, states).Answer == nil {
			t.Fatalf("no answer in %s", phase)
		}
	}
}

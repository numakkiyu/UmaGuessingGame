package cluepuzzle

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"testing"
)

func TestCancellationNeverPublishesSolutionOrPrivateClueUnion(t *testing.T) {
	in := teamInput()
	prep, err := New().Prepare(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []modes.Viewer{{Role: "spectator"}, {Role: "referee", IsReferee: true}, {MembershipID: in.Participants[0].MembershipID, Subject: in.Subject[0], CanControl: true}} {
		raw, err := New().Project(modes.ProjectInput{Phase: modes.PhaseCancelled, Viewer: v, Public: prep.Public, Private: prep.Private, RevealNow: true})
		if err != nil {
			t.Fatal(err)
		}
		var p projection
		json.Unmarshal(raw, &p)
		if p.Answer != nil {
			t.Fatalf("cancel leaked %s", raw)
		}
		if v.Role == "spectator" && (p.Self != nil || len(p.PrivateClues) > 0 || len(p.TeamDraft) > 0) {
			t.Fatal("observer private leak")
		}
	}
	if _, err = New().Settle(modes.SettleInput{Public: prep.Public, Private: prep.Private, Phase: modes.PhaseCancelled}); err == nil {
		t.Fatal("cancel settled")
	}
}
func TestCluePositionSemanticsMatchAuthorisedSource(t *testing.T) {
	prep, _, priv := prepareRound(t, "position-semantics", `{"cards":5,"participants":3}`, 3)
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseRevealed} {
		p := project(t, prep, modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, phase, nil)
		clues := append([]clueView{}, p.PrivateClues...)
		if p.Answer != nil {
			clues = append(clues, p.Answer.Constraints...)
		}
		byID := map[string]constraint{}
		for _, c := range priv.Constraints {
			byID[c.ConstraintID] = c
		}
		for _, c := range clues {
			if c.Position != byID[c.ConstraintID].Position {
				t.Fatalf("position lost: %+v", c)
			}
		}
		for _, c := range p.Shared {
			if c.Position != byID[c.ConstraintID].Position {
				t.Fatalf("shared position lost: %+v", c)
			}
		}
	}
}

func TestTeamPublicClueCountsAggregateMembersWithoutPublishingTexts(t *testing.T) {
	in := teamInput()
	prep, err := New().Prepare(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	var priv privateState
	json.Unmarshal(prep.Private, &priv)
	raw, err := New().Project(modes.ProjectInput{Phase: modes.PhaseCollecting, Viewer: modes.Viewer{Role: "spectator"}, Public: prep.Public, Private: prep.Private})
	if err != nil {
		t.Fatal(err)
	}
	var p projection
	json.Unmarshal(raw, &p)
	for _, item := range p.Progress {
		want := 0
		for _, member := range priv.Teams[item.SubjectID] {
			want += len(priv.PrivateClues[member])
		}
		if want == 0 || item.ClueCount != want {
			t.Fatalf("progress %+v want %d", item, want)
		}
	}
	if p.Answer != nil || len(p.PrivateClues) > 0 || p.Self != nil || len(p.TeamDraft) > 0 {
		t.Fatal("count disclosed private information")
	}
}

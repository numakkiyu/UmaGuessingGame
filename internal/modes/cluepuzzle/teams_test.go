package cluepuzzle

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

func teamInput() modes.PrepareInput {
	in := modes.PrepareInput{Scope: modes.Scope{ModeID: ModeID, RulesVersion: RulesVersion, DatasetVersion: "test", ContentPackVersion: "test", Seed: "team-member-clues"}, Config: json.RawMessage(`{"cards":5}`), Content: packOf()}
	for team, n := range []int{3, 5} {
		subject := modes.Subject{ID: fmt.Sprintf("t%d", team), Kind: modes.SubjectTeam}
		in.Subject = append(in.Subject, subject)
		for i := 0; i < n; i++ {
			in.Participants = append(in.Participants, modes.Participant{MembershipID: fmt.Sprintf("t%d-p%d", team, i), Subject: subject})
		}
	}
	return in
}

func TestTeamTopologyCluesDraftAndIndependentScores(t *testing.T) {
	in := teamInput()
	prep, err := New().Prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var priv privateState
	json.Unmarshal(prep.Private, &priv)
	if len(priv.PrivateClues) != 8 {
		t.Fatalf("clue owners: %d", len(priv.PrivateClues))
	}
	sets := []map[string]bool{{}, {}}
	for _, p := range in.Participants {
		clues := priv.PrivateClues[p.MembershipID]
		if len(clues) == 0 {
			t.Fatalf("%s has no clue", p.MembershipID)
		}
		for _, c := range clues {
			sets[int(p.Subject.ID[1]-'0')][c.ConstraintID] = true
			if strings.Contains(c.Text, "card-") {
				t.Fatalf("opaque card name: %s", c.Text)
			}
		}
	}
	if !reflect.DeepEqual(sets[0], sets[1]) {
		t.Fatal("unequal teams received different information")
	}
	states := map[string]json.RawMessage{"t0": modeutil.Marshal(subjectState{Assignment: priv.Solution, Submissions: 1})}
	for _, p := range in.Participants {
		v := modes.Viewer{MembershipID: p.MembershipID, Subject: p.Subject, CanControl: strings.HasSuffix(p.MembershipID, "p0")}
		raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: states, Viewer: v, Phase: modes.PhaseCollecting})
		if err != nil {
			t.Fatal(err)
		}
		var out projection
		json.Unmarshal(raw, &out)
		if len(out.PrivateClues) != len(priv.PrivateClues[p.MembershipID]) || out.SelfIsController != v.CanControl {
			t.Fatalf("wrong own view: %s", raw)
		}
		for i, clue := range out.PrivateClues {
			if clue.ConstraintID != priv.PrivateClues[p.MembershipID][i].ConstraintID {
				t.Fatalf("%s received another member's clue", p.MembershipID)
			}
		}
		for _, clue := range out.PrivateClues {
			if !sets[int(p.Subject.ID[1]-'0')][clue.ConstraintID] {
				t.Fatal("unknown clue")
			}
		}
		if p.Subject.ID == "t1" && len(out.TeamDraft) != 0 {
			t.Fatal("opponent assignment leaked")
		}
	}
	for _, v := range []modes.Viewer{{Role: "host"}, {Role: "spectator"}, {Role: "display"}, {MembershipID: "new-identity", Subject: in.Subject[0], CanControl: true}, {MembershipID: "t0-p0", Subject: in.Subject[1], CanControl: true}} {
		raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: states, Viewer: v, Phase: modes.PhaseCollecting})
		if err != nil {
			t.Fatal(err)
		}
		var out projection
		json.Unmarshal(raw, &out)
		if len(out.PrivateClues) != 0 || out.Self != nil || out.SelfIsController || len(out.TeamDraft) > 0 || out.Answer != nil {
			t.Fatalf("unauthorised view: %s", raw)
		}
	}
	scores, err := New().Settle(modes.SettleInput{Public: prep.Public, Private: prep.Private, States: states})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores[0].Subject.ID != "t0" || scores[0].ScoreUnits != 10000 || scores[1].ScoreUnits != 0 {
		t.Fatalf("scores=%+v", scores)
	}
	// A member joining the same team after preparation still has no write seat.
	decision, err := New().Apply(modes.ApplyInput{MembershipID: "new-identity", Subject: in.Subject[0], Public: prep.Public, Private: prep.Private, Action: modes.ModeAction{Kind: ActionKindAssignment, Payload: json.RawMessage(`{"assignments":[]}`)}})
	if err != nil || decision.Accepted || decision.Code != CodeNotOnRoster {
		t.Fatalf("new member write: %+v %v", decision, err)
	}
}

func TestRosterRejectsUnassignedSmallAndDuplicateMembers(t *testing.T) {
	in := teamInput()
	for _, mutate := range []func(*modes.PrepareInput){
		func(in *modes.PrepareInput) { in.Subject[0].Kind = modes.SubjectPlayer },
		func(in *modes.PrepareInput) { in.Participants = in.Participants[1:] },
		func(in *modes.PrepareInput) { in.Participants[1].MembershipID = in.Participants[0].MembershipID },
	} {
		candidate := teamInput()
		mutate(&candidate)
		if New().ValidateRoster(candidate.Subject, candidate.Participants) == nil {
			t.Fatal("invalid roster accepted")
		}
	}
	if err := New().ValidateRoster(in.Subject, in.Participants); err != nil {
		t.Fatal(err)
	}
}

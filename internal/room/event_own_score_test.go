package room_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// perSubjectScoreMode gives every subject a score derived from its own id, so a
// copy-one-result bug cannot produce matching scores by accident.
type perSubjectScoreMode struct {
	sharedStub
}

func (m *perSubjectScoreMode) Prepare(_ context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	public, _ := json.Marshal(struct {
		Subjects []modes.Subject
	}{in.Subject})
	return modes.Prepared{Public: public}, nil
}

func (m *perSubjectScoreMode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var state struct {
		Subjects []modes.Subject
	}
	if err := json.Unmarshal(in.Public, &state); err != nil {
		return nil, err
	}
	const maxUnits = 10000
	out := make([]modes.Score, 0, len(state.Subjects))
	for _, subject := range state.Subjects {
		// Deterministic but distinct per subject: a stable hash of the id.
		var h uint32 = 2166136261
		for i := 0; i < len(subject.ID); i++ {
			h ^= uint32(subject.ID[i])
			h *= 16777619
		}
		score := int(h%9+1) * 1000 // 1000..9000, never 0 or max
		out = append(out, modes.Score{Subject: subject, ScoreUnits: score, MaxScoreUnits: maxUnits})
	}
	return out, nil
}

// TestEventStandingsKeepEachTeamsOwnScore is the missing half of the rehearsal
// evidence: four teams, four different scores, standings must reflect each team's
// own result rather than a shared copy.
func TestEventStandingsKeepEachTeamsOwnScore(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	mode := &perSubjectScoreMode{sharedStub: sharedStub{id: "sort"}}
	reg, err := modes.NewRegistry(mode)
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "event-own-score"})
	t.Cleanup(svc.Close)

	// One organiser (host, not competing) plus four players.
	sessions := []string{"owner", "p1", "p2", "p3", "p4"}
	for _, id := range sessions {
		if _, err := db.Write().Exec(
			`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at)
			 VALUES(?,?,?,?,'player',0,9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatal(err)
		}
	}

	rules := json.RawMessage(`{"modeId":"sort","informationStyle":"open","config":{"questionSeconds":35}}`)
	made, err := svc.CreateRoom(ctx, "owner", room.RoomCreateInput{
		Name: "独立计分", Kind: "event", ModeConfig: rules, MaxPlayers: 8, Visibility: "invite",
	})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := svc.LoadActor(ctx, "owner", made.Room.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range sessions[1:] {
		if _, err := svc.Join(ctx, id, made.Room.ID, room.JoinInput{
			InviteCode: made.InviteCode, RequestedRole: "player",
		}); err != nil {
			t.Fatal(err)
		}
	}
	event, err := svc.CreateEvent(ctx, "owner", room.CreateEventInput{
		RoomID: made.Room.ID, Name: "四队独立计分", TiePolicy: "shared-rank",
	})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := svc.CreateSegment(ctx, actor, event.ID, room.SegmentCreateInput{
		ModeConfig: rules, Label: "一局定分", ScoreBudget: 100, RoundCount: 1, Competitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	round, err := svc.CreateRound(ctx, actor, room.RoundCreateInput{ModeConfig: rules, SegmentID: seg.ID})
	if err != nil {
		t.Fatal(err)
	}
	version := round.PhaseVersion
	for _, target := range []string{"briefing", "collecting", "locked", "revealed", "settled"} {
		res, terr := svc.CreateTransition(ctx, actor, round.ID, room.TransitionInput{
			Target: target, ExpectedPhaseVersion: version,
		})
		if terr != nil {
			t.Fatalf("transition %s: %v", target, terr)
		}
		version = res.PhaseVersion
	}

	standing, err := svc.GetStandings(ctx, "owner", event.ID)
	if err != nil {
		t.Fatalf("standings: %v", err)
	}
	if len(standing.Items) != 4 {
		t.Fatalf("standings rows = %d, want 4 (one per team)", len(standing.Items))
	}

	/*
	 * Standings are keyed by MEMBERSHIP id (mem_…), not by session id, because a
	 * member's scoring subject is their membership. So the expected score has to be
	 * looked up through the membership that belongs to each session.
	 */
	members, err := svc.ListMembers(ctx, made.Room.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	perSubject := map[string]int{}
	for _, m := range members {
		if m.Role != room.RolePlayer {
			continue
		}
		perSubject[m.ID] = expectedScoreFor(m.ID)
	}
	if len(perSubject) != 4 {
		t.Fatalf("player memberships = %d, want 4", len(perSubject))
	}

	seenScores := map[int]bool{}
	seenSubjects := map[string]bool{}
	for _, item := range standing.Items {
		if seenSubjects[item.SubjectID] {
			t.Fatalf("duplicate subject %q in standings", item.SubjectID)
		}
		seenSubjects[item.SubjectID] = true

		want, ok := perSubject[item.SubjectID]
		if !ok {
			t.Fatalf("standings carry an unknown subject %q", item.SubjectID)
		}
		if item.ScoreUnits != want {
			t.Fatalf("subject %s scored %d, want its own %d — a shared copy would look like this",
				item.SubjectID, item.ScoreUnits, want)
		}
		seenScores[item.ScoreUnits] = true
	}

	// The four scores must be genuinely different, otherwise this test could pass
	// while every team received the same result.
	if len(seenScores) < 2 {
		ids := make([]string, 0, len(perSubject))
		for id := range perSubject {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		t.Fatalf("all four teams ended with the same score; the fixture cannot "+
			"distinguish independent scoring from a copied result (scores: %v)", perSubject)
	}
}

// expectedScoreFor mirrors perSubjectScoreMode.Settle's arithmetic so the test can
// state the expected number without calling into the mode.
func expectedScoreFor(subjectID string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(subjectID); i++ {
		h ^= uint32(subjectID[i])
		h *= 16777619
	}
	return int(h%9+1) * 1000
}

package room_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestStrategyHostCannotSkipRaceTurns(t *testing.T) {
	db, svc, clock := newStrategyService(t)
	roomID, roundID := seedStrategyRound(t, db, *clock, clock.Add(20*time.Second))
	host := room.Actor{SessionID: "ses_owner", RoomID: roomID, Capabilities: map[room.Capability]bool{room.CapManageRoom: true}}
	for _, tc := range []struct{ phase, target string }{{"collecting", "locked"}, {"locked", "revealed"}, {"revealed", "settled"}} {
		if _, err := db.Write().ExecContext(context.Background(), `UPDATE rounds SET status=?,phase_version=1 WHERE id=?`, tc.phase, roundID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateTransition(context.Background(), host, roundID, room.TransitionInput{Target: tc.target, ExpectedPhaseVersion: 1}); err == nil {
			t.Fatalf("host skipped unfinished race with %s", tc.target)
		}
		if got := committedCount(t, db, roundID); got != 0 {
			t.Fatalf("host manufactured %d turns", got)
		}
	}
	if _, err := svc.CreateTransition(context.Background(), host, roundID, room.TransitionInput{Target: "cancel", ExpectedPhaseVersion: 1}); err != nil {
		t.Fatal("cancel must remain possible", err)
	}
}

func TestStrategyBriefingCannotAcceptDecisions(t *testing.T) {
	db, svc, clock := newStrategyService(t)
	roomID, roundID := seedStrategyRound(t, db, *clock, clock.Add(20*time.Second))
	if _, err := db.Write().ExecContext(context.Background(), `UPDATE rounds SET status='briefing' WHERE id=?`, roundID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateSubmission(context.Background(), strategyActor(roomID, "mem_a"), roundID, room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "strategy.action", Payload: json.RawMessage(`{"turn":1,"action":"steady"}`)}})
	if err == nil || committedCount(t, db, roundID) != 0 {
		t.Fatal("briefing accepted decision", err)
	}
}

func TestStrategyLegacyEarlyLockStopsWithoutRepeatedSettlement(t *testing.T) {
	for _, phase := range []string{"locked", "revealed"} {
		t.Run(phase, func(t *testing.T) {
			db, svc, clock := newStrategyService(t)
			_, roundID := seedStrategyRound(t, db, *clock, clock.Add(-time.Second))
			if _, err := db.Write().ExecContext(context.Background(), `UPDATE rounds SET status=? WHERE id=?`, phase, roundID); err != nil {
				t.Fatal(err)
			}
			n, err := svc.SweepExpired(context.Background())
			if err != nil || n != 1 {
				t.Fatal("freeze legacy", n, err)
			}
			n, err = svc.SweepExpired(context.Background())
			if err != nil || n != 0 {
				t.Fatal("repeated invalid settlement", n, err)
			}
			if _, present := readDeadline(t, db, roundID); present {
				t.Fatal("invalid race still scheduled")
			}
			if committedCount(t, db, roundID) != 0 {
				t.Fatal("invented old turns")
			}
			var results int
			if err := db.Read().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM round_results WHERE round_id=?`, roundID).Scan(&results); err != nil {
				t.Fatal(err)
			}
			if results != 0 {
				t.Fatal("scored incomplete old race")
			}
		})
	}
}

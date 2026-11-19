package room_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestMatchIdleCancellation(t *testing.T) {
	for _, scenario := range []string{"none", "one-action", "late-action", "disabled", "friend", "boundary", "four", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			policy := deployment.Policy{}
			if scenario == "disabled" {
				v := false
				policy.Matchmaking.IdleEnabled = &v
			}
			svc, db := matchingService(t, policy)
			ctx := context.Background()
			pool, n := "classic-duel", 2
			if scenario == "four" {
				pool, n = "classic-four", 4
			}
			tickets := make([]room.Ticket, n)
			for i := range tickets {
				tickets[i] = queueMatch(t, svc, "s"+strconv.Itoa(i), pool)
			}
			var done room.Ticket
			for i, v := range tickets {
				var err error
				done, err = svc.RespondToMatch(ctx, "s"+strconv.Itoa(i), v.ID, true)
				if err != nil {
					t.Fatal(err)
				}
			}
			r, err := svc.CurrentRoundSummary(ctx, done.RoomID)
			if err != nil || r == nil {
				t.Fatal(err)
			}
			actor, err := svc.LoadActor(ctx, "s0", done.RoomID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "friend" {
				_, err = db.Write().Exec(`UPDATE rooms SET config_json=json_set(config_json,'$.kind','friend') WHERE id=?`, done.RoomID)
				if err != nil {
					t.Fatal(err)
				}
			}
			base := time.Now().UTC()
			// Move the fixed answering window, not the machine clock used for arrival.
			start := base.Add(-61 * time.Second)
			if scenario == "boundary" {
				start = base.Add(-60 * time.Second)
			}
			end := start.Add(180 * time.Second)
			if scenario == "one-action" {
				_, err = svc.CreateSubmission(ctx, actor, r.ID, room.SubmissionInput{PhaseVersion: r.PhaseVersion, ModeAction: modes.ModeAction{Kind: "classic.guess", Payload: json.RawMessage(`{"characterId":"1001"}`)}})
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err = db.Write().Exec(`UPDATE rounds SET created_at=?,deadline_at=? WHERE id=?`, start.UnixMilli(), end.UnixMilli(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "late-action" || scenario == "rejected" {
				action := modes.ModeAction{Kind: "classic.guess", Payload: json.RawMessage(`{"characterId":"1001"}`)}
				if scenario == "rejected" {
					action.Kind = ""
				}
				_, err = svc.CreateSubmission(ctx, actor, r.ID, room.SubmissionInput{PhaseVersion: r.PhaseVersion, ModeAction: action})
				if err == nil {
					t.Fatal("late or malformed action established activity")
				}
				if scenario == "late-action" && problem.As(err).Code != "matchmaking-idle" {
					t.Fatal(err)
				}
			}
			svc.SetClock(func() time.Time { return base })
			if _, err = svc.SweepExpired(ctx); err != nil {
				t.Fatal(err)
			}
			var phase string
			if err = db.Read().QueryRow(`SELECT status FROM rounds WHERE id=?`, r.ID).Scan(&phase); err != nil {
				t.Fatal(err)
			}
			cancelled := scenario == "none" || scenario == "late-action" || scenario == "four" || scenario == "rejected"
			if (phase == "cancelled") != cancelled {
				t.Fatalf("phase %s", phase)
			}
			if cancelled {
				var scores, open int
				if err = db.Read().QueryRow(`SELECT COUNT(*) FROM round_results WHERE round_id=?`, r.ID).Scan(&scores); err != nil {
					t.Fatal(err)
				}
				if err = db.Read().QueryRow(`SELECT COUNT(*) FROM matchmaking_tickets WHERE room_id=? AND status IN ('playing','ready')`, done.RoomID).Scan(&open); err != nil {
					t.Fatal(err)
				}
				if scores != 0 || open != 0 {
					t.Fatal("idle match scored or retained tickets")
				}
				if _, err = svc.SweepExpired(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

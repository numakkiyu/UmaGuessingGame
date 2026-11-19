package room_test

import (
	"context"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/rapport"
	"github.com/bhcn/umaguessinggame/internal/room"
	"testing"
	"time"
)

// Reuse the minimal persisted-room fixture, replacing only the mode and public
// clock state. Real preference privacy and scoring are covered by rapport tests.
func TestRapportCannotSkipQuestionsAndLegacyTimersStopOnce(t *testing.T) {
	for _, phase := range []string{"collecting", "locked", "revealed"} {
		t.Run(phase, func(t *testing.T) {
			db, _, clock := newStrategyService(t)
			rid, id := seedStrategyRound(t, db, *clock, clock.Add(-time.Second))
			reg, err := modes.NewRegistry(rapport.New())
			if err != nil {
				t.Fatal(err)
			}
			svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "rapport"})
			defer svc.Close()
			svc.SetClock(func() time.Time { return *clock })
			if _, err := db.Write().Exec(`UPDATE rounds SET mode_id='rapport',status=?,config_json='{"simultaneousTurns":true,"questions":2,"questionSeconds":20}' WHERE id=?`, phase, id); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Write().Exec(`UPDATE round_private_state SET state_json='{"live":{"currentQuestion":1},"questions":[{},{}]}' WHERE round_id=? AND scope='public'`, id); err != nil {
				t.Fatal(err)
			}
			actor := room.Actor{SessionID: "ses_owner", RoomID: rid, Capabilities: map[room.Capability]bool{room.CapManageRoom: true}}
			target := map[string]string{"collecting": "locked", "locked": "revealed", "revealed": "settled"}[phase]
			if _, err := svc.CreateTransition(context.Background(), actor, id, room.TransitionInput{Target: target, ExpectedPhaseVersion: 1}); err == nil {
				t.Fatal("host skipped unfinished question")
			}
			if phase != "collecting" {
				n, err := svc.SweepExpired(context.Background())
				if err != nil || n != 1 {
					t.Fatal("freeze legacy", n, err)
				}
				n, err = svc.SweepExpired(context.Background())
				if err != nil || n != 0 {
					t.Fatal("repeated legacy settlement", n, err)
				}
				if _, present := readDeadline(t, db, id); present {
					t.Fatal("legacy timer still scheduled")
				}
			}
			var version int64
			if err := db.Read().QueryRow(`SELECT phase_version FROM rounds WHERE id=?`, id).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CreateTransition(context.Background(), actor, id, room.TransitionInput{Target: "cancel", ExpectedPhaseVersion: version}); err != nil {
				t.Fatal("cancel blocked", err)
			}
		})
	}
}

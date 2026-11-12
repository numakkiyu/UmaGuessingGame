package room_test

import (
	"context"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestRestartPausesHostedActivitiesWithoutChangingAnswerOrExistingPause(t *testing.T) {
	for _, phase := range []modes.Phase{modes.PhaseBriefing, modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseRevealed} {
		for _, window := range []string{"remaining", "expired", "untimed", "already-paused", "friend"} {
			t.Run(string(phase)+"/"+window, func(t *testing.T) {
				ctx := context.Background()
				db := newDB(t)
				svc := room.New(db, room.Config{}, room.Options{Epoch: "restart-test"})
				t.Cleanup(svc.Close)
				sid, rid, id := seedRoundInPhase(t, db, svc, phase, window == "already-paused")
				now := time.Now().Truncate(time.Millisecond)
				svc.SetClock(func() time.Time { return now })
				if window != "friend" {
					if _, err := db.Write().Exec(`UPDATE rooms SET config_json=json_set(config_json,'$.kind','event') WHERE id=?`, rid); err != nil {
						t.Fatal(err)
					}
				}
				var deadline any = now.Add(20 * time.Second).UnixMilli()
				if window == "expired" {
					deadline = now.Add(-time.Second).UnixMilli()
				} else if window == "untimed" || window == "already-paused" {
					deadline = nil
				}
				if _, err := db.Write().Exec(`UPDATE rounds SET deadline_at=? WHERE id=?`, deadline, id); err != nil {
					t.Fatal(err)
				}
				want := 1
				if window == "friend" || window == "already-paused" || (window == "untimed" && (phase == modes.PhaseLocked || phase == modes.PhaseRevealed)) {
					want = 0
				}
				if n, err := svc.PauseInterruptedEvents(ctx); err != nil || n != want {
					t.Fatalf("startup = %d, %v, want %d", n, err, want)
				}
				if n, err := svc.PauseInterruptedEvents(ctx); err != nil || n != 0 {
					t.Fatalf("second startup changed existing pause: %d, %v", n, err)
				}
				var storedPhase, secret string
				var paused, remaining *int64
				var version int64
				if err := db.Read().QueryRow(`SELECT status,paused_at,paused_remaining_ms,phase_version FROM rounds WHERE id=?`, id).Scan(&storedPhase, &paused, &remaining, &version); err != nil {
					t.Fatal(err)
				}
				if err := db.Read().QueryRow(`SELECT state_json FROM round_private_state WHERE round_id=? AND scope='round'`, id).Scan(&secret); err != nil {
					t.Fatal(err)
				}
				if storedPhase != string(phase) || secret != `{"answerCharacterId":"secret"}` {
					t.Fatal("startup changed phase or secret")
				}
				if want == 0 {
					if version != 1 || (window == "already-paused" && (remaining == nil || *remaining != 120000)) {
						t.Fatal("untouched round changed")
					}
					return
				}
				if paused == nil || version != 2 || (window == "untimed" && remaining != nil) || (window == "remaining" && (remaining == nil || *remaining != 20000)) || (window == "expired" && (remaining == nil || *remaining != 0)) {
					t.Fatalf("wrong recovered clock: paused=%v remaining=%v version=%d", paused, remaining, version)
				}
				actor, err := svc.LoadActor(ctx, sid, rid)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := svc.CreateTransition(ctx, actor, id, room.TransitionInput{Target: "settled", ExpectedPhaseVersion: version}); err == nil {
					t.Fatal("paused activity allowed a forward transition")
				}
				if _, err := svc.CreateTransition(ctx, actor, id, room.TransitionInput{Target: "resume", ExpectedPhaseVersion: version}); err != nil {
					t.Fatal(err)
				}
				var resumed *int64
				if err := db.Read().QueryRow(`SELECT deadline_at FROM rounds WHERE id=?`, id).Scan(&resumed); err != nil {
					t.Fatal(err)
				}
				if window == "untimed" && resumed != nil {
					t.Fatal("untimed briefing became an immediate deadline")
				}
			})
		}
	}
}

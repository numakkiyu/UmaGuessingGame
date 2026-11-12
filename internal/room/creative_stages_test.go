package room_test

import (
	"context"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"testing"
	"time"
)

func TestCreativeReviewAndVoteClocksPauseAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "staged", true: "legacy"}[legacy], func(t *testing.T) {
			ctx := context.Background()
			db := newDB(t)
			svc := room.New(db, room.Config{}, room.Options{Epoch: "creative-clock"})
			defer svc.Close()
			sessionID, roomID, roundID := seedRoundInPhase(t, db, svc, modes.PhaseCollecting, false)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.Write().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			public := `{"subphase":"writing","config":{"separateWindows":true,"writeSeconds":90,"voteSeconds":30},"subjects":[],"onBallot":{"w1":true,"w2":true}}`
			if legacy {
				public = `{"config":{"writeSeconds":90,"voteSeconds":30}}`
			}
			exec(`UPDATE rounds SET mode_id='creative',config_json='{"separateWindows":true,"writeSeconds":90,"voteSeconds":30}',deadline_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), roundID)
			exec(`UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='public'`, public, roundID)
			exec(`UPDATE round_private_state SET state_json='{"textOf":{"w1":"作品甲","w2":"作品乙"},"authorOf":{"w1":"p1","w2":"p2"}}' WHERE round_id=? AND scope='round'`, roundID)
			actor, err := svc.LoadActor(ctx, sessionID, roomID)
			if err != nil {
				t.Fatal(err)
			}
			read := func() (string, int64, *int64) {
				t.Helper()
				var phase string
				var version int64
				var deadline *int64
				if err := db.Read().QueryRow(`SELECT status,phase_version,deadline_at FROM rounds WHERE id=?`, roundID).Scan(&phase, &version, &deadline); err != nil {
					t.Fatal(err)
				}
				return phase, version, deadline
			}
			transition := func(target string, excluded []string) error {
				_, v, _ := read()
				_, err := svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: target, ExpectedPhaseVersion: v, ExcludedOptionIDs: excluded})
				return err
			}
			if _, err := svc.SweepExpired(ctx); err != nil {
				t.Fatal(err)
			}
			phase, v, deadline := read()
			if phase != "locked" || v != 2 || deadline != nil {
				t.Fatal("writing/legacy did not stop at untimed review")
			}
			if n, err := svc.SweepExpired(ctx); err != nil || n != 0 {
				t.Fatal("old/review timer loops")
			}
			if transition("revealed", nil) == nil || transition("settled", nil) == nil {
				t.Fatal("host skipped ballot")
			}
			if legacy {
				if transition("creative-voting", nil) == nil {
					t.Fatal("legacy opened old ballot")
				}
				return
			}
			if transition("creative-voting", []string{"unknown"}) == nil || transition("creative-voting", []string{"w1", "w1"}) == nil {
				t.Fatal("invalid review exclusions accepted")
			}
			if transition("pause", nil) == nil {
				t.Fatal("untimed locked review should not create a new clock")
			}
			before := time.Now().UnixMilli()
			if err := transition("creative-voting", []string{"w1"}); err != nil {
				t.Fatal(err)
			}
			phase, _, deadline = read()
			if phase != "collecting" || deadline == nil || *deadline < before+30000 || *deadline > time.Now().UnixMilli()+30000 {
				t.Fatal("vote did not receive fresh 30 seconds")
			}
			if err := transition("pause", nil); err != nil {
				t.Fatal(err)
			}
			if transition("locked", nil) == nil {
				t.Fatal("paused ballot closed")
			}
			if err := transition("resume", nil); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE rounds SET deadline_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), roundID)
			if _, err := svc.SweepExpired(ctx); err != nil {
				t.Fatal(err)
			}
			phase, _, deadline = read()
			if phase != "locked" || deadline == nil {
				t.Fatal("ballot expiry did not lock for reveal")
			}
			var closed string
			if err := db.Read().QueryRow(`SELECT json_extract(state_json,'$.subphase') FROM round_private_state WHERE round_id=? AND scope='public'`, roundID).Scan(&closed); err != nil || closed != "closed" {
				t.Fatal("ballot not frozen at deadline", closed, err)
			}
		})
	}
}

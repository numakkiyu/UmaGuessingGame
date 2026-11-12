package room_test

import (
	"context"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"testing"
	"time"
)

func TestAudioStartRequiresPreparedRoster(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test"})
	defer svc.Close()
	sessionID, roomID, roundID := seedRoundInPhase(t, db, svc, modes.PhaseBriefing, false)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Write().ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE rounds SET mode_id='audio',config_json='{"answerSeconds":90}' WHERE id=?`, roundID)
	exec(`UPDATE round_private_state SET state_json='{"subjects":[{"subjectId":"p1","subjectKind":"player"},{"subjectId":"p2","subjectKind":"player"}]}' WHERE round_id=? AND scope='public'`, roundID)
	actor, err := svc.LoadActor(ctx, sessionID, roomID)
	if err != nil {
		t.Fatal(err)
	}
	start := func() error {
		_, err := svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: "collecting", ExpectedPhaseVersion: 1})
		return err
	}
	if start() == nil {
		t.Fatal("started with nobody prepared")
	}
	exec(`INSERT INTO participant_round_state (round_id,subject_id,subject_kind,state_json,updated_at) VALUES (?, 'p1','player','{"ready":true}',?)`, roundID, time.Now().UnixMilli())
	if start() == nil {
		t.Fatal("started with only one prepared")
	}
	exec(`INSERT INTO participant_round_state (round_id,subject_id,subject_kind,state_json,updated_at) VALUES (?, 'p2','player','{"ready":true,"playbackFailures":["clip-1"]}',?)`, roundID, time.Now().UnixMilli())
	if start() == nil {
		t.Fatal("started despite media fault")
	}
	exec(`UPDATE participant_round_state SET state_json='{"ready":true}' WHERE round_id=? AND subject_id='p2'`, roundID)
	if err := start(); err != nil {
		t.Fatal(err)
	}
}

func TestAudioFaultExpiryLocksWithoutRevealingOrScoring(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test"})
	defer svc.Close()
	_, _, roundID := seedRoundInPhase(t, db, svc, modes.PhaseCollecting, false)
	now := time.Now()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Write().ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE rounds SET mode_id='audio',deadline_at=? WHERE id=?`, now.Add(-time.Second).UnixMilli(), roundID)
	exec(`INSERT INTO participant_round_state(round_id,subject_id,subject_kind,state_json,updated_at) VALUES(?,'p1','player','{"playbackFailures":["clip-1"]}',?)`, roundID, now.UnixMilli())
	if _, err := svc.SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var phase string
	var deadline *int64
	if err := db.Read().QueryRowContext(ctx, `SELECT status,deadline_at FROM rounds WHERE id=?`, roundID).Scan(&phase, &deadline); err != nil {
		t.Fatal(err)
	}
	if phase != "locked" || deadline != nil {
		t.Fatalf("phase=%s deadline=%v", phase, deadline)
	}
	if _, err := svc.SweepExpired(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Read().QueryRowContext(ctx, `SELECT count(*) FROM round_results WHERE round_id=?`, roundID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("fault scored")
	}
}

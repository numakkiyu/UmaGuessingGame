package room_test

import (
	"context"
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/room"
	"testing"
	"time"
)

func TestBluffReviewSeparateClockAndModeration(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test"})
	defer svc.Close()
	sessionID, roomID, roundID := seedRoundInPhase(t, db, svc, modes.PhaseCollecting, false)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Write().ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now().UnixMilli()
	exec(`UPDATE rounds SET mode_id='bluff',config_json='{"writeSeconds":60,"voteSeconds":30}',deadline_at=? WHERE id=?`, now-100, roundID)
	exec(`UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='public'`, `{"subphase":"writing","subjects":[{"subjectId":"p1","subjectKind":"player"},{"subjectId":"p2","subjectKind":"player"},{"subjectId":"p3","subjectKind":"player"}],"config":{"writeSeconds":60,"voteSeconds":30}}`, roundID)
	exec(`UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='round'`, `{"truth":"真实答案","truthOptionId":"truth","fallbackAnswers":["备用答案甲","备用答案乙","备用答案丙"],"authorOf":{}}`, roundID)
	actor, e := svc.LoadActor(ctx, sessionID, roomID)
	if e != nil {
		t.Fatal(e)
	}
	read := func() (string, int64, *int64) {
		t.Helper()
		var phase string
		var v int64
		var deadline *int64
		if e := db.Read().QueryRowContext(ctx, `SELECT status,phase_version,deadline_at FROM rounds WHERE id=?`, roundID).Scan(&phase, &v, &deadline); e != nil {
			t.Fatal(e)
		}
		return phase, v, deadline
	}
	if _, e = svc.SweepExpired(ctx); e != nil {
		t.Fatal(e)
	}
	phase, v, deadline := read()
	if phase != "locked" || v != 2 || deadline != nil {
		t.Fatalf("writing deadline %s %d %v", phase, v, deadline)
	}
	if _, e = svc.SweepExpired(ctx); e != nil {
		t.Fatal(e)
	}
	phase, v, deadline = read()
	if phase != "locked" || v != 2 || deadline != nil {
		t.Fatal("review auto advanced")
	}
	transition := func(target string, ids []string) error {
		_, e := svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: target, ExpectedPhaseVersion: 2, ExcludedOptionIDs: ids})
		return e
	}
	if transition("revealed", nil) == nil || transition("settled", nil) == nil {
		t.Fatal("review bypass")
	}
	original := actor
	actor.Capabilities = nil
	if transition("bluff-voting", nil) == nil {
		t.Fatal("unprivileged moderation")
	}
	actor = original
	if transition("bluff-voting", []string{"truth"}) == nil || transition("bluff-voting", []string{"unknown"}) == nil {
		t.Fatal("bad exclusion")
	}
	var public string
	if e = db.Read().QueryRowContext(ctx, `SELECT state_json FROM round_private_state WHERE round_id=? AND scope='public'`, roundID).Scan(&public); e != nil {
		t.Fatal(e)
	}
	var ballot struct {
		Options []struct {
			OptionID string `json:"optionId"`
		}
		Subphase string `json:"subphase"`
	}
	json.Unmarshal([]byte(public), &ballot)
	if ballot.Subphase != "review" || len(ballot.Options) != 4 {
		t.Fatal("review mutated", public)
	}
	dropped := ""
	for _, o := range ballot.Options {
		if o.OptionID != "truth" {
			dropped = o.OptionID
			break
		}
	}
	before := time.Now().UnixMilli()
	if e = transition("bluff-voting", []string{dropped}); e != nil {
		t.Fatal(e)
	}
	phase, v, deadline = read()
	if phase != "collecting" || v != 3 || deadline == nil || *deadline < before+30000 || *deadline > time.Now().UnixMilli()+30000 {
		t.Fatal("vote clock", phase, v, deadline)
	}
	if transition("bluff-voting", nil) == nil {
		t.Fatal("stale transition accepted")
	}
	if _, e = svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: "pause", ExpectedPhaseVersion: 3}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: "locked", ExpectedPhaseVersion: 4}); e == nil {
		t.Fatal("paused phase closed")
	}
	if _, e = svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: "resume", ExpectedPhaseVersion: 4}); e != nil {
		t.Fatal(e)
	}
	for _, step := range []struct {
		target  string
		version int64
	}{{"pause", 5}, {"resume", 6}} {
		if _, e = svc.CreateTransition(ctx, actor, roundID, room.TransitionInput{Target: step.target, ExpectedPhaseVersion: step.version}); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE rounds SET deadline_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), roundID)
	if _, e = svc.SweepExpired(ctx); e != nil {
		t.Fatal(e)
	}
	phase, _, deadline = read()
	if phase != "locked" || deadline == nil {
		t.Fatal("vote expiry did not lock with reveal window")
	}
}

func TestBluffFinalTextAutoClosesWithCurrentReceipt(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	registry, e := modes.NewRegistry(bluff.New())
	if e != nil {
		t.Fatal(e)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: registry, Epoch: "test"})
	defer svc.Close()
	sessionID, roomID, roundID := seedRoundInPhase(t, db, svc, modes.PhaseCollecting, false)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Write().ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE rounds SET mode_id='bluff',config_json='{"writeSeconds":60,"voteSeconds":30}' WHERE id=?`, roundID)
	public, _ := json.Marshal(map[string]any{"subphase": "writing", "decoyCount": 2, "subjects": []map[string]any{{"subjectId": "mem_" + roomID, "subjectKind": "player"}, {"subjectId": "p2", "subjectKind": "player"}, {"subjectId": "p3", "subjectKind": "player"}}})
	exec(`UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='public'`, string(public), roundID)
	exec(`UPDATE round_private_state SET state_json='{"truth":"真实答案","truthOptionId":"truth","authorOf":{},"decoys":[]}' WHERE round_id=? AND scope='round'`, roundID)
	actor, e := svc.LoadActor(ctx, sessionID, roomID)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := svc.CreateSubmission(ctx, actor, roundID, room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "bluff.decoy", Payload: json.RawMessage(`{"kind":"bluff.decoy","text":"最后一份假答案"}`)}})
	if e != nil {
		t.Fatal(e)
	}
	var phase string
	var version int64
	var deadline *int64
	if e = db.Read().QueryRowContext(ctx, `SELECT status,phase_version,deadline_at FROM rounds WHERE id=?`, roundID).Scan(&phase, &version, &deadline); e != nil {
		t.Fatal(e)
	}
	if phase != "locked" || deadline != nil || version != 2 || receipt.PhaseVersion != 2 {
		t.Fatalf("closure receipt phase=%s v=%d receipt=%d deadline=%v", phase, version, receipt.PhaseVersion, deadline)
	}
}
func TestBluffLegacyExpiryFailsClosed(t *testing.T) {
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseLocked} {
		t.Run(string(phase), func(t *testing.T) {
			ctx := context.Background()
			db := newDB(t)
			svc := room.New(db, room.Config{}, room.Options{Epoch: "test"})
			defer svc.Close()
			sid, rid, id := seedRoundInPhase(t, db, svc, phase, false)
			if _, e := db.Write().ExecContext(ctx, `UPDATE rounds SET mode_id='bluff',deadline_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), id); e != nil {
				t.Fatal(e)
			}
			if _, e := svc.SweepExpired(ctx); e != nil {
				t.Fatal(e)
			}
			var status string
			var deadline *int64
			if e := db.Read().QueryRowContext(ctx, `SELECT status,deadline_at FROM rounds WHERE id=?`, id).Scan(&status, &deadline); e != nil {
				t.Fatal(e)
			}
			if status != "locked" || deadline != nil {
				t.Fatal("legacy advanced", status)
			}
			actor, e := svc.LoadActor(ctx, sid, rid)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = svc.CreateTransition(ctx, actor, id, room.TransitionInput{Target: "revealed", ExpectedPhaseVersion: 2}); e == nil {
				t.Fatal("legacy revealed")
			}
		})
	}
}

func TestBluffAllIdentifiedDoNotWaitForEmptyBallot(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test"})
	defer svc.Close()
	sid, rid, id := seedRoundInPhase(t, db, svc, modes.PhaseLocked, false)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Write().ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE rounds SET mode_id='bluff',config_json='{"writeSeconds":60,"voteSeconds":30}',deadline_at=NULL WHERE id=?`, id)
	exec(`UPDATE round_private_state SET state_json='{"subphase":"review","subjects":[{"subjectId":"p1"},{"subjectId":"p2"},{"subjectId":"p3"}]}' WHERE round_id=? AND scope='public'`, id)
	exec(`UPDATE round_private_state SET state_json='{"truth":"真实答案","truthOptionId":"truth","decoys":[{"decoyId":"fallback","text":"夹具备选","authorIds":[]}]}' WHERE round_id=? AND scope='round'`, id)
	for _, subject := range []string{"p1", "p2", "p3"} {
		exec(`INSERT INTO participant_round_state(round_id,subject_id,subject_kind,state_json,updated_at)VALUES(?,?,'player','{"decoyCount":1,"decoyText":"真实答案"}',?)`, id, subject, time.Now().UnixMilli())
	}
	actor, e := svc.LoadActor(ctx, sid, rid)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateTransition(ctx, actor, id, room.TransitionInput{Target: "bluff-voting", ExpectedPhaseVersion: 1}); e != nil {
		t.Fatal(e)
	}
	var phase string
	var deadline int64
	if e = db.Read().QueryRowContext(ctx, `SELECT status,deadline_at FROM rounds WHERE id=?`, id).Scan(&phase, &deadline); e != nil {
		t.Fatal(e)
	}
	if phase != "locked" || deadline > time.Now().UnixMilli()+1000 {
		t.Fatal("waited for non-voters", phase, deadline)
	}
}

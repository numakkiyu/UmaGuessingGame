package room_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

func TestPauseRequiresCounterpartyAndCannotReplay(t *testing.T) {
	for _, accept := range []bool{true, false} {
		t.Run(map[bool]string{true: "accept", false: "reject"}[accept], func(t *testing.T) {
			f := newTeamRound(t, "memory")
			ctx := context.Background()
			req, err := f.svc.CreatePauseRequest(ctx, f.actor("a1"), f.roundID, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"a1", "a2"} {
				_, err = f.svc.RespondToPause(ctx, f.actor(id), f.roundID, req.ID, accept)
				if err == nil || problem.As(err).Code != "pause-counterparty-required" {
					t.Fatalf("%s self response: %v", id, err)
				}
			}
			result, err := f.svc.RespondToPause(ctx, f.actor("b1"), f.roundID, req.ID, accept)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status == "pending" {
				t.Fatal("response not closed")
			}
			_, err = f.svc.RespondToPause(ctx, f.actor("b1"), f.roundID, req.ID, !accept)
			if err == nil || problem.As(err).Code != "pause-request-closed" {
				t.Fatalf("replayed pause: %v", err)
			}
		})
	}
}

func TestPauseRequestCannotCrossPhase(t *testing.T) {
	f := newTeamRound(t, "memory")
	ctx := context.Background()
	req, err := f.svc.CreatePauseRequest(ctx, f.actor("a1"), f.roundID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE rounds SET phase_version=phase_version+1 WHERE id=?`, f.roundID)
	_, err = f.svc.RespondToPause(ctx, f.actor("b1"), f.roundID, req.ID, true)
	if err == nil || problem.As(err).Code != "pause-request-stale" {
		t.Fatalf("stale pause: %v", err)
	}
}

func TestQueuedActorCannotRetainChangedIdentity(t *testing.T) {
	for _, change := range []string{"logout", "expired", "role", "team"} {
		t.Run(change, func(t *testing.T) {
			f := newTeamRound(t, "memory")
			actor := f.actor("a1")
			switch change {
			case "logout":
				f.exec(t, `UPDATE sessions SET revoked_at=1 WHERE id='ses_a1'`)
			case "expired":
				f.exec(t, `UPDATE sessions SET expires_at=1 WHERE id='ses_a1'`)
			case "role":
				f.exec(t, `UPDATE memberships SET role='spectator' WHERE id='a1'`)
			case "team":
				f.exec(t, `UPDATE memberships SET team_id='mem_b' WHERE id='a1'`)
			}
			_, err := f.svc.CreatePauseRequest(context.Background(), actor, f.roundID, 1)
			if err == nil || problem.As(err).Code != "membership-inactive" {
				t.Fatalf("stale actor retained authority: %v", err)
			}
		})
	}
}

func TestRecoveryTransfersSeatAndAuthorityAtomically(t *testing.T) {
	f := newTeamRound(t, "memory")
	ctx := context.Background()
	f.exec(t, `UPDATE rooms SET owner_session_id='ses_a1',config_json=json_set(config_json,'$.kind','event') WHERE id=?`, f.roomID)
	f.exec(t, `INSERT INTO room_grants(id,room_id,session_id,capability,granted_at) VALUES('grant',?,'ses_a1','host',0)`, f.roomID)
	f.exec(t, `INSERT INTO event_sessions(id,room_id,host_session_id,name,status,created_at,updated_at) VALUES('ev',?,'ses_a1','test','running',0,0)`, f.roomID)
	f.exec(t, `INSERT INTO participant_round_state(round_id,subject_id,subject_kind,state_json,updated_at) VALUES(?,'mem_a','team','{"attempts":3}',0)`, f.roundID)
	actor := f.actor("a1")
	actor.Capabilities[room.CapHost] = true
	grant, err := f.svc.CreateRecoveryGrant(ctx, actor, "a1", "换设备继续玩")
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.New(f.db, session.Config{})
	fail := errors.New("storage unavailable")
	_, _, err = f.svc.RedeemRecoveryGrant(ctx, grant.Code, func(ctx context.Context, tx *sql.Tx, name string) (string, error) {
		if _, err := sessions.CreateInTransaction(ctx, tx, name); err != nil {
			return "", err
		}
		return "", fail
	})
	if !errors.Is(err, fail) {
		t.Fatalf("expected factory failure: %v", err)
	}
	var count int
	if err := f.db.Read().QueryRow(`SELECT COUNT(*) FROM sessions WHERE display_name=?`, "训练员a1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("session escaped rollback: %d %v", count, err)
	}
	var restored session.CreateResult
	membership, roomID, err := f.svc.RedeemRecoveryGrant(ctx, grant.Code, func(ctx context.Context, tx *sql.Tx, name string) (string, error) {
		var err error
		restored, err = sessions.CreateInTransaction(ctx, tx, name)
		return restored.Session.ID, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if membership != "a1" || roomID != f.roomID {
		t.Fatal("seat identity changed")
	}
	if _, err := sessions.Lookup(ctx, restored.Token); err != nil {
		t.Fatal(err)
	}
	next, err := f.svc.LoadActor(ctx, restored.Session.ID, f.roomID)
	if err != nil || next.MembershipID() != "a1" || !next.Has(room.CapHost) {
		t.Fatalf("restored actor: %+v %v", next, err)
	}
	if _, err := f.svc.LoadActor(ctx, "ses_a1", f.roomID); err == nil {
		t.Fatal("old identity still has room access")
	}
	for _, query := range []string{`SELECT owner_session_id FROM rooms WHERE id=?`, `SELECT session_id FROM room_grants WHERE room_id=?`, `SELECT host_session_id FROM event_sessions WHERE room_id=?`} {
		var id string
		if err := f.db.Read().QueryRow(query, f.roomID).Scan(&id); err != nil || id != restored.Session.ID {
			t.Fatalf("authority not transferred: %s %v", id, err)
		}
	}
	// A queued actor resolved before recovery cannot create a new pause afterwards.
	if _, err := f.svc.CreatePauseRequest(ctx, actor, f.roundID, 1); err == nil || problem.As(err).Code != "membership-inactive" {
		t.Fatalf("old actor still writes: %v", err)
	}
	if _, err := f.svc.CreatePauseRequest(ctx, next, f.roundID, 1); err != nil {
		t.Fatal("new identity cannot continue", err)
	}
	_, _, err = f.svc.RedeemRecoveryGrant(ctx, grant.Code, func(context.Context, *sql.Tx, string) (string, error) {
		t.Fatal("replayed grant created session")
		return "", nil
	})
	if err == nil || problem.As(err).Code != "grant-already-used" {
		t.Fatalf("replayed grant: %v", err)
	}
	var state string
	if err := f.db.Read().QueryRow(`SELECT state_json FROM participant_round_state WHERE round_id=? AND subject_id='mem_a'`, f.roundID).Scan(&state); err != nil || state != `{"attempts":3}` {
		t.Fatalf("lost progress: %s %v", state, err)
	}
}

package room

import (
	"context"
	"crypto/rand"
	"database/sql"
	"math/big"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

type HostTransferInput struct {
	ExpectedRoomVersion int64
	TargetMembershipID  string
	Command             *CreationCommand
}
type HostTransfer struct {
	ID                 string
	RoomID             string
	TargetMembershipID string
	RoomVersion        int64
}

// TransferHost keeps ownership, live control and the active programme in one
// transaction. Participation roles and frozen scoring subjects are unchanged.
func (s *Service) TransferHost(ctx context.Context, sessionID, roomID string, in HostTransferInput) (HostTransfer, error) {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return HostTransfer{}, problem.Internal(err)
	}
	defer tx.Rollback()
	if id, err := createdResource(ctx, tx, "host_transfers", "actor_session_id", sessionID, in.Command); err != nil {
		return HostTransfer{}, err
	} else if id != "" {
		var result HostTransfer
		err = tx.QueryRowContext(ctx, `SELECT id,room_id,target_membership_id,room_version FROM host_transfers WHERE id=?`, id).Scan(&result.ID, &result.RoomID, &result.TargetMembershipID, &result.RoomVersion)
		if err != nil {
			return HostTransfer{}, problem.Internal(err)
		}
		if result.RoomID != roomID || result.TargetMembershipID != in.TargetMembershipID {
			return HostTransfer{}, problem.Conflict("idempotency-conflict", "transfer command belongs to another request")
		}
		return result, nil
	}
	rm, err := s.loadRoomQ(ctx, tx, roomID)
	if err != nil {
		return HostTransfer{}, err
	}
	if rm.Config.Kind == "matchmaking" {
		return HostTransfer{}, problem.Forbidden("matchmaking-reserved", "matched rooms have no player host")
	}
	if rm.OwnerID != sessionID {
		return HostTransfer{}, problem.Forbidden("host-required", "only the current host may transfer the room")
	}
	if rm.Status.Terminal() {
		return HostTransfer{}, problem.Conflict("room-closed", "this room has ended")
	}
	if rm.RoomVersion != in.ExpectedRoomVersion {
		return HostTransfer{}, problem.Conflict("room-version-conflict", "the room changed; reload before transferring")
	}
	target, err := loadMembershipByIDQ(ctx, tx, in.TargetMembershipID)
	if err != nil {
		return HostTransfer{}, err
	}
	if target.RoomID != roomID || !target.Active() || target.Role == RoleDisplay || target.SessionID == sessionID {
		return HostTransfer{}, problem.Unprocessable("invalid-host-target", "choose another active member of this room")
	}
	now := s.nowMS()
	var valid int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id=? AND kind='player' AND revoked_at IS NULL AND expires_at>?`, target.SessionID, now).Scan(&valid); err != nil {
		return HostTransfer{}, problem.Internal(err)
	}
	if valid != 1 {
		return HostTransfer{}, problem.Unprocessable("invalid-host-target", "the chosen member can no longer host")
	}
	if err = s.assignHostQ(ctx, tx, roomID, sessionID, target, now, "manual"); err != nil {
		return HostTransfer{}, err
	}
	version, err := bumpRoomVersion(ctx, tx, roomID, now)
	if err != nil {
		return HostTransfer{}, err
	}
	id, err := newID("htf_")
	if err != nil {
		return HostTransfer{}, problem.Internal(err)
	}
	key, hash, err := in.Command.identity()
	if err != nil {
		return HostTransfer{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO host_transfers(id,room_id,actor_session_id,target_membership_id,room_version,creation_key,creation_hash,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, roomID, sessionID, target.ID, version, key, hash, now); err != nil {
		return HostTransfer{}, problem.Internal(err)
	}
	if err = emitOutbox(ctx, tx, roomID, "state.changed", map[string]any{"resource": "room", "roomVersion": version}, now); err != nil {
		return HostTransfer{}, err
	}
	if err = tx.Commit(); err != nil {
		return HostTransfer{}, problem.Internal(err)
	}
	return HostTransfer{ID: id, RoomID: roomID, TargetMembershipID: target.ID, RoomVersion: version}, nil
}

func (s *Service) assignHostQ(ctx context.Context, tx *sql.Tx, roomID, previous string, target Membership, now int64, reason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE rooms SET owner_session_id=? WHERE id=? AND owner_session_id=?`, target.SessionID, roomID, previous); err != nil {
		return problem.Internal(err)
	}
	// Clear every historical control grant; role='host' is participation only.
	if _, err := tx.ExecContext(ctx, `UPDATE room_grants SET revoked_at=? WHERE room_id=? AND revoked_at IS NULL`, now, roomID); err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE event_sessions SET host_session_id=?,event_version=event_version+1,updated_at=? WHERE room_id=? AND status NOT IN ('completed','cancelled')`, target.SessionID, now, roomID); err != nil {
		return problem.Internal(err)
	}
	return writeAudit(ctx, tx, roomID, "", previous, "host.transferred", map[string]any{"targetMembershipId": target.ID, "reason": reason}, now)
}

func (s *Service) handoffOnLeaveQ(ctx context.Context, tx *sql.Tx, actor Actor, target Membership, now int64) error {
	owner := actorSessionOfRoomOwner(ctx, tx, actor.RoomID)
	if target.SessionID != owner {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id FROM memberships m JOIN sessions s ON s.id=m.session_id WHERE m.room_id=? AND m.id<>? AND m.left_at IS NULL AND m.revoked_at IS NULL AND m.role<>'display' AND s.kind='player' AND s.revoked_at IS NULL AND s.expires_at>? ORDER BY m.id`, actor.RoomID, target.ID, now)
	if err != nil {
		return problem.Internal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return problem.Internal(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return problem.Internal(err)
	}
	if len(ids) > 0 {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(ids))))
		if err != nil {
			return problem.Internal(err)
		}
		next, err := loadMembershipByIDQ(ctx, tx, ids[index.Int64()])
		if err != nil {
			return err
		}
		return s.assignHostQ(ctx, tx, actor.RoomID, owner, next, now, "departure")
	}
	// No successor: end the room rather than leaving an abandoned live contest.
	if _, err = tx.ExecContext(ctx, `UPDATE rooms SET status='cancelled' WHERE id=?`, actor.RoomID); err != nil {
		return problem.Internal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE rounds SET status='cancelled',phase_version=phase_version+1,deadline_at=NULL,paused_at=NULL,updated_at=? WHERE room_id=? AND status NOT IN ('settled','cancelled')`, now, actor.RoomID); err != nil {
		return problem.Internal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE event_sessions SET status='cancelled',event_version=event_version+1,updated_at=? WHERE room_id=? AND status NOT IN ('completed','cancelled')`, now, actor.RoomID); err != nil {
		return problem.Internal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE control_leases SET revoked_at=? WHERE room_id=? AND revoked_at IS NULL`, now, actor.RoomID); err != nil {
		return problem.Internal(err)
	}
	return writeAudit(ctx, tx, actor.RoomID, "", owner, "room.closed-without-host", map[string]any{}, now)
}

// A repeat of one's completed departure is successful even if the first HTTP
// receipt was interrupted. It confers no access to the departed room.
func (s *Service) LeaveOrRemoveBySession(ctx context.Context, sessionID, roomID, membershipID string) error {
	actor, err := s.LoadActor(ctx, sessionID, roomID)
	if err != nil {
		m, loadErr := s.loadMembershipByID(ctx, membershipID)
		if loadErr == nil && m.RoomID == roomID && m.SessionID == sessionID && !m.Active() {
			return nil
		}
		return err
	}
	return s.LeaveOrRemoveMembership(ctx, actor, membershipID)
}

// Control authority is rechecked after entering the write transaction, so an
// already-resolved former host cannot race a handoff with a queued command.
func (s *Service) validateControllerQ(ctx context.Context, tx *sql.Tx, actor Actor) error {
	if actor.Membership != nil {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN sessions s ON s.id=m.session_id
			WHERE m.id=? AND m.room_id=? AND m.session_id=? AND m.role=? AND COALESCE(m.team_id,'')=?
			AND m.left_at IS NULL AND m.revoked_at IS NULL AND s.revoked_at IS NULL AND s.expires_at>?`,
			actor.Membership.ID, actor.RoomID, actor.SessionID, actor.Membership.Role, actor.Membership.TeamID, s.nowMS()).Scan(&active); err != nil {
			return problem.Internal(err)
		}
		if active != 1 {
			return problem.Forbidden("membership-inactive", "this member has left the room")
		}
	}
	if !actor.Has(CapManageRoom) && !actor.Has(CapHost) {
		return nil
	}
	var owner string
	if err := tx.QueryRowContext(ctx, `SELECT owner_session_id FROM rooms WHERE id=? AND status NOT IN ('archived','cancelled')`, actor.RoomID).Scan(&owner); err != nil {
		if err == sql.ErrNoRows {
			return errRoomNotFound
		}
		return problem.Internal(err)
	}
	if owner != actor.SessionID {
		return problem.Forbidden("host-changed", "the room host has changed; reload before continuing")
	}
	return nil
}

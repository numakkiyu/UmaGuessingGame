package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

// TeamController contains public coordination facts, never a credential.
type TeamController struct {
	TeamID       string
	MembershipID string
	DisplayName  string
	Generation   int64
}

func teamControlSlot(roundID, teamID string) string { return "round:" + roundID + ":team:" + teamID }

func (s *Service) teamControllerQ(ctx context.Context, q queryer, r Round, teamID string) (TeamController, error) {
	c := TeamController{TeamID: teamID}
	// Per-team history shadows legacy round-wide history, even after expiry or
	// revocation: an obsolete representative must never become active again.
	var expires int64
	var revoked, left, removed sql.NullInt64
	var role string
	err := q.QueryRowContext(ctx, `SELECT m.id,s.display_name,l.generation,l.expires_at,l.revoked_at,m.left_at,m.revoked_at,m.role FROM control_leases l
 JOIN memberships m ON m.id=l.membership_id JOIN sessions s ON s.id=m.session_id
 WHERE l.room_id=? AND l.round_id=?
 AND (l.slot=? OR (l.slot=? AND m.team_id=?))
 ORDER BY CASE WHEN l.slot=? THEN 0 ELSE 1 END,l.generation DESC LIMIT 1`, r.RoomID, r.ID, teamControlSlot(r.ID, teamID), "round:"+r.ID, teamID, teamControlSlot(r.ID, teamID)).Scan(&c.MembershipID, &c.DisplayName, &c.Generation, &expires, &revoked, &left, &removed, &role)
	if err == nil {
		if expires > s.nowMS() && !revoked.Valid && !left.Valid && !removed.Valid && (role == string(RolePlayer) || role == string(RoleTeamCaptain)) {
			return c, nil
		}
		// Fallback is a new control epoch, not a return to generation zero. This
		// also invalidates an old tab when the captain happens to regain control.
		c.Generation++
		c.MembershipID, c.DisplayName = "", ""
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c, problem.Internal(fmt.Errorf("room: team control: %w", err))
	}
	// Before a handover the captain represents the team. If they have left, the
	// first remaining member can continue instead of leaving the whole team stuck.
	err = q.QueryRowContext(ctx, `SELECT m.id,s.display_name FROM memberships m JOIN sessions s ON s.id=m.session_id
 WHERE m.room_id=? AND m.team_id=? AND m.left_at IS NULL AND m.revoked_at IS NULL AND m.role IN ('player','team-captain')
 ORDER BY CASE WHEN m.role='team-captain' THEN 0 ELSE 1 END,m.joined_at,m.id LIMIT 1`, r.RoomID, teamID).Scan(&c.MembershipID, &c.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, problem.Internal(err)
	}
	return c, nil
}

// replaceControlQ is shared by host handovers and automatic turn rotation.
// Callers hold the write transaction and have already checked current authority.
func replaceControlQ(ctx context.Context, tx *sql.Tx, r Round, membershipID, slot string, generation, now int64) (ControlLease, error) {
	id, err := newID("lse_")
	if err != nil {
		return ControlLease{}, problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE control_leases SET revoked_at=? WHERE room_id=? AND slot=? AND revoked_at IS NULL`, now, r.RoomID, slot); err != nil {
		return ControlLease{}, problem.Internal(fmt.Errorf("room: revoke control: %w", err))
	}
	expires := now + 4*60*60*1000
	if _, err := tx.ExecContext(ctx, `INSERT INTO control_leases (id,room_id,round_id,slot,membership_id,generation,issued_at,expires_at) VALUES (?,?,?,?,?,?,?,?)`, id, r.RoomID, r.ID, slot, membershipID, generation, now, expires); err != nil {
		return ControlLease{}, problem.Internal(fmt.Errorf("room: replace control: %w", err))
	}
	return ControlLease{ID: id, RoomID: r.RoomID, RoundID: r.ID, Slot: slot, MembershipID: membershipID, Generation: generation, IssuedAt: millis(now), ExpiresAt: millis(expires)}, nil
}

// rotateTeamControlsQ compares mode-defined boundaries, so a first memory flip
// or one team's early race decision cannot hand over control prematurely.
func (s *Service) rotateTeamControlsQ(ctx context.Context, tx *sql.Tx, r Round, mode modes.Mode, beforePublic, afterPublic json.RawMessage, changedSubject string, beforeState json.RawMessage, now int64) error {
	progress, ok := mode.(modes.TeamControlProgress)
	if !ok {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,team_id FROM memberships WHERE room_id=? AND team_id IS NOT NULL AND role IN ('player','team-captain') AND left_at IS NULL AND revoked_at IS NULL ORDER BY team_id,CASE WHEN role='team-captain' THEN 0 ELSE 1 END,joined_at,id`, r.RoomID)
	if err != nil {
		return problem.Internal(err)
	}
	type roster struct {
		team    string
		members []string
	}
	var teams []roster
	for rows.Next() {
		var member, team string
		if err := rows.Scan(&member, &team); err != nil {
			rows.Close()
			return problem.Internal(err)
		}
		if len(teams) == 0 || teams[len(teams)-1].team != team {
			teams = append(teams, roster{team: team})
		}
		teams[len(teams)-1].members = append(teams[len(teams)-1].members, member)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return problem.Internal(err)
	}
	for _, team := range teams {
		afterState, err := participantStateQ(ctx, tx, r.ID, team.team)
		if err != nil {
			return err
		}
		priorState := afterState
		if team.team == changedSubject {
			priorState = beforeState
		}
		before, _, err := progress.ControlTurn(beforePublic, priorState)
		if err != nil {
			return problem.Internal(err)
		}
		after, more, err := progress.ControlTurn(afterPublic, afterState)
		if err != nil {
			return problem.Internal(err)
		}
		if !more || before == after {
			continue
		}
		current, err := s.teamControllerQ(ctx, tx, r, team.team)
		if err != nil {
			return err
		}
		next := 0
		for i, id := range team.members {
			if id == current.MembershipID {
				next = (i + 1) % len(team.members)
				break
			}
		}
		if _, err := replaceControlQ(ctx, tx, r, team.members[next], teamControlSlot(r.ID, team.team), current.Generation+1, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) teamControllersQ(ctx context.Context, q interface {
	queryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, r Round) ([]TeamController, error) {
	if s.individualRoundSeats(r) {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT team_id FROM memberships WHERE room_id=? AND team_id IS NOT NULL AND role IN ('player','team-captain') AND left_at IS NULL AND revoked_at IS NULL ORDER BY team_id`, r.RoomID)
	if err != nil {
		return nil, problem.Internal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, problem.Internal(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, problem.Internal(err)
	}
	var out []TeamController
	for _, id := range ids {
		c, err := s.teamControllerQ(ctx, q, r, id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if s.cfg.Policy.Identity.DistinguishNames() && len(out) > 0 {
		members, err := s.ListMembers(ctx, r.RoomID)
		if err != nil {
			return nil, err
		}
		names := map[string]string{}
		for _, m := range members {
			names[m.ID] = m.DisplayName
		}
		for i := range out {
			if name, ok := names[out[i].MembershipID]; ok {
				out[i].DisplayName = name
			}
		}
	}
	return out, nil
}

func (s *Service) requireTeamControllerQ(ctx context.Context, q queryer, r Round, actor Actor) error {
	if s.individualRoundSeats(r) {
		return nil
	}
	if actor.Membership == nil || actor.Membership.TeamID == "" {
		return nil
	}
	c, err := s.teamControllerQ(ctx, q, r, actor.Membership.TeamID)
	if err != nil {
		return err
	}
	if c.MembershipID != actor.MembershipID() {
		return problem.Conflict("team-controller-required", "only the current team representative may submit")
	}
	return nil
}

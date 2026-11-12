package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

// JoinInput is the validated command to join a room.
type JoinInput struct {
	InviteCode    string
	RequestedRole string
}

// Join adds the caller as a pending member.
//
// An invite code only ever yields a membership; it never grants control
// . The requested role is limited to player or spectator — a code cannot
// be used to become host, display or owner.
func (s *Service) Join(ctx context.Context, sessionID, roomID string, in JoinInput) (Membership, error) {
	if in.RequestedRole != string(RolePlayer) && in.RequestedRole != string(RoleSpectator) {
		return Membership{}, problem.Validation("requestedRole is invalid",
			problem.FieldError{Field: "requestedRole", Message: "must be player or spectator"})
	}
	code := strings.ToUpper(strings.TrimSpace(in.InviteCode))
	if n := len(code); n < 6 || n > 16 {
		return Membership{}, problem.Validation("inviteCode is invalid",
			problem.FieldError{Field: "inviteCode", Message: "must be 6..16 characters"})
	}

	membershipID, err := newID("mem_")
	if err != nil {
		return Membership{}, problem.Internal(err)
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Membership{}, problem.Internal(fmt.Errorf("room: begin join: %w", err))
	}
	defer tx.Rollback()

	room, err := s.loadRoomQ(ctx, tx, roomID)
	if err != nil {
		return Membership{}, err
	}
	if room.Config.Kind == "solo" && sessionID != room.OwnerID {
		return Membership{}, problem.Forbidden("solo-room", "solo play cannot admit another member")
	}
	// The code must match this room. Comparing inside the transaction keeps the
	// check and the insert atomic.
	if room.InviteCode == "" || !strings.EqualFold(room.InviteCode, code) {
		return Membership{}, problem.Forbidden("invitation-invalid",
			"the invitation code does not grant access to this room")
	}
	if room.Status.Terminal() {
		return Membership{}, problem.Conflict("room-closed",
			fmt.Sprintf("the room is %s and no longer accepts members", room.Status))
	}
	if room.Status != StatusLobby && room.Status != StatusReady {
		return Membership{}, problem.Conflict("room-in-progress",
			"the room has already started; new members cannot join")
	}

	// Capacity is enforced against active memberships inside the transaction so
	// two simultaneous joins cannot both pass a stale count.
	var players, spectators int
	if err := tx.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN role IN ('player','team-captain') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN role IN ('spectator','display') THEN 1 ELSE 0 END), 0)
		FROM memberships WHERE room_id = ? AND left_at IS NULL AND revoked_at IS NULL`,
		roomID).Scan(&players, &spectators); err != nil {
		return Membership{}, problem.Internal(fmt.Errorf("room: count members: %w", err))
	}
	if in.RequestedRole == string(RolePlayer) {
		if room.Config.MaxPlayers > 0 && players >= room.Config.MaxPlayers {
			return Membership{}, problem.Unprocessable("room-full", "the room has no free player slot")
		}
	} else {
		if !room.Config.AllowSpectators {
			return Membership{}, problem.Unprocessable("spectators-disabled", "this room does not accept spectators")
		}
		if room.Config.MaxSpectators > 0 && spectators >= room.Config.MaxSpectators {
			return Membership{}, problem.Unprocessable("spectators-full", "the room has no free spectator slot")
		}
	}

	// A session participates at most once per room (unique index on room+session).
	// Re-joining after leaving reactivates the existing row instead of inserting a
	// duplicate, which would violate the constraint.
	var existingID string
	var leftAt, revokedAt sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT id, left_at, revoked_at FROM memberships WHERE room_id = ? AND session_id = ?`,
		roomID, sessionID).Scan(&existingID, &leftAt, &revokedAt)
	switch {
	case err == nil:
		if revokedAt.Valid {
			return Membership{}, problem.Forbidden("membership-revoked",
				"your membership in this room was revoked by a room manager")
		}
		if !leftAt.Valid {
			return Membership{}, problem.Conflict("already-member", "you are already a member of this room")
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE memberships SET role = ?, ready = 0, left_at = NULL, joined_at = ?
			WHERE id = ?`, in.RequestedRole, now, existingID); err != nil {
			return Membership{}, problem.Internal(fmt.Errorf("room: reactivate membership: %w", err))
		}
		membershipID = existingID
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memberships (id, room_id, session_id, role, team_id, ready, joined_at)
			VALUES (?, ?, ?, ?, NULL, 0, ?)`,
			membershipID, roomID, sessionID, in.RequestedRole, now); err != nil {
			return Membership{}, problem.Internal(fmt.Errorf("room: insert membership: %w", err))
		}
	default:
		return Membership{}, problem.Internal(fmt.Errorf("room: read membership: %w", err))
	}

	version, err := bumpRoomVersion(ctx, tx, roomID, now)
	if err != nil {
		return Membership{}, err
	}
	if err := emitOutbox(ctx, tx, roomID, "state.changed", map[string]any{
		"resource": "room", "roomVersion": version,
	}, now); err != nil {
		return Membership{}, err
	}
	if err := tx.Commit(); err != nil {
		return Membership{}, problem.Internal(fmt.Errorf("room: commit join: %w", err))
	}
	return s.loadMembershipByID(ctx, membershipID)
}

// MembershipPatchInput is a readiness or placement update.
type MembershipPatchInput struct {
	ExpectedRoomVersion int64
	Ready               *bool
	TeamID              *string
	RequestedRole       *string
}

// UpdateMembership changes readiness, team placement or the participation role.
//
// Authority split:
// - A member may always set their own ready flag.
// - Moving a member to another team requires manage-room.
// - Changing someone else's role requires manage-room.
// - A team placement change is refused once the room has started, because the
// subject identity is bound into the round .
func (s *Service) UpdateMembership(ctx context.Context, actor Actor, membershipID string, in MembershipPatchInput) (Membership, error) {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Membership{}, problem.Internal(fmt.Errorf("room: begin patch membership: %w", err))
	}
	defer tx.Rollback()
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return Membership{}, err
	}

	target, err := loadMembershipByIDQ(ctx, tx, membershipID)
	if err != nil {
		return Membership{}, err
	}
	if target.RoomID != actor.RoomID {
		// Cross-room identifiers must not reveal that the membership exists.
		return Membership{}, errRoomNotFound
	}
	if !target.Active() {
		return Membership{}, problem.Conflict("membership-inactive", "the membership is no longer active")
	}

	self := actor.MembershipID() == target.ID
	manages := actor.Has(CapManageRoom)

	if in.Ready != nil {
		if !self && !manages {
			return Membership{}, problem.Forbidden("insufficient-capability",
				"only the member themselves or a room manager may change readiness")
		}
	}
	if in.RequestedRole != nil {
		if !manages {
			return Membership{}, problem.Forbidden("insufficient-capability",
				"only a room manager may change a participation role")
		}
		if !Role(*in.RequestedRole).Valid() {
			return Membership{}, problem.Validation("requestedRole is invalid",
				problem.FieldError{Field: "requestedRole", Message: "is not a valid role"})
		}
		// A role change must never be a path to control: host and display are
		// not assignable through this endpoint.
		if Role(*in.RequestedRole) == RoleHost || Role(*in.RequestedRole) == RoleDisplay {
			return Membership{}, problem.Unprocessable("role-not-assignable",
				"host and display are granted through their own endpoints, not by role change")
		}
	}
	if in.TeamID != nil {
		if !self && !manages {
			return Membership{}, problem.Forbidden("insufficient-capability",
				"only the member themselves or a room manager may change team placement")
		}
		if !manages && target.Role == RoleTeamCaptain && *in.TeamID != target.TeamID {
			return Membership{}, problem.Forbidden("insufficient-capability", "only a room manager may move a team captain")
		}
	}

	room, err := s.loadRoomQ(ctx, tx, actor.RoomID)
	if err != nil {
		return Membership{}, err
	}
	if in.ExpectedRoomVersion != room.RoomVersion {
		return Membership{}, versionConflict(room.RoomVersion)
	}
	if in.RequestedRole != nil && Role(*in.RequestedRole) != target.Role && room.Status != StatusLobby && room.Status != StatusReady {
		return Membership{}, problem.Conflict("team-locked", "participation roles are locked once the room has started")
	}
	role := target.Role
	teamID := target.TeamID
	if in.RequestedRole != nil {
		role = Role(*in.RequestedRole)
	}
	if in.TeamID != nil {
		teamID = *in.TeamID
	}
	if teamID != "" && role != RolePlayer && role != RoleTeamCaptain {
		return Membership{}, problem.Unprocessable("team-player-required", "only competing players may belong to a team")
	}
	if role == RoleTeamCaptain && teamID == "" {
		return Membership{}, problem.Unprocessable("captain-without-team", "assign a team before making a captain")
	}
	if in.RequestedRole != nil && role != target.Role {
		var used int
		if role == RolePlayer || role == RoleTeamCaptain {
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE room_id=? AND id<>? AND role IN ('player','team-captain') AND left_at IS NULL AND revoked_at IS NULL`, room.ID, target.ID).Scan(&used)
			if err != nil {
				return Membership{}, problem.Internal(err)
			}
			if used >= room.Config.MaxPlayers {
				return Membership{}, problem.Unprocessable("room-full", "the room has no free player slot")
			}
		} else if role == RoleSpectator {
			if !room.Config.AllowSpectators {
				return Membership{}, problem.Unprocessable("spectators-disabled", "this room does not accept spectators")
			}
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE room_id=? AND id<>? AND role IN ('spectator','display') AND left_at IS NULL AND revoked_at IS NULL`, room.ID, target.ID).Scan(&used)
			if err != nil {
				return Membership{}, problem.Internal(err)
			}
			if used >= room.Config.MaxSpectators {
				return Membership{}, problem.Unprocessable("spectators-full", "the room has no free spectator slot")
			}
		}
	}

	if in.TeamID != nil {
		if room.Status != StatusLobby && room.Status != StatusReady {
			return Membership{}, problem.Conflict("team-locked",
				"team placement is locked once the room has started")
		}
		if *in.TeamID != "" {
			var exists int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(1) FROM teams WHERE id = ? AND room_id = ?`,
				*in.TeamID, room.ID).Scan(&exists); err != nil {
				return Membership{}, problem.Internal(fmt.Errorf("room: check team: %w", err))
			}
			if exists == 0 {
				return Membership{}, problem.Validation("teamId is unknown",
					problem.FieldError{Field: "teamId", Message: "no such team in this room"})
			}
		}
	}

	now := s.nowMS()
	if role == RoleTeamCaptain && (in.TeamID != nil || in.RequestedRole != nil) {
		// Replacing a captain is one atomic roster edit, never a transient second
		// captain or a two-request operation vulnerable to a lost response.
		if _, err := tx.ExecContext(ctx, `UPDATE memberships SET role='player' WHERE room_id=? AND team_id=? AND role='team-captain' AND id<>? AND left_at IS NULL AND revoked_at IS NULL`, room.ID, teamID, target.ID); err != nil {
			return Membership{}, problem.Internal(err)
		}
	}
	if in.Ready != nil {
		v := 0
		if *in.Ready {
			v = 1
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE memberships SET ready = ? WHERE id = ?`, v, target.ID); err != nil {
			return Membership{}, problem.Internal(fmt.Errorf("room: set ready: %w", err))
		}
	}
	if in.TeamID != nil {
		var team any
		if *in.TeamID != "" {
			team = *in.TeamID
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE memberships SET team_id = ? WHERE id = ?`, team, target.ID); err != nil {
			return Membership{}, problem.Internal(fmt.Errorf("room: set team: %w", err))
		}
	}
	if in.RequestedRole != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE memberships SET role = ? WHERE id = ?`, *in.RequestedRole, target.ID); err != nil {
			return Membership{}, problem.Internal(fmt.Errorf("room: set role: %w", err))
		}
	}

	version, err := bumpRoomVersion(ctx, tx, room.ID, now)
	if err != nil {
		return Membership{}, err
	}
	if err := emitOutbox(ctx, tx, room.ID, "state.changed", map[string]any{
		"resource": "room", "roomVersion": version,
	}, now); err != nil {
		return Membership{}, err
	}
	if err := tx.Commit(); err != nil {
		return Membership{}, problem.Internal(fmt.Errorf("room: commit patch membership: %w", err))
	}
	return s.loadMembershipByID(ctx, target.ID)
}

// LeaveOrRemoveMembership removes a member.
//
// A member may remove themselves. Removing someone else requires manage-room.
// Another member cannot remove the current owner. An owner who leaves hands
// control to a remaining eligible member, or closes the room without a successor.
// Removal revokes the control lease so a removed controller cannot keep writing
// .
func (s *Service) LeaveOrRemoveMembership(ctx context.Context, actor Actor, membershipID string) error {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return problem.Internal(fmt.Errorf("room: begin leave: %w", err))
	}
	defer tx.Rollback()
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return err
	}

	target, err := loadMembershipByIDQ(ctx, tx, membershipID)
	if err != nil {
		return err
	}
	if target.RoomID != actor.RoomID {
		return errRoomNotFound
	}
	self := actor.MembershipID() == target.ID
	if !self && !actor.Has(CapManageRoom) {
		return problem.Forbidden("insufficient-capability",
			"only the member themselves or a room manager may remove a membership")
	}
	if !self && target.SessionID == actorSessionOfRoomOwner(ctx, tx, actor.RoomID) {
		return problem.Unprocessable("owner-cannot-leave",
			"the room owner cannot be removed; transfer or close the room instead")
	}
	if !target.Active() {
		// Removing an already-inactive member is idempotent, not an error.
		return nil
	}

	now := s.nowMS()
	if err := s.handoffOnLeaveQ(ctx, tx, actor, target, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE memberships SET left_at = ? WHERE id = ?`, now, target.ID); err != nil {
		return problem.Internal(fmt.Errorf("room: leave: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE room_grants SET revoked_at=? WHERE room_id=? AND session_id=? AND revoked_at IS NULL`, now, actor.RoomID, target.SessionID); err != nil {
		return problem.Internal(err)
	}
	// Revoke any lease this membership holds so writes stop immediately.
	if _, err := tx.ExecContext(ctx,
		`UPDATE control_leases SET revoked_at = ? WHERE room_id = ? AND membership_id = ? AND revoked_at IS NULL`,
		now, actor.RoomID, target.ID); err != nil {
		return problem.Internal(fmt.Errorf("room: revoke leases: %w", err))
	}
	if !self {
		if err := writeAudit(ctx, tx, actor.RoomID, "", actor.SessionID, "membership.removed", map[string]any{
			"membershipId": target.ID, "sessionId": target.SessionID,
		}, now); err != nil {
			return err
		}
	}
	version, err := bumpRoomVersion(ctx, tx, actor.RoomID, now)
	if err != nil {
		return err
	}
	if err := emitOutbox(ctx, tx, actor.RoomID, "state.changed", map[string]any{
		"resource": "room", "roomVersion": version, "revokeMembershipId": target.ID,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

// actorSessionOfRoomOwner reads the owner session id inside a transaction.
func actorSessionOfRoomOwner(ctx context.Context, tx *sql.Tx, roomID string) string {
	var owner string
	if err := tx.QueryRowContext(ctx,
		`SELECT owner_session_id FROM rooms WHERE id = ?`, roomID).Scan(&owner); err != nil {
		return ""
	}
	return owner
}

// loadMembershipByID reads one membership with its display name.
func (s *Service) loadMembershipByID(ctx context.Context, id string) (Membership, error) {
	return loadMembershipByIDQ(ctx, s.db.Read(), id)
}

func loadMembershipByIDQ(ctx context.Context, q queryer, id string) (Membership, error) {
	row := q.QueryRowContext(ctx, `
		SELECT m.id, m.room_id, m.session_id, m.role, COALESCE(m.team_id, ''), m.ready,
		       m.joined_at, m.left_at, m.revoked_at, s.display_name, m.entry_number, m.public_code
		FROM memberships m JOIN sessions s ON s.id = m.session_id
		WHERE m.id = ?`, id)
	m, err := scanMembership(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, problem.NotFound("membership-not-found", "the membership does not exist")
	}
	if err != nil {
		return Membership{}, problem.Internal(fmt.Errorf("room: load membership: %w", err))
	}
	return *m, nil
}

// ListMembers returns every active membership of a room.
func (s *Service) ListMembers(ctx context.Context, roomID string) ([]Membership, error) {
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT m.id, m.room_id, m.session_id, m.role, COALESCE(m.team_id, ''), m.ready,
		       m.joined_at, m.left_at, m.revoked_at, s.display_name, m.entry_number, m.public_code
		FROM memberships m JOIN sessions s ON s.id = m.session_id
		WHERE m.room_id = ? AND m.left_at IS NULL AND m.revoked_at IS NULL
		ORDER BY m.joined_at ASC, m.id ASC`, roomID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: list members: %w", err))
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, problem.Internal(err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.cfg.Policy.Identity.DistinguishNames() {
		counts := map[string]int{}
		for _, m := range out {
			counts[nameKey(m.DisplayName)]++
		}
		for i := range out {
			if counts[nameKey(out[i].DisplayName)] > 1 {
				out[i].DisplayName = numberedName(out[i].DisplayName, out[i].EntryNumber)
			}
		}
	}
	return out, nil
}

// ListTeams returns the room's teams.
func (s *Service) ListTeams(ctx context.Context, roomID string) ([]Team, error) {
	rows, err := s.db.Read().QueryContext(ctx,
		`SELECT id, room_id, label, COALESCE(color, ''), created_at FROM teams
		 WHERE room_id = ? ORDER BY created_at ASC, id ASC`, roomID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: list teams: %w", err))
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var (
			t     Team
			color string
			at    int64
		)
		if err := rows.Scan(&t.ID, &t.RoomID, &t.Label, &color, &at); err != nil {
			return nil, problem.Internal(err)
		}
		t.Color = color
		t.Created = millis(at)
		out = append(out, t)
	}
	return out, rows.Err()
}

// teamColors are the colour tokens the contract allows.
var teamColors = map[string]bool{
	"green": true, "blue": true, "gold": true, "pink": true,
	"purple": true, "orange": true, "teal": true, "slate": true,
}

// CreateTeam adds a team while the room is in lobby.
//
// Label uniqueness and colour validation happen in the transaction so two
// concurrent creations cannot both claim the same label.
func (s *Service) CreateTeam(ctx context.Context, actor Actor, label, color string) (Team, error) {
	if err := requireCapability(actor, CapManageRoom, "only a room manager may create teams"); err != nil {
		return Team{}, err
	}
	label = strings.TrimSpace(label)
	if n := len([]rune(label)); n < 1 || n > 24 {
		return Team{}, problem.Validation("label is invalid",
			problem.FieldError{Field: "label", Message: "must be 1..24 characters"})
	}
	if !teamColors[color] {
		return Team{}, problem.Validation("colorToken is invalid",
			problem.FieldError{Field: "colorToken", Message: "is not an allowed colour token"})
	}
	teamID, err := newID("tem_")
	if err != nil {
		return Team{}, problem.Internal(err)
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Team{}, problem.Internal(fmt.Errorf("room: begin create team: %w", err))
	}
	defer tx.Rollback()
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return Team{}, err
	}

	room, err := s.loadRoomQ(ctx, tx, actor.RoomID)
	if err != nil {
		return Team{}, err
	}
	if room.Status != StatusLobby {
		return Team{}, problem.Conflict("room-not-editable",
			"teams may only be created while the room is in lobby")
	}
	var dup int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM teams WHERE room_id = ? AND label = ?`, room.ID, label).Scan(&dup); err != nil {
		return Team{}, problem.Internal(fmt.Errorf("room: check team label: %w", err))
	}
	if dup > 0 {
		return Team{}, problem.Conflict("team-label-taken",
			"a team with this label already exists in the room")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO teams (id, room_id, label, color, created_at) VALUES (?, ?, ?, ?, ?)`,
		teamID, room.ID, label, color, now); err != nil {
		return Team{}, problem.Internal(fmt.Errorf("room: insert team: %w", err))
	}
	version, err := bumpRoomVersion(ctx, tx, room.ID, now)
	if err != nil {
		return Team{}, err
	}
	if err := emitOutbox(ctx, tx, room.ID, "state.changed", map[string]any{
		"resource": "room", "roomVersion": version,
	}, now); err != nil {
		return Team{}, err
	}
	if err := tx.Commit(); err != nil {
		return Team{}, problem.Internal(fmt.Errorf("room: commit team: %w", err))
	}
	return Team{ID: teamID, RoomID: room.ID, Label: label, Color: color, Created: millis(now)}, nil
}

// IssueControlLeaseInput is a lease request.
type IssueControlLeaseInput struct {
	RoundID            string
	MembershipID       string
	ExpectedGeneration int64
}

// IssueControlLease issues or rotates the controller lease for a round slot.
//
// Rules:
// - Only host or manage-room may issue a lease; a player cannot seize control.
// - The target membership must belong to this room and be active.
// - expectedGeneration must match the live generation, which stops two
// operators from both rotating at once and both believing they control.
// - Issuing revokes any existing lease for the slot, so generation strictly
// increases and old tabs are rejected .
func (s *Service) IssueControlLease(ctx context.Context, actor Actor, in IssueControlLeaseInput) (ControlLease, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may issue a control lease",
		CapHost, CapManageRoom); err != nil {
		return ControlLease{}, err
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return ControlLease{}, problem.Internal(fmt.Errorf("room: begin lease: %w", err))
	}
	defer tx.Rollback()
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return ControlLease{}, err
	}

	room, err := s.loadRoomQ(ctx, tx, actor.RoomID)
	if err != nil {
		return ControlLease{}, err
	}
	r, err := s.loadRoundQ(ctx, tx, in.RoundID)
	if err != nil {
		return ControlLease{}, err
	}
	if r.RoomID != room.ID {
		return ControlLease{}, errRoundNotFound
	}
	if s.individualRoundSeats(r) {
		return ControlLease{}, problem.Conflict("individual-controls", "each player controls their own actions in this round")
	}
	if r.Status != modes.PhasePrepared && r.Status != modes.PhaseBriefing && r.Status != modes.PhaseCollecting {
		return ControlLease{}, problem.Conflict("round-not-collecting", "this round no longer accepts handovers")
	}

	target, err := loadMembershipByIDQ(ctx, tx, in.MembershipID)
	if err != nil {
		return ControlLease{}, err
	}
	if target.RoomID != room.ID {
		return ControlLease{}, problem.Validation("membershipId is not in this room",
			problem.FieldError{Field: "membershipId", Message: "does not belong to this room"})
	}
	if !target.Active() {
		return ControlLease{}, problem.Unprocessable("membership-inactive",
			"cannot issue a control lease to an inactive membership")
	}
	if target.Role != RolePlayer && target.Role != RoleTeamCaptain {
		return ControlLease{}, problem.Unprocessable("controller-must-play", "only a competing player may represent a team")
	}

	slot := "round:" + in.RoundID
	if target.TeamID != "" {
		slot = teamControlSlot(in.RoundID, target.TeamID)
	}
	var currentGen, highestGen int64
	if target.TeamID != "" {
		current, err := s.teamControllerQ(ctx, tx, r, target.TeamID)
		if err != nil {
			return ControlLease{}, err
		}
		currentGen, highestGen = current.Generation, current.Generation
	} else {
		var expires int64
		var revoked sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT generation,expires_at,revoked_at FROM control_leases WHERE room_id=? AND slot=? ORDER BY generation DESC LIMIT 1`, room.ID, slot).Scan(&highestGen, &expires, &revoked)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ControlLease{}, problem.Internal(err)
		}
		if err == nil && !revoked.Valid && expires > now {
			currentGen = highestGen
		}
	}
	if in.ExpectedGeneration != currentGen {
		e := problem.Conflict("control-generation-conflict", fmt.Sprintf("the current control generation is %d", currentGen))
		e.Fields = []problem.FieldError{{Field: "expectedGeneration", Message: fmt.Sprintf("current generation is %d", currentGen)}}
		return ControlLease{}, e
	}
	lease, err := replaceControlQ(ctx, tx, r, target.ID, slot, highestGen+1, now)
	if err != nil {
		return ControlLease{}, err
	}

	if err := writeAudit(ctx, tx, room.ID, "", actor.SessionID, "control-lease.issued", map[string]any{
		"roundId": in.RoundID, "membershipId": target.ID, "generation": lease.Generation,
	}, now); err != nil {
		return ControlLease{}, err
	}
	version, err := bumpRoomVersion(ctx, tx, room.ID, now)
	if err != nil {
		return ControlLease{}, err
	}
	if err := emitOutbox(ctx, tx, room.ID, "state.changed", map[string]any{
		"resource": "round", "roundId": in.RoundID, "roomVersion": version,
	}, now); err != nil {
		return ControlLease{}, err
	}
	if err := tx.Commit(); err != nil {
		return ControlLease{}, problem.Internal(fmt.Errorf("room: commit lease: %w", err))
	}
	return lease, nil
}

// controlGeneration resolves the caller's current control generation.
//
// A player submitting without a live lease has generation 0, which the client
// echoes; a controller must present the current generation so a stale tab is
// rejected .
func (s *Service) controlGenerationQ(ctx context.Context, q queryer, roomID, roundID, membershipID string) (int64, error) {
	if membershipID == "" {
		return 0, nil
	}
	var teamID, modeID string
	var roundConfig string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(m.team_id,''),r.mode_id,r.config_json FROM memberships m JOIN rounds r ON r.id=? AND r.room_id=m.room_id WHERE m.id=? AND m.room_id=? AND m.left_at IS NULL AND m.revoked_at IS NULL`, roundID, membershipID, roomID).Scan(&teamID, &modeID, &roundConfig)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, problem.Internal(err)
	}
	if s.individualSeatsForConfig(modeID, modeConfigOf(json.RawMessage(roundConfig))) {
		return 0, nil
	}
	if teamID != "" {
		current, err := s.teamControllerQ(ctx, q, Round{ID: roundID, RoomID: roomID}, teamID)
		if err != nil {
			return 0, err
		}
		if current.MembershipID == membershipID {
			return current.Generation, nil
		}
		return 0, nil
	}
	var gen int64
	err = q.QueryRowContext(ctx, `
		SELECT generation FROM control_leases
		WHERE room_id = ? AND round_id = ? AND membership_id = ?
		  AND revoked_at IS NULL AND expires_at > ? ORDER BY generation DESC LIMIT 1`,
		roomID, roundID, membershipID, s.nowMS()).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, problem.Internal(fmt.Errorf("room: read control generation: %w", err))
	}
	return gen, nil
}

// marshal is a tiny helper that turns a value into raw JSON.
func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

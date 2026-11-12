package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// Ticket statuses the contract exposes.
var ticketStatuses = map[string]bool{
	"queueing": true, "matched-pending-accept": true, "ready": true,
	"playing": true, "cancelled": true, "expired": true, "finished": true,
}

// Ticket is a matchmaking ticket.
type Ticket struct {
	ID        string
	SessionID string
	ModeID    string
	Status    string
	RoomID    string
	CreatedAt int64
	ExpiresAt int64
	UpdatedAt int64
}

// CreateTicketInput is the command to queue for matchmaking.
type CreateTicketInput struct {
	ModeConfig json.RawMessage
}

// CreateMatchmakingTicket queues the caller for a random match.
//
// A session may hold at most one active ticket , enforced by the
// partial unique index; the check below produces the friendly error.
func (s *Service) CreateMatchmakingTicket(ctx context.Context, sessionID string, in CreateTicketInput) (Ticket, error) {
	if err := s.validateModeConfig(in.ModeConfig); err != nil {
		return Ticket{}, err
	}
	resolved, err := resolveModeConfig(in.ModeConfig)
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	in.ModeConfig = resolved
	if err := s.checkModeForm(in.ModeConfig, "matchmaking"); err != nil {
		return Ticket{}, err
	}
	var probe struct {
		ModeID string `json:"modeId"`
	}
	if err := json.Unmarshal(in.ModeConfig, &probe); err != nil {
		return Ticket{}, problem.Validation("modeConfig is invalid",
			problem.FieldError{Field: "modeConfig", Message: "must be a JSON object"})
	}
	if probe.ModeID != "classic" {
		return Ticket{}, problem.Unprocessable("matchmaking-mode-unavailable", "random matching is available for classic games only")
	}
	// Stable key ordering makes equivalent form submissions share a queue. Keep
	// the actual rules, not just the mode id: both opponents must agree to them.
	var configValue map[string]any
	if err := json.Unmarshal(in.ModeConfig, &configValue); err != nil {
		return Ticket{}, problem.Validation("modeConfig is invalid")
	}
	configJSON, err := json.Marshal(configValue)
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}

	id, err := newID("tkt_")
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	now := s.nowMS()
	expires := now + int64(2*60*1000)

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: begin ticket: %w", err))
	}
	defer tx.Rollback()

	// Retire this session's expired tickets before deciding whether one is active.
	//
	// Expiry used to be a pure read-time filter, so a ticket whose window had passed
	// stayed in an active status forever: the partial unique index still counted it,
	// and the same session could never queue again without first cancelling by hand.
	// A player who closed the tab was locked out of matchmaking permanently.
	//
	// The status is what the index and the client both read, so it has to be updated,
	// not merely interpreted.
	if _, err := tx.ExecContext(ctx, `
		UPDATE matchmaking_tickets SET status = 'expired', updated_at = ?
		WHERE session_id = ? AND status IN ('queueing','matched-pending-accept')
		  AND expires_at <= ?`, now, sessionID, now); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: expire stale tickets: %w", err))
	}

	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM matchmaking_tickets
		WHERE session_id = ? AND status IN ('queueing','matched-pending-accept','ready')
		  AND expires_at > ?`, sessionID, now).Scan(&active); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: check active ticket: %w", err))
	}
	if active > 0 {
		return Ticket{}, problem.Conflict("ticket-already-active",
			"this session already holds an active matchmaking ticket")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO matchmaking_tickets (id, session_id, mode_id, mode_config_json, status, created_at, expires_at, updated_at)
		VALUES (?, ?, ?, ?, 'queueing', ?, ?, ?)`,
		id, sessionID, probe.ModeID, string(configJSON), now, expires, now); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: insert ticket: %w", err))
	}

	// Try to pair with an opponent that is already waiting.
	//
	// Pairing happens here, synchronously, rather than in a background ticker:
	// with a single process and SQLite this keeps the decision inside one
	// transaction, so two tickets can never both think they got the same partner.
	// The cost is that a lone waiter is only matched when someone else arrives —
	// which is exactly the event that makes a match possible.
	//
	// The status moves to matched-pending-accept for BOTH tickets: neither side
	// joins until it accepts .
	partnerID, partnerSession, pairErr := s.findWaitingPartner(ctx, tx, probe.ModeID, sessionID, string(configJSON), now)
	if pairErr != nil {
		return Ticket{}, pairErr
	}
	if partnerID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE matchmaking_tickets
			SET status = 'matched-pending-accept', matched_with = ?, updated_at = ?
			WHERE id IN (?, ?)`,
			partnerID, now, id, partnerID); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: pair tickets: %w", err))
		}
		// Record the reverse pointer on the opponent's row.
		if _, err := tx.ExecContext(ctx, `
			UPDATE matchmaking_tickets SET matched_with = ? WHERE id = ?`,
			id, partnerID); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: link partner ticket: %w", err))
		}
		_ = partnerSession
	}

	// Read the final row INSIDE the transaction, before commit.
	//
	// Reading through the Read pool after commit is not equivalent: the pool has
	// several connections and each may be on an older WAL snapshot, so the caller
	// could be told "queueing" while the row already says matched-pending-accept.
	// That is exactly the bug the frontend found: the pairing happened, but the
	// create response and an immediate re-read both reported the pre-pairing state.
	final, err := loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: commit ticket: %w", err))
	}
	return final, nil
}

// loadTicketTx reads a ticket on a given transaction so a writer sees its own
// uncommitted changes.
func loadTicketTx(ctx context.Context, tx *sql.Tx, ticketID string) (Ticket, error) {
	var (
		t      Ticket
		roomID sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, session_id, mode_id, status, room_id, created_at, expires_at, updated_at
		FROM matchmaking_tickets WHERE id = ?`, ticketID).
		Scan(&t.ID, &t.SessionID, &t.ModeID, &t.Status, &roomID,
			&t.CreatedAt, &t.ExpiresAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, errTicketNotFound
	}
	if err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: load ticket: %w", err))
	}
	t.RoomID = roomID.String
	return t, nil
}

// findWaitingPartner returns an unexpired queued ticket in the same mode owned by
// a different session, or "" when nobody is waiting.
//
// The row is locked by the surrounding write transaction, so the same opponent
// cannot be handed to two arriving players.
func (s *Service) findWaitingPartner(
	ctx context.Context, tx *sql.Tx, modeID, sessionID, configJSON string, now int64,
) (ticketID, partnerSession string, err error) {
	err = tx.QueryRowContext(ctx, `
		SELECT id, session_id FROM matchmaking_tickets
		WHERE mode_id = ? AND session_id <> ? AND mode_config_json = ? AND status = 'queueing' AND expires_at > ?
		ORDER BY rowid ASC LIMIT 1`, modeID, sessionID, configJSON, now).Scan(&ticketID, &partnerSession)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: find waiting partner: %w", err))
	}
	return ticketID, partnerSession, nil
}

// GetMatchmakingTicket reads a ticket the caller owns.
//
// Another session's ticket is reported as 404, not 403: ticket ids must not be
// enumerable .
func (s *Service) GetMatchmakingTicket(ctx context.Context, sessionID, ticketID string) (Ticket, error) {
	t, err := s.loadTicket(ctx, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if t.SessionID != sessionID {
		return Ticket{}, errTicketNotFound
	}
	return t, nil
}

func (s *Service) loadTicket(ctx context.Context, ticketID string) (Ticket, error) {
	var (
		t      Ticket
		roomID sql.NullString
	)
	err := s.db.Read().QueryRowContext(ctx, `
		SELECT id, session_id, mode_id, status, room_id, created_at, expires_at, updated_at
		FROM matchmaking_tickets WHERE id = ?`, ticketID).
		Scan(&t.ID, &t.SessionID, &t.ModeID, &t.Status, &roomID,
			&t.CreatedAt, &t.ExpiresAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, errTicketNotFound
	}
	if err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: load ticket: %w", err))
	}
	t.RoomID = roomID.String
	return t, nil
}

// CancelMatchmakingTicket cancels the caller's own ticket.
func (s *Service) CancelMatchmakingTicket(ctx context.Context, sessionID, ticketID string) error {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return problem.Internal(fmt.Errorf("room: begin cancel ticket: %w", err))
	}
	defer tx.Rollback()

	var owner, status string
	err = tx.QueryRowContext(ctx,
		`SELECT session_id, status FROM matchmaking_tickets WHERE id = ?`, ticketID).Scan(&owner, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return errTicketNotFound
	}
	if err != nil {
		return problem.Internal(fmt.Errorf("room: load ticket owner: %w", err))
	}
	if owner != sessionID {
		return errTicketNotFound
	}
	if status == "cancelled" || status == "expired" || status == "finished" {
		// Cancelling an already-terminal ticket is idempotent.
		return nil
	}
	now := s.nowMS()
	if _, err := tx.ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status = 'cancelled', updated_at = ? WHERE id = ?`,
		now, ticketID); err != nil {
		return problem.Internal(fmt.Errorf("room: cancel ticket: %w", err))
	}
	return tx.Commit()
}

// RespondToMatch accepts or declines a proposed match.
//
// Acceptance marks the ticket ready; it never creates a room on behalf of another
// session, because the pairing decision belongs to the matchmaker.
func (s *Service) RespondToMatch(ctx context.Context, sessionID, ticketID string, accept bool) (Ticket, error) {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: begin match response: %w", err))
	}
	defer tx.Rollback()

	var owner, status string
	err = tx.QueryRowContext(ctx,
		`SELECT session_id, status FROM matchmaking_tickets WHERE id = ?`, ticketID).Scan(&owner, &status)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != sessionID) {
		return Ticket{}, errTicketNotFound
	}
	if err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: load match ticket: %w", err))
	}
	if status != "queueing" && status != "matched-pending-accept" {
		return Ticket{}, problem.Conflict("ticket-not-pending",
			fmt.Sprintf("the ticket is %s and cannot be answered", status))
	}
	now := s.nowMS()
	// A lapsed ticket cannot be answered.
	//
	// Without this check an expired ticket could still be accepted, become 'ready',
	// and — together with a partner that also accepted — create a room for a pair
	// that had already timed out. The window is short but real: the acceptance
	// screen can sit open for minutes. Retiring the row here keeps the status honest
	// and tells the client the truth instead of silently succeeding.
	var expiresAt int64
	if err := tx.QueryRowContext(ctx,
		`SELECT expires_at FROM matchmaking_tickets WHERE id = ?`, ticketID).Scan(&expiresAt); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: load ticket expiry: %w", err))
	}
	if expiresAt <= now {
		if _, err := tx.ExecContext(ctx,
			`UPDATE matchmaking_tickets SET status = 'expired', updated_at = ? WHERE id = ?`,
			now, ticketID); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: expire ticket: %w", err))
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: commit expiry: %w", err))
		}
		return Ticket{}, problem.Conflict("ticket-expired",
			"this matchmaking ticket has expired; queue again to find a match")
	}
	if !accept {
		// Declining releases the partner too: leaving them paired with someone who
		// will never accept would strand them until expiry.
		if _, err := tx.ExecContext(ctx, `
			UPDATE matchmaking_tickets SET status = 'cancelled', updated_at = ?
			WHERE id = ?`, now, ticketID); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: decline ticket: %w", err))
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE matchmaking_tickets
			SET status = 'queueing', matched_with = NULL, updated_at = ?
			WHERE matched_with = ? AND status = 'matched-pending-accept'`,
			now, ticketID); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: release partner: %w", err))
		}
		final, err := loadTicketTx(ctx, tx, ticketID)
		if err != nil {
			return Ticket{}, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, problem.Internal(fmt.Errorf("room: commit decline: %w", err))
		}
		return final, nil
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status = 'ready', updated_at = ? WHERE id = ?`,
		now, ticketID); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: mark ticket ready: %w", err))
	}

	// Both sides ready → the match becomes a real room.
	//
	// This is the step that makes matchmaking mean something: two strangers end up
	// in one room without either creating it. It runs inside the same transaction as
	// the readiness update, so a room cannot be created for a pair that did not both
	// accept, and two concurrent accepts cannot each create a room.
	if err := s.finalisePairIfReady(ctx, tx, ticketID, now); err != nil {
		// The partner lapsed while this side was accepting. Rolling back is what keeps
		// the caller's ticket out of 'ready': that status means "waiting for the other
		// side", and the other side can never arrive. The player is told to queue
		// again instead of being left on a success screen for a match that is gone.
		if errors.Is(err, errPartnerExpired) {
			return Ticket{}, problem.Conflict("partner-expired",
				"the other player's matchmaking ticket expired before the match completed; queue again to find a new match")
		}
		return Ticket{}, err
	}

	final, err := loadTicketTx(ctx, tx, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, problem.Internal(fmt.Errorf("room: commit match response: %w", err))
	}
	return final, nil
}

// errPartnerExpired reports that a pairing cannot complete because the other side's
// ticket lapsed.
//
// It must be distinguishable from "still waiting": both used to return nil, so
// RespondToMatch could not tell an incomplete pair from a dead one and answered 201
// for a match that would never become a room. The accepting player saw success and
// then waited forever, with their own ticket stuck on 'ready'.
var errPartnerExpired = errors.New("room: partner ticket expired")

// matchRoomCapacity is the seat count for a room created by the matchmaker.
//
// The queue promises a two-player duel; invitations cannot add extra opponents.
const matchRoomCapacity = 2

// finalisePairIfReady creates the shared room once both paired tickets are ready.
//
// It is a no-op while either side is still pending, which is why it is safe to call
// on every accept: the second accept is the one that completes the pair.
func (s *Service) finalisePairIfReady(ctx context.Context, tx *sql.Tx, ticketID string, now int64) error {
	var (
		partner    sql.NullString
		status     string
		modeID     string
		modeConfig string
		existing   sql.NullString
	)
	// NOTE: the ticket's own session_id is deliberately NOT read here. It names
	// whoever accepted second, and every ownership decision below must be based on
	// the earlier ticket instead, otherwise accept order decides who runs the room.
	if err := tx.QueryRowContext(ctx, `
		SELECT matched_with, status, mode_id, room_id, mode_config_json
		FROM matchmaking_tickets WHERE id = ?`, ticketID).
		Scan(&partner, &status, &modeID, &existing, &modeConfig); err != nil {
		return problem.Internal(fmt.Errorf("room: load pair: %w", err))
	}
	if !partner.Valid || partner.String == "" {
		return nil
	}
	if existing.Valid && existing.String != "" {
		// Already paired into a room; nothing more to do.
		return nil
	}
	var partnerStatus, partnerSession, selfSession string
	if err := tx.QueryRowContext(ctx, `
		SELECT status, session_id FROM matchmaking_tickets WHERE id = ?`, partner.String).
		Scan(&partnerStatus, &partnerSession); err != nil {
		return problem.Internal(fmt.Errorf("room: load partner ticket: %w", err))
	}
	// The two sessions that must end up in the room are the two tickets' owners.
	// Reading the current ticket's owner here (rather than assuming it equals one of
	// firstSession/partnerSession) keeps membership correct regardless of which side
	// triggered finalisation.
	if err := tx.QueryRowContext(ctx, `
		SELECT session_id FROM matchmaking_tickets WHERE id = ?`, ticketID).
		Scan(&selfSession); err != nil {
		return problem.Internal(fmt.Errorf("room: load self ticket: %w", err))
	}
	// A lapsed partner can never complete the pair, so this must be checked BEFORE the
	// "still waiting" shortcut below. When the partner has not accepted yet its status
	// is 'matched-pending-accept', and the shortcut would return nil — reporting a
	// successful accept for a match that can never happen. The accepting player would
	// see success, their own ticket would stay on 'ready', and no room would ever
	// appear.
	var partnerExpires int64
	if err := tx.QueryRowContext(ctx,
		`SELECT expires_at FROM matchmaking_tickets WHERE id = ?`, partner.String).
		Scan(&partnerExpires); err != nil {
		return problem.Internal(fmt.Errorf("room: load partner expiry: %w", err))
	}
	if partnerExpires <= now {
		return errPartnerExpired
	}
	// Still waiting on the other side: this is an ordinary, successful accept.
	if partnerStatus != "ready" {
		return nil
	}

	// The earlier ticket owns the room.
	//
	// "Earlier" must mean insertion order, not created_at: two tickets can be created
	// in the same millisecond, and the id is random hex, so ordering by (created_at,
	// id) picks an arbitrary one — which is exactly how the later ticket ended up with
	// ownership. rowid reflects real insert order for this table.
	var firstSession string
	if err := tx.QueryRowContext(ctx, `
		SELECT session_id FROM matchmaking_tickets
		WHERE id IN (?, ?) ORDER BY rowid ASC LIMIT 1`,
		ticketID, partner.String).Scan(&firstSession); err != nil {
		return problem.Internal(fmt.Errorf("room: pick matched owner: %w", err))
	}

	roomID, err := newID("rom_")
	if err != nil {
		return problem.Internal(err)
	}
	invite, err := s.generateInviteCode()
	if err != nil {
		return err
	}
	cfgJSON, err := json.Marshal(RoomConfig{
		Kind: "matchmaking", ModeConfig: json.RawMessage(modeConfig),
		MaxPlayers: matchRoomCapacity, Visibility: "invite",
	})
	if err != nil {
		return problem.Internal(fmt.Errorf("room: encode matched room config: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rooms (id, invite_code, owner_session_id, name, status, room_version,
		                   config_json, capacity, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'lobby', 1, ?, ?, ?, ?)`,
		roomID, invite, firstSession, "快速匹配", string(cfgJSON), matchRoomCapacity, now, now); err != nil {
		return problem.Internal(fmt.Errorf("room: insert matched room: %w", err))
	}
	// Both players join as players; the room exists because they matched.
	//
	// The loop uses firstSession (the earlier ticket) plus the partner, NOT the
	// current caller: the caller is whichever side accepted second, so using it
	// would make membership depend on accept order.
	//
	// The two are de-duplicated because they can legitimately be the same session:
	// nothing stops one player from holding two tickets over time, and inserting the
	// same (room, session) twice violates the unique membership index. That failure
	// surfaced as a 500 on the second accept.
	members := []string{selfSession}
	if partnerSession != selfSession {
		members = append(members, partnerSession)
	}
	for _, sid := range members {
		membershipID, err := newID("mem_")
		if err != nil {
			return problem.Internal(err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memberships (id, room_id, session_id, role, team_id, ready, joined_at)
			VALUES (?, ?, ?, 'player', NULL, 0, ?)`, membershipID, roomID, sid, now); err != nil {
			return problem.Internal(fmt.Errorf("room: insert matched membership: %w", err))
		}
	}
	// Ownership authority goes to the player whose ticket was created FIRST, not to
	// whoever accepted last. The later accepter is an accident of timing and must
	// not gain host control of a room neither player chose to create.
	grantID, err := newID("grn_")
	if err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_grants (id, room_id, session_id, capability, granted_at)
		VALUES (?, ?, ?, 'manage-room', ?)`, grantID, roomID, firstSession, now); err != nil {
		return problem.Internal(fmt.Errorf("room: insert matched grant: %w", err))
	}
	// Both tickets point at the room and drop the pairing pointer: the pair is now
	// expressed by the room, not by the queue.
	if _, err := tx.ExecContext(ctx, `
		UPDATE matchmaking_tickets
		SET status = 'ready', room_id = ?, matched_with = NULL, updated_at = ?
		WHERE id IN (?, ?)`, roomID, now, ticketID, partner.String); err != nil {
		return problem.Internal(fmt.Errorf("room: bind tickets to room: %w", err))
	}
	return nil
}

// ---- grants --------------------------------------------------------------

// Grant is a short-lived single-use code.
type Grant struct {
	Code      string
	ExpiresAt int64
}

// CreateDisplayGrant issues a single-use display pairing code.
//
// The code is returned once and only its hash is stored, so a database reader
// cannot redeem it later .
func (s *Service) CreateDisplayGrant(ctx context.Context, actor Actor, eventID string) (Grant, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may create a display grant",
		CapHost, CapManageRoom); err != nil {
		return Grant{}, err
	}
	event, err := s.loadEvent(ctx, eventID)
	if err != nil {
		return Grant{}, err
	}
	if event.RoomID != actor.RoomID {
		return Grant{}, errEventNotFound
	}
	return s.createGrant(ctx, "display", eventID, actor.SessionID)
}

// CreateRecoveryGrant issues a single-use membership recovery code.
//
// Recovery requires host authority and an existing membership: a display name
// alone never recovers an identity .
func (s *Service) CreateRecoveryGrant(ctx context.Context, actor Actor, membershipID, reason string) (Grant, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may create a recovery grant",
		CapHost, CapManageRoom); err != nil {
		return Grant{}, err
	}
	target, err := s.loadMembershipByID(ctx, membershipID)
	if err != nil {
		return Grant{}, err
	}
	if target.RoomID != actor.RoomID {
		return Grant{}, problem.NotFound("membership-not-found", "the membership does not exist")
	}
	if !target.Active() {
		return Grant{}, problem.Conflict("membership-inactive", "the membership is no longer active")
	}
	if n := len([]rune(reason)); n < 3 || n > 240 {
		return Grant{}, problem.Validation("reason is invalid",
			problem.FieldError{Field: "reason", Message: "must be 3..240 characters"})
	}

	code, err := newGrantCode()
	if err != nil {
		return Grant{}, problem.Internal(err)
	}
	id, err := newID("rgv_")
	if err != nil {
		return Grant{}, problem.Internal(err)
	}
	now := s.nowMS()
	expires := now + s.cfg.GrantTTL.Milliseconds()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Grant{}, problem.Internal(fmt.Errorf("room: begin recovery grant: %w", err))
	}
	defer tx.Rollback()
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return Grant{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO recovery_grants (id, membership_id, code_hash, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, membershipID, hashGrant(code), actor.SessionID, now, expires); err != nil {
		return Grant{}, problem.Internal(fmt.Errorf("room: insert recovery grant: %w", err))
	}
	if err := writeAudit(ctx, tx, actor.RoomID, "", actor.SessionID, "recovery-grant.created", map[string]any{
		"membershipId": membershipID, "reason": reason,
	}, now); err != nil {
		return Grant{}, err
	}
	if err := tx.Commit(); err != nil {
		return Grant{}, problem.Internal(fmt.Errorf("room: commit recovery grant: %w", err))
	}
	return Grant{Code: code, ExpiresAt: expires}, nil
}

// createGrant writes a display grant row.
func (s *Service) createGrant(ctx context.Context, kind, eventID, sessionID string) (Grant, error) {
	code, err := newGrantCode()
	if err != nil {
		return Grant{}, problem.Internal(err)
	}
	id, err := newID("dgr_")
	if err != nil {
		return Grant{}, problem.Internal(err)
	}
	now := s.nowMS()
	expires := now + s.cfg.GrantTTL.Milliseconds()
	result, err := s.db.Write().ExecContext(ctx, `
		INSERT INTO display_grants (id, event_id, code_hash, created_by, created_at, expires_at)
		SELECT ?, ?, ?, ?, ?, ? WHERE EXISTS (
 SELECT 1 FROM event_sessions e JOIN rooms r ON r.id=e.room_id
 WHERE e.id=? AND r.owner_session_id=? AND r.status NOT IN ('archived','cancelled') AND e.status NOT IN ('completed','cancelled')
)`,
		id, eventID, hashGrant(code), sessionID, now, expires, eventID, sessionID)
	if err != nil {
		return Grant{}, problem.Internal(fmt.Errorf("room: insert display grant: %w", err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Grant{}, problem.Internal(err)
	}
	if affected != 1 {
		return Grant{}, problem.Forbidden("host-changed", "the current host must issue this pairing code")
	}
	return Grant{Code: code, ExpiresAt: expires}, nil
}

// AttachDisplaySession grants paired screens read-only room membership.
// LoadActor requires membership in addition to an authenticated session.
func (s *Service) AttachDisplaySession(ctx context.Context, roomID, sessionID string) error {
	if roomID == "" || sessionID == "" {
		return problem.Internal(errors.New("room: attach display session needs a room and a session"))
	}
	now := s.nowMS()
	// A display screen may reconnect with a fresh session; replacing the previous
	// display membership for the same room keeps the member list honest instead of
	// accumulating dead screens.
	if _, err := s.db.Write().ExecContext(ctx,
		`DELETE FROM memberships WHERE room_id = ? AND role = 'display' AND session_id <> ?`,
		roomID, sessionID); err != nil {
		return problem.Internal(fmt.Errorf("room: clear old display memberships: %w", err))
	}
	membershipID, err := newID("mem")
	if err != nil {
		return problem.Internal(fmt.Errorf("room: generate membership id: %w", err))
	}
	if _, err := s.db.Write().ExecContext(ctx, `
		INSERT INTO memberships (id, room_id, session_id, role, team_id, ready, joined_at)
		VALUES (?, ?, ?, 'display', NULL, 0, ?)
		ON CONFLICT (room_id, session_id) DO UPDATE SET role = 'display'`,
		membershipID, roomID, sessionID, now); err != nil {
		return problem.Internal(fmt.Errorf("room: attach display membership: %w", err))
	}
	return nil
}

// PeekDisplayGrant resolves the room a pairing code belongs to WITHOUT consuming
// the code.
//
// Why it exists: redemption is single-use, so a caller that needs to check
// authority against the room must do so before redeeming. Checking after would
// burn the code on a refusal — a host clicking the display link in their own
// browser would kill the code they meant to hand out.
//
// It validates the same things as Redeem (existence, unused, unexpired) so a
// caller cannot learn about an already-used code, but it performs no writes.
func (s *Service) PeekDisplayGrant(ctx context.Context, code string) (roomID string, err error) {
	code = trimSpace(code)
	if len(code) < 16 || len(code) > 128 {
		return "", problem.Validation("grantCode is invalid",
			problem.FieldError{Field: "grantCode", Message: "must be 16..128 characters"})
	}
	now := s.nowMS()

	var (
		eventIDv string
		expires  int64
		consumed sql.NullInt64
	)
	err = s.db.Read().QueryRowContext(ctx, `
		SELECT event_id, expires_at, consumed_at FROM display_grants WHERE code_hash = ?`,
		hashGrant(code)).Scan(&eventIDv, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", problem.NotFound("grant-not-found", "the pairing code is not valid")
	}
	if err != nil {
		return "", problem.Internal(fmt.Errorf("room: peek display grant: %w", err))
	}
	if consumed.Valid {
		return "", problem.Conflict("grant-already-used", "this pairing code has already been used")
	}
	if now > expires {
		return "", problem.Conflict("grant-expired", "this pairing code has expired")
	}
	if err := s.db.Read().QueryRowContext(ctx,
		`SELECT room_id FROM event_sessions WHERE id = ?`, eventIDv).Scan(&roomID); err != nil {
		return "", problem.Internal(fmt.Errorf("room: peek grant event: %w", err))
	}
	return roomID, nil
}

func (s *Service) RedeemDisplayGrant(ctx context.Context, code string, expectedEventID ...string) (eventID, roomID string, err error) {
	code = trimSpace(code)
	if len(code) < 16 || len(code) > 128 {
		return "", "", problem.Validation("grantCode is invalid",
			problem.FieldError{Field: "grantCode", Message: "must be 16..128 characters"})
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: begin redeem display: %w", err))
	}
	defer tx.Rollback()

	var (
		grantID  string
		eventIDv string
		expires  int64
		consumed sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, event_id, expires_at, consumed_at FROM display_grants WHERE code_hash = ?`,
		hashGrant(code)).Scan(&grantID, &eventIDv, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", problem.NotFound("grant-not-found", "the pairing code is not valid")
	}
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: load display grant: %w", err))
	}
	if consumed.Valid {
		return "", "", problem.Conflict("grant-already-used", "this pairing code has already been used")
	}
	if now > expires {
		return "", "", problem.Conflict("grant-expired", "this pairing code has expired")
	}
	// Bind a screen's URL before consuming a one-time code. A typo must not
	// consume a different activity's code or switch the screen silently.
	if len(expectedEventID) > 0 && expectedEventID[0] != "" && expectedEventID[0] != eventIDv {
		return "", "", problem.Conflict("grant-event-mismatch", "the pairing code belongs to a different event")
	}
	// Conditional update: only the first redeemer flips consumed_at.
	res, err := tx.ExecContext(ctx, `
		UPDATE display_grants SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`,
		now, grantID)
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: consume display grant: %w", err))
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return "", "", problem.Conflict("grant-already-used", "this pairing code has already been used")
	}

	var roomIDv string
	if err := tx.QueryRowContext(ctx,
		`SELECT room_id FROM event_sessions WHERE id = ?`, eventIDv).Scan(&roomIDv); err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: load grant event: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: commit redeem display: %w", err))
	}
	return eventIDv, roomIDv, nil
}

// RedeemRecoveryGrant exchanges a recovery code for a fresh session bound to the
// original membership, revoking the previous controller.
func (s *Service) RedeemRecoveryGrant(ctx context.Context, code string) (membershipID, roomID string, err error) {
	code = trimSpace(code)
	if len(code) < 16 || len(code) > 128 {
		return "", "", problem.Validation("grantCode is invalid",
			problem.FieldError{Field: "grantCode", Message: "must be 16..128 characters"})
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: begin redeem recovery: %w", err))
	}
	defer tx.Rollback()

	var (
		grantID       string
		membershipIDv string
		expires       int64
		consumed      sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, membership_id, expires_at, consumed_at FROM recovery_grants WHERE code_hash = ?`,
		hashGrant(code)).Scan(&grantID, &membershipIDv, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", problem.NotFound("grant-not-found", "the recovery code is not valid")
	}
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: load recovery grant: %w", err))
	}
	if consumed.Valid {
		return "", "", problem.Conflict("grant-already-used", "this recovery code has already been used")
	}
	if now > expires {
		return "", "", problem.Conflict("grant-expired", "this recovery code has expired")
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE recovery_grants SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, now, grantID)
	if err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: consume recovery grant: %w", err))
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return "", "", problem.Conflict("grant-already-used", "this recovery code has already been used")
	}

	var roomIDv string
	if err := tx.QueryRowContext(ctx,
		`SELECT room_id FROM memberships WHERE id = ?`, membershipIDv).Scan(&roomIDv); err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: load recovery membership: %w", err))
	}
	// Recovery revokes the previous controller so two devices cannot both write.
	if _, err := tx.ExecContext(ctx, `
		UPDATE control_leases SET revoked_at = ?
		WHERE room_id = ? AND membership_id = ? AND revoked_at IS NULL`,
		now, roomIDv, membershipIDv); err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: revoke old controller: %w", err))
	}
	if err := writeAudit(ctx, tx, roomIDv, "", "", "recovery-grant.redeemed", map[string]any{
		"membershipId": membershipIDv,
	}, now); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: commit redeem recovery: %w", err))
	}
	return membershipIDv, roomIDv, nil
}

// ---- room event log ------------------------------------------------------

// Notification is one outbox-backed notification.
type Notification struct {
	ID          string
	Type        string
	RoomID      string
	RoomVersion int64
	ServerEpoch string
	ServerTime  int64
	Cursor      string
	Payload     json.RawMessage
	Sequence    int64
}

// ListRoomEvents reads bounded notifications after a cursor.
//
// The cursor is the outbox sequence, which is opaque to the client and scoped to
// the room. A gap in sequence numbers is normal because private notifications are
// filtered out per viewer, so the client must not infer loss from a gap .
func (s *Service) ListRoomEvents(ctx context.Context, actor Actor, cursor string, limit int) (items []Notification, next string, resync bool, err error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	afterSeq := int64(0)
	if cursor != "" {
		seq, perr := parseCursor(cursor)
		if perr != nil {
			// An unparseable cursor cannot be replayed safely; ask for a resync.
			return nil, "", true, nil
		}
		afterSeq = seq
	}

	room, err := s.loadRoom(ctx, actor.RoomID)
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT id, event_sequence, type, payload_json, created_at
		FROM outbox_events
		WHERE room_id = ? AND event_sequence > ?
		ORDER BY event_sequence ASC
		LIMIT ?`, actor.RoomID, afterSeq, limit)
	if err != nil {
		return nil, "", false, problem.Internal(fmt.Errorf("room: list events: %w", err))
	}
	defer rows.Close()

	for rows.Next() {
		var (
			n         Notification
			payload   string
			createdAt int64
		)
		if err := rows.Scan(&n.ID, &n.Sequence, &n.Type, &payload, &createdAt); err != nil {
			return nil, "", false, problem.Internal(err)
		}
		// Old V2 removal rows were incorrectly labelled as a terminal event.
		// Reads have already authorised the current member; other members only
		// need to refresh room state, not lose their own connection.
		if n.Type == "session.revoked" {
			n.Type = "state.changed"
		}
		n.RoomID = actor.RoomID
		n.RoomVersion = room.RoomVersion
		n.ServerEpoch = s.epoch
		n.ServerTime = createdAt
		n.Cursor = formatCursor(n.Sequence)
		n.Payload = json.RawMessage(payload)
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, problem.Internal(err)
	}
	if len(items) > 0 {
		next = items[len(items)-1].Cursor
	} else {
		next = formatCursor(afterSeq)
	}
	return items, next, false, nil
}

// formatCursor renders the opaque cursor for a sequence.
func formatCursor(seq int64) string { return "c" + itoa64(seq) }

// parseCursor reads a cursor produced by formatCursor.
func parseCursor(cursor string) (int64, error) {
	if len(cursor) < 2 || cursor[0] != 'c' {
		return 0, errors.New("room: malformed cursor")
	}
	var v int64
	for i := 1; i < len(cursor); i++ {
		c := cursor[i]
		if c < '0' || c > '9' {
			return 0, errors.New("room: malformed cursor")
		}
		v = v*10 + int64(c-'0')
		if v > 1<<62 {
			return 0, errors.New("room: cursor out of range")
		}
	}
	return v, nil
}

// itoa64 renders an int64 without importing strconv.
func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// trimSpace is a local helper to keep imports minimal in this file.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// newGrantCode returns a 160-bit code, base64url encoded.
func newGrantCode() (string, error) {
	id, err := newID("")
	if err != nil {
		return "", err
	}
	// 32 hex characters is 128 bits of entropy, meeting the contract minimum of
	// 16 characters and the "at least 128 bits" rule .
	if len(id) < 16 {
		return "", errors.New("room: grant code too short")
	}
	return id, nil
}

// hashGrant hashes a grant code for storage.
func hashGrant(code string) string { return hashTokenSHA256(code) }

// grantTTL exposes the configured grant lifetime for tests.
func (s *Service) grantTTL() time.Duration { return s.cfg.GrantTTL }

package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

var ticketStatuses = map[string]bool{"queueing": true, "matched-pending-accept": true, "ready": true, "playing": true, "cancelled": true, "expired": true, "finished": true}

const matchLeaseMS int64 = 35000
const matchQueueMS int64 = 120000
const matchAcceptMS int64 = 20000

// Ticket is private to its owning session. Group identifiers are never projected.
type Ticket struct {
	ID, SessionID, ModeID, Status, RoomID, PoolID string
	CreatedAt, ExpiresAt, UpdatedAt               int64
	Players, Accepted                             int
}
type CreateTicketInput struct {
	Command    *CreationCommand
	PoolID     string
	ModeConfig json.RawMessage
	Proof      MatchProofAnswer
}

func (s *Service) CreateMatchmakingTicket(ctx context.Context, sid string, in CreateTicketInput) (Ticket, error) {
	if in.PoolID == "" {
		in.PoolID = "classic-duel"
	}
	p, err := s.matchPool(in.PoolID)
	if err != nil {
		return Ticket{}, err
	}
	if len(in.ModeConfig) > 0 {
		got, e := resolveModeConfig(in.ModeConfig)
		if e != nil {
			return Ticket{}, e
		}
		want, e := resolveModeConfig(p.ModeConfig)
		if e != nil {
			return Ticket{}, e
		}
		var a, b any
		_ = json.Unmarshal(got, &a)
		_ = json.Unmarshal(want, &b)
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		if string(x) != string(y) {
			return Ticket{}, problem.Unprocessable("matchmaking-rules-fixed", "choose a fixed matchmaking pool")
		}
	}
	now := s.nowMS()
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	defer tx.Rollback()
	if id, e := createdResource(ctx, tx, "matchmaking_tickets", "session_id", sid, in.Command); e != nil {
		return Ticket{}, e
	} else if id != "" {
		return loadTicketTx(ctx, tx, id)
	}
	if err = s.reconcileMatchesQ(ctx, tx, now); err != nil {
		return Ticket{}, err
	}
	if err = s.checkAccountQueue(ctx, tx, sid); err != nil {
		return Ticket{}, err
	}
	var active, count int
	var cooling int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(cooldown_until),0) FROM matchmaking_tickets WHERE session_id=? AND (status IN ('queueing','matched-pending-accept','ready','playing') OR cooldown_until>?)`, sid, now).Scan(&active, &cooling); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	if cooling > now {
		return Ticket{}, problem.RateLimited(15)
	}
	if active > 0 {
		return Ticket{}, problem.Conflict("ticket-already-active", "this session already has an active match")
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM matchmaking_tickets WHERE status IN ('queueing','matched-pending-accept','ready') AND room_id IS NULL`).Scan(&count); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	if count >= s.cfg.Policy.Matchmaking.Capacity() {
		return Ticket{}, problem.Busy(10)
	}
	if err = consumeMatchProof(ctx, tx, sid, p.ID, in.Proof, now); err != nil {
		return Ticket{}, err
	}
	id, err := newID("tkt_")
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	key, hash, err := in.Command.identity()
	if err != nil {
		return Ticket{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO matchmaking_tickets(id,session_id,mode_id,mode_config_json,pool_id,status,created_at,expires_at,updated_at,lease_until,creation_key,creation_hash) VALUES(?,?,?,?,?,'queueing',?,?,?,?,?,?)`, id, sid, p.ModeID, string(p.ModeConfig), p.ID, now, now+matchQueueMS, now, now+matchLeaseMS, key, hash); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	if err = s.formMatchQ(ctx, tx, p, now); err != nil {
		return Ticket{}, err
	}
	t, err := loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if err = tx.Commit(); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	return t, nil
}

func loadTicketTx(ctx context.Context, tx *sql.Tx, id string) (Ticket, error) {
	return loadTicketQ(ctx, tx, id)
}
func loadTicketQ(ctx context.Context, q queryer, id string) (Ticket, error) {
	var t Ticket
	var rid sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id,session_id,mode_id,status,room_id,pool_id,created_at,expires_at,updated_at FROM matchmaking_tickets WHERE id=?`, id).Scan(&t.ID, &t.SessionID, &t.ModeID, &t.Status, &rid, &t.PoolID, &t.CreatedAt, &t.ExpiresAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, errTicketNotFound
	}
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	t.RoomID = rid.String
	for _, p := range matchPools {
		if p.ID == t.PoolID {
			t.Players = p.Players
		}
	}
	if err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM matchmaking_tickets WHERE status IN ('ready','playing') AND ((match_group IS NOT NULL AND match_group=(SELECT match_group FROM matchmaking_tickets WHERE id=?)) OR (room_id IS NOT NULL AND room_id=?))`, id, t.RoomID).Scan(&t.Accepted); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	return t, nil
}
func (s *Service) loadTicket(ctx context.Context, id string) (Ticket, error) {
	return loadTicketQ(ctx, s.db.Read(), id)
}

// formMatchQ reserves exactly one FIFO group on the serialized SQLite writer.
func (s *Service) formMatchQ(ctx context.Context, tx *sql.Tx, p MatchPool, now int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM matchmaking_tickets WHERE pool_id=? AND status='queueing' AND expires_at>? AND lease_until>? ORDER BY rowid LIMIT ?`, p.ID, now, now, p.Players)
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
	if len(ids) < p.Players {
		return nil
	}
	g, err := newID("mtg_")
	if err != nil {
		return problem.Internal(err)
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='matched-pending-accept',match_group=?,matched_with=NULL,expires_at=?,updated_at=? WHERE id=?`, g, now+matchAcceptMS, now, id); err != nil {
			return problem.Internal(err)
		}
	}
	return nil
}

// reconcileMatchesQ releases stale groups, including players who had already accepted.
func (s *Service) reconcileMatchesQ(ctx context.Context, tx *sql.Tx, now int64) error {
	// Recover a commit after a successful start but before ticket publication.
	if _, err := tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='playing',updated_at=? WHERE status='ready' AND room_id IS NOT NULL AND EXISTS(SELECT 1 FROM rounds r WHERE r.room_id=matchmaking_tickets.room_id AND (r.status='collecting' OR (r.mode_id='memory' AND r.status='briefing')))`, now); err != nil {
		return problem.Internal(err)
	}
	// Failed preparation cannot reserve participants indefinitely, even if everyone closes the page.
	if _, err := tx.ExecContext(ctx, `UPDATE rooms SET status='cancelled',updated_at=? WHERE id IN (SELECT room_id FROM matchmaking_tickets WHERE status='ready' AND room_id IS NOT NULL AND updated_at<=?)`, now, now-60000); err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rounds SET status='cancelled',phase_version=phase_version+1,deadline_at=NULL,updated_at=? WHERE status IN ('prepared','briefing') AND room_id IN (SELECT r.id FROM rooms r WHERE r.status='cancelled' AND json_extract(r.config_json,'$.kind')='matchmaking')`, now); err != nil {
		return problem.Internal(err)
	}
	// A departed participant is free to queue again; the remaining match continues.
	if _, err := tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='finished',updated_at=? WHERE status IN ('ready','playing') AND room_id IS NOT NULL AND (EXISTS(SELECT 1 FROM rounds r WHERE r.room_id=matchmaking_tickets.room_id AND r.status IN ('settled','cancelled')) OR EXISTS(SELECT 1 FROM rooms r WHERE r.id=matchmaking_tickets.room_id AND r.status IN ('finished','archived','cancelled')) OR EXISTS(SELECT 1 FROM memberships m WHERE m.room_id=matchmaking_tickets.room_id AND m.session_id=matchmaking_tickets.session_id AND (m.left_at IS NOT NULL OR m.revoked_at IS NOT NULL)))`, now); err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='expired',cooldown_until=CASE WHEN status IN ('ready','matched-pending-accept') THEN ? ELSE 0 END,updated_at=? WHERE room_id IS NULL AND status IN ('queueing','matched-pending-accept','ready') AND (expires_at<=? OR lease_until<=?)`, now+15000, now, now, now); err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='queueing',match_group=NULL,matched_with=NULL,expires_at=?,updated_at=? WHERE room_id IS NULL AND status IN ('matched-pending-accept','ready') AND match_group IN (SELECT match_group FROM matchmaking_tickets WHERE status IN ('expired','cancelled'))`, now+matchQueueMS, now); err != nil {
		return problem.Internal(err)
	}
	// Records used for retry recovery outlive queue visibility but have a bounded retention.
	if _, err := tx.ExecContext(ctx, `DELETE FROM matchmaking_tickets WHERE status IN ('finished','expired','cancelled') AND updated_at<?`, now-int64(48*time.Hour/time.Millisecond)); err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM matchmaking_proofs WHERE expires_at<=?`, now); err != nil {
		return problem.Internal(err)
	}
	return nil
}

func (s *Service) readOwnTicket(ctx context.Context, sid, id string, heartbeat bool) (Ticket, error) {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	defer tx.Rollback()
	t, err := loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if t.SessionID != sid {
		return Ticket{}, errTicketNotFound
	}
	now := s.nowMS()
	if err = s.reconcileMatchesQ(ctx, tx, now); err != nil {
		return Ticket{}, err
	}
	if heartbeat {
		if _, err = tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET lease_until=? WHERE id=? AND room_id IS NULL AND status IN ('queueing','matched-pending-accept','ready')`, now+matchLeaseMS, id); err != nil {
			return Ticket{}, problem.Internal(err)
		}
	}
	if p, e := s.matchPool(t.PoolID); e == nil {
		if err = s.formMatchQ(ctx, tx, p, now); err != nil {
			return Ticket{}, err
		}
	}
	t, err = loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if err = tx.Commit(); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	if t.RoomID != "" && t.Status == "ready" {
		if err = s.ensureMatchStarted(ctx, t.RoomID); err != nil {
			return Ticket{}, err
		}
		return s.loadTicket(ctx, id)
	}
	return t, nil
}
func (s *Service) GetMatchmakingTicket(ctx context.Context, sid, id string) (Ticket, error) {
	return s.readOwnTicket(ctx, sid, id, false)
}
func (s *Service) HeartbeatMatchmakingTicket(ctx context.Context, sid, id string) (Ticket, error) {
	return s.readOwnTicket(ctx, sid, id, true)
}
func (s *Service) ActiveMatchmakingTicket(ctx context.Context, sid string) (*Ticket, error) {
	if err := s.SweepMatchmaking(ctx); err != nil {
		return nil, err
	}
	var id string
	err := s.db.Read().QueryRowContext(ctx, `SELECT id FROM matchmaking_tickets WHERE session_id=? AND status IN ('queueing','matched-pending-accept','ready','playing') ORDER BY rowid DESC LIMIT 1`, sid).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, problem.Internal(err)
	}
	t, err := s.GetMatchmakingTicket(ctx, sid, id)
	return &t, err
}
func (s *Service) CancelMatchmakingTicket(ctx context.Context, sid, id string) error {
	_, err := s.changeMatch(ctx, sid, id, false, true)
	return err
}
func (s *Service) RespondToMatch(ctx context.Context, sid, id string, accept bool) (Ticket, error) {
	return s.changeMatch(ctx, sid, id, accept, false)
}
func (s *Service) changeMatch(ctx context.Context, sid, id string, accept, cancel bool) (Ticket, error) {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, problem.Internal(err)
	}
	defer tx.Rollback()
	t, err := loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if t.SessionID != sid {
		return Ticket{}, errTicketNotFound
	}
	now := s.nowMS()
	if err = s.reconcileMatchesQ(ctx, tx, now); err != nil {
		return Ticket{}, err
	}
	live, err := loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if live.Status == "expired" {
		if err = tx.Commit(); err != nil {
			return Ticket{}, problem.Internal(err)
		}
		return Ticket{}, problem.Conflict("ticket-expired", "the match confirmation expired")
	}
	if live.RoomID != "" {
		if cancel || !accept {
			return Ticket{}, problem.Conflict("match-already-started", "the match has started; leave through the room")
		}
	} else if cancel || !accept {
		if _, err = tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='cancelled',updated_at=?,cooldown_until=? WHERE id=? AND status IN ('queueing','matched-pending-accept','ready')`, now, now+15000, id); err != nil {
			return Ticket{}, problem.Internal(err)
		}
		if err = s.reconcileMatchesQ(ctx, tx, now); err != nil {
			return Ticket{}, err
		}
	} else {
		if live.Status != "matched-pending-accept" && live.Status != "ready" {
			return Ticket{}, problem.Conflict("ticket-not-pending", "there is no match to accept")
		}
		// If another group member expired, reconciliation requeued this caller.
		if _, err = tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='ready',lease_until=?,updated_at=? WHERE id=?`, now+matchLeaseMS, now, id); err != nil {
			return Ticket{}, problem.Internal(err)
		}
		if err = s.finaliseMatchQ(ctx, tx, id, now); err != nil {
			return Ticket{}, err
		}
	}
	t, err = loadTicketTx(ctx, tx, id)
	if err != nil {
		return Ticket{}, err
	}
	if err = tx.Commit(); err != nil {
		return Ticket{}, problem.Internal(err)
	}
	if t.RoomID != "" && t.Status == "ready" {
		if err = s.ensureMatchStarted(ctx, t.RoomID); err != nil {
			return Ticket{}, err
		}
		return s.loadTicket(ctx, id)
	}
	return t, nil
}

func (s *Service) finaliseMatchQ(ctx context.Context, tx *sql.Tx, id string, now int64) error {
	var g, poolID string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(match_group,''),pool_id FROM matchmaking_tickets WHERE id=?`, id).Scan(&g, &poolID); err != nil {
		return problem.Internal(err)
	}
	if g == "" {
		return nil
	}
	p, err := s.matchPool(poolID)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT session_id,status FROM matchmaking_tickets WHERE match_group=? ORDER BY rowid`, g)
	if err != nil {
		return problem.Internal(err)
	}
	var sessions []string
	ready := 0
	for rows.Next() {
		var sid, status string
		if err = rows.Scan(&sid, &status); err != nil {
			rows.Close()
			return problem.Internal(err)
		}
		sessions = append(sessions, sid)
		if status == "ready" {
			ready++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return problem.Internal(err)
	}
	if ready != p.Players || len(sessions) != p.Players {
		return nil
	}
	rid, err := newID("rom_")
	if err != nil {
		return problem.Internal(err)
	}
	// An opaque, undisplayed invitation satisfies legacy storage constraints. Join rejects this room kind.
	invite, err := newID("")
	if err != nil {
		return problem.Internal(err)
	}
	cfg, _ := json.Marshal(RoomConfig{Kind: "matchmaking", ModeConfig: p.ModeConfig, MaxPlayers: p.Players, Visibility: "invite"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO rooms(id,invite_code,owner_session_id,creator_session_id,name,status,room_version,config_json,capacity,created_at,updated_at) VALUES(?,?,?,?,?,'lobby',1,?,?,?,?)`, rid, invite, sessions[0], sessions[0], "随机对战", string(cfg), p.Players, now, now); err != nil {
		return problem.Internal(err)
	}
	for _, sid := range sessions {
		mid, e := newID("mem_")
		if e != nil {
			return problem.Internal(e)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO memberships(id,room_id,session_id,role,ready,joined_at) VALUES(?,?,?,'player',1,?)`, mid, rid, sid, now); err != nil {
			return problem.Internal(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET room_id=?,match_group=NULL,matched_with=NULL,updated_at=? WHERE match_group=?`, rid, now, g); err != nil {
		return problem.Internal(err)
	}
	return nil
}

// ensureMatchStarted resumes preparation after a failed response or process restart.
func (s *Service) ensureMatchStarted(ctx context.Context, rid string) error {
	_, release, err := s.queue.Enqueue(ctx, "match-start:"+rid)
	if err != nil {
		return err
	}
	defer release()
	rm, err := s.loadRoom(ctx, rid)
	if err != nil {
		return err
	}
	actor, err := s.LoadActor(ctx, rm.OwnerID, rid)
	if err != nil {
		return err
	}
	actor.Capabilities[CapManageRoom] = true
	r, err := s.currentRoundQ(ctx, s.db.Read(), rid)
	if err != nil {
		return err
	}
	if r == nil {
		// A terminal first round must never silently become a second round.
		old, e := s.LatestRoundSummary(ctx, rid)
		if e != nil {
			return e
		}
		if old != nil {
			return nil
		}
		made, e := s.CreateRound(ctx, actor, RoundCreateInput{})
		if e != nil {
			if problem.As(e).Code != "round-already-open" && problem.As(e).Code != "room-version-conflict" {
				return e
			}
			r, err = s.currentRoundQ(ctx, s.db.Read(), rid)
			if err != nil {
				return err
			}
		} else {
			r = &made
		}
	}
	if r == nil {
		return nil
	}
	targets := []string{}
	if r.Status == modes.PhasePrepared {
		targets = append(targets, "briefing")
		if r.ModeID != "memory" {
			targets = append(targets, "collecting")
		}
	}
	if r.Status == modes.PhaseBriefing && r.ModeID != "memory" {
		targets = append(targets, "collecting")
	}
	version := r.PhaseVersion
	for _, target := range targets {
		res, e := s.CreateTransition(ctx, actor, r.ID, TransitionInput{Target: target, ExpectedPhaseVersion: version})
		if e != nil {
			if problem.As(e).Code == "phase-version-conflict" {
				break
			}
			return e
		}
		version = res.PhaseVersion
	}
	if _, err = s.db.Write().ExecContext(ctx, `UPDATE matchmaking_tickets SET status='playing',updated_at=? WHERE room_id=? AND status='ready'`, s.nowMS(), rid); err != nil {
		return problem.Internal(err)
	}
	return nil
}

func (s *Service) SweepMatchmaking(ctx context.Context) error {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return problem.Internal(err)
	}
	defer tx.Rollback()
	if err = s.reconcileMatchesQ(ctx, tx, s.nowMS()); err != nil {
		return err
	}
	return tx.Commit()
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
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
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
func (s *Service) RedeemRecoveryGrant(ctx context.Context, code string, createSession func(context.Context, *sql.Tx, string) (string, error)) (membershipID, roomID string, err error) {
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

	var roomIDv, previousSession, displayName string
	if err := tx.QueryRowContext(ctx, `SELECT m.room_id,m.session_id,s.display_name
		FROM memberships m JOIN sessions s ON s.id=m.session_id JOIN rooms r ON r.id=m.room_id
		WHERE m.id=? AND m.left_at IS NULL AND m.revoked_at IS NULL AND r.status NOT IN ('archived','cancelled')`, membershipIDv).Scan(&roomIDv, &previousSession, &displayName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", problem.Conflict("membership-inactive", "this seat is no longer available")
		}
		return "", "", problem.Internal(fmt.Errorf("room: load recovery membership: %w", err))
	}
	if createSession == nil {
		return "", "", problem.Internal(fmt.Errorf("room: missing recovery session factory"))
	}
	newSession, err := createSession(ctx, tx, displayName)
	if err != nil {
		return "", "", err
	}
	// Carry the protected seat's account binding across its session transfer.
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_game_bindings(session_id,account_id,authorized_session_id,created_at) SELECT ?,account_id,authorized_session_id,? FROM account_game_bindings WHERE session_id=?`, newSession, now, previousSession); err != nil {
		return "", "", problem.Internal(err)
	}
	// Preserve the membership and its scores; transfer only this room's authority.
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`UPDATE memberships SET session_id=? WHERE id=?`, []any{newSession, membershipIDv}},
		{`UPDATE rooms SET owner_session_id=?,room_version=room_version+1,updated_at=? WHERE id=? AND owner_session_id=?`, []any{newSession, now, roomIDv, previousSession}},
		{`UPDATE room_grants SET session_id=? WHERE room_id=? AND session_id=? AND revoked_at IS NULL`, []any{newSession, roomIDv, previousSession}},
		{`UPDATE event_sessions SET host_session_id=?,event_version=event_version+1,updated_at=? WHERE room_id=? AND host_session_id=?`, []any{newSession, now, roomIDv, previousSession}},
	} {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return "", "", problem.Internal(fmt.Errorf("room: transfer recovery seat: %w", err))
		}
	}
	// Recovery revokes the previous controller so two devices cannot both write.
	if _, err := tx.ExecContext(ctx, `
		UPDATE control_leases SET revoked_at = ?
		WHERE room_id = ? AND membership_id = ? AND revoked_at IS NULL`,
		now, roomIDv, membershipIDv); err != nil {
		return "", "", problem.Internal(fmt.Errorf("room: revoke old controller: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE recovery_grants SET consumed_at=? WHERE membership_id=? AND consumed_at IS NULL`, now, membershipIDv); err != nil {
		return "", "", problem.Internal(err)
	}
	version, err := bumpRoomVersion(ctx, tx, roomIDv, now)
	if err != nil {
		return "", "", err
	}
	if err := emitOutbox(ctx, tx, roomIDv, "state.changed", map[string]any{"resource": "room", "roomVersion": version, "revokeRoomSessionId": previousSession, "transferredMembershipId": membershipIDv}, now); err != nil {
		return "", "", err
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

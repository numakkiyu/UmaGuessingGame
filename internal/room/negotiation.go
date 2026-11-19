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

// PauseRequest is a pending consent-based pause.
type PauseRequest struct {
	ID           string
	RoundID      string
	RoomID       string
	RequesterID  string
	Status       string
	ExpiresAt    int64
	PhaseVersion int64
}

// RematchRequest is a pending new-round confirmation.
type RematchRequest struct {
	ID          string
	RoundID     string
	RoomID      string
	RequesterID string
	Status      string
	NewRoundID  string
}

// Trade is a pending instance-card trade.
type Trade struct {
	ID                    string
	RoundID               string
	RoomID                string
	SenderMembershipID    string
	RecipientMembershipID string
	OfferedInstanceID     string
	RequestedInstanceID   string
	Status                string
	ExpiresAt             int64
}

// Negotiations persist audit facts and participant state in the same transaction
// as any phase change they cause.

// negotiationStore writes a negotiation fact.
type negotiationStore struct {
	// kind is "pause", "rematch" or "trade".
	kind string
}

func (n negotiationStore) detail(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// CreatePauseRequest opens a classic consent-based pause.
//
// Only a participant of a live collecting window may ask. The request does not
// pause anything by itself: the counterparty must accept, which is what
// stops one player freezing the clock unilaterally.
func (s *Service) CreatePauseRequest(ctx context.Context, actor Actor, roundID string, phaseVersion int64) (PauseRequest, error) {
	rm, loadErr := s.loadRoom(ctx, actor.RoomID)
	if loadErr != nil {
		return PauseRequest{}, loadErr
	}
	if rm.Config.Kind == "matchmaking" {
		return PauseRequest{}, problem.Conflict("matchmaking-pause-disabled", "random matches use a fixed running clock")
	}
	subject, err := actor.Subject()
	if err != nil {
		return PauseRequest{}, err
	}
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return PauseRequest{}, err
	}
	defer release()
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return PauseRequest{}, problem.Internal(fmt.Errorf("room: begin pause request: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return PauseRequest{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return PauseRequest{}, err
	}
	if r.RoomID != actor.RoomID {
		return PauseRequest{}, errRoundNotFound
	}
	subject, err = s.roundSubject(r, actor)
	if err != nil {
		return PauseRequest{}, err
	}
	if r.Status != modes.PhaseCollecting && r.Status != modes.PhaseBriefing {
		return PauseRequest{}, problem.Conflict("not-pausable",
			fmt.Sprintf("the round is %s and cannot be paused", r.Status))
	}
	if r.Paused() {
		return PauseRequest{}, problem.Conflict("already-paused", "the round is already paused")
	}
	if phaseVersion != r.PhaseVersion {
		return PauseRequest{}, phaseConflict(r.PhaseVersion)
	}
	if r.DeadlineAt != nil && now > r.DeadlineAt.UnixMilli() {
		return PauseRequest{}, problem.Conflict("deadline-passed", "the pause request arrived after the window closed")
	}

	id, err := newID("psr_")
	if err != nil {
		return PauseRequest{}, problem.Internal(err)
	}
	expires := now + 60_000
	if err := writeAudit(ctx, tx, r.RoomID, "", actor.SessionID, "pause.requested", map[string]any{
		"requestId": id, "roundId": r.ID, "subjectId": subject.ID,
		"phaseVersion": r.PhaseVersion, "expiresAt": expires,
	}, now); err != nil {
		return PauseRequest{}, err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return PauseRequest{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return PauseRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return PauseRequest{}, problem.Internal(fmt.Errorf("room: commit pause request: %w", err))
	}
	return PauseRequest{
		ID: id, RoundID: r.ID, RoomID: r.RoomID, RequesterID: subject.ID,
		Status: "pending", ExpiresAt: expires,
	}, nil
}

// RespondToPause accepts or rejects a pause request.
//
// Acceptance pauses the round inside this transaction: deadline becomes
// remainingMs and phaseVersion advances, so the client cannot compute its own
// pause state.
func (s *Service) RespondToPause(ctx context.Context, actor Actor, roundID, requestID string, accept bool) (PauseRequest, error) {
	rm, loadErr := s.loadRoom(ctx, actor.RoomID)
	if loadErr != nil {
		return PauseRequest{}, loadErr
	}
	if rm.Config.Kind == "matchmaking" {
		return PauseRequest{}, problem.Conflict("matchmaking-pause-disabled", "random matches use a fixed running clock")
	}
	if _, err := actor.Subject(); err != nil {
		return PauseRequest{}, err
	}
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return PauseRequest{}, err
	}
	defer release()
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return PauseRequest{}, problem.Internal(fmt.Errorf("room: begin pause response: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return PauseRequest{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return PauseRequest{}, err
	}
	if r.RoomID != actor.RoomID {
		return PauseRequest{}, errRoundNotFound
	}

	req, err := loadPauseRequestQ(ctx, tx, r.RoomID, requestID)
	if err != nil {
		return PauseRequest{}, err
	}
	if req.RoundID != r.ID {
		return PauseRequest{}, problem.NotFound("pause-request-not-found",
			"the pause request does not belong to this round")
	}
	subject, err := s.roundSubject(r, actor)
	if err != nil {
		return PauseRequest{}, err
	}
	if subject.ID == req.RequesterID {
		return PauseRequest{}, problem.Forbidden("pause-counterparty-required", "the requesting side cannot respond to its own pause request")
	}
	if req.Status != "pending" {
		return PauseRequest{}, problem.Conflict("pause-request-closed",
			fmt.Sprintf("the pause request is already %s", req.Status))
	}
	if req.PhaseVersion != r.PhaseVersion {
		return PauseRequest{}, problem.Conflict("pause-request-stale", "the round has advanced since this pause was requested")
	}
	if now > req.ExpiresAt {
		return PauseRequest{}, problem.Conflict("pause-request-expired", "the pause request has expired")
	}

	status := "rejected"
	if accept {
		if r.Paused() {
			return PauseRequest{}, problem.Conflict("already-paused", "the round is already paused")
		}
		if r.Status != modes.PhaseCollecting && r.Status != modes.PhaseBriefing {
			return PauseRequest{}, problem.Conflict("not-pausable",
				"the round is no longer in a pausable phase")
		}
		if r.DeadlineAt != nil && now > r.DeadlineAt.UnixMilli() {
			return PauseRequest{}, problem.Conflict("deadline-passed", "the pause response arrived after the window closed")
		}
		var remaining int64
		if r.DeadlineAt != nil {
			remaining = r.DeadlineAt.UnixMilli() - now
			if remaining < 0 {
				remaining = 0
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE rounds SET paused_at = ?, paused_remaining_ms = ?, paused_previous_phase = ?,
			                  deadline_at = NULL, phase_version = phase_version + 1, updated_at = ?
			WHERE id = ?`,
			now, remaining, string(r.Status), now, r.ID); err != nil {
			return PauseRequest{}, problem.Internal(fmt.Errorf("room: apply pause: %w", err))
		}
		status = "accepted"
	}

	if err := writeAudit(ctx, tx, r.RoomID, "", actor.SessionID, "pause."+status, map[string]any{
		"requestId": req.ID, "roundId": r.ID,
	}, now); err != nil {
		return PauseRequest{}, err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return PauseRequest{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "phase.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return PauseRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return PauseRequest{}, problem.Internal(fmt.Errorf("room: commit pause response: %w", err))
	}
	req.Status = status
	return req, nil
}

// loadPauseRequestQ reconstructs a pause request from its audit row.
func loadPauseRequestQ(ctx context.Context, tx *sql.Tx, roomID, requestID string) (PauseRequest, error) {
	// The pause request id lives inside detail_json, not in audit_events.id.
	//
	// writeAudit assigns its own row id ("aud_..."), while the request id handed to the
	// client is "psr_..." and stored only in the detail payload. Matching on
	// audit_events.id therefore never found the row, so approving a pause always failed
	// with pause-request-not-found even though the request had been created.
	var detail string
	err := tx.QueryRowContext(ctx, `
		SELECT detail_json FROM audit_events
		WHERE room_id = ? AND action = 'pause.requested'
		  AND json_extract(detail_json, '$.requestId') = ?
		ORDER BY rowid DESC LIMIT 1`, roomID, requestID).Scan(&detail)
	if errors.Is(err, sql.ErrNoRows) {
		return PauseRequest{}, problem.NotFound("pause-request-not-found", "the pause request does not exist")
	}
	if err != nil {
		return PauseRequest{}, problem.Internal(fmt.Errorf("room: load pause request: %w", err))
	}
	var d struct {
		RequestID    string `json:"requestId"`
		RoundID      string `json:"roundId"`
		SubjectID    string `json:"subjectId"`
		ExpiresAt    int64  `json:"expiresAt"`
		PhaseVersion int64  `json:"phaseVersion"`
	}
	if err := json.Unmarshal([]byte(detail), &d); err != nil {
		return PauseRequest{}, problem.Internal(fmt.Errorf("room: decode pause request: %w", err))
	}
	status := "pending"
	var response string
	err = tx.QueryRowContext(ctx, `SELECT action FROM audit_events
		WHERE room_id=? AND action IN ('pause.accepted','pause.rejected')
		AND json_extract(detail_json,'$.requestId')=? ORDER BY rowid DESC LIMIT 1`, roomID, requestID).Scan(&response)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PauseRequest{}, problem.Internal(err)
	}
	if err == nil {
		status = strings.TrimPrefix(response, "pause.")
	}
	return PauseRequest{
		ID: requestID, RoundID: d.RoundID, RoomID: roomID,
		RequesterID: d.SubjectID, Status: status, ExpiresAt: d.ExpiresAt, PhaseVersion: d.PhaseVersion,
	}, nil
}

// RequestRematch records a participant's confirmation of a new round.
//
// A new round is only created when every participant has accepted; the old
// round is never overwritten . The replacement is created as a
// fresh prepared round, preserving the old results.
func (s *Service) RequestRematch(ctx context.Context, actor Actor, roundID string, accept bool) (RematchRequest, error) {
	rm, loadErr := s.loadRoom(ctx, actor.RoomID)
	if loadErr != nil {
		return RematchRequest{}, loadErr
	}
	if rm.Config.Kind == "matchmaking" {
		return RematchRequest{}, problem.Conflict("matchmaking-rematch-disabled", "return to matchmaking for a new game")
	}
	subject, err := actor.Subject()
	if err != nil {
		return RematchRequest{}, err
	}
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return RematchRequest{}, err
	}
	defer release()
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return RematchRequest{}, problem.Internal(fmt.Errorf("room: begin rematch: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return RematchRequest{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return RematchRequest{}, err
	}
	if r.RoomID != actor.RoomID {
		return RematchRequest{}, errRoundNotFound
	}
	// Rematch is only meaningful after the round has finished for scoring.
	if r.Status != modes.PhaseSettled && r.Status != modes.PhaseRevealed {
		return RematchRequest{}, problem.Conflict("round-not-finished",
			"a rematch can only be requested after the round is revealed or settled")
	}

	id, err := newID("rmt_")
	if err != nil {
		return RematchRequest{}, problem.Internal(err)
	}
	status := "rejected"
	// newRound 只在「全员同意」时被填上；其余情况保持空串，
	// 这样 RematchRequest.NewRoundID 只在真的创建了新回合时才有值。
	newRound := ""
	if accept {
		status = "pending"
	}
	if err := writeAudit(ctx, tx, r.RoomID, "", actor.SessionID, "rematch."+status, map[string]any{
		"requestId": id, "roundId": r.ID, "subjectId": subject.ID, "accept": accept,
	}, now); err != nil {
		return RematchRequest{}, err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return RematchRequest{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return RematchRequest{}, err
	}

	// A rematch creates the replacement round, and only once everyone has accepted.
	//
	// This used to stop at the audit write: both players could accept and nothing
	// happened, so "rematch" recorded an intention and never acted on it. The rule is
	// unanimous consent because a new round resets everyone's score — one player must
	// not be able to discard a result the others want to keep.
	if accept {
		accepted, eligible, err := rematchAcceptancesQ(ctx, tx, r.RoomID, r.ID)
		if err != nil {
			return RematchRequest{}, err
		}
		if accepted >= eligible && eligible > 0 {
			/*
			 * 新回合 id 必须留下来。
			 *
			 * createRematchRoundQ 一直返回新回合 id，但这里用 `_` 丢掉了，
			 * 于是 RematchRequest.NewRoundID 永远是空串：前端在重赛达成后
			 * 拿不到新回合，只能靠轮询房间快照猜。
			 *
			 * 契约里 newRoundId 是可选字段，所以这不算契约违约——
			 * 但它让一个已经算好的值白算，属于真实的信息丢失。
			 */
			newRoundID, err := s.createRematchRoundQ(ctx, tx, r, now)
			if err != nil {
				return RematchRequest{}, err
			}
			newRound = newRoundID
			status = "accepted"
		}
	}

	if err := tx.Commit(); err != nil {
		return RematchRequest{}, problem.Internal(fmt.Errorf("room: commit rematch: %w", err))
	}
	return RematchRequest{
		ID: id, RoundID: r.ID, RoomID: r.RoomID, RequesterID: subject.ID, Status: status,
		NewRoundID: newRound,
	}, nil
}

// rematchAcceptancesQ counts how many distinct players accepted a rematch for this round
// and how many players are eligible to accept.
//
// Only accepting rows count, and duplicates from one player are collapsed: pressing the
// button twice must not look like two people agreeing.
func rematchAcceptancesQ(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, roomID, roundID string) (accepted, eligible int, err error) {
	err = q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM memberships
		WHERE room_id = ? AND role = 'player'`, roomID).Scan(&eligible)
	if err != nil {
		return 0, 0, problem.Internal(fmt.Errorf("room: count eligible rematch voters: %w", err))
	}
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT json_extract(detail_json, '$.subjectId')
		FROM audit_events
		WHERE room_id = ? AND action = 'rematch.pending'
		  AND json_extract(detail_json, '$.roundId') = ?
		  AND json_extract(detail_json, '$.accept') = 1`, roomID, roundID)
	if err != nil {
		return 0, eligible, problem.Internal(fmt.Errorf("room: count rematch acceptances: %w", err))
	}
	defer rows.Close()
	for rows.Next() {
		var sid sql.NullString
		if err := rows.Scan(&sid); err != nil {
			return 0, eligible, problem.Internal(fmt.Errorf("room: scan rematch acceptance: %w", err))
		}
		if sid.Valid && sid.String != "" {
			accepted++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, eligible, problem.Internal(fmt.Errorf("room: iterate rematch acceptances: %w", err))
	}
	return accepted, eligible, nil
}

// createRematchRoundQ inserts the replacement round with the same configuration.
//
// The old round is left untouched: its results stay readable, which is what "the old
// round is never overwritten" in the doc comment means in practice.
func (s *Service) createRematchRoundQ(ctx context.Context, tx *sql.Tx, old Round, now int64) (string, error) {
	id, err := newID("rnd_")
	if err != nil {
		return "", problem.Internal(err)
	}
	// The replacement copies the mode, rules/dataset/pack versions, seed and config so it
	// is the same game with fresh state. ordinal advances so the new round sorts after the
	// old one and the room's "current round" query picks it up.
	/*
	 * 先把旧回合收尾，再插入新回合。
	 *
	 * rounds 表有唯一索引 idx_rounds_room_current：每个房间只允许一个
	 * "活"回合（status NOT IN ('settled','cancelled')）。
	 * 重赛的前置是 revealed 或 settled，而 revealed 仍然算活回合——
	 * 直接插入新回合会撞这个索引，整笔交易抛错，接口返回 500。
	 *
	 * 端到端实测复现：A 申请成功，B 同意时 500 internal-error。
	 * 这里在同一个交易里把旧回合推进到 settled，既符合"重赛即换一局"的语义，
	 * 也让唯一索引自然成立。
	 */
	if _, err := tx.ExecContext(ctx, `
		UPDATE rounds SET status = 'settled', phase_version = phase_version + 1,
		                  deadline_at = NULL, updated_at = ?
		 WHERE id = ? AND status NOT IN ('settled', 'cancelled')`,
		now, old.ID); err != nil {
		return "", problem.Internal(fmt.Errorf("room: settle old round before rematch: %w", err))
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at, paused_at,
		                    seed, config_json, created_at, updated_at)
		SELECT ?, room_id, ordinal + 1, mode_id, rules_version, dataset_version,
		       content_pack_version, 'prepared', 1, NULL, NULL,
		       seed, config_json, ?, ?
		FROM rounds WHERE id = ?`,
		id, now, now, old.ID)
	if err != nil {
		return "", problem.Internal(fmt.Errorf("room: create rematch round: %w", err))
	}
	if err := writeAudit(ctx, tx, old.RoomID, "", "", "rematch.created", map[string]any{
		"previousRoundId": old.ID, "roundId": id,
	}, now); err != nil {
		return "", err
	}
	return id, nil
}

// CreateTrade proposes an instance-card trade.
//
// Both parties must hold the named instances, and the exchange is applied
// atomically on acceptance so a double sell is impossible .
func (s *Service) CreateTrade(ctx context.Context, actor Actor, roundID string, in TradeInput) (Trade, error) {
	subject, err := actor.Subject()
	if err != nil {
		return Trade{}, err
	}
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return Trade{}, err
	}
	defer release()
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: begin trade: %w", err))
	}
	defer tx.Rollback()
	commandScope := RouteScope("createTrade", roundID)
	if trade, replayed, err := replayGameplayQ[Trade](ctx, tx, actor.SessionID, commandScope, in.Command, s.nowMS()); err != nil || replayed {
		return trade, err
	}
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Trade{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return Trade{}, err
	}
	if r.RoomID != actor.RoomID {
		return Trade{}, errRoundNotFound
	}
	if r.ModeID != "trade" {
		return Trade{}, problem.Unprocessable("not-a-trade-round",
			"trades are only available in a trade mode round")
	}
	if err := requireTradeWindow(r, now); err != nil {
		return Trade{}, err
	}
	subject, err = s.roundSubject(r, actor)
	if err != nil {
		return Trade{}, err
	}
	senderState, err := participantStateQ(ctx, tx, r.ID, subject.ID)
	if err != nil {
		return Trade{}, err
	}
	if isSurrendered(senderState) {
		return Trade{}, problem.Conflict("participant-surrendered", "this participant has surrendered")
	}
	if phaseVersion := in.PhaseVersion; phaseVersion != r.PhaseVersion {
		return Trade{}, phaseConflict(r.PhaseVersion)
	}
	if in.RecipientMembershipID == actor.MembershipID() {
		return Trade{}, problem.Unprocessable("self-trade", "you cannot trade with yourself")
	}
	recipient, err := loadMembershipByIDQ(ctx, tx, in.RecipientMembershipID)
	if err != nil {
		return Trade{}, err
	}
	if recipient.RoomID != r.RoomID || !recipient.Active() {
		return Trade{}, problem.Validation("recipientMembershipId is invalid",
			problem.FieldError{Field: "recipientMembershipId", Message: "is not an active member of this room"})
	}
	recipientState, err := participantStateQ(ctx, tx, r.ID, recipient.ID)
	if err != nil {
		return Trade{}, err
	}
	if isSurrendered(recipientState) {
		return Trade{}, problem.Conflict("trade-closed", "the recipient has surrendered")
	}
	// Card ownership is read from the sender's own private state, which is the
	// only authority for what they hold.
	if !holdsInstance(senderState, in.OfferedInstanceID) {
		return Trade{}, problem.Unprocessable("card-not-held",
			"you do not hold the offered card instance")
	}

	// The proposal must go through the mode, not around it.
	//
	// This endpoint used to write an audit row and nothing else, so the proposal never
	// reached publicState.trades: the recipient had no tradeId to accept, the frontend
	// saw an empty log, and trading was unusable even though every request returned
	// success. The mode's Apply is the only place that appends to the log and folds the
	// new hands, so it is called here and the resulting state is persisted below.
	mode, ok := s.modes.Get(r.ModeID)
	if !ok {
		return Trade{}, problem.NotImplemented("modes-unavailable",
			"the round's mode is not registered on this server")
	}
	publicState, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return Trade{}, err
	}
	privateState, err := roundPrivateStateQ(ctx, tx, r.ID)
	if err != nil {
		return Trade{}, err
	}
	submitted, err := acceptedCountQ(ctx, tx, r.ID, subject.ID)
	if err != nil {
		return Trade{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"kind":                "trade.propose",
		"recipientId":         in.RecipientMembershipID,
		"offeredInstanceId":   in.OfferedInstanceID,
		"requestedInstanceId": in.RequestedInstanceID,
	})
	if err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: encode trade action: %w", err))
	}
	decision, err := mode.Apply(modes.ApplyInput{
		Scope:           r.Scope(),
		Subject:         subject,
		Action:          modes.ModeAction{Kind: "trade.propose", Payload: payload},
		Public:          publicState,
		Private:         privateState,
		State:           senderState,
		SubmissionCount: submitted,
		ReceivedAt:      now,
	})
	if err != nil {
		return Trade{}, problem.Unprocessable("trade-rejected",
			fmt.Sprintf("the mode rejected this trade: %s", err.Error()))
	}
	if !decision.Accepted {
		return Trade{}, problem.Unprocessable(
			nonEmpty(decision.Code, "trade-rejected"),
			nonEmpty(decision.Detail, "the mode did not accept this trade"))
	}
	// Persist the shared fold. Apply can only return the caller's own state, so a mode
	// that mutates shared state (the trade log, the hands) exposes CommitShared and the
	// room layer writes its output. Without this the proposal never reaches
	// publicState.trades and the recipient has nothing to accept.
	if sm, ok := mode.(sharedStateMode); ok {
		publicAfter, privateAfter, serr := sm.CommitShared(modes.ApplyInput{
			Scope:           r.Scope(),
			Subject:         subject,
			Action:          modes.ModeAction{Kind: "trade.propose", Payload: payload},
			Public:          publicState,
			Private:         privateState,
			State:           senderState,
			SubmissionCount: submitted,
			ReceivedAt:      now,
		}, decision)
		if serr != nil {
			return Trade{}, problem.Internal(fmt.Errorf("room: commit trade shared state: %w", serr))
		}
		if err := writeRoundState(ctx, tx, r.ID, "public", publicAfter, now); err != nil {
			return Trade{}, err
		}
		if err := writeRoundState(ctx, tx, r.ID, "round", privateAfter, now); err != nil {
			return Trade{}, err
		}
	}
	// Merge the mode's state into the existing participant state instead of replacing it.
	//
	// The trade mode's Decision carries the caller's bookkeeping (open proposal,
	// incoming ids) but not the hand: the hand lives in the shared public projection.
	// Writing Decision.State over the row dropped "hand", so the proposer stopped
	// holding the card they had just offered and accepting failed with card-not-held.
	if len(decision.State) > 0 && json.Valid(decision.State) {
		merged := map[string]json.RawMessage{}
		if len(senderState) > 0 {
			_ = json.Unmarshal(senderState, &merged)
		}
		var patch map[string]json.RawMessage
		if err := json.Unmarshal(decision.State, &patch); err == nil {
			for k, v := range patch {
				merged[k] = v
			}
		}
		body, merr := json.Marshal(merged)
		if merr != nil {
			return Trade{}, problem.Internal(fmt.Errorf("room: merge trade state: %w", merr))
		}
		if err := updateParticipantStateQ(ctx, tx, r.ID, subject.ID, body, now); err != nil {
			return Trade{}, err
		}
	}
	// Read the exact id from this decision. The shared log is sorted by id,
	// so its last element is not necessarily the proposal just created.
	var created struct {
		TradeID string `json:"tradeId"`
	}
	if err := json.Unmarshal(decision.Feedback, &created); err != nil || created.TradeID == "" {
		return Trade{}, problem.Internal(fmt.Errorf("room: trade decision missing proposal identity"))
	}
	id := created.TradeID

	expires := r.DeadlineAt.UnixMilli()
	if err := writeAudit(ctx, tx, r.RoomID, "", actor.SessionID, "trade.proposed", map[string]any{
		"tradeId": id, "roundId": r.ID, "senderMembershipId": actor.MembershipID(),
		"recipientMembershipId": in.RecipientMembershipID,
		"offeredInstanceId":     in.OfferedInstanceID,
		"requestedInstanceId":   in.RequestedInstanceID,
		"expiresAt":             expires,
	}, now); err != nil {
		return Trade{}, err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return Trade{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return Trade{}, err
	}
	result := Trade{
		ID: id, RoundID: r.ID, RoomID: r.RoomID,
		SenderMembershipID:    actor.MembershipID(),
		RecipientMembershipID: in.RecipientMembershipID,
		OfferedInstanceID:     in.OfferedInstanceID,
		RequestedInstanceID:   in.RequestedInstanceID,
		Status:                "pending", ExpiresAt: expires,
	}
	if err := s.saveGameplayQ(ctx, tx, actor.SessionID, commandScope, in.Command, result, now); err != nil {
		return Trade{}, err
	}
	if err := tx.Commit(); err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: commit trade: %w", err))
	}
	return result, nil
}

// TradeInput is a trade proposal.
type TradeInput struct {
	Command               *CreationCommand
	PhaseVersion          int64
	RecipientMembershipID string
	OfferedInstanceID     string
	RequestedInstanceID   string
}

// RespondToTrade accepts or rejects a trade atomically.
//
// Only the named recipient may respond. Acceptance moves both card instances in
// the same transaction, so an instance can never be in two hands.
func (s *Service) RespondToTrade(ctx context.Context, actor Actor, roundID, tradeID string, accept bool, command *CreationCommand) (Trade, error) {
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return Trade{}, err
	}
	defer release()
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: begin trade response: %w", err))
	}
	defer tx.Rollback()
	// Include both path resources: a receipt for one round cannot replay through
	// another round's URL even when the trade id and body were copied.
	commandScope := RouteScope("respondToTrade", roundID+":"+tradeID)
	if trade, replayed, err := replayGameplayQ[Trade](ctx, tx, actor.SessionID, commandScope, command, s.nowMS()); err != nil || replayed {
		return trade, err
	}
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Trade{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return Trade{}, err
	}
	if r.RoomID != actor.RoomID {
		return Trade{}, errRoundNotFound
	}
	if err := requireTradeWindow(r, now); err != nil {
		return Trade{}, err
	}
	responder, err := s.roundSubject(r, actor)
	if err != nil {
		return Trade{}, err
	}
	responderState, err := participantStateQ(ctx, tx, r.ID, responder.ID)
	if err != nil {
		return Trade{}, err
	}
	if isSurrendered(responderState) {
		return Trade{}, problem.Conflict("participant-surrendered", "this participant has surrendered")
	}
	trade, err := loadTradeQ(ctx, tx, r.RoomID, tradeID)
	if err != nil {
		return Trade{}, err
	}
	if trade.RoundID != r.ID {
		return Trade{}, problem.NotFound("trade-not-found", "the trade does not belong to this round")
	}
	if trade.Status != "pending" {
		return Trade{}, problem.Conflict("trade-closed",
			fmt.Sprintf("the trade is already %s", trade.Status))
	}
	// A proposal lives for its negotiation window. Pause/resume extends that
	// window; the audit's original deadline must not expire it during a pause.
	// requireTradeWindow checked the authoritative deadline, and an earlier
	// window's proposals are already marked expired by ExpireTurn.
	trade.ExpiresAt = r.DeadlineAt.UnixMilli()
	// Only the named recipient may accept; the proposer may not self-accept.
	if actor.MembershipID() != trade.RecipientMembershipID {
		return Trade{}, problem.Forbidden("not-the-recipient",
			"only the named recipient may respond to this trade")
	}
	proposerState, err := participantStateQ(ctx, tx, r.ID, trade.SenderMembershipID)
	if err != nil {
		return Trade{}, err
	}
	if isSurrendered(proposerState) {
		return Trade{}, problem.Conflict("trade-closed", "the proposer has surrendered")
	}

	// The swap must go through the mode so the public projection and the private
	// hands move together.
	//
	// This used to rewrite only the two private state rows, which left
	// publicState.hands unchanged: the trade reported success while every client
	// still saw the old hands. The mode's CommitShared folds the swap into both the
	// public log (status accepted, hands exchanged) and the server-only state.
	mode, ok := s.modes.Get(r.ModeID)
	if !ok {
		return Trade{}, problem.NotImplemented("modes-unavailable",
			"the round's mode is not registered on this server")
	}
	publicState, perr := roundPublicStateQ(ctx, tx, r.ID)
	if perr != nil {
		return Trade{}, perr
	}
	privateState, perr := roundPrivateStateQ(ctx, tx, r.ID)
	if perr != nil {
		return Trade{}, perr
	}
	payload, perr := json.Marshal(map[string]any{
		"kind": "trade.respond", "tradeId": trade.ID, "accept": accept,
	})
	if perr != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: encode trade response: %w", perr))
	}
	applyIn := modes.ApplyInput{
		Scope:      r.Scope(),
		Subject:    responder,
		Action:     modes.ModeAction{Kind: "trade.respond", Payload: payload},
		Public:     publicState,
		Private:    privateState,
		ReceivedAt: now,
	}
	decision, aerr := mode.Apply(applyIn)
	if aerr != nil {
		return Trade{}, problem.Unprocessable("trade-rejected",
			fmt.Sprintf("the mode rejected this response: %s", aerr.Error()))
	}
	if !decision.Accepted {
		return Trade{}, problem.Unprocessable(
			nonEmpty(decision.Code, "trade-rejected"),
			nonEmpty(decision.Detail, "the mode did not accept this response"))
	}
	if sm, ok := mode.(sharedStateMode); ok {
		publicAfter, privateAfter, serr := sm.CommitShared(applyIn, decision)
		if serr != nil {
			return Trade{}, problem.Internal(fmt.Errorf("room: commit trade response: %w", serr))
		}
		if err := writeRoundState(ctx, tx, r.ID, "public", publicAfter, now); err != nil {
			return Trade{}, err
		}
		if err := writeRoundState(ctx, tx, r.ID, "round", privateAfter, now); err != nil {
			return Trade{}, err
		}
	}

	status := "rejected"
	if accept {
		if err := applyTradeSwap(ctx, tx, r.ID, trade, now); err != nil {
			return Trade{}, err
		}
		status = "accepted"
	}
	if err := writeAudit(ctx, tx, r.RoomID, "", actor.SessionID, "trade."+status, map[string]any{
		"tradeId": trade.ID, "roundId": r.ID,
	}, now); err != nil {
		return Trade{}, err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return Trade{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return Trade{}, err
	}
	trade.Status = status
	if err := s.saveGameplayQ(ctx, tx, actor.SessionID, commandScope, command, trade, now); err != nil {
		return Trade{}, err
	}
	if err := tx.Commit(); err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: commit trade response: %w", err))
	}
	return trade, nil
}

// applyTradeSwap moves both instances inside the transaction.
//
// Re-checking ownership here (rather than trusting the proposal) is what makes
// a double sell impossible: the check and the write are in the same statement
// sequence under the single-writer pool.
func applyTradeSwap(ctx context.Context, tx *sql.Tx, roundID string, t Trade, now int64) error {
	senderState, err := participantStateQ(ctx, tx, roundID, membershipSubjectID(t.SenderMembershipID))
	if err != nil {
		return err
	}
	recipientState, err := participantStateQ(ctx, tx, roundID, membershipSubjectID(t.RecipientMembershipID))
	if err != nil {
		return err
	}
	if !holdsInstance(senderState, t.OfferedInstanceID) {
		return problem.Unprocessable("card-not-held", "the proposer no longer holds the offered card")
	}
	if !holdsInstance(recipientState, t.RequestedInstanceID) {
		return problem.Unprocessable("card-not-held", "the recipient no longer holds the requested card")
	}
	if err := writeHand(ctx, tx, roundID, membershipSubjectID(t.SenderMembershipID),
		moveInstance(senderState, t.OfferedInstanceID, t.RequestedInstanceID), now); err != nil {
		return err
	}
	return writeHand(ctx, tx, roundID, membershipSubjectID(t.RecipientMembershipID),
		moveInstance(recipientState, t.RequestedInstanceID, t.OfferedInstanceID), now)
}

// membershipSubjectID maps a membership id to its player subject id.
//
// Player subjects use the membership id directly (see Actor.Subject), so this is
// an identity function that exists to document the invariant.
func membershipSubjectID(membershipID string) string { return membershipID }

// holdsInstance reports whether the participant state contains an instance.
func holdsInstance(state json.RawMessage, instanceID string) bool {
	if instanceID == "" || len(state) == 0 {
		return false
	}
	// The hand is a plain array of instance ids, matching publicState.hands.
	//
	// This used to expect [{"instanceId": "..."}], which no mode ever wrote, so the
	// check always failed and every trade proposal was rejected with card-not-held
	// even when the player held the card. The shape is pinned by
	// TestSubjectPrivateCarriesHand in the trade package.
	var s struct {
		Hand []string `json:"hand"`
	}
	if err := json.Unmarshal(state, &s); err != nil {
		return false
	}
	for _, h := range s.Hand {
		if h == instanceID {
			return true
		}
	}
	return false
}

// moveInstance removes give and appends take, returning the new state JSON.
func moveInstance(state json.RawMessage, give, take string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state, &fields); err != nil {
		return state
	}
	var hand []string
	if err := json.Unmarshal(fields["hand"], &hand); err != nil {
		return state
	}
	for i, id := range hand {
		if id == give {
			hand[i] = take
			break
		}
	}
	body, err := json.Marshal(hand)
	if err != nil {
		return state
	}
	fields["hand"] = body
	// Preserve secret objectives and proposal bookkeeping while replacing a card.
	result, err := json.Marshal(fields)
	if err != nil {
		return state
	}
	return result
}

// writeHand persists a participant's hand state.
func writeHand(ctx context.Context, tx *sql.Tx, roundID, subjectID string, state json.RawMessage, now int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO participant_round_state (round_id, subject_id, subject_kind, state_json, updated_at)
		VALUES (?, ?, 'player', ?, ?)
		ON CONFLICT (round_id, subject_id) DO UPDATE SET
			state_json = excluded.state_json, updated_at = excluded.updated_at`,
		roundID, subjectID, string(state), now); err != nil {
		return problem.Internal(fmt.Errorf("room: write hand: %w", err))
	}
	return nil
}

// loadTradeQ reconstructs a trade from its audit row.
func loadTradeQ(ctx context.Context, tx *sql.Tx, roomID, tradeID string) (Trade, error) {
	var detail string
	err := tx.QueryRowContext(ctx, `
		SELECT detail_json FROM audit_events
		WHERE room_id = ? AND action = 'trade.proposed'
		  AND json_extract(detail_json, '$.tradeId') = ?
		ORDER BY rowid DESC LIMIT 1`, roomID, tradeID).Scan(&detail)
	if errors.Is(err, sql.ErrNoRows) {
		return Trade{}, problem.NotFound("trade-not-found", "the trade does not exist")
	}
	if err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: load trade: %w", err))
	}
	var d struct {
		TradeID               string `json:"tradeId"`
		RoundID               string `json:"roundId"`
		SenderMembershipID    string `json:"senderMembershipId"`
		RecipientMembershipID string `json:"recipientMembershipId"`
		OfferedInstanceID     string `json:"offeredInstanceId"`
		RequestedInstanceID   string `json:"requestedInstanceId"`
		ExpiresAt             int64  `json:"expiresAt"`
	}
	if err := json.Unmarshal([]byte(detail), &d); err != nil {
		return Trade{}, problem.Internal(fmt.Errorf("room: decode trade: %w", err))
	}
	public, err := roundPublicStateQ(ctx, tx, d.RoundID)
	if err != nil {
		return Trade{}, err
	}
	var board struct {
		Trades []struct {
			ID     string `json:"tradeId"`
			Status string `json:"status"`
		} `json:"trades"`
	}
	if err := json.Unmarshal(public, &board); err != nil {
		return Trade{}, problem.Internal(err)
	}
	status := ""
	for _, record := range board.Trades {
		if record.ID == tradeID {
			status = record.Status
			break
		}
	}
	if status == "" {
		return Trade{}, problem.NotFound("trade-not-found", "the trade is not present on this board")
	}
	return Trade{
		ID: tradeID, RoundID: d.RoundID, RoomID: roomID,
		SenderMembershipID: d.SenderMembershipID, RecipientMembershipID: d.RecipientMembershipID,
		OfferedInstanceID: d.OfferedInstanceID, RequestedInstanceID: d.RequestedInstanceID,
		Status: status, ExpiresAt: d.ExpiresAt,
	}, nil
}

// updateRoundPublicStateQ stores the public question state the mode returned from Apply.
func updateRoundPublicStateQ(ctx context.Context, tx *sql.Tx, roundID string, body json.RawMessage) error {
	if len(body) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE round_private_state SET state_json = ? WHERE round_id = ? AND scope = 'public'`,
		string(body), roundID); err != nil {
		return problem.Internal(fmt.Errorf("room: update public state: %w", err))
	}
	return nil
}

// updateParticipantStateQ stores a subject's own state after an accepted action.
func updateParticipantStateQ(ctx context.Context, tx *sql.Tx, roundID, subjectID string, body json.RawMessage, now int64) error {
	if len(body) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO participant_round_state (round_id, subject_id, subject_kind, state_json, updated_at)
		VALUES (?, ?, 'player', ?, ?)
		ON CONFLICT (round_id, subject_id) DO UPDATE SET
			state_json = excluded.state_json, updated_at = excluded.updated_at`,
		roundID, subjectID, string(body), now); err != nil {
		return problem.Internal(fmt.Errorf("room: update participant state: %w", err))
	}
	return nil
}

// requireTradeWindow protects dedicated trade endpoints just like submissions.
func requireTradeWindow(r Round, now int64) error {
	if r.ModeID != "trade" {
		return problem.Unprocessable("not-a-trade-round", "trades are only available in a trade round")
	}
	if r.Status != modes.PhaseCollecting {
		return problem.Conflict("round-not-collecting", "this round does not accept trades now")
	}
	if r.Paused() {
		return problem.Conflict("round-paused", "this round is paused")
	}
	if r.DeadlineAt == nil || now >= r.DeadlineAt.UnixMilli() {
		return problem.Conflict("deadline-passed", "this negotiation window has ended")
	}
	return nil
}

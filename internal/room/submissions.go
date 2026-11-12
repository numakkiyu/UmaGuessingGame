package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
)

// SubmissionInput is a typed mode action from a participant.
type SubmissionInput struct {
	Command            *CreationCommand
	PhaseVersion       int64
	SubmissionRevision int64
	ControlGeneration  int64
	ModeAction         modes.ModeAction
}

// SubmissionReceipt is the accepted-action receipt.
//
// The receipt carries only the caller's own already-public feedback. Answer
// material is never included .
type SubmissionReceipt struct {
	SubmissionID       string
	ReceiptID          string
	AcceptedAt         int64
	RoomVersion        int64
	PhaseVersion       int64
	SubmissionRevision int64
	Feedback           json.RawMessage
}

// CreateSubmission persists one idempotent mode action.
//
// Full command path — every step is load-bearing:
//
// 1. The subject comes from the server-side membership, never the body.
// 2. Body size, CSRF and Origin are enforced by the HTTP layer before this runs.
// 3. The idempotency receipt is consulted before phaseVersion/revision, so a
// retry that lands after a phase change replays instead of being rejected.
// 4. receivedAt is assigned on enqueue and compared to deadlineAt.
// 5. A short transaction re-checks phase, phaseVersion, control generation and
// the per-slot revision uniqueness in SQL.
// 6. The submission, the head, the participant state and the outbox event are
// written in one transaction.
// 7. Only after commit does the caller see success; the broadcast is an outbox
// row published by the dispatcher, never a pre-commit send.
func (s *Service) CreateSubmission(ctx context.Context, actor Actor, roundID string, in SubmissionInput) (SubmissionReceipt, error) {
	if s.modes == nil {
		return SubmissionReceipt{}, problem.NotImplemented("modes-unavailable",
			"no game mode is registered on this server yet, so submissions cannot be accepted")
	}
	var subject modes.Subject
	if err := in.ModeAction.Validate(); err != nil {
		return SubmissionReceipt{}, problem.Validation("modeAction is invalid",
			problem.FieldError{Field: "modeAction", Message: err.Error()})
	}

	// Enqueue before touching the database: the arrival time must reflect when
	// the command entered the room's queue, not when the write lock was granted.
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	defer release()
	receivedMS := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: begin submission: %w", err))
	}
	defer tx.Rollback()
	commandScope := RouteScope("createSubmission", roundID)
	if receipt, replayed, err := replayGameplayQ[SubmissionReceipt](ctx, tx, actor.SessionID, commandScope, in.Command, s.nowMS()); err != nil || replayed {
		return receipt, err
	}
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return SubmissionReceipt{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if r.RoomID != actor.RoomID {
		return SubmissionReceipt{}, errRoundNotFound
	}
	creativeVote := r.ModeID == "creative" && in.ModeAction.Kind == creative.ActionKindVote
	if creativeVote {
		if actor.Membership == nil {
			return SubmissionReceipt{}, problem.Forbidden("not-a-participant", "an active room membership is required")
		}
		var role string
		if err := tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE id=? AND room_id=? AND session_id=? AND left_at IS NULL AND revoked_at IS NULL`, actor.MembershipID(), r.RoomID, actor.SessionID).Scan(&role); err != nil {
			if err == sql.ErrNoRows {
				return SubmissionReceipt{}, problem.Forbidden("membership-inactive", "this member has left the room")
			}
			return SubmissionReceipt{}, problem.Internal(err)
		}
		if role != string(actor.Membership.Role) || (!actor.Has(CapPlay) && actor.Membership.Role != RoleSpectator) {
			return SubmissionReceipt{}, problem.Forbidden("not-a-participant", "this role cannot vote")
		}
		subject = creative.VoteSubject(actor.SessionID)
	} else {
		subject, err = s.roundSubject(r, actor)
		if err != nil {
			return SubmissionReceipt{}, err
		}
		if err := s.requireTeamControllerQ(ctx, tx, r, actor); err != nil {
			return SubmissionReceipt{}, err
		}
	}

	// Phase and window checks, in SQL-visible state.
	if r.Status != modes.PhaseCollecting && r.Status != modes.PhaseBriefing {
		return SubmissionReceipt{}, problem.Conflict("round-not-collecting",
			fmt.Sprintf("the round is %s and does not accept submissions", r.Status))
	}
	if (r.ModeID == "rapport" && s.individualRoundSeats(r) || r.ModeID == "bluff" || r.ModeID == "strategy-race" || r.ModeID == "creative") && r.Status != modes.PhaseCollecting {
		return SubmissionReceipt{}, problem.Conflict("round-not-collecting", "answering starts after briefing")
	}
	if r.ModeID == "audio" {
		if in.ModeAction.Kind == "audio.matches" && r.Status != modes.PhaseCollecting {
			return SubmissionReceipt{}, problem.Conflict("audio-not-started", "audio pairing starts after preparation")
		}
		if in.ModeAction.Kind == "audio.ready" && r.Status == modes.PhaseCollecting {
			var report struct {
				PlaybackFailures []string `json:"playbackFailures"`
			}
			if json.Unmarshal(in.ModeAction.Payload, &report) != nil || len(report.PlaybackFailures) == 0 {
				return SubmissionReceipt{}, problem.Conflict("audio-already-started", "only playback faults can be reported after start")
			}
		}
	}
	if r.Paused() {
		return SubmissionReceipt{}, problem.Conflict("round-paused", "the round is paused")
	}
	if r.Status == modes.PhaseBriefing && s.previewWindowMS(r) > 0 {
		return SubmissionReceipt{}, problem.Conflict("preview-in-progress", "answering starts after the viewing window")
	}
	// The deadline comparison uses the server-assigned arrival time only.
	if r.DeadlineAt != nil && receivedMS > r.DeadlineAt.UnixMilli() {
		return SubmissionReceipt{}, problem.Conflict("deadline-passed",
			"the submission arrived after the window closed")
	}
	if in.PhaseVersion != r.PhaseVersion {
		return SubmissionReceipt{}, phaseConflict(r.PhaseVersion)
	}

	// Control generation: a participant who is not the controller must present 0.
	gen := int64(0)
	if !creativeVote {
		gen, err = s.controlGenerationQ(ctx, tx, actor.RoomID, r.ID, actor.MembershipID())
	}
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if gen != 0 && in.ControlGeneration != gen {
		e := problem.Conflict("control-generation-conflict",
			fmt.Sprintf("the control lease generation is now %d", gen))
		e.Fields = []problem.FieldError{{Field: "controlGeneration",
			Message: fmt.Sprintf("current generation is %d", gen)}}
		return SubmissionReceipt{}, e
	}
	if gen == 0 && in.ControlGeneration != 0 {
		return SubmissionReceipt{}, problem.Conflict("control-generation-conflict",
			"you do not hold a control lease for this round")
	}

	// Submission revision guards against two tabs overwriting each other.
	headRevision, headExists, err := submissionHeadQ(ctx, tx, r.ID, subject.ID, slotFor(in.ModeAction.Kind))
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if headExists {
		if in.SubmissionRevision != headRevision {
			e := problem.Conflict("submission-revision-conflict",
				fmt.Sprintf("the current submission revision is %d", headRevision))
			e.Fields = []problem.FieldError{{Field: "submissionRevision",
				Message: fmt.Sprintf("current revision is %d", headRevision)}}
			return SubmissionReceipt{}, e
		}
	} else if in.SubmissionRevision != 0 {
		return SubmissionReceipt{}, problem.Conflict("submission-revision-conflict",
			"the first submission for this slot must use submissionRevision 0")
	}

	mode, ok := s.modes.Get(r.ModeID)
	if !ok {
		return SubmissionReceipt{}, problem.NotImplemented("modes-unavailable",
			fmt.Sprintf("mode %s is not registered, so the action cannot be evaluated", r.ModeID))
	}

	publicState, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if r.ModeID == "creative" {
		stage, err := creative.Stage(publicState)
		if err != nil {
			return SubmissionReceipt{}, problem.Internal(err)
		}
		if stage == "legacy" {
			return SubmissionReceipt{}, problem.Conflict("creative-legacy-round", "cancel this older round and prepare a new one")
		}
	}
	// Private state stays inside this function and is passed only to the mode.
	privateState, err := roundPrivateStateQ(ctx, tx, r.ID)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	currentState, err := participantStateQ(ctx, tx, r.ID, subject.ID)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	accepted, err := acceptedCountQ(ctx, tx, r.ID, subject.ID)
	if err != nil {
		return SubmissionReceipt{}, err
	}

	applyInput := modes.ApplyInput{
		MembershipID:    actor.MembershipID(),
		Scope:           r.Scope(),
		Subject:         subject,
		Action:          in.ModeAction,
		Public:          publicState,
		Private:         privateState,
		State:           currentState,
		SubmissionCount: accepted,
		ReceivedAt:      receivedMS,
	}
	var decision modes.Decision
	if creativeVote {
		decision, err = creative.ApplySessionVote(applyInput, actor.SessionID, actor.Membership.Role == RoleSpectator)
	} else {
		decision, err = mode.Apply(applyInput)
	}
	if err != nil {
		return SubmissionReceipt{}, problem.Unprocessable("action-rejected",
			fmt.Sprintf("the mode rejected this action: %s", err.Error()))
	}
	if !decision.Accepted {
		// A business rejection is a 422 with an explicit code. It never mutates
		// state and never looks like a success .
		failure := problem.Unprocessable(
			nonEmpty(decision.Code, "action-rejected"),
			nonEmpty(decision.Detail, "the mode did not accept this action"))
		for _, issue := range decision.Issues {
			failure.WithField(issue.Field, issue.Code)
		}
		return SubmissionReceipt{}, failure
	}

	now := receivedMS
	submissionID, err := newID("sub_")
	if err != nil {
		return SubmissionReceipt{}, problem.Internal(err)
	}
	receiptID, err := newID("rcp_")
	if err != nil {
		return SubmissionReceipt{}, problem.Internal(err)
	}
	nextRevision := in.SubmissionRevision + 1
	slot := slotFor(in.ModeAction.Kind)
	payload, err := json.Marshal(in.ModeAction)
	if err != nil {
		return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: encode action: %w", err))
	}

	// The unique index on (round, subject, slot, revision) is the SQL-level guard
	// against a double write that slipped past the head check.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO submissions (id, round_id, subject_id, subject_kind, slot, revision,
		                         action_kind, payload_json, received_at, accepted, outcome_code, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 'accepted', ?)`,
		submissionID, r.ID, subject.ID, string(subject.Kind), slot, nextRevision,
		in.ModeAction.Kind, string(payload), now, now); err != nil {
		return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: insert submission: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO submission_heads (round_id, subject_id, slot, revision, submission_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (round_id, subject_id, slot) DO UPDATE SET
			revision = excluded.revision,
			submission_id = excluded.submission_id`,
		r.ID, subject.ID, slot, nextRevision, submissionID); err != nil {
		return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: upsert submission head: %w", err))
	}

	// Participant state is only written when the mode produced one.
	if len(decision.State) > 0 && json.Valid(decision.State) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO participant_round_state (round_id, subject_id, subject_kind, state_json, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (round_id, subject_id) DO UPDATE SET
				state_json = excluded.state_json,
				updated_at = excluded.updated_at`,
			r.ID, subject.ID, string(subject.Kind), string(decision.State), now); err != nil {
			return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: upsert participant state: %w", err))
		}
	}

	// A mode that owns round-level shared state (an occupancy board, a shared
	// deck) folds the accepted action into it here, in the SAME transaction as the
	// submission.
	//
	// Without this step a shared board never reaches the database: modes.Mode.Apply
	// can only return the caller's own participant state, so two players claiming
	// cells would each see their own move and nobody would see the board. The
	// extension is optional, and CommitShared is pure and deterministic, so a
	// retried transaction recomputes byte-identical state instead of corrupting the
	// board (modeutil.SharedStateMode documents the contract).
	if sm, ok := mode.(sharedStateMode); ok {
		publicAfter, privateAfter, serr := sm.CommitShared(modes.ApplyInput{
			MembershipID:    actor.MembershipID(),
			Scope:           r.Scope(),
			Subject:         subject,
			Action:          in.ModeAction,
			Public:          publicState,
			Private:         privateState,
			State:           currentState,
			SubmissionCount: accepted,
			ReceivedAt:      receivedMS,
		}, decision)
		if serr != nil {
			return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: commit shared state: %w", serr))
		}
		if err := writeRoundState(ctx, tx, r.ID, "public", publicAfter, now); err != nil {
			return SubmissionReceipt{}, err
		}
		if err := writeRoundState(ctx, tx, r.ID, "round", privateAfter, now); err != nil {
			return SubmissionReceipt{}, err
		}
	}

	if r.ModeID == "bluff" && r.Status == modes.PhaseCollecting {
		public, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return SubmissionReceipt{}, err
		}
		complete, err := bluff.WritingComplete(public)
		if err != nil {
			return SubmissionReceipt{}, problem.Internal(err)
		}
		if complete {
			if err := s.finishBluffWritingQ(ctx, tx, r, time.UnixMilli(now)); err != nil {
				return SubmissionReceipt{}, err
			}
			r.Status = modes.PhaseLocked
			r.PhaseVersion++
		}
		private, err := roundPrivateStateQ(ctx, tx, r.ID)
		if err != nil {
			return SubmissionReceipt{}, err
		}
		states, err := participantStatesQ(ctx, tx, r.ID)
		if err != nil {
			return SubmissionReceipt{}, err
		}
		over, err := bluff.VotingComplete(public, private, states)
		if err != nil {
			return SubmissionReceipt{}, problem.Internal(err)
		}
		if over {
			decision.RoundOver = true
		}
	}
	// Classic guesses need their own uniqueness rows so the "same character twice"
	// rule is enforced by SQL, not by application memory .
	if in.ModeAction.Kind == "classic.guess" {
		if err := recordClassicGuess(ctx, tx, r.ID, subject.ID, in.ModeAction.Payload, decision, now); err != nil {
			return SubmissionReceipt{}, err
		}
	}

	// Accepted actions must never invoke timeout defaults. Renew the clock only
	// after the shared state moves to another turn, not after a partial commit.
	if progress, ok := mode.(modes.TurnProgress); ok && r.Status == modes.PhaseCollecting && s.turnWindowMS(r) > 0 {
		before, _, err := progress.OpenTurn(publicState)
		if err != nil {
			return SubmissionReceipt{}, problem.Internal(err)
		}
		afterPublic, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return SubmissionReceipt{}, err
		}
		after, more, err := progress.OpenTurn(afterPublic)
		if err != nil {
			return SubmissionReceipt{}, problem.Internal(err)
		}
		if !more {
			decision.RoundOver = true
		} else if before != after {
			if _, err := tx.ExecContext(ctx,
				`UPDATE rounds SET deadline_at = ?, updated_at = ? WHERE id = ? AND status = ?`,
				now+s.turnWindowMS(r), now, r.ID, string(modes.PhaseCollecting)); err != nil {
				return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: refresh turn clock: %w", err))
			}
		}
	}

	// A mode may declare that its outcome is now fixed for everyone (first-correct
	// in classic). Close the round inside this same transaction so a client that
	// reads the round immediately after its receipt already sees the new phase —
	// otherwise the winner would see "collecting" and keep guessing until the
	// deadline sweeper caught up.
	afterPublic, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if err := s.rotateTeamControlsQ(ctx, tx, r, mode, publicState, afterPublic, subject.ID, currentState, now); err != nil {
		return SubmissionReceipt{}, err
	}
	closedPhase := r.Status
	if decision.RoundOver && r.Status == modes.PhaseCollecting {
		closedPhase = modes.PhaseLocked
		if _, err := tx.ExecContext(ctx,
			`UPDATE rounds SET status = ?, phase_version = phase_version + 1, deadline_at = ?, updated_at = ?
			 WHERE id = ? AND status = ?`,
			string(closedPhase), now+1000, now, r.ID, string(r.Status)); err != nil {
			return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: close round: %w", err))
		}
	}

	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return SubmissionReceipt{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return SubmissionReceipt{}, err
	}

	receipt := SubmissionReceipt{
		SubmissionID:       submissionID,
		ReceiptID:          receiptID,
		AcceptedAt:         now,
		RoomVersion:        roomVersion,
		PhaseVersion:       r.PhaseVersion,
		SubmissionRevision: nextRevision,
		Feedback:           decision.Feedback,
	}
	if closedPhase != r.Status {
		receipt.PhaseVersion++
	}
	if err := s.saveGameplayQ(ctx, tx, actor.SessionID, commandScope, in.Command, receipt, now); err != nil {
		return SubmissionReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return SubmissionReceipt{}, problem.Internal(fmt.Errorf("room: commit submission: %w", err))
	}
	return receipt, nil
}

// recordClassicGuess inserts the classic guess row.
//
// The attempt ordinal is derived inside the transaction so two simultaneous
// guesses cannot both claim the same ordinal, and the unique index on
// (round, subject, character_id) rejects a repeated character without consuming
// an attempt .
func recordClassicGuess(ctx context.Context, tx *sql.Tx, roundID, subjectID string,
	payload json.RawMessage, decision modes.Decision, now int64) error {
	var guess struct {
		CharacterID string `json:"characterId"`
	}
	if err := json.Unmarshal(payload, &guess); err != nil || guess.CharacterID == "" {
		return problem.Validation("modeAction is invalid",
			problem.FieldError{Field: "modeAction.characterId", Message: "must be present"})
	}
	var next int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(attempt), 0) + 1 FROM classic_guesses
		 WHERE round_id = ? AND subject_id = ?`, roundID, subjectID).Scan(&next); err != nil {
		return problem.Internal(fmt.Errorf("room: next guess attempt: %w", err))
	}
	correct := 0
	if decision.Finished {
		correct = 1
	}
	feedback := decision.Feedback
	if len(feedback) == 0 {
		feedback = json.RawMessage("{}")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO classic_guesses (round_id, subject_id, attempt, character_id, correct,
		                             feedback_json, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		roundID, subjectID, next, guess.CharacterID, correct, string(feedback), now); err != nil {
		return problem.Internal(fmt.Errorf("room: insert classic guess: %w", err))
	}
	return nil
}

// slotFor maps an action kind to its submission slot.
//
// A slot is the addressable unit a revision belongs to: revising a lineup must
// not collide with a vote. The mapping is explicit rather than derived from the
// kind string, so an unknown kind cannot silently invent a slot.
func slotFor(kind string) string {
	switch kind {
	case "classic.guess":
		return "guess"
	case "grid.claim":
		return "grid"
	case "draft.lineup":
		return "lineup"
	case "race.order", "sort.order":
		return "order"
	case "path.route":
		return "route"
	case "memory.flip":
		return "flip"
	case "bluff.decoy":
		return "decoy"
	case "bluff.vote", "creative.vote":
		return "vote"
	case "clue.assignment":
		return "assignment"
	case "strategy.action":
		return "action"
	case "rapport.answer":
		return "answer"
	case "creative.work":
		return "work"
	case "pedigree.assignment":
		return "assignment"
	case "audio.matches":
		return "matches"
	case "audio.ready":
		return "ready"
	}
	return "action"
}

// submissionHeadQ reads the current head revision for a slot.
func submissionHeadQ(ctx context.Context, tx *sql.Tx, roundID, subjectID, slot string) (int64, bool, error) {
	var rev int64
	err := tx.QueryRowContext(ctx, `
		SELECT revision FROM submission_heads
		WHERE round_id = ? AND subject_id = ? AND slot = ?`,
		roundID, subjectID, slot).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, problem.Internal(fmt.Errorf("room: read submission head: %w", err))
	}
	return rev, true, nil
}

// participantStateQ reads one subject's participant state.
func participantStateQ(ctx context.Context, tx *sql.Tx, roundID, subjectID string) (json.RawMessage, error) {
	var body string
	err := tx.QueryRowContext(ctx, `
		SELECT state_json FROM participant_round_state WHERE round_id = ? AND subject_id = ?`,
		roundID, subjectID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read participant state: %w", err))
	}
	return json.RawMessage(body), nil
}

// acceptedCountQ counts the subject's accepted actions in this round.
func acceptedCountQ(ctx context.Context, tx *sql.Tx, roundID, subjectID string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM submissions
		WHERE round_id = ? AND subject_id = ? AND accepted = 1`,
		roundID, subjectID).Scan(&n)
	if err != nil {
		return 0, problem.Internal(fmt.Errorf("room: count submissions: %w", err))
	}
	return n, nil
}

// nonEmpty returns fallback when v is empty.
func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// CurrentSubmissionRevision reports the live revision for the caller's slot.
//
// The round view needs it so a client can present the correct revision without
// guessing .
//
// A revision is per SLOT, not per subject: submission_heads is keyed by
// (round, subject, slot) and CreateSubmission compares the caller's revision
// against the head of the slot its action belongs to. Taking MAX across slots
// therefore reported the wrong number for any mode with more than one slot, and a
// client that trusted it was refused with submission-revision-conflict even though
// it had done nothing wrong. Use SubmissionRevisionsBySlot for those modes; this
// single-value accessor stays for the modes that only ever submit one slot.
func (s *Service) CurrentSubmissionRevision(ctx context.Context, roundID, subjectID string) (int64, error) {
	var rev int64
	err := s.db.Read().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(revision), 0) FROM submission_heads WHERE round_id = ? AND subject_id = ?`,
		roundID, subjectID).Scan(&rev)
	if err != nil {
		return 0, problem.Internal(fmt.Errorf("room: read revision: %w", err))
	}
	return rev, nil
}

// SubmissionRevisionsBySlot reports the live revision for every slot the subject
// has ever submitted to in this round.
//
// Slots with no head are simply absent: the caller's first submission to such a
// slot must use revision 0, which is what CreateSubmission requires. Returning an
// explicit map (rather than defaulting every known slot to 0) keeps "never
// submitted" and "submitted at revision 0" distinguishable, so a client cannot
// mistake one for the other.
func (s *Service) SubmissionRevisionsBySlot(ctx context.Context, roundID, subjectID string) (map[string]int64, error) {
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT slot, revision FROM submission_heads WHERE round_id = ? AND subject_id = ?`,
		roundID, subjectID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read slot revisions: %w", err))
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var slot string
		var rev int64
		if err := rows.Scan(&slot, &rev); err != nil {
			return nil, problem.Internal(fmt.Errorf("room: scan slot revision: %w", err))
		}
		out[slot] = rev
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Internal(fmt.Errorf("room: iterate slot revisions: %w", err))
	}
	return out, nil
}

// SubmissionSlotsForMode lists the slots a mode can submit to, in a stable order.
//
// The view publishes the per-slot revisions for exactly these keys, so a client
// knows which revision to send for the action it is about to take without knowing
// the server's internal slot names by heart. An unknown mode reports no slots, and
// the client then falls back to the single submissionRevision.
func SubmissionSlotsForMode(modeID string, kinds []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		slot := slotFor(kind)
		if slot == "" || seen[slot] {
			continue
		}
		seen[slot] = true
		out = append(out, slot)
	}
	sort.Strings(out)
	return out
}

// CreateSurrender records that a participant left the round.
//
// Surrender ends the round for that subject only; it never grants kick or
// removal authority to the surrendering player .
func (s *Service) CreateSurrender(ctx context.Context, actor Actor, roundID string, phaseVersion int64) (surrenderID string, acceptedAt int64, err error) {
	subject, err := actor.Subject()
	if err != nil {
		return "", 0, err
	}
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return "", 0, err
	}
	defer release()
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return "", 0, problem.Internal(fmt.Errorf("room: begin surrender: %w", err))
	}
	defer tx.Rollback()

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return "", 0, err
	}
	if r.RoomID != actor.RoomID {
		return "", 0, errRoundNotFound
	}
	if err := s.requireTeamControllerQ(ctx, tx, r, actor); err != nil {
		return "", 0, err
	}
	if r.Status != modes.PhaseCollecting && r.Status != modes.PhaseBriefing {
		return "", 0, problem.Conflict("round-not-collecting",
			fmt.Sprintf("the round is %s and cannot be surrendered", r.Status))
	}
	if phaseVersion != r.PhaseVersion {
		return "", 0, phaseConflict(r.PhaseVersion)
	}

	id, err := newID("srd_")
	if err != nil {
		return "", 0, problem.Internal(err)
	}
	// Surrender is recorded as a state change so settlement can read it back
	// without a dedicated table (the schema has none for it).
	state, err := participantStateQ(ctx, tx, r.ID, subject.ID)
	if err != nil {
		return "", 0, err
	}
	merged := map[string]any{}
	if len(state) > 0 {
		_ = json.Unmarshal(state, &merged)
	}
	merged["surrendered"] = true
	merged["surrenderedAt"] = now
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return "", 0, problem.Internal(fmt.Errorf("room: encode surrender state: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO participant_round_state (round_id, subject_id, subject_kind, state_json, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (round_id, subject_id) DO UPDATE SET
			state_json = excluded.state_json, updated_at = excluded.updated_at`,
		r.ID, subject.ID, string(subject.Kind), string(mergedJSON), now); err != nil {
		return "", 0, problem.Internal(fmt.Errorf("room: write surrender: %w", err))
	}

	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return "", 0, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID, "roomVersion": roomVersion,
	}, now); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, problem.Internal(fmt.Errorf("room: commit surrender: %w", err))
	}
	return id, now, nil
}

// sharedStateMode is the optional mode extension that owns round-level shared
// state.
//
// It is declared structurally here rather than imported from
// internal/modes/modeutil so the room package does not depend on a helper package
// a mode author may reorganise. The method set is identical to
// modeutil.SharedStateMode, so any mode satisfying that interface satisfies this
// one.
type sharedStateMode interface {
	modes.Mode
	CommitShared(in modes.ApplyInput, dec modes.Decision) (public, private json.RawMessage, err error)
}

// writeRoundState upserts one scope of round_private_state.
//
// scope "public" holds the viewer-safe question state and scope "round" holds the
// server-only secrets, matching how CreateRound seeds them. A nil or invalid value
// leaves the existing row untouched rather than blanking it: a mode that returns no
// update must not erase state another action already committed.
func writeRoundState(ctx context.Context, tx *sql.Tx, roundID, scope string, value json.RawMessage, now int64) error {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, ?, ?)
		ON CONFLICT (round_id, scope) DO UPDATE SET state_json = excluded.state_json`,
		roundID, scope, string(value)); err != nil {
		return problem.Internal(fmt.Errorf("room: write round %s state: %w", scope, err))
	}
	return nil
}

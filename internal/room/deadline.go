package room

// Deadline sweeping advances timed phases even when no client remains connected.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/audio"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
)

// SweepInterval is how often expired deadlines are checked.
//
// One second is chosen against the shortest configured window (question timers go as low
// as a few seconds): a coarser tick would let a very short round sit visibly past its
// deadline, while a tighter one would burn a query per tick for no benefit.
const SweepInterval = time.Second

// timedPhases are the phases that carry a deadline and therefore expire.
//
// revealed is included because a reveal that nobody dismisses would otherwise hold the
// room open; it advances to settled. settled and cancelled are terminal and never swept.
var timedPhases = map[modes.Phase]modes.Phase{
	modes.PhaseBriefing:   modes.PhaseCollecting,
	modes.PhaseCollecting: modes.PhaseLocked,
	modes.PhaseLocked:     modes.PhaseRevealed,
	modes.PhaseRevealed:   modes.PhaseSettled,
}

// RunDeadlineSweeper advances expired rounds until ctx is cancelled.
//
// It is safe to call once per process: the single-writer SQLite connection plus the
// per-round phase_version check in the transition path mean two sweepers cannot both
// advance the same round.
func (s *Service) RunDeadlineSweeper(ctx context.Context) {
	ticker := time.NewTicker(SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepExpired(ctx); err != nil {
				// A failed sweep must not kill the loop: the next tick retries. The
				// error is reported through the service's logger by the caller, not
				// swallowed into a silent no-op.
				s.sweepErr = err
			} else {
				s.sweepErr = nil
			}
		}
	}
}

// SweepExpired advances every round whose deadline has passed.
//
// It returns how many rounds it advanced. Each round is advanced in its own transaction
// so one stuck round cannot block the others.
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	now := s.now().UTC()
	if err := s.SweepMatchmaking(ctx); err != nil {
		return 0, err
	}
	idleCount, err := s.sweepIdleMatches(ctx, now)
	if err != nil {
		return 0, err
	}
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT id FROM rounds
		WHERE deadline_at IS NOT NULL
		  AND deadline_at <= ?
		  AND paused_at IS NULL
		  AND status IN (?, ?, ?, ?)`,
		now.UnixMilli(),
		modes.PhaseBriefing, modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseRevealed)
	if err != nil {
		return 0, fmt.Errorf("room: query expired rounds: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("room: scan expired round: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("room: iterate expired rounds: %w", err)
	}
	rows.Close()

	advanced := idleCount
	for _, id := range ids {
		ok, err := s.advanceExpired(ctx, id, now)
		if err != nil {
			return advanced, err
		}
		if ok {
			advanced++
		}
	}
	return advanced, nil
}

// expireTurnQ closes the round's open turn on the server's authority.
//
// The mode records the default action for every team that did not commit, so the
// committed log stays the single source of truth and nothing downstream needs a
// "was this a timeout" special case. If a turn is still open afterwards the round
// gets a fresh turn deadline and stays collecting; when no turn is left the round
// enters the normal locked/revealed/settled sequence.
//
// It is idempotent against repeated sweeps: the mode only appends commits for
// teams that have none, and the phase_version guard keeps a concurrent host
// transition from being overwritten.
func (s *Service) expireTurnQ(ctx context.Context, tx *sql.Tx, r Round, now time.Time) (bool, error) {
	mode, ok := s.modes.Get(r.ModeID)
	if !ok {
		return false, nil
	}
	expirer, ok := mode.(modes.TurnExpirer)
	if !ok {
		return false, nil
	}
	public, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return false, err
	}
	next, more, err := expirer.ExpireTurn(public)
	if err != nil {
		return false, fmt.Errorf("room: expire turn: %w", err)
	}
	if len(next) > 0 && string(next) != string(public) {
		if err := updateRoundPublicStateQ(ctx, tx, r.ID, next); err != nil {
			return false, err
		}
		if err := s.rotateTeamControlsQ(ctx, tx, r, mode, public, next, "", nil, now.UnixMilli()); err != nil {
			return false, err
		}
	}
	status := modes.PhaseCollecting
	deadline := now.Add(time.Duration(s.turnWindowMS(r)) * time.Millisecond).UnixMilli()
	if !more {
		status = modes.PhaseLocked
		deadline = now.Add(time.Second).UnixMilli()
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE rounds SET status = ?, deadline_at = ?, phase_version = phase_version + 1, updated_at = ?
		 WHERE id = ? AND phase_version = ? AND status = ?`,
		status, deadline, now.UnixMilli(), r.ID, r.PhaseVersion, string(modes.PhaseCollecting)); err != nil {
		return false, fmt.Errorf("room: refresh turn deadline: %w", err)
	}
	if _, err := bumpRoomVersion(ctx, tx, r.RoomID, now.UnixMilli()); err != nil {
		return false, err
	}

	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{
		"resource": "round", "roundId": r.ID,
	}, now.UnixMilli()); err != nil {
		return false, err
	}
	return true, nil
}

// advanceExpired moves one round to its next phase if its deadline has passed.
//
// It re-reads the round inside the transaction: the id came from a query a moment ago,
// and a host transition may have moved the round in between. The phase_version guard
// makes the update a no-op in that case rather than an overwrite.
func (s *Service) advanceExpired(ctx context.Context, roundID string, now time.Time) (bool, error) {
	receivedAt, release, err := s.queue.Enqueue(ctx, "")
	if err != nil {
		return false, err
	}
	defer release()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("room: begin sweep tx: %w", err)
	}
	defer tx.Rollback()

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return false, err
	}
	next, ok := timedPhases[r.Status]
	if !ok {
		return false, nil
	}
	if r.DeadlineAt == nil || r.DeadlineAt.After(now) || r.PausedAt != nil {
		return false, nil
	}

	if r.ModeID == "creative" && r.Status != modes.PhaseBriefing {
		handled, err := s.creativeExpiryQ(ctx, tx, r, now)
		if err != nil {
			return false, err
		}
		if handled {
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	// Historical early locks may predate the host gate. Stop their timer once;
	// do not fabricate actions/scores or fail the same settlement every tick.
	if (r.ModeID == "strategy-race" || r.ModeID == "rapport" || r.ModeID == "trade") && (r.Status == modes.PhaseLocked || r.Status == modes.PhaseRevealed) {
		public, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return false, err
		}
		more, err := hasUnfinishedWindows(r.ModeID, public)
		if err != nil {
			return false, err
		}
		if more {
			if _, err := tx.ExecContext(ctx, `UPDATE rounds SET deadline_at=NULL,phase_version=phase_version+1,updated_at=? WHERE id=?`, now.UnixMilli(), r.ID); err != nil {
				return false, err
			}
			if _, err := bumpRoomVersion(ctx, tx, r.RoomID, now.UnixMilli()); err != nil {
				return false, err
			}
			if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{"resource": "round", "roundId": r.ID}, now.UnixMilli()); err != nil {
				return false, err
			}
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	if r.ModeID == "bluff" && r.Status != modes.PhaseRevealed {
		public, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return false, err
		}
		stage, err := bluff.Stage(public)
		if err != nil {
			return false, err
		}
		if stage == "legacy" {
			if _, err := tx.ExecContext(ctx, `UPDATE rounds SET status='locked',phase_version=phase_version+1,deadline_at=NULL,updated_at=? WHERE id=?`, now.UnixMilli(), r.ID); err != nil {
				return false, err
			}
			if _, err := bumpRoomVersion(ctx, tx, r.RoomID, now.UnixMilli()); err != nil {
				return false, err
			}
			if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{"resource": "round", "roundId": r.ID}, now.UnixMilli()); err != nil {
				return false, err
			}
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return true, nil
		}
		if stage == "writing" && r.Status == modes.PhaseCollecting {
			if err := s.finishBluffWritingQ(ctx, tx, r, now); err != nil {
				return false, err
			}
			if _, err := bumpRoomVersion(ctx, tx, r.RoomID, now.UnixMilli()); err != nil {
				return false, err
			}
			if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{"resource": "round", "roundId": r.ID}, now.UnixMilli()); err != nil {
				return false, err
			}
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	/*
	 * Turn-based modes get a turn clock, not a phase clock.
	 *
	 * When the deadline that just expired is the CURRENT TURN's deadline, the
	 * round must NOT advance to locked: it has to close that turn (defaulting
	 * every silent team) and start the next turn's clock. Advancing the phase
	 * here is what made a race where nobody submitted sit on turn 1 for the
	 * whole 160 seconds while the screen said "每回合 20 秒".
	 */
	if r.Status == modes.PhaseCollecting && s.turnWindowMS(r) > 0 {
		expired, err := s.expireTurnQ(ctx, tx, r, now)
		if err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("room: commit turn expiry: %w", err)
		}
		return expired, nil
	}

	// Audio preparation is a roster-wide start gate, including timed briefing.
	var audioFault bool
	if r.ModeID == "audio" {
		public, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return false, err
		}
		states, err := participantStatesQ(ctx, tx, r.ID)
		if err != nil {
			return false, err
		}
		if next == modes.PhaseCollecting {
			ready, err := audio.AllPrepared(public, states)
			if err != nil {
				return false, err
			}
			if !ready {
				return false, nil
			}
		}
		for _, raw := range states {
			var state struct {
				PlaybackFailures []string `json:"playbackFailures"`
			}
			if err := json.Unmarshal(raw, &state); err != nil {
				return false, err
			}
			if len(state.PlaybackFailures) > 0 {
				audioFault = true
			}
		}
		// Stop at the locked window for host cancellation instead of repeatedly
		// attempting a known-invalid settlement on every scheduler tick.
		if audioFault && (next == modes.PhaseRevealed || next == modes.PhaseSettled) {
			return false, nil
		}
	}
	// Keep timed phases moving without relying on a connected host. Short reveal
	// windows preserve the phase order; only settled writes final scores.
	var deadline any
	switch next {
	case modes.PhaseCollecting:
		window := s.collectingWindowMS(r)
		if turn := s.turnWindowMS(r); turn > 0 {
			window = turn
		}
		deadline = now.UnixMilli() + window
	case modes.PhaseLocked:
		if !audioFault {
			deadline = now.Add(time.Second).UnixMilli()
		}
	case modes.PhaseRevealed:
		deadline = now.Add(3 * time.Second).UnixMilli()
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE rounds SET status = ?, phase_version = phase_version + 1,
		   deadline_at = ?, updated_at = ? WHERE id = ? AND phase_version = ?`,
		next, deadline, now.UnixMilli(), roundID, r.PhaseVersion); err != nil {
		return false, fmt.Errorf("room: advance expired round: %w", err)
	}
	if err := s.syncSegmentStatusQ(ctx, tx, r.ID, next, now.UnixMilli()); err != nil {
		return false, err
	}
	if next == modes.PhaseSettled {
		if err := s.settleRoundQ(ctx, tx, r, next, now.UnixMilli()); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE rooms SET status = 'finished', updated_at = ? WHERE id = ? AND status = 'playing'`, now.UnixMilli(), r.RoomID); err != nil {
			return false, err
		}
	}
	if _, err := bumpRoomVersion(ctx, tx, r.RoomID, now.UnixMilli()); err != nil {
		return false, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{"resource": "round", "roundId": r.ID}, now.UnixMilli()); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("room: commit sweep: %w", err)
	}
	_ = receivedAt
	return true, nil
}

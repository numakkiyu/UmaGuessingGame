package room

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"time"
)

func closeBluffWritingQ(ctx context.Context, tx *sql.Tx, r Round, now int64) error {
	public, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	private, err := roundPrivateStateQ(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	nextPublic, nextPrivate, err := bluff.CloseWriting(public, private, r.Seed)
	if err != nil {
		return problem.Conflict("bluff-writing-closed", "writing is already closed")
	}
	if err := writeRoundState(ctx, tx, r.ID, "public", nextPublic, now); err != nil {
		return err
	}
	if err := writeRoundState(ctx, tx, r.ID, "round", nextPrivate, now); err != nil {
		return err
	}
	return nil
}

// bluffTransitionQ handles writing/review/voting inside existing authorised,
// version-checked transitions. Pause/resume retains the current stage's clock.
func (s *Service) bluffTransitionQ(ctx context.Context, tx *sql.Tx, r Round, in TransitionInput, now int64) (modes.Phase, int64, *int64, error) {
	public, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return "", 0, nil, err
	}
	stage, err := bluff.Stage(public)
	if err != nil {
		return "", 0, nil, err
	}
	if stage == "legacy" && in.Target != "cancel" {
		return "", 0, nil, problem.Conflict("bluff-legacy-round", "cancel this older round and prepare a new one")
	}
	if r.Paused() && in.Target != "resume" && in.Target != "cancel" {
		return "", 0, nil, problem.Conflict("round-paused", "resume before changing the stage")
	}
	if in.Target == "bluff-voting" {
		if r.Status != modes.PhaseLocked || r.Paused() || stage != "review" {
			return "", 0, nil, problem.Conflict("bluff-review-not-open", "review is not open")
		}
		private, err := roundPrivateStateQ(ctx, tx, r.ID)
		if err != nil {
			return "", 0, nil, err
		}
		pub, priv, err := bluff.OpenVoting(public, private, r.Seed, in.ExcludedOptionIDs)
		if err != nil {
			return "", 0, nil, problem.Unprocessable("bluff-review-invalid", "check the excluded choices and retain at least two reviewed options")
		}
		if err := writeRoundState(ctx, tx, r.ID, "public", pub, now); err != nil {
			return "", 0, nil, err
		}
		if err := writeRoundState(ctx, tx, r.ID, "round", priv, now); err != nil {
			return "", 0, nil, err
		}
		_, voteMS, err := bluff.Windows(modeConfigOf(r.Config))
		if err != nil {
			return "", 0, nil, err
		}
		deadline := now + voteMS
		states, err := participantStatesQ(ctx, tx, r.ID)
		if err != nil {
			return "", 0, nil, err
		}
		complete, err := bluff.VotingComplete(pub, priv, states)
		if err != nil {
			return "", 0, nil, problem.Internal(err)
		}
		if complete {
			deadline = now + 1000
			return modes.PhaseLocked, r.PhaseVersion + 1, &deadline, nil
		}
		return modes.PhaseCollecting, r.PhaseVersion + 1, &deadline, nil
	}
	if stage == "review" && in.Target != "cancel" {
		return "", 0, nil, problem.Conflict("bluff-review-required", "finish moderation before starting the ballot")
	}
	if stage == "writing" && (in.Target == "revealed" || in.Target == "settled") {
		return "", 0, nil, problem.Conflict("bluff-writing-open", "close writing and review the choices before voting")
	}
	phase, version, deadline, _, _, _, err := s.applyTransition(r, in, now)
	if err != nil {
		return "", 0, nil, err
	}
	if stage == "writing" && in.Target == "locked" {
		if err := closeBluffWritingQ(ctx, tx, r, now); err != nil {
			return "", 0, nil, err
		}
		deadline = nil
	}
	return phase, version, deadline, nil
}
func (s *Service) finishBluffWritingQ(ctx context.Context, tx *sql.Tx, r Round, now time.Time) error {
	if err := closeBluffWritingQ(ctx, tx, r, now.UnixMilli()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rounds SET status='locked',phase_version=phase_version+1,deadline_at=NULL,updated_at=? WHERE id=? AND phase_version=?`, now.UnixMilli(), r.ID, r.PhaseVersion); err != nil {
		return fmt.Errorf("room: close bluff writing: %w", err)
	}
	return s.syncSegmentStatusQ(ctx, tx, r.ID, modes.PhaseLocked, now.UnixMilli())
}

package room

import (
	"context"
	"database/sql"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
)

func creativeStageQ(ctx context.Context, tx *sql.Tx, r Round, closeWriting bool, now int64) error {
	public, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	var next []byte
	if closeWriting {
		next, err = creative.CloseWriting(public)
	} else {
		next, err = creative.CloseVoting(public)
	}
	if err != nil {
		return problem.Conflict("creative-stage-closed", "this stage has already closed")
	}
	return writeRoundState(ctx, tx, r.ID, "public", next, now)
}

func (s *Service) creativeTransitionQ(ctx context.Context, tx *sql.Tx, r Round, in TransitionInput, now int64) (modes.Phase, int64, *int64, error) {
	public, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return "", 0, nil, err
	}
	stage, err := creative.Stage(public)
	if err != nil {
		return "", 0, nil, err
	}
	if stage == "legacy" && in.Target != "cancel" {
		return "", 0, nil, problem.Conflict("creative-legacy-round", "cancel this older round and prepare a new one")
	}
	if r.Paused() && in.Target != "resume" && in.Target != "cancel" {
		return "", 0, nil, problem.Conflict("round-paused", "resume before changing the stage")
	}
	if in.Target == "creative-voting" {
		if r.Status != modes.PhaseLocked || r.Paused() || stage != "review" {
			return "", 0, nil, problem.Conflict("creative-review-not-open", "review is not open")
		}
		private, err := roundPrivateStateQ(ctx, tx, r.ID)
		if err != nil {
			return "", 0, nil, err
		}
		next, err := creative.OpenVoting(public, private, in.ExcludedOptionIDs)
		if err != nil {
			return "", 0, nil, problem.Unprocessable("creative-review-invalid", "check the excluded works")
		}
		if err = writeRoundState(ctx, tx, r.ID, "public", next, now); err != nil {
			return "", 0, nil, err
		}
		_, voteMS, err := creative.Windows(modeConfigOf(r.Config))
		if err != nil {
			return "", 0, nil, err
		}
		deadline := now + voteMS
		return modes.PhaseCollecting, r.PhaseVersion + 1, &deadline, nil
	}
	if stage == "review" && in.Target != "cancel" && in.Target != "pause" && in.Target != "resume" {
		return "", 0, nil, problem.Conflict("creative-review-required", "finish review before opening the ballot")
	}
	if (stage == "writing" || stage == "voting") && (in.Target == "revealed" || in.Target == "settled") {
		return "", 0, nil, problem.Conflict("creative-stage-open", "close the current stage first")
	}
	phase, version, deadline, _, _, _, err := s.applyTransition(r, in, now)
	if err != nil {
		return "", 0, nil, err
	}
	if in.Target == "locked" && (stage == "writing" || stage == "voting") {
		if err := creativeStageQ(ctx, tx, r, stage == "writing", now); err != nil {
			return "", 0, nil, err
		}
		deadline = nil
		if stage == "voting" {
			n := now + 1000
			deadline = &n
		}
	}
	return phase, version, deadline, nil
}

// creativeExpiryQ stops legacy/inconsistent clocks once and owns the two actual
// timed windows. Review has no deadline and never silently approves works.
func (s *Service) creativeExpiryQ(ctx context.Context, tx *sql.Tx, r Round, now time.Time) (bool, error) {
	public, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return false, err
	}
	stage, err := creative.Stage(public)
	if err != nil {
		return false, err
	}
	if stage == "closed" {
		return false, nil
	}
	var deadline any
	if stage == "writing" && r.Status == modes.PhaseCollecting {
		if err = creativeStageQ(ctx, tx, r, true, now.UnixMilli()); err != nil {
			return false, err
		}
	} else if stage == "voting" && r.Status == modes.PhaseCollecting {
		if err = creativeStageQ(ctx, tx, r, false, now.UnixMilli()); err != nil {
			return false, err
		}
		deadline = now.Add(time.Second).UnixMilli()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE rounds SET status='locked',phase_version=phase_version+1,deadline_at=?,updated_at=? WHERE id=? AND phase_version=?`, deadline, now.UnixMilli(), r.ID, r.PhaseVersion); err != nil {
		return false, err
	}
	if err = s.syncSegmentStatusQ(ctx, tx, r.ID, modes.PhaseLocked, now.UnixMilli()); err != nil {
		return false, err
	}
	if _, err = bumpRoomVersion(ctx, tx, r.RoomID, now.UnixMilli()); err != nil {
		return false, err
	}
	if err = emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{"resource": "round", "roundId": r.ID}, now.UnixMilli()); err != nil {
		return false, err
	}
	return true, nil
}

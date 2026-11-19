package room

import (
	"context"
	"database/sql"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// matchIdleExpiredQ uses the fixed matching rules' answering window. The preview
// is excluded, and only an accepted game action establishes activity.
func (s *Service) matchIdleExpiredQ(ctx context.Context, tx *sql.Tx, r Round, receivedMS int64) (bool, error) {
	seconds := s.cfg.Policy.Matchmaking.IdleWindow()
	if seconds == 0 || r.Status != modes.PhaseCollecting || r.DeadlineAt == nil {
		return false, nil
	}
	start := r.DeadlineAt.UnixMilli() - s.collectingWindowMS(r)
	if receivedMS <= start+int64(seconds)*1000 {
		return false, nil
	}
	var idle bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(json_extract(config_json,'$.kind')='matchmaking',0)
 AND NOT EXISTS(SELECT 1 FROM submissions WHERE round_id=? AND accepted=1)
 FROM rooms WHERE id=?`, r.ID, r.RoomID).Scan(&idle)
	return idle, err
}

func (s *Service) sweepIdleMatches(ctx context.Context, now time.Time) (int, error) {
	if s.cfg.Policy.Matchmaking.IdleWindow() == 0 {
		return 0, nil
	}
	rows, err := s.db.Read().QueryContext(ctx, `SELECT r.id,r.room_id FROM rounds r JOIN rooms rm ON rm.id=r.room_id
 WHERE r.status='collecting' AND r.paused_at IS NULL AND r.deadline_at IS NOT NULL
 AND r.created_at<? AND json_extract(rm.config_json,'$.kind')='matchmaking'
 AND NOT EXISTS(SELECT 1 FROM submissions sub WHERE sub.round_id=r.id AND sub.accepted=1)`, now.UnixMilli()-int64(s.cfg.Policy.Matchmaking.IdleWindow())*1000)
	if err != nil {
		return 0, err
	}
	type candidate struct{ round, room string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.round, &c.room); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, c := range candidates {
		changed, err := s.cancelIdleMatch(ctx, c.round, c.room, now)
		if err != nil {
			return count, err
		}
		if changed {
			count++
		}
	}
	return count, nil
}

func (s *Service) cancelIdleMatch(ctx context.Context, roundID, roomID string, now time.Time) (bool, error) {
	// The same room queue orders submissions and cleanup. An action already
	// admitted before the idle boundary completes before cancellation is decided.
	_, release, err := s.queue.Enqueue(ctx, roomID)
	if err != nil {
		return false, err
	}
	defer release()
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return false, err
	}
	expired, err := s.matchIdleExpiredQ(ctx, tx, r, now.UnixMilli())
	if err != nil || !expired {
		return false, err
	}
	ms := now.UnixMilli()
	if _, err = tx.ExecContext(ctx, `UPDATE rounds SET status='cancelled',phase_version=phase_version+1,deadline_at=NULL,updated_at=? WHERE id=?`, ms, r.ID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE rooms SET status='cancelled',updated_at=? WHERE id=?`, ms, r.RoomID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE matchmaking_tickets SET status='finished',updated_at=?,cooldown_until=? WHERE room_id=? AND status IN ('ready','playing')`, ms, ms+15000, r.RoomID); err != nil {
		return false, err
	}
	if _, err = bumpRoomVersion(ctx, tx, r.RoomID, ms); err != nil {
		return false, err
	}
	if err = emitOutbox(ctx, tx, r.RoomID, "state.changed", map[string]any{"resource": "round", "roundId": r.ID, "reason": "match-idle"}, ms); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

package room

import (
	"context"
	"fmt"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// PauseInterruptedEvents runs before accepting requests or starting the deadline
// sweeper. An interrupted hosted activity must wait for its host, rather than
// silently reveal or score answers while everybody is reconnecting.
func (s *Service) PauseInterruptedEvents(ctx context.Context) (int, error) {
	count := 0
	for {
		rows, err := s.db.Read().QueryContext(ctx, `SELECT r.id FROM rounds r JOIN rooms room ON room.id=r.room_id
			WHERE json_extract(room.config_json,'$.kind')='event' AND r.paused_at IS NULL
			AND (r.status IN ('briefing','collecting') OR (r.status IN ('locked','revealed') AND r.deadline_at IS NOT NULL))
			ORDER BY r.id LIMIT 100`)
		if err != nil {
			return count, fmt.Errorf("room: read interrupted activities: %w", err)
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return count, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return count, err
		}
		if len(ids) == 0 {
			return count, nil
		}
		for _, id := range ids {
			if err := s.pauseInterruptedEvent(ctx, id); err != nil {
				return count, err
			}
			count++
		}
	}
}

func (s *Service) pauseInterruptedEvent(ctx context.Context, id string) error {
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := s.loadRoundQ(ctx, tx, id)
	if err != nil {
		return err
	}
	if r.PausedAt != nil || r.Status == modes.PhaseSettled || r.Status == modes.PhaseCancelled {
		return nil
	}
	now := s.nowMS()
	var remaining *int64
	if r.DeadlineAt != nil {
		ms := max(int64(0), r.DeadlineAt.UnixMilli()-now)
		remaining = &ms
	}
	version := r.PhaseVersion + 1
	if _, err := tx.ExecContext(ctx, `UPDATE rounds SET deadline_at=NULL,paused_at=?,paused_remaining_ms=?,paused_previous_phase=status,phase_version=?,updated_at=? WHERE id=?`, now, nullableInt(remaining), version, now, id); err != nil {
		return err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "phase.changed", map[string]any{"resource": "round", "roundId": id, "roomVersion": roomVersion, "phaseVersion": version, "phase": string(r.Status)}, now); err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, r.RoomID, "", "", "round.restart-paused", map[string]any{"roundId": id, "phaseVersion": version}, now); err != nil {
		return err
	}
	return tx.Commit()
}

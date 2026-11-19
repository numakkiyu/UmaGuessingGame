package room

import (
	"context"
	"database/sql"
	"errors"
)

// CommandBudgetRoom resolves a shared quota only for an active member or owner.
// Joining guests remain protected by their session and address limits; a failed
// invite must not consume the room's command budget.
func (s *Service) CommandBudgetRoom(ctx context.Context, sessionID, kind, resourceID string) (string, error) {
	var query string
	switch kind {
	case "room":
		query = `SELECT r.id FROM rooms r WHERE r.id=?`
	case "round":
		query = `SELECT r.id FROM rooms r JOIN rounds x ON x.room_id=r.id WHERE x.id=?`
	case "event":
		query = `SELECT r.id FROM rooms r JOIN event_sessions x ON x.room_id=r.id WHERE x.id=?`
	default:
		return "", nil
	}
	query += ` AND (r.owner_session_id=? OR EXISTS (SELECT 1 FROM memberships m
		WHERE m.room_id=r.id AND m.session_id=? AND m.left_at IS NULL AND m.revoked_at IS NULL))`
	var roomID string
	err := s.db.Read().QueryRowContext(ctx, query, resourceID, sessionID, sessionID).Scan(&roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return roomID, err
}

package room

import (
	"context"
	"database/sql"
	"errors"
)

// AccessForm resolves nested resources before the HTTP account gate runs.
func (s *Service) AccessForm(ctx context.Context, kind, id string) (string, error) {
	query := `SELECT CASE WHEN EXISTS(SELECT 1 FROM event_sessions e WHERE e.room_id=r.id) THEN 'event' ELSE json_extract(r.config_json,'$.kind') END FROM rooms r `
	switch kind {
	case "room":
		query += `WHERE r.id=?`
	case "round":
		query += `JOIN rounds x ON x.room_id=r.id WHERE x.id=?`
	case "event":
		query += `JOIN event_sessions x ON x.room_id=r.id WHERE x.id=?`
	case "membership":
		query += `JOIN memberships x ON x.room_id=r.id WHERE x.id=?`
	default:
		return "", nil
	}
	var form string
	err := s.db.Read().QueryRowContext(ctx, query, id).Scan(&form)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return form, err
}
func (s *Service) RecoveryAccess(ctx context.Context, code string) (sid, form string, err error) {
	var roomID string
	err = s.db.Read().QueryRowContext(ctx, `SELECT m.session_id,m.room_id FROM recovery_grants g JOIN memberships m ON m.id=g.membership_id WHERE g.code_hash=? AND g.consumed_at IS NULL AND g.expires_at>?`, hashGrant(trimSpace(code)), s.nowMS()).Scan(&sid, &roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	form, err = s.AccessForm(ctx, "room", roomID)
	return
}

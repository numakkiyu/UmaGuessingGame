package room

import "context"

// StreamIdentityActive checks the complete binding captured by a live stream.
// A recovered seat keeps its membership ID, but belongs to a different session.
func (s *Service) StreamIdentityActive(ctx context.Context, roomID, membershipID, sessionID, role string) (bool, error) {
	var active int
	err := s.db.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m
		JOIN sessions s ON s.id=m.session_id JOIN rooms r ON r.id=m.room_id
		WHERE m.id=? AND m.room_id=? AND m.session_id=? AND m.role=?
		AND m.left_at IS NULL AND m.revoked_at IS NULL
		AND s.revoked_at IS NULL AND s.expires_at>?
		AND r.status NOT IN ('archived','cancelled')`, membershipID, roomID, sessionID, role, s.nowMS()).Scan(&active)
	return active == 1, err
}

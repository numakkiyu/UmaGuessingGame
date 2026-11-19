package room

import (
	"context"
	"database/sql"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// One verified account may occupy only one active seat in the same room.
func (s *Service) checkAccountSeat(ctx context.Context, tx *sql.Tx, sid, roomID, form string) error {
	if !s.cfg.Policy.Accounts.Required(form) {
		return nil
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_game_bindings own
 JOIN account_game_bindings other ON other.account_id=own.account_id AND other.session_id<>own.session_id
 JOIN memberships m ON m.session_id=other.session_id
 WHERE own.session_id=? AND m.room_id=? AND m.left_at IS NULL AND m.revoked_at IS NULL`, sid, roomID).Scan(&count)
	if err != nil {
		return problem.Internal(err)
	}
	if count > 0 {
		return problem.Conflict("account-seat-active", "这个账户已经在房间里了，请使用原来的席位")
	}
	return nil
}
func (s *Service) checkAccountQueue(ctx context.Context, tx *sql.Tx, sid string) error {
	if !s.cfg.Policy.Accounts.Required("matchmaking") {
		return nil
	}
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_game_bindings own
 JOIN account_game_bindings other ON other.account_id=own.account_id AND other.session_id<>own.session_id
 JOIN matchmaking_tickets t ON t.session_id=other.session_id
 WHERE own.session_id=? AND t.status IN ('queueing','matched-pending-accept','ready','playing')`, sid).Scan(&count)
	if err != nil {
		return problem.Internal(err)
	}
	if count > 0 {
		return problem.Conflict("account-queue-active", "这个账户已经在匹配或对局中，请先完成当前一场")
	}
	return nil
}

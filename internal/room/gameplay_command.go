package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// Gameplay results share the existing receipt retention policy, but
// have a separate namespace from HTTP response bodies. Both the game mutation
// and this result are committed together; an interrupted HTTP receipt is safe.
func replayGameplayQ[T any](ctx context.Context, tx *sql.Tx, sessionID, scope string, command *CreationCommand, now int64) (T, bool, error) {
	var result T
	if command == nil {
		return result, false, nil
	}
	_, hash, err := command.identity()
	if err != nil {
		return result, false, err
	}
	var storedHash, raw string
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,body_json,expires_at FROM idempotency_receipts WHERE session_id=? AND route_scope=? AND idem_key=?`, sessionID, "gameplay:"+scope, command.Key).Scan(&storedHash, &raw, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, problem.Internal(err)
	}
	if storedHash != hash {
		return result, false, problem.Conflict("idempotency-key-reused", "the gameplay command belongs to a different request")
	}
	if expires <= now {
		return result, false, problem.Conflict("confirmation-expired", "the gameplay confirmation window has ended")
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, false, problem.Internal(err)
	}
	return result, true, nil
}

func (s *Service) saveGameplayQ(ctx context.Context, tx *sql.Tx, sessionID, scope string, command *CreationCommand, result any, now int64) error {
	if command == nil {
		return nil
	}
	return SaveReceipt(ctx, tx, sessionID, "gameplay:"+scope, command.Key, command.Payload, 200, result, now, s.receiptTTLMS())
}

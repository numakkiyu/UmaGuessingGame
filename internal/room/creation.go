package room

import (
	"context"
	"database/sql"
	"errors"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// CreationCommand is supplied by the HTTP adapter, never deserialized from a
// player-controlled body. It binds a resource to its original command atomically.
type CreationCommand struct {
	Key     string
	Payload []byte
}

func (c *CreationCommand) identity() (any, any, error) {
	if c == nil {
		return nil, nil, nil
	}
	if err := ValidateIdempotencyKey(c.Key); err != nil {
		return nil, nil, err
	}
	hash, err := HashPayload(c.Payload)
	return c.Key, hash, err
}
func createdResource(ctx context.Context, q queryer, table, owner, sessionID string, c *CreationCommand) (string, error) {
	if c == nil {
		return "", nil
	}
	key, hash, err := c.identity()
	if err != nil {
		return "", err
	}
	var id, stored string
	// Call sites pass only fixed identifiers, never request-supplied table names.
	err = q.QueryRowContext(ctx, "SELECT id,creation_hash FROM "+table+" WHERE "+owner+"=? AND creation_key=?", sessionID, key).Scan(&id, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", problem.Internal(err)
	}
	if stored != hash {
		return "", problem.Conflict("idempotency-conflict", "the creation key was used with a different request")
	}
	return id, nil
}

package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// IdempotencyKeyMin is the contract minimum length for Idempotency-Key
// (16..128 printable characters, ).
const IdempotencyKeyMin = 16

// IdempotencyKeyMax is the contract maximum length.
const IdempotencyKeyMax = 128

// Receipt is a stored idempotent command result.
type Receipt struct {
	StatusCode int
	Body       json.RawMessage
	// Replayed reports that this result came from the store rather than a fresh
	// execution.
	Replayed bool
}

// ValidateIdempotencyKey enforces the contract's length and character rules.
//
// The key must be printable ASCII so it can be logged safely and compared
// byte-for-byte across clients.
func ValidateIdempotencyKey0(key string) error {
	if key == "" {
		return problem.BadRequest("idempotency-key-missing",
			"Idempotency-Key is required for this operation")
	}
	if len(key) < IdempotencyKeyMin || len(key) > IdempotencyKeyMax {
		return problem.Validation("Idempotency-Key has an invalid length",
			problem.FieldError{Field: "Idempotency-Key",
				Message: fmt.Sprintf("must be %d..%d characters", IdempotencyKeyMin, IdempotencyKeyMax)})
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x21 || c > 0x7e {
			return problem.Validation("Idempotency-Key has an invalid character",
				problem.FieldError{Field: "Idempotency-Key", Message: "must be printable ASCII without spaces"})
		}
	}
	return nil
}

// HashPayload canonicalises and hashes a request payload.
//
// The payload is re-encoded through encoding/json so that key order and
// insignificant whitespace cannot make two identical requests look different.
// Numbers survive as json.Number to avoid float re-formatting.
func HashPayload(payload []byte) (string, error) {
	if len(payload) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:]), nil
	}
	dec := json.NewDecoder(newByteReader(payload))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", problem.BadRequest("invalid-json", "the request body is not valid JSON")
	}
	canon, err := json.Marshal(v)
	if err != nil {
		return "", problem.BadRequest("invalid-json", "the request body could not be canonicalised")
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// LookupReceipt reads a stored receipt for session+scope+key.
//
// Ordering matters : the receipt is consulted before phaseVersion and
// revision are checked, otherwise a network retry that lands after a phase
// change would be rejected instead of replayed.
func (s *Service) LookupReceipt(ctx context.Context, sessionID, routeScope, key string) (*Receipt, error) {
	var (
		status  int
		body    string
		payload string
	)
	err := s.db.Read().QueryRowContext(ctx, `
		SELECT status_code, body_json, payload_hash
		FROM idempotency_receipts
		WHERE session_id = ? AND route_scope = ? AND idem_key = ? AND expires_at > ?`,
		sessionID, routeScope, key, s.nowMS()).Scan(&status, &body, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read receipt: %w", err))
	}
	return &Receipt{StatusCode: status, Body: json.RawMessage(body), Replayed: true}, nil
}

// receiptConflictPayload reports the stored payload hash for diagnostics.
func (s *Service) receiptPayloadHash(ctx context.Context, sessionID, routeScope, key string) (string, error) {
	var hash string
	err := s.db.Read().QueryRowContext(ctx, `
		SELECT payload_hash FROM idempotency_receipts
		WHERE session_id = ? AND route_scope = ? AND idem_key = ?`,
		sessionID, routeScope, key).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", problem.Internal(fmt.Errorf("room: read receipt hash: %w", err))
	}
	return hash, nil
}

// CheckIdempotent performs the full pre-flight: it returns a replay when the
// key was already used with the same payload, and 409 when the payload differs.
//
// A caller that receives a nil receipt must execute the command and then call
// SaveReceipt inside the same transaction as the business write.
func (s *Service) CheckIdempotent(ctx context.Context, sessionID, routeScope, key string, payload []byte) (*Receipt, error) {
	if err := ValidateIdempotencyKey0(key); err != nil {
		return nil, err
	}
	payloadHash, err := HashPayload(payload)
	if err != nil {
		return nil, err
	}
	receipt, err := s.LookupReceipt(ctx, sessionID, routeScope, key)
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, nil
	}
	storedHash, err := s.receiptPayloadHash(ctx, sessionID, routeScope, key)
	if err != nil {
		return nil, err
	}
	if storedHash != "" && storedHash != payloadHash {
		return nil, problem.Conflict("idempotency-key-reused",
			"this Idempotency-Key was already used with a different request payload")
	}
	return receipt, nil
}

// SaveReceipt records the command result inside the caller's transaction.
//
// Writing the receipt in the same transaction as the business state is what
// makes "the retry returns the original receipt" true even if the process dies
// immediately after commit .
func SaveReceipt(ctx context.Context, tx *sql.Tx, sessionID, routeScope, key string, payload []byte, statusCode int, body any, now int64, ttlMS int64) error {
	payloadHash, err := HashPayload(payload)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return problem.Internal(fmt.Errorf("room: encode receipt body: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_receipts
			(session_id, route_scope, idem_key, payload_hash, status_code, body_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id, route_scope, idem_key) DO UPDATE SET
			payload_hash = excluded.payload_hash,
			status_code  = excluded.status_code,
			body_json    = excluded.body_json,
			created_at   = excluded.created_at,
			expires_at   = excluded.expires_at`,
		sessionID, routeScope, key, payloadHash, statusCode, string(encoded), now, now+ttlMS); err != nil {
		return problem.Internal(fmt.Errorf("room: save receipt: %w", err))
	}
	return nil
}

// receiptTTLMS is the configured receipt retention in milliseconds.
func (s *Service) receiptTTLMS() int64 { return s.cfg.IdempotencyTTL.Milliseconds() }

// byteReader avoids importing bytes just for one decode.
type byteReader struct {
	b []byte
	i int
}

func newByteReader(b []byte) *byteReader { return &byteReader{b: b} }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, errEOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

// errEOF mirrors io.EOF without importing io into this file's hot path.
var errEOF = errors.New("EOF")

// RouteScope builds the idempotency scope string.
//
// The scope is session + operationId + resource id , so the same key on a
// different resource is a different receipt rather than a false replay.
func RouteScope(operationID, resourceID string) string {
	if resourceID == "" {
		return operationID
	}
	return operationID + ":" + resourceID
}

// SaveReceiptStandalone writes a receipt outside a caller-managed transaction.
//
// Handlers whose business write is a single statement (rename, revoke, delete)
// still need the receipt persisted so a retry replays. They cannot share the
// business transaction because there is no multi-statement transaction to join, so
// the receipt is written immediately afterwards in its own statement. The window
// between the two writes is documented: a crash there loses the receipt, and the
// retry would re-run an idempotent operation, which is safe for these operations
// because they are naturally idempotent (rename to the same value, revoke twice).
func (s *Service) SaveReceiptStandalone(ctx context.Context, sessionID, routeScope, key string, payload []byte, statusCode int, body any) error {
	payloadHash, err := HashPayload(payload)
	if err != nil {
		return err
	}
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return problem.Internal(fmt.Errorf("room: encode receipt body: %w", err))
		}
	}
	if encoded == nil {
		encoded = []byte("null")
	}
	now := s.nowMS()
	if _, err := s.db.Write().ExecContext(ctx, `
		INSERT INTO idempotency_receipts
			(session_id, route_scope, idem_key, payload_hash, status_code, body_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id, route_scope, idem_key) DO UPDATE SET
			payload_hash = excluded.payload_hash,
			status_code  = excluded.status_code,
			body_json    = excluded.body_json,
			created_at   = excluded.created_at,
			expires_at   = excluded.expires_at`,
		sessionID, routeScope, key, payloadHash, statusCode, string(encoded),
		now, now+s.receiptTTLMS()); err != nil {
		return problem.Internal(fmt.Errorf("room: save standalone receipt: %w", err))
	}
	return nil
}

// ValidateIdempotencyKey exposes the key validation to the HTTP layer.
func ValidateIdempotencyKey(key string) error { return ValidateIdempotencyKey0(key) }

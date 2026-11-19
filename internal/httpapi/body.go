package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/jsoninput"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// nowMS is the server clock in UTC milliseconds. It is the only clock used for
// serverTime and deadline comparisons .
func nowMS() int64 { return time.Now().UTC().UnixMilli() }

// jsonUnmarshal is a thin alias so handler files do not all import encoding/json.
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// readRawBody reads and replaces the request body so it can be both hashed for
// idempotency and decoded.
//
// It is called only on write routes, which are already size-bounded by the
// middleware chain; the limit is re-applied here so a handler that reads the body
// itself cannot exceed it.
func readRawBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, problemTooLarge()
		}
		return nil, problem.BadRequest("body-unreadable", "the request body could not be read")
	}
	if int64(len(buf)) > maxBodyBytes {
		return nil, problemTooLarge()
	}
	// Restore the body so the handler's decoder still sees it.
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf, nil
}

// replayIfPresent returns the stored receipt for an idempotency key.
//
// It reports replayed=true when a response was already written, so the caller
// simply returns. This is the step that must precede any phaseVersion or revision
// comparison .
func (s *Server) replayIfPresent(w http.ResponseWriter, r *http.Request, sessionID, operationID, resourceID, key string, payload []byte) (bool, error) {
	if s.rooms == nil {
		return false, nil
	}
	if key == "" {
		return false, nil
	}
	scope := routeScope(operationID, resourceID)
	receipt, err := s.rooms.CheckIdempotent(r.Context(), sessionID, scope, key, payload)
	if err != nil {
		return false, err
	}
	if receipt == nil {
		return false, nil
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Idempotent-Replay", "true")
	w.WriteHeader(receipt.StatusCode)
	if receipt.StatusCode != http.StatusNoContent && len(receipt.Body) > 0 {
		_, _ = w.Write(receipt.Body)
	}
	return true, nil
}

// storeReceipt persists the command result for replay.
//
// A failure here is not silently ignored: returning success without a receipt
// would mean a network retry creates a second action, so the caller surfaces the
// error even though the business write already committed. That trade-off is
// recorded as a known limitation in the delivery notes.
func (s *Server) storeReceipt(r *http.Request, sessionID, operationID, resourceID, key string, payload []byte, status int, body any) error {
	if s.rooms == nil || key == "" {
		return nil
	}
	return s.rooms.SaveReceiptStandalone(r.Context(), sessionID,
		routeScope(operationID, resourceID), key, payload, status, body)
}

// routeScope builds the idempotency scope for an operation and resource.
func routeScope(operationID, resourceID string) string {
	if resourceID == "" {
		return operationID
	}
	return operationID + ":" + resourceID
}

// problemTooLarge builds the 413 for an oversized body.
func problemTooLarge() error { return problem.TooLarge(maxBodyBytes) }

// decodeJSON decodes the already-buffered body bytes into dst.
//
// Write handlers buffer the body once via readRawBody so the same bytes can be
// hashed for idempotency and decoded for the contract. Decoding the buffered copy
// (rather than the stream) is what guarantees the hash and the parsed value describe
// the same request; reading the body twice would leave the second reader with EOF.
func decodeJSON(dst any, raw []byte) error {
	if len(raw) == 0 {
		return problem.BadRequest("body-required", "a JSON request body is required")
	}
	if err := jsoninput.ValidateObject(raw); err != nil {
		return problem.BadRequest("invalid-json", "the request body must be one JSON object with unique fields and bounded depth")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return problem.BadRequest("invalid-json",
			"the request body is not valid JSON for this operation")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return problem.BadRequest("invalid-json",
			"the request body must contain exactly one JSON object")
	}
	return nil
}

// decodeRequiredBody reads a required body of type T from the request stream.
func decodeRequiredBody[T any](w http.ResponseWriter, r *http.Request, limit int64) (T, error) {
	var zero T
	var body T
	if err := decodeBody(w, r, &body, limit); err != nil {
		return zero, err
	}
	return body, nil
}

// requireIdempotencyKey enforces the header on operations the contract marks as
// requiring it .
//
// The key is mandatory: without it a network retry would create a second action,
// which is exactly the failure mode the contract's Idempotency-Key parameter
// exists to prevent.
func requireIdempotencyKey(r *http.Request) (string, error) {
	key := r.Header.Get("Idempotency-Key")
	if err := room.ValidateIdempotencyKey(key); err != nil {
		return "", err
	}
	return key, nil
}

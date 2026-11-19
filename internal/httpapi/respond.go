// Package httpapi binds HTTP requests and maps authorized domain results to explicit response DTOs.
// Internal storage and mode state are not serialized directly to clients.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"

	"github.com/bhcn/umaguessinggame/internal/storage"
)

// maxBodyBytes bounds every request body. A guess payload is a few hundred
// bytes; 64 KiB leaves room for a full lineup or creative work without letting a
// client stream unbounded data into the process .
const maxBodyBytes = 64 << 10

// maxEventStreamBody bounds the SSE/WS handshake request bodies (they carry none).
const maxEventStreamBody = 4 << 10

// decodeBody reads a JSON body into dst under the size limit.
//
// Unknown fields are rejected: the contract declares
// additionalProperties:false on every request schema, so accepting extras would
// let a client smuggle fields the server never validated.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	if limit <= 0 {
		limit = maxBodyBytes
	}
	if r.Body == nil {
		return problem.BadRequest("body-required", "a JSON request body is required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if asMaxBytes(err, &maxErr) {
			return problem.TooLarge(limit)
		}
		return problem.BadRequest("body-unreadable", "the request body could not be read")
	}
	return decodeJSON(dst, raw)
}

// asMaxBytes reports whether err is a body-limit violation.
func asMaxBytes(err error, target **http.MaxBytesError) bool {
	if e, ok := err.(*http.MaxBytesError); ok {
		*target = e
		return true
	}
	return false
}

// writeJSON writes a successful JSON response with the required headers.
//
// Every response carries X-Request-Id and Date ; private API responses are
// no-store so no shared cache can retain them.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if loc := h.Get("Location"); loc != "" {
		// Location was set by the caller before writing.
		_ = loc
	}
	w.WriteHeader(status)
	if body == nil || status == http.StatusNoContent {
		return
	}
	// Marshal first, then write, so the exact bytes a client receives are the same
	// bytes stored in the idempotency receipt. json.Encoder would append a newline,
	// which would make a replay differ from the original response by one byte and
	// break byte-for-byte replay comparison.
	buf, err := json.Marshal(body)
	if err != nil {
		// The status is already committed; only a log line is possible.
		s.log.Warn("response encode failed", "requestId", requestIDOf(r))
		return
	}
	if _, err := w.Write(buf); err != nil {
		s.log.Warn("response write failed", "requestId", requestIDOf(r))
	}
}

// writeNoContent writes a 204 with the mandatory headers.
func (s *Server) writeNoContent(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// writeError renders a Problem Details response and records the failure.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	// A database that ran out of wait budget is a capacity condition, not a fault.
	//
	// This is checked before problem.As so a raw driver error — which carries no
	// status of its own and would become a 500 — is reported as 503 + retryable.
	// The distinction matters to clients: 500 says "do not retry", while the same
	// request typically succeeds a moment later under load. It was found under a
	// 100-player load test, where 4 read connections served 30+ concurrent readers.
	// A client that walked away is not a server fault.
	//
	// When the caller cancels — a route change aborts an in-flight query, a tab is
	// closed, a reconnect re-fetches and supersedes the old request — the server did
	// nothing wrong and nobody is waiting for this response. Recording it as a 500
	// pollutes error metrics and, under load, makes a healthy server look broken: a
	// load test that counts 5xx will report failures that never affected a user.
	//
	// Nothing is written because the connection is already gone; writing a body would
	// only produce another error.
	if errors.Is(err, context.Canceled) {
		return
	}

	e := problem.As(err)
	// A deadline the server imposed on itself is still a server-side capacity
	// signal, so it keeps the 503 mapping below; only an explicit client cancel is
	// swallowed above.
	if e.Status == http.StatusInternalServerError && storage.IsBusy(err) {
		e = problem.StorageBusy(1)
	}
	s.observeProblem(r, e)
	problem.Write(w, r, requestIDOf(r), e)
}

// pathParam reads a required path parameter, enforcing the contract's length
// bound.
func pathParam(r *http.Request, name string) (string, error) {
	v := r.PathValue(name)
	if v == "" {
		return "", problem.NotFound("not-found", "the requested resource does not exist")
	}
	if len(v) > 128 {
		return "", problem.Validation("path parameter is too long",
			problem.FieldError{Field: name, Message: "must be at most 128 characters"})
	}
	return v, nil
}

// pagination reads the cursor and limit query parameters.
func pagination(r *http.Request) (cursor string, limit int, err error) {
	q := r.URL.Query()
	cursor = q.Get("cursor")
	limit = 20
	if raw := q.Get("limit"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return "", 0, problem.Validation("limit is invalid",
				problem.FieldError{Field: "limit", Message: "must be an integer"})
		}
		if n < 1 || n > 100 {
			return "", 0, problem.Validation("limit is out of range",
				problem.FieldError{Field: "limit", Message: "must be 1..100"})
		}
		limit = n
	}
	return cursor, limit, nil
}

// rfc3339 renders a millisecond timestamp as RFC3339 UTC.
func rfc3339(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

// rfc3339Ptr renders an optional timestamp, returning nil for absent values so
// the field is omitted rather than emitted as an empty string.
func rfc3339Ptr(ms *int64) *string {
	if ms == nil {
		return nil
	}
	v := rfc3339(*ms)
	return &v
}

// trimLower normalises a query string for comparison.
func trimLower(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

// Package problem implements RFC 9457 error responses matching the published API contract.
package problem

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// FieldError names one rejected input field. The contract requires both keys.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Payload is the serialised Problem object.
type Payload struct {
	Type        string       `json:"type"`
	Title       string       `json:"title"`
	Status      int          `json:"status"`
	Detail      string       `json:"detail,omitempty"`
	Instance    string       `json:"instance,omitempty"`
	Code        string       `json:"code"`
	RequestID   string       `json:"requestId"`
	Retryable   *bool        `json:"retryable,omitempty"`
	FieldErrors []FieldError `json:"fieldErrors,omitempty"`
}

// Error is a domain error that already knows its HTTP representation.
//
// Domain layers (session, room) return *Error directly so that a failure keeps
// its status and stable machine code across the service boundary instead of
// being flattened into a 500.
type Error struct {
	// Status is the authoritative HTTP status code.
	Status int
	// Code is a stable, machine-readable identifier. Clients branch on this,
	// never on Title or Detail.
	Code string
	// Detail is human-readable and must never leak file paths, SQL or secrets.
	Detail string
	// Fields carries per-field validation results, if any.
	Fields []FieldError
	// Retryable marks transient conditions (busy, rate limited).
	Retryable bool
	// RetryAfter, when positive, is emitted as Retry-After seconds.
	RetryAfter int
	// cause is retained for server-side logging only; it is never serialised.
	cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s (%d): %s", e.Code, e.Status, e.Detail)
	}
	return fmt.Sprintf("%s (%d)", e.Code, e.Status)
}

// Unwrap exposes the wrapped cause for errors.Is/As in server-side logging.
func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches a server-side cause without changing the wire response.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

// WithField appends a field error.
func (e *Error) WithField(field, message string) *Error {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: message})
	return e
}

// WithDetail replaces the human-readable detail.
func (e *Error) WithDetail(detail string) *Error {
	e.Detail = detail
	return e
}

// New builds an error with an explicit status and code.
func New(status int, code, detail string) *Error {
	return &Error{Status: status, Code: code, Detail: detail}
}

// BadRequest reports a structurally invalid request.
func BadRequest(code, detail string) *Error { return New(http.StatusBadRequest, code, detail) }

// Validation reports invalid input with per-field detail.
func Validation(detail string, fields ...FieldError) *Error {
	return &Error{Status: http.StatusBadRequest, Code: "invalid-request", Detail: detail, Fields: fields}
}

// Unauthorized reports a missing or revoked session.
func Unauthorized(code, detail string) *Error { return New(http.StatusUnauthorized, code, detail) }

// Forbidden reports an authorisation or CSRF/Origin failure.
func Forbidden(code, detail string) *Error { return New(http.StatusForbidden, code, detail) }

// NotFound reports an invisible-or-absent resource. The two cases are
// deliberately indistinguishable .
func NotFound(code, detail string) *Error { return New(http.StatusNotFound, code, detail) }

// Conflict reports a state or idempotency conflict.
func Conflict(code, detail string) *Error { return New(http.StatusConflict, code, detail) }

// Unprocessable reports a well-formed request the rules do not allow.
func Unprocessable(code, detail string) *Error {
	return New(http.StatusUnprocessableEntity, code, detail)
}

// TooLarge reports a body over the configured limit.
func TooLarge(limit int64) *Error {
	return New(http.StatusRequestEntityTooLarge, "payload-too-large",
		fmt.Sprintf("request body exceeds %d bytes", limit))
}

// RateLimited reports a tripped limiter with a Retry-After hint.
func RateLimited(retryAfter int) *Error {
	e := New(http.StatusTooManyRequests, "rate-limited", "too many requests; retry later")
	e.Retryable = true
	e.RetryAfter = retryAfter
	return e
}

// Busy reports a saturated bounded queue. It is never a silent drop .
func Busy(retryAfter int) *Error {
	e := New(http.StatusServiceUnavailable, "busy", "the room command queue is saturated; retry")
	e.Retryable = true
	e.RetryAfter = retryAfter
	return e
}

// StorageBusy reports a transient capacity condition: the database could not serve
// the request within its own wait budget because too many others were competing.
//
// It is a 503 with retryable set, not a 500. The distinction is not cosmetic: a 500
// tells a client "this request is broken, do not retry", while the same request
// usually succeeds a millisecond later under load. Reporting it as an internal fault
// both hides the real cause and pushes clients into abandoning work that would have
// completed. The detail carries no internals — a player should read "try again",
// not "SQLITE_BUSY".
//
// It is separate from Busy, which reports a saturated room command queue: the two
// have different causes and different operators can fix them independently.
func StorageBusy(retryAfter int) *Error {
	if retryAfter <= 0 {
		retryAfter = 1
	}
	e := New(http.StatusServiceUnavailable, "temporarily-busy",
		"the server is handling a lot of requests right now; please try again")
	e.Retryable = true
	e.RetryAfter = retryAfter
	return e
}

// NotImplemented reports an operation whose required dependency is unavailable.
func NotImplemented(code, detail string) *Error {
	return New(http.StatusNotImplemented, code, detail)
}

// Internal reports an unexpected server fault with an opaque detail.
func Internal(cause error) *Error {
	e := New(http.StatusInternalServerError, "internal-error", "internal server error")
	e.cause = cause
	return e
}

// As normalises any error into an *Error, defaulting to a 500.
func As(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal(err)
}

// titles maps status codes to their canonical RFC 9457 titles.
var titles = map[int]string{
	400: "Bad Request",
	401: "Unauthorized",
	403: "Forbidden",
	404: "Not Found",
	405: "Method Not Allowed",
	409: "Conflict",
	412: "Precondition Failed",
	413: "Content Too Large",
	415: "Unsupported Media Type",
	422: "Unprocessable Content",
	429: "Too Many Requests",
	500: "Internal Server Error",
	501: "Not Implemented",
	503: "Service Unavailable",
}

// Title returns the canonical title for the error's status.
func (e *Error) Title() string {
	if t, ok := titles[e.Status]; ok {
		return t
	}
	return "Error"
}

// Payload renders the serialisable representation.
func (e *Error) Payload(requestID, instance string) Payload {
	p := Payload{
		Type:        "/problems/" + e.Code,
		Title:       e.Title(),
		Status:      e.Status,
		Detail:      e.Detail,
		Instance:    instance,
		Code:        e.Code,
		RequestID:   requestID,
		FieldErrors: e.Fields,
	}
	if e.Retryable {
		retryable := true
		p.Retryable = &retryable
	}
	return p
}

// Write serialises the error as application/problem+json.
//
// Callers pass the request-scoped identifier so the response can be correlated
// with the server log without exposing anything private.
func Write(w http.ResponseWriter, r *http.Request, requestID string, err error) {
	e := As(err)
	if e == nil {
		e = Internal(errors.New("problem: nil error"))
	}
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if e.RetryAfter > 0 {
		h.Set("Retry-After", fmt.Sprintf("%d", e.RetryAfter))
	}
	instance := ""
	if r != nil && r.URL != nil {
		instance = r.URL.Path
	}
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e.Payload(requestID, instance))
}

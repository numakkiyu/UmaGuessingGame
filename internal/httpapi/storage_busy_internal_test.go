package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

// TestStorageIsBusyClassification pins that the classifier is narrow.
//
// Only the SQLite busy/locked family counts. A real fault — a missing table, a
// constraint violation, an I/O error — must never be downgraded to "try again",
// because that would tell a client to retry something that can never succeed.
func TestStorageIsBusyClassification(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("database is locked (5) (SQLITE_BUSY)"), true},
		{errors.New("database table is locked"), true},
		{errors.New("SQLITE_BUSY"), true},
		{errors.New("no such table: rooms"), false},
		{errors.New("constraint failed: UNIQUE constraint failed"), false},
		{errors.New("disk I/O error"), false},
		{errors.New("context deadline exceeded"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := storage.IsBusy(c.err); got != c.want {
			t.Errorf("IsBusy(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// TestStorageBusyResponseShape proves the wire representation a client sees.
func TestStorageBusyResponseShape(t *testing.T) {
	e := problem.StorageBusy(1)
	if e.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", e.Status)
	}
	if e.Code != "temporarily-busy" {
		t.Errorf("code = %q, want temporarily-busy", e.Code)
	}
	if !e.Retryable {
		t.Error("a capacity condition must be marked retryable")
	}
	if e.RetryAfter <= 0 {
		t.Errorf("retryAfter = %d, want positive", e.RetryAfter)
	}
	// The detail must not leak internals: a player should read "try again", not
	// "SQLITE_BUSY" or a table name.
	lower := strings.ToLower(e.Detail)
	for _, term := range []string{"sqlite", "locked", "database", "sql"} {
		if strings.Contains(lower, term) {
			t.Errorf("detail leaks internal term %q: %q", term, e.Detail)
		}
	}
}

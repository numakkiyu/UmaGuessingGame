package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/observability"
)

// TestClientCancelIsNotAServerFault pins that a cancelled request never becomes a 500.
//
// A caller that walks away — a route change aborting an in-flight query, a closed
// tab, a reconnect superseding the previous fetch — leaves the server healthy and
// nobody waiting for the response. Reporting it as 500 both pollutes error metrics
// and makes a healthy server look broken: a load test counting 5xx reported hundreds
// of failures that never affected a user.
//
// The assertion is on what reaches the wire: no Problem Details body at all.
func TestClientCancelIsNotAServerFault(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/x", nil)

	srv.writeError(rec, req, context.Canceled)

	if rec.Code == http.StatusInternalServerError {
		t.Errorf("a cancelled request must not be reported as 500; body=%s", rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("nothing must be written for a cancelled request, got %q", rec.Body.String())
	}
}

// TestWrappedClientCancelIsAlsoSwallowed proves the check survives error wrapping.
//
// Domain layers wrap with %w, so a bare errors.Is at the top of the chain is not
// enough — the cancel is usually several layers down.
func TestWrappedClientCancelIsAlsoSwallowed(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/x", nil)

	wrapped := errors.Join(errors.New("room: load grants"), context.Canceled)
	srv.writeError(rec, req, wrapped)

	if rec.Code == http.StatusInternalServerError {
		t.Errorf("a wrapped cancel must not become 500; body=%s", rec.Body.String())
	}
}

// TestRealFaultStillProduces500 makes sure the swallow is narrow: an ordinary
// internal failure must still be reported, or real bugs would vanish silently.
func TestRealFaultStillProduces500(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/x", nil)

	srv.writeError(rec, req, errors.New("boom"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a real fault must stay 500, got %d", rec.Code)
	}
}

// newTestServer builds a Server with the minimum dependencies writeError touches.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		metrics: observability.NewMetrics(),
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestStreamingRouteOwnsItsLifetime(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.RequestTimeout = 5 * time.Millisecond
	for _, operation := range []string{"streamRoomEvents", "upgradeRoomSocket", "getRoom"} {
		t.Run(operation, func(t *testing.T) {
			h := srv.wrap(route{operationID: operation, auth: "none", handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
					if operation != "getRoom" {
						t.Error("REST timeout killed streaming route")
					}
				case <-time.After(25 * time.Millisecond):
					if operation == "getRoom" {
						t.Error("REST request lost its timeout")
					}
				}
			}}, routeInfo{OperationID: operation})
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		})
	}
}

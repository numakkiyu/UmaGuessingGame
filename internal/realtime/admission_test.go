package realtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamAdmissionBoundsAndReleases(t *testing.T) {
	h := &Handler{cfg: Config{MaxConnections: 3, MaxConnectionsPerRoom: 2, MaxConnectionsPerSession: 1}, byRoom: map[string]int{}, bySession: map[string]int{}}
	ident := Identity{RoomID: "r", SessionID: "s"}
	release, ok := h.admit(httptest.NewRecorder(), ident)
	if !ok {
		t.Fatal("first connection refused")
	}
	rejected := httptest.NewRecorder()
	if _, ok := h.admit(rejected, ident); ok || rejected.Code != http.StatusTooManyRequests || rejected.Header().Get("Retry-After") == "" {
		t.Fatal("same session bypassed admission")
	}
	second, ok := h.admit(httptest.NewRecorder(), Identity{RoomID: "r", SessionID: "s2"})
	if !ok {
		t.Fatal("second room member refused")
	}
	if _, ok := h.admit(httptest.NewRecorder(), Identity{RoomID: "r", SessionID: "s3"}); ok {
		t.Fatal("room cap bypassed")
	}
	third, ok := h.admit(httptest.NewRecorder(), Identity{RoomID: "other", SessionID: "s3"})
	if !ok {
		t.Fatal("hot room consumed another room's capacity")
	}
	if connections, rooms, sessions := h.ConnectionStats(); connections != 3 || rooms != 2 || sessions != 3 {
		t.Fatalf("incorrect aggregate connection statistics: %d %d %d", connections, rooms, sessions)
	}
	if _, ok := h.admit(httptest.NewRecorder(), Identity{RoomID: "another", SessionID: "s4"}); ok {
		t.Fatal("global cap bypassed")
	}
	release()
	release()
	second()
	third()
	if h.active != 0 || len(h.byRoom) != 0 || len(h.bySession) != 0 {
		t.Fatal("closed connections leaked capacity or identity keys")
	}
	if connections, rooms, sessions := h.ConnectionStats(); connections != 0 || rooms != 0 || sessions != 0 {
		t.Fatalf("released capacity still reported: %d %d %d", connections, rooms, sessions)
	}
	release, ok = h.admit(httptest.NewRecorder(), ident)
	if !ok {
		t.Fatal("released slot could not be used again")
	}
	release()
}

type stalledAuthorizer struct{}

func (stalledAuthorizer) AuthorizeWebSocket(r *http.Request, _ string) (Identity, bool, error) {
	<-r.Context().Done()
	return Identity{}, false, r.Context().Err()
}

func (a stalledAuthorizer) AuthorizeSSE(r *http.Request, room string) (Identity, bool, error) {
	return a.AuthorizeWebSocket(r, room)
}

func TestHandshakeTimeoutDoesNotAllocateSubscription(t *testing.T) {
	h := &Handler{auth: stalledAuthorizer{}, cfg: Config{HandshakeTimeout: time.Millisecond}}
	for _, serve := range []func(http.ResponseWriter, *http.Request){h.ServeSSE, h.ServeWebSocket} {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetPathValue("roomId", "room")
		r.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		serve(w, r)
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "2" || h.active != 0 {
			t.Fatalf("stalled authentication was not bounded before allocation: %d %s", w.Code, w.Body)
		}
	}
}

package realtime

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// newTestServer wires a real HTTP server around the Handler so the end-to-end
// tests exercise the actual upgrade path, the actual Origin check and the actual
// cookie-based authorisation seam, not a mocked request.
func newTestServer(t *testing.T, hub *Hub, auth Authorizer, tweak func(*Config)) *httptest.Server {
	t.Helper()
	cfg := hub.Config()
	cfg.RequireOrigin = true
	if tweak != nil {
		tweak(&cfg)
	}
	h, err := NewHandler(hub, auth, cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/rooms/{roomId}/socket", h.ServeWebSocket)
	mux.HandleFunc("GET /api/v1/rooms/{roomId}/events", h.ServeSSE)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// wsURL converts the test server URL into a ws:// URL.
func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

// dialWS opens a real WebSocket against the test server with the given Origin.
func dialWS(t *testing.T, srv *httptest.Server, path, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	opts := &websocket.DialOptions{}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": []string{origin}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL(srv, path), opts)
}

// readServerMessage reads and decodes one notification from a live socket.
func readServerMessage(t *testing.T, conn *websocket.Conn) ServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m ServerMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("live message fails the contract: %v", err)
	}
	return m
}

// TestLiveWebSocketEndToEnd is the real transport test: a genuine WebSocket
// handshake, a genuine hello, a genuine notification and a genuine close.
func TestLiveWebSocketEndToEnd(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-e2e", "sess-e2e", "mem-e2e"
	auth := stubAuthorizer{ident: identity(roomID, memberID, sessionID, "player"), ok: true}
	srv := newTestServer(t, hub, auth, nil)

	conn, resp, err := dialWS(t, srv, "/api/v1/rooms/"+roomID+"/socket", srv.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d, want 101", resp.StatusCode)
	}

	hello := readServerMessage(t, conn)
	if hello.Type != TypeHello {
		t.Fatalf("first live frame = %q, want hello", hello.Type)
	}
	if hello.ServerEpoch != hub.Epoch() {
		t.Fatalf("hello epoch = %q, want %q", hello.ServerEpoch, hub.Epoch())
	}

	waitSubscribed(t, hub, 1)
	if err := hub.Publish(Publication{
		EventID: "evt-e2e", Type: TypePhaseChanged, RoomID: roomID,
		RoomVersion: 5, PhaseVersion: 3, Seq: 1,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := readServerMessage(t, conn)
	if got.Type != TypePhaseChanged || got.EventID != "evt-e2e" {
		t.Fatalf("live frame = %s/%s, want phase.changed/evt-e2e", got.Type, got.EventID)
	}
	if got.PhaseVer == nil || *got.PhaseVer != 3 {
		t.Fatalf("phaseVersion = %v, want 3", got.PhaseVer)
	}
	if got.Cursor == "" {
		t.Fatal("live message has no cursor")
	}
}

// TestLiveWebSocketRejectsCrossOrigin proves the Origin check holds over a real
// TCP connection: the server answers 403 and never upgrades.
func TestLiveWebSocketRejectsCrossOrigin(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-xo", "sess-xo", "mem-xo"
	auth := stubAuthorizer{ident: identity(roomID, memberID, sessionID, "player"), ok: true}
	srv := newTestServer(t, hub, auth, nil)

	conn, resp, err := dialWS(t, srv, "/api/v1/rooms/"+roomID+"/socket", "https://evil.example")
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "should not be here")
		t.Fatal("a cross-origin handshake succeeded")
	}
	if resp == nil {
		t.Fatalf("no HTTP response for the rejected handshake: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content type = %q, want problem+json", got)
	}
	if hub.SubscriptionCount() != 0 {
		t.Fatal("a rejected handshake created a subscription")
	}
}

// TestLiveWebSocketRejectsNonMember proves membership is enforced over a real
// connection too.
func TestLiveWebSocketRejectsNonMember(t *testing.T) {
	hub := newTestHub(t, nil)
	auth := stubAuthorizer{ok: false}
	srv := newTestServer(t, hub, auth, nil)

	conn, resp, err := dialWS(t, srv, "/api/v1/rooms/room-nm/socket", srv.URL)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "should not be here")
		t.Fatal("a non-member handshake succeeded")
	}
	if resp == nil {
		t.Fatalf("no HTTP response: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestLiveWebSocketClosesOnBusinessFrame proves a real client sending a guess over
// the socket is disconnected with a policy violation, not silently ignored.
func TestLiveWebSocketClosesOnBusinessFrame(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-bizlive", "sess-bizlive", "mem-bizlive"
	auth := stubAuthorizer{ident: identity(roomID, memberID, sessionID, "player"), ok: true}
	srv := newTestServer(t, hub, auth, nil)

	conn, _, err := dialWS(t, srv, "/api/v1/rooms/"+roomID+"/socket", srv.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if got := readServerMessage(t, conn); got.Type != TypeHello {
		t.Fatalf("first frame = %q, want hello", got.Type)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText,
		[]byte(`{"type":"guess","characterId":"special-week"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The server must close the socket. Reading returns an error whose status is the
	// policy violation we chose.
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("the connection survived a business frame")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v, want %v (policy violation)", status, websocket.StatusPolicyViolation)
	}
}

// TestLiveWebSocketPingPong proves the accepted frame works over a real socket.
func TestLiveWebSocketPingPong(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-pp", "sess-pp", "mem-pp"
	auth := stubAuthorizer{ident: identity(roomID, memberID, sessionID, "player"), ok: true}
	srv := newTestServer(t, hub, auth, nil)

	conn, _, err := dialWS(t, srv, "/api/v1/rooms/"+roomID+"/socket", srv.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if got := readServerMessage(t, conn); got.Type != TypeHello {
		t.Fatalf("first frame = %q, want hello", got.Type)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText,
		[]byte(`{"type":"ping","clientMessageId":"m1","sentAt":"2026-11-08T08:00:00Z"}`)); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	if got := readServerMessage(t, conn); got.Type != TypePong {
		t.Fatalf("frame after ping = %q, want pong", got.Type)
	}
}

// TestLiveSSEStream proves the degraded channel works over real HTTP: the correct
// content type, an id line carrying the opaque cursor, and a data line carrying the
// same document the WebSocket would send.
func TestLiveSSEStream(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-sselive", "sess-sselive", "mem-sselive"
	auth := stubAuthorizer{ident: identity(roomID, memberID, sessionID, "player"), ok: true}
	srv := newTestServer(t, hub, auth, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/rooms/"+roomID+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Origin", srv.URL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no (a buffering proxy would batch the stream)", got)
	}

	waitSubscribed(t, hub, 1)
	if err := hub.Publish(Publication{
		EventID: "evt-sse-live", Type: TypePhaseChanged, RoomID: roomID,
		RoomVersion: 2, PhaseVersion: 2, Seq: 1,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Parse the SSE stream: collect id: and data: lines.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var (
		sawID   bool
		sawData bool
		eventID string
	)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			sawID = true
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		sawData = true
		var m ServerMessage
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
			t.Fatalf("SSE data is not a ServerMessage: %v", err)
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("SSE message fails the contract: %v", err)
		}
		if m.Type == TypePhaseChanged && m.EventID == "evt-sse-live" {
			eventID = m.EventID
			break
		}
	}
	if !sawID {
		t.Fatal("the SSE stream never sent an id line; EventSource cannot resume without it")
	}
	if !sawData {
		t.Fatal("the SSE stream never sent a data line")
	}
	if eventID != "evt-sse-live" {
		t.Fatalf("did not observe the published event over SSE (last = %q)", eventID)
	}
}

// TestLiveSSERejectsCrossOrigin proves the SSE path applies the same Origin rule.
func TestLiveSSERejectsCrossOrigin(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-ssexo", "sess-ssexo", "mem-ssexo"
	auth := stubAuthorizer{ident: identity(roomID, memberID, sessionID, "player"), ok: true}
	srv := newTestServer(t, hub, auth, nil)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/rooms/"+roomID+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

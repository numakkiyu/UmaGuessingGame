package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bhcn/umaguessinggame/internal/weborigin"
	"github.com/coder/websocket"
)

// Authorizer resolves subscription identity without coupling transports to session storage.
// Missing or invalid credentials return (zero, false, nil).
type Authorizer interface {
	// AuthorizeWebSocket authorises a WebSocket handshake.
	AuthorizeWebSocket(r *http.Request, roomID string) (Identity, bool, error)
	// AuthorizeSSE authorises an SSE stream request.
	AuthorizeSSE(r *http.Request, roomID string) (Identity, bool, error)
}

// Handler serves the WebSocket upgrade and the SSE fallback.
//
// Both endpoints are registered by internal/httpapi; this type only implements the
// two handlers so that routing, middleware and Problem Details stay in one place.
type Handler struct {
	hub         *Hub
	auth        Authorizer
	cfg         Config
	origins     originSet
	admissionMu sync.Mutex
	active      int
	byRoom      map[string]int
	bySession   map[string]int
	handshakes  chan struct{}
}

// ConnectionStats returns aggregate admission counts without exposing room or
// session identifiers. Sampling uses the same lock as admission and release.
func (h *Handler) ConnectionStats() (connections, rooms, sessions int) {
	h.admissionMu.Lock()
	defer h.admissionMu.Unlock()
	return h.active, len(h.byRoom), len(h.bySession)
}

// admit reserves capacity before a stream sends 200 or upgrades. Rejections
// allocate no hub subscriber; release also runs after cancelled handshakes.
func (h *Handler) admit(w http.ResponseWriter, ident Identity) (func(), bool) {
	h.admissionMu.Lock()
	if h.active >= h.cfg.MaxConnections || h.byRoom[ident.RoomID] >= h.cfg.MaxConnectionsPerRoom || h.bySession[ident.SessionID] >= h.cfg.MaxConnectionsPerSession {
		h.admissionMu.Unlock()
		w.Header().Set("Retry-After", "5")
		writeProblem(w, http.StatusTooManyRequests, "stream-capacity", "connection capacity reached; retry later")
		return nil, false
	}
	h.active++
	h.byRoom[ident.RoomID]++
	h.bySession[ident.SessionID]++
	h.admissionMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.admissionMu.Lock()
			defer h.admissionMu.Unlock()
			h.active--
			h.byRoom[ident.RoomID]--
			h.bySession[ident.SessionID]--
			if h.byRoom[ident.RoomID] == 0 {
				delete(h.byRoom, ident.RoomID)
			}
			if h.bySession[ident.SessionID] == 0 {
				delete(h.bySession, ident.SessionID)
			}
		})
	}, true
}

// enterHandshake limits membership lookups separately from established streams.
func (h *Handler) enterHandshake(w http.ResponseWriter) (func(), bool) {
	select {
	case h.handshakes <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-h.handshakes }) }, true
	default:
		w.Header().Set("Retry-After", "2")
		writeProblem(w, http.StatusServiceUnavailable, "stream-capacity", "stream handshakes are busy; retry later")
		return nil, false
	}
}

// originSet implements the same-origin check plus explicit allow-list entries.
//
// and both require Origin validation on the handshake. coder/websocket
// already rejects a cross-origin handshake by default, but it does so with its own
// semantics (request host plus patterns) and it returns a plain HTTP error. This
// type does the check *before* the upgrade so that a rejection is an ordinary
// 403 with no upgrade attempt, and so the allow-list is validated once at
// construction instead of per request.
type originSet struct {
	require bool
	allow   []string
}

// NewHandler builds the HTTP handler. A nil Authorizer makes every request 401,
// which is the safe default rather than an open endpoint.
func NewHandler(hub *Hub, auth Authorizer, cfg Config) (*Handler, error) {
	if hub == nil {
		return nil, errors.New("realtime: handler requires a hub")
	}
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Handler{
		hub:        hub,
		auth:       auth,
		cfg:        cfg,
		byRoom:     make(map[string]int),
		bySession:  make(map[string]int),
		handshakes: make(chan struct{}, 64),
		origins:    originSet{require: cfg.RequireOrigin, allow: cfg.AllowedOrigins},
	}, nil
}

// allowed reports whether a request's Origin is acceptable for its Host.
//
// Rules:
//
// - Same origin (Origin host == request Host) is always accepted: the SPA is
// served by this process .
// - An entry in AllowedOrigins is accepted, either as a bare host or a full
// scheme://host. No wildcard is supported; Config.Validate rejects "*".
// - A missing Origin is rejected when RequireOrigin is set. Browsers always send
// Origin on a WebSocket handshake, so a missing header means a non-browser
// client, and a non-browser client has no cookie-based authorisation story
// here anyway.
func (o originSet) allowed(r *http.Request) (bool, string) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if o.require {
			return false, "missing Origin header"
		}
		return true, ""
	}
	host, scheme, ok := splitOrigin(origin)
	if !ok {
		return false, "malformed Origin header"
	}
	if weborigin.Same(r, scheme, host) {
		return true, ""
	}
	for _, pat := range o.allow {
		if strings.Contains(pat, "://") {
			if strings.EqualFold(pat, scheme+"://"+host) {
				return true, ""
			}
			continue
		}
		if strings.EqualFold(pat, host) {
			return true, ""
		}
	}
	return false, "Origin not allowed for this host"
}

// splitOrigin rejects URL components that are not part of a serialized origin.
func splitOrigin(origin string) (host, scheme string, ok bool) {
	scheme, host, ok = weborigin.Parse(origin)
	return host, scheme, ok
}

// writeProblem emits a minimal problem document for a rejected handshake.
//
// The full application/problem+json builder lives in internal/httpapi . This
// package only needs the pre-upgrade rejections, and duplicating the whole builder
// here would create two sources of truth for the error contract. The fields emitted
// are the required subset: type, title, status, detail.
func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"type":"about:blank","title":%q,"status":%d,"code":%q,"detail":%q}`,
		http.StatusText(status), status, code, detail)
}

// ServeWebSocket upgrades and serves one WebSocket subscription.
//
// Ordering of the checks is the security property:
//
// 1. Origin, before any upgrade. A cross-site page cannot open a socket even if it
// somehow has a cookie.
// 2. Membership, before any state is created. An unauthorised caller gets 401/403
// and never occupies a hub slot.
// 3. Only then upgrade and start serving.
func (h *Handler) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	finishHandshake, ok := h.enterHandshake(w)
	if !ok {
		return
	}
	defer finishHandshake()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(h.cfg.HandshakeTimeout))

	roomID := r.PathValue("roomId")
	if roomID == "" {
		writeProblem(w, http.StatusBadRequest, "missing-room", "roomId is required")
		return
	}
	if ok, why := h.origins.allowed(r); !ok {
		writeProblem(w, http.StatusForbidden, "origin-rejected", why)
		return
	}
	if h.auth == nil {
		writeProblem(w, http.StatusUnauthorized, "unauthenticated", "no authorizer configured")
		return
	}
	ident, ok, err := h.authorize(r, roomID, true)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			w.Header().Set("Retry-After", "2")
			writeProblem(w, http.StatusServiceUnavailable, "auth-timeout", "authorization is temporarily busy")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "auth-failed", "authorization lookup failed")
		return
	}
	if !ok {
		writeProblem(w, http.StatusForbidden, "not-a-member", "an active membership is required")
		return
	}
	if ident.RoomID == "" {
		ident.RoomID = roomID
	}
	if err := ident.Validate(); err != nil {
		writeProblem(w, http.StatusForbidden, "not-a-member", "membership is incomplete")
		return
	}

	release, admitted := h.admit(w, ident)
	if !admitted {
		return
	}
	defer release()

	// Accept rejects a cross-origin handshake itself; the pre-check above already
	// produced a cleaner error, so this is a second line of defence rather than the
	// primary control.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
		OriginPatterns:     h.originHosts(),
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		// Accept has already written its own response.
		return
	}
	finishHandshake()
	conn.SetReadLimit(h.cfg.MaxClientMessageBytes)

	cursor := r.URL.Query().Get("cursor")
	roomVersion := parseVersion(r.URL.Query().Get("roomVersion"))

	err = h.hub.Serve(r.Context(), SubscribeOptions{
		Identity:    ident,
		Channel:     ChannelWS,
		Cursor:      cursor,
		RoomVersion: roomVersion,
		Transport:   newWSTransport(conn),
	})
	_ = err // Serve records the reason on the subscription; the socket is gone.
}

// ServeSSE serves the SSE fallback.
//
// SSE is the first degraded channel . It carries the same ServerMessage
// documents as WS so a client uses one decoder for both; the cursor doubles as the
// SSE event id, which lets the browser's EventSource reconnect with Last-Event-ID
// without any client-side bookkeeping.
//
// There is no client-to-server frame on SSE, so no ping/ack: liveness is the
// comment keep-alive.
func (h *Handler) ServeSSE(w http.ResponseWriter, r *http.Request) {
	finishHandshake, ok := h.enterHandshake(w)
	if !ok {
		return
	}
	defer finishHandshake()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(h.cfg.HandshakeTimeout))

	roomID := r.PathValue("roomId")
	if roomID == "" {
		writeProblem(w, http.StatusBadRequest, "missing-room", "roomId is required")
		return
	}
	if ok, why := h.origins.allowed(r); !ok {
		writeProblem(w, http.StatusForbidden, "origin-rejected", why)
		return
	}
	if h.auth == nil {
		writeProblem(w, http.StatusUnauthorized, "unauthenticated", "no authorizer configured")
		return
	}
	ident, ok, err := h.authorize(r, roomID, false)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			w.Header().Set("Retry-After", "2")
			writeProblem(w, http.StatusServiceUnavailable, "auth-timeout", "authorization is temporarily busy")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "auth-failed", "authorization lookup failed")
		return
	}
	if !ok {
		writeProblem(w, http.StatusForbidden, "not-a-member", "an active membership is required")
		return
	}
	if ident.RoomID == "" {
		ident.RoomID = roomID
	}
	if err := ident.Validate(); err != nil {
		writeProblem(w, http.StatusForbidden, "not-a-member", "membership is incomplete")
		return
	}

	release, admitted := h.admit(w, ident)
	if !admitted {
		return
	}
	defer release()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "streaming-unsupported", "the server cannot flush")
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Connection", "keep-alive")
	// Reverse proxies that buffer would turn the stream into a batch .
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	finishHandshake()

	// Last-Event-ID is what a browser EventSource sends on reconnect; the query
	// parameter lets a non-browser client resume explicitly.
	cursor := r.Header.Get("Last-Event-ID")
	if cursor == "" {
		cursor = r.URL.Query().Get("cursor")
	}
	roomVersion := parseVersion(r.URL.Query().Get("roomVersion"))

	_ = h.hub.Serve(r.Context(), SubscribeOptions{
		Identity:    ident,
		Channel:     ChannelSSE,
		Cursor:      cursor,
		RoomVersion: roomVersion,
		Transport:   newSSETransport(w, flusher),
	})
}

// originHosts adapts already validated origins to coder/websocket host patterns.
func (h *Handler) originHosts() []string {
	hosts := make([]string, 0, len(h.cfg.AllowedOrigins))
	for _, allowed := range h.cfg.AllowedOrigins {
		if _, host, ok := weborigin.Parse(allowed); ok {
			hosts = append(hosts, host)
		} else {
			hosts = append(hosts, allowed)
		}
	}
	return hosts
}

func (h *Handler) authorize(r *http.Request, roomID string, websocket bool) (Identity, bool, error) {
	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.HandshakeTimeout)
	defer cancel()
	if websocket {
		return h.auth.AuthorizeWebSocket(r.WithContext(ctx), roomID)
	}
	return h.auth.AuthorizeSSE(r.WithContext(ctx), roomID)
}

// parseVersion parses a version query parameter, tolerating garbage by treating it
// as "unknown" rather than rejecting the connection: a malformed version hint must
// not be able to block a reconnect, and the client re-fetches a snapshot anyway.
func parseVersion(raw string) int64 {
	if raw == "" {
		return 0
	}
	var v int64
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int64(c-'0')
		if v > 1<<53 {
			return 0
		}
	}
	return v
}

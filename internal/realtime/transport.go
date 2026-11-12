package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Channel identifies the transport a subscription is riding on. allows
// exactly one primary channel at a time: WS, then SSE, then ETag polling. This
// package implements the first two; polling is a plain REST concern owned by
// internal/httpapi.
type Channel string

const (
	ChannelWS  Channel = "ws"
	ChannelSSE Channel = "sse"
)

// ErrNotReadable reports that a transport cannot carry client frames. SSE is
// server-to-client only, so the read loop is simply not started for it.
var ErrNotReadable = errors.New("realtime: transport is not readable")

// transport is the only seam between this package's delivery logic and the
// concrete wire protocol.
//
// Keeping it an interface has one concrete benefit beyond testability: the
// WebSocket library choice (see doc.go) appears in exactly one file. If
// coder/websocket is ever replaced, only newWSConn changes.
type transport interface {
	// WriteFrame sends one notification. The frame is the encoded ServerMessage;
	// the message itself is passed alongside because SSE wants the cursor as the
	// event id and re-encoding it would be waste.
	WriteFrame(ctx context.Context, m ServerMessage, frame []byte) error
	// ReadFrame blocks for one client frame.
	ReadFrame(ctx context.Context) ([]byte, error)
	// Ping checks liveness at the protocol level. It returns ErrPingUnsupported
	// when the transport has no ping (SSE uses comment keep-alive instead).
	Ping(ctx context.Context) error
	// KeepAlive emits an out-of-band liveness signal (an SSE comment).
	KeepAlive(ctx context.Context) error
	// NeedsKeepAlive reports whether the writer loop should run a keep-alive
	// ticker.
	NeedsKeepAlive() bool
	// Close terminates the connection. status follows RFC 6455 close codes; SSE
	// ignores it.
	Close(status int, reason string) error
	// Channel reports which transport this is.
	Channel() Channel
}

// ErrPingUnsupported is returned by transports without a protocol ping.
var ErrPingUnsupported = errors.New("realtime: ping unsupported")

// wsConn adapts *websocket.Conn to transport.
type wsConn struct {
	c *websocket.Conn
}

func newWSTransport(c *websocket.Conn) transport { return &wsConn{c: c} }

func (w *wsConn) Channel() Channel { return ChannelWS }

func (w *wsConn) WriteFrame(ctx context.Context, _ ServerMessage, frame []byte) error {
	return w.c.Write(ctx, websocket.MessageText, frame)
}

func (w *wsConn) ReadFrame(ctx context.Context) ([]byte, error) {
	typ, data, err := w.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		// A binary frame is not part of the contract. coder/websocket has already
		// unmasked it, so this is a cheap check rather than a stream concern.
		return nil, fmt.Errorf("realtime: unexpected %v frame", typ)
	}
	return data, nil
}

func (w *wsConn) Ping(ctx context.Context) error { return w.c.Ping(ctx) }

func (w *wsConn) KeepAlive(context.Context) error { return nil }

func (w *wsConn) NeedsKeepAlive() bool { return false }

func (w *wsConn) Close(status int, reason string) error {
	// A close handshake with a peer that has already vanished can block; the
	// caller's write timeout does not cover it, so bound it here and fall back to
	// CloseNow. Losing the close handshake is cosmetic, leaking the goroutine is
	// not.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := w.c.Close(websocket.StatusCode(status), reason)
	if err != nil && ctx.Err() != nil {
		_ = w.c.CloseNow()
	}
	return err
}

// sseConn adapts an http.ResponseWriter to transport.
//
// SSE is a degraded channel: notifications only, no client frames. The client
// resumes by reconnecting with its cursor, which this package accepts through the
// Last-Event-ID header or the cursor query parameter.
type sseConn struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSETransport(w http.ResponseWriter, f http.Flusher) transport {
	return &sseConn{w: w, flusher: f}
}

func (s *sseConn) Channel() Channel { return ChannelSSE }

// WriteFrame emits one SSE event. The cursor doubles as the SSE event id so that
// a browser EventSource reconnect carries Last-Event-ID back to us without the
// client having to track anything. This is the one place where the opaque cursor
// is deliberately exposed to a browser API, and it is still opaque to the client:
// EventSource stores and replays it verbatim.
func (s *sseConn) WriteFrame(_ context.Context, m ServerMessage, frame []byte) error {
	// The event name must be on the wire, not only inside the payload.
	//
	// EventSource dispatches a frame carrying "event: x" to the listener registered
	// for x and NOT to onmessage; without it every frame falls through to onmessage.
	// Emitting only id+data therefore silently changes the delivery contract: a
	// client that listens for named events stops hearing them, and one that relies on
	// onmessage for everything starts receiving frames it never expected.
	//
	// The type is already the first field of the payload, so it is read from there
	// rather than re-derived: one source of truth, no chance of the two disagreeing.
	if name := eventName(m); name != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", name); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "id: %s\ndata: %s\n\n", m.Cursor, frame); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// eventName returns the SSE event name for a message, or "" when the type is empty.
func eventName(m ServerMessage) string {
	return string(m.Type)
}

func (s *sseConn) ReadFrame(context.Context) ([]byte, error) { return nil, ErrNotReadable }

func (s *sseConn) Ping(context.Context) error { return ErrPingUnsupported }

func (s *sseConn) KeepAlive(context.Context) error {
	// A comment line keeps proxies and the client's idle timer from tearing the
	// stream down .
	if _, err := fmt.Fprint(s.w, ": keep-alive\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *sseConn) NeedsKeepAlive() bool { return true }

func (s *sseConn) Close(int, string) error { return nil }

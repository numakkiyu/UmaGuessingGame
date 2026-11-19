package realtime

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSEFrameCarriesEventName pins the wire format the browser depends on.
//
// EventSource routes a frame with "event: x" to the listener registered for x and
// does NOT deliver it to onmessage. A frame with only id+data goes to onmessage
// instead. Emitting one shape where the other was expected therefore changes the
// delivery contract silently: named-event listeners go quiet, and onmessage handlers
// start receiving frames they never asked for.
//
// This regressed when the hub replaced the polling transport — the polling path
// emitted "event:" and the hub path did not, so every frame fell through to
// onmessage. It only surfaced because a client happened to register both.
func TestSSEFrameCarriesEventName(t *testing.T) {
	rec := httptest.NewRecorder()
	conn := newSSETransport(rec, nopFlusher{})

	msg := ServerMessage{
		Type:   "state.changed",
		Cursor: "cur-1",
	}
	if err := conn.WriteFrame(context.Background(), msg, []byte(`{"type":"state.changed"}`)); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	out := rec.Body.String()
	if !strings.Contains(out, "event: state.changed\n") {
		t.Errorf("the frame must carry an event name; got %q", out)
	}
	if !strings.Contains(out, "id: cur-1\n") {
		t.Errorf("the frame must carry the cursor as its id; got %q", out)
	}
	if !strings.Contains(out, "data: ") {
		t.Errorf("the frame must carry a data line; got %q", out)
	}
	// SSE requires a blank line to terminate the frame.
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("the frame must end with a blank line; got %q", out)
	}
}

// TestSSEEventNameComesFromTheMessage keeps the event name and the payload from
// drifting: both are derived from the same field.
func TestSSEEventNameComesFromTheMessage(t *testing.T) {
	for _, typ := range []MessageType{"hello", "state.changed", "presence.changed", "session.revoked"} {
		rec := httptest.NewRecorder()
		conn := newSSETransport(rec, nopFlusher{})
		if err := conn.WriteFrame(context.Background(), ServerMessage{Type: typ, Cursor: "c"}, []byte("{}")); err != nil {
			t.Fatalf("write frame: %v", err)
		}
		if want := "event: " + string(typ) + "\n"; !strings.Contains(rec.Body.String(), want) {
			t.Errorf("type %q: want %q in %q", typ, want, rec.Body.String())
		}
	}
}

// nopFlusher satisfies http.Flusher; httptest.ResponseRecorder already does, but
// naming it keeps the intent obvious.
type nopFlusher struct{}

func (nopFlusher) Flush() {}

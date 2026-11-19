package realtime

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOriginRejectsMalformedSameHost(t *testing.T) {
	o := originSet{require: true}
	for _, raw := range []string{"game.example", "evil://game.example", "http://game.example/path", "http://game.example?x", "http://game.example#x", "http://user@game.example", "https://game.example"} {
		if ok, _ := o.allowed(newRequestWithOrigin(raw, "game.example")); ok {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestClientAckFloodDisconnectsAndReleasesSubscription(t *testing.T) {
	h := newTestHub(t, nil)
	tr := newFakeTransport()
	for i := 0; i < 60; i++ {
		tr.inbound <- []byte(`{"type":"ack","cursor":"x"}`)
	}
	done := make(chan error, 1)
	go func() {
		done <- h.Serve(context.Background(), SubscribeOptions{Identity: identity("room-flood", "member-flood", "session-flood", "player"), Transport: tr})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flood remained connected")
	}
	if h.SubscriptionCount() != 0 {
		t.Fatal("subscription leaked")
	}
}

type sseDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *sseDeadlineWriter) SetWriteDeadline(d time.Time) error { w.deadline = d; return nil }

func TestSSEWritesUseContextDeadline(t *testing.T) {
	w := &sseDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	tr := newSSETransport(w, w)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := tr.WriteFrame(ctx, ServerMessage{Type: TypeHello}, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if !w.deadline.Equal(deadline) {
		t.Fatal("frame ignored deadline")
	}
	w.deadline = time.Time{}
	if err := tr.KeepAlive(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.deadline.Equal(deadline) {
		t.Fatal("keepalive ignored deadline")
	}
}

func TestClientFramesRejectDuplicateFields(t *testing.T) {
	for _, raw := range []string{`{"type":"ack","type":"ping"}`, `{"type":"ack","cursor":"a","Cursor":"b"}`} {
		if _, err := DecodeClientMessage([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

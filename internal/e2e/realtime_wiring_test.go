package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// contextWithTimeout is a tiny helper so the stream test cannot hang the suite.
func contextWithTimeout(seconds int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
}

// TestStreamIsPushBasedNotPolling pins that the SSE endpoint is served by the real
// hub, not the polling fallback.
//
// This is a wiring assertion, not a behavioural one, and it exists because the
// project shipped for a long time with the realtime package fully written (5656
// lines, outbox dispatcher, backpressure, cursor replay, revoke frames) and never
// referenced from the HTTP layer. The polling fallback works well enough that
// nothing failed, so the gap was invisible: every notification was late by up to one
// tick, and a revocation could take 150ms to reach a client that should already be
// cut off.
//
// The distinguishing signal on the wire is the hello frame's event id: the hub mints
// "hello-sub-<n>", while the fallback emits a plain hello with no subscription id.
func TestStreamIsPushBasedNotPolling(t *testing.T) {
	h := newHarness(t)
	h.createSession("push-host")

	roomID := h.createRoom("classic")

	req, err := http.NewRequest(http.MethodGet, h.base+"/api/v1/rooms/"+roomID+"/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Origin", h.base)
	req.Header.Set("Accept", "text/event-stream")

	ctx, cancel := contextWithTimeout(3)
	defer cancel()
	resp, err := h.client.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}

	// Read just far enough to see the hello frame.
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	head := string(buf[:n])

	if !strings.Contains(head, "hello") {
		t.Fatalf("the stream must open with a hello frame, got %q", head)
	}
	if !strings.Contains(head, "hello-sub-") {
		t.Errorf("the hello frame must come from the push hub (eventId hello-sub-<n>), "+
			"which means the polling fallback is serving this endpoint; got %q", head)
	}
}

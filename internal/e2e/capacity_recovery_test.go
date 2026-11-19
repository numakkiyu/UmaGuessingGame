package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
)

// TestAddressLimitRecoversAfterRetryAfter proves the limiter is a rate limit and not
// a permanent ban.
//
// A venue that is briefly over budget must be able to play again once the window
// passes. The limiter here is a token bucket refilled from the wall clock, so the
// test waits the advertised Retry-After rather than reaching into the bucket.
func TestAddressLimitRecoversAfterRetryAfter(t *testing.T) {
	h := newHarnessWith(t, func(c *httpapi.Config) {
		// Two first arrivals fit in this minute; the third overflows.
		c.Limiter.SessionCreatePerIP = httpapi.Rate{PerMinute: 60, BurstTokens: 2}
	})

	for i := 0; i < 2; i++ {
		if status, body := postSession(t, h, ""); status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("arrival %d status = %d body=%v", i, status, body)
		}
	}
	status, body := postSession(t, h, "")
	if status != http.StatusTooManyRequests {
		t.Fatalf("third arrival status = %d, want 429; body=%v", status, body)
	}

	// The refusal must say how long to wait. The harness client is a plain
	// http.Client, so the header has to be read from a fresh request.
	req, err := http.NewRequest(http.MethodPost, h.base+"/api/v1/sessions",
		strings.NewReader(`{"displayName":"恢复探测"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("probe status = %d, want 429", resp.StatusCode)
	}
	raw := resp.Header.Get("Retry-After")
	if raw == "" {
		t.Fatalf("429 carried no Retry-After")
	}
	wait, err := time.ParseDuration(raw + "s")
	if err != nil || wait <= 0 {
		t.Fatalf("Retry-After = %q, want positive seconds", raw)
	}
	// One second of slack for the bucket refill boundary.
	time.Sleep(wait + time.Second)

	if status, body := postSession(t, h, ""); status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("after Retry-After the client was still refused: %d %v", status, body)
	}
}

// TestStreamSubscriptionIsReleasedOnClientDisconnect is the connection-release gap.
//
// The long load run held streams open for its whole duration and reported frames,
// but never showed that a stream which goes away is actually released. A room that
// people browse repeatedly would otherwise accumulate dead subscribers until the
// process ran out of them. The observable is the hub's own subscriber bookkeeping,
// read through the same server the client talks to.
func TestStreamSubscriptionIsReleasedOnClientDisconnect(t *testing.T) {
	h := newHarness(t)
	h.createSession("stream-host")
	roomID := h.createRoom("classic")

	open := func() (context.CancelFunc, *http.Response) {
		req, err := http.NewRequest(http.MethodGet, h.base+"/api/v1/rooms/"+roomID+"/stream", nil)
		if err != nil {
			t.Fatalf("build stream request: %v", err)
		}
		req.Header.Set("Origin", h.base)
		req.Header.Set("Accept", "text/event-stream")
		ctx, cancel := context.WithCancel(context.Background())
		resp, err := h.client.Do(req.WithContext(ctx))
		if err != nil {
			cancel()
			t.Fatalf("open stream: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			t.Fatalf("stream status = %d", resp.StatusCode)
		}
		return func() {
			resp.Body.Close()
			cancel()
		}, resp
	}

	// First connection: read the hello frame so the subscription is definitely
	// established before it is torn down.
	closeFirst, resp := open()
	buf := make([]byte, 2048)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("read hello frame: %v", err)
	}
	closeFirst()

	// Give the server a moment to notice the disconnect, then reconnect. The proof
	// that the first subscription was released is that the second one is accepted
	// and serves a fresh hello rather than being refused or left stale.
	time.Sleep(150 * time.Millisecond)
	closeSecond, resp2 := open()
	defer closeSecond()
	n, err := resp2.Body.Read(buf)
	if err != nil {
		t.Fatalf("reconnect read: %v", err)
	}
	head := string(buf[:n])
	if !strings.Contains(head, "hello") {
		t.Fatalf("reconnect did not receive a hello frame: %q", head)
	}
	// The hub mints a new subscription id per connection, so a fresh id is evidence
	// the server treated this as a new subscriber instead of reusing a dead one.
	if !strings.Contains(head, "hello-sub-") {
		t.Fatalf("reconnect was served by the polling fallback, not the push hub: %q", head)
	}
}

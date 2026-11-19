package httpapi_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFallbackSSEStopsOnSessionExpiry(t *testing.T) {
	h := newHarness(t)
	client, sess := h.newSession("过期测试")
	rid := h.seedRoom(t, sess.ID, "fallback")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", h.server.URL+"/api/v1/rooms/"+rid+"/stream", nil)
	req.Header.Set("Origin", h.server.URL)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("SSE status %d", res.StatusCode)
	}
	if _, err := h.db.Write().Exec(`UPDATE sessions SET expires_at=1 WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(res.Body)
	found := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: session.revoked") {
			found = true
		}
	}
	if !found || scanner.Err() != nil {
		t.Fatalf("expired SSE not revoked: found=%v err=%v", found, scanner.Err())
	}
}

func TestFallbackSSERequiresMembership(t *testing.T) {
	h := newHarness(t)
	client, sess := h.newSession("房主")
	rid := h.seedRoom(t, sess.ID, "fallback")
	if _, err := h.db.Write().Exec(`DELETE FROM memberships WHERE room_id=?`, rid); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", h.server.URL+"/api/v1/rooms/"+rid+"/stream", nil)
	req.Header.Set("Origin", h.server.URL)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("owner without membership SSE status %d", res.StatusCode)
	}
}

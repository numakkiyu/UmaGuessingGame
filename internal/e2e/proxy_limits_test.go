package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
)

// postSession sends a real HTTP session-create over the assembled server.
// The forwarded value lets a test forge X-Forwarded-For exactly as a client would.
func postSession(t *testing.T, h *harness, forwarded string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.base+"/api/v1/sessions", bytes.NewReader([]byte(`{"displayName":"limit-probe"}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("POST /sessions: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("POST /sessions returned non-JSON (%d): %s", resp.StatusCode, raw)
		}
	}
	return resp.StatusCode, body
}

func sessionRows(t *testing.T, h *harness) int {
	t.Helper()
	var n int
	if err := h.db.Read().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// TestForgedForwardedHeaderCannotBypassAddressLimit is the H01 real-HTTP/SQLite
// regression: a direct caller rotating X-Forwarded-For must not gain fresh
// address buckets, and a rejected request must not write a session row.
func TestForgedForwardedHeaderCannotBypassAddressLimit(t *testing.T) {
	h := newHarnessWith(t, func(c *httpapi.Config) {
		// TrustProxy stays off: forwarding headers from a direct caller are ignored.
		c.Limiter.SessionCreatePerIP = httpapi.Rate{PerMinute: 1, BurstTokens: 2}
	})
	// The transport peer is 127.0.0.1 for every request, so all three share one
	// bucket no matter which forged address they claim.
	for i, forged := range []string{"198.51.100.1", "198.51.100.2"} {
		if status, body := postSession(t, h, forged); status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("request %d with forged %s status = %d body=%v", i, forged, status, body)
		}
	}
	// The same browser cookie reuses its session row, so the honest baseline is
	// the row count before the rejected call, not the number of HTTP calls.
	before := sessionRows(t, h)
	status, body := postSession(t, h, "198.51.100.3")
	if status != http.StatusTooManyRequests {
		t.Fatalf("rotating X-Forwarded-For bypassed the address limit: status = %d body=%v", status, body)
	}
	if body["csrfToken"] != nil || body["sessionId"] != nil {
		t.Fatalf("rejected create returned a credential: %v", body)
	}
	if n := sessionRows(t, h); n != before {
		t.Fatalf("rejected request wrote an identity: sessions = %d, want %d", n, before)
	}
}

// TestTrustedProxyKeepsOneBucketPerRightmostClient proves that behind a trusted
// proxy the boundary is the rightmost untrusted hop, so left-hand junk cannot
// split a real client across buckets and a genuinely different client still can.
func TestTrustedProxyKeepsOneBucketPerRightmostClient(t *testing.T) {
	loopback := netip.MustParsePrefix("127.0.0.0/8")
	h := newHarnessWith(t, func(c *httpapi.Config) {
		c.TrustProxy = true
		c.TrustedProxies = []netip.Prefix{loopback}
		c.Limiter.SessionCreatePerIP = httpapi.Rate{PerMinute: 1, BurstTokens: 2}
	})
	for _, chain := range []string{"198.51.100.1, 203.0.113.7", "198.51.100.2, 203.0.113.7"} {
		if status, body := postSession(t, h, chain); status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("chain %q status = %d body=%v", chain, status, body)
		}
	}
	// Same real client, different leftmost prefix: still the same bucket.
	before := sessionRows(t, h)
	status, body := postSession(t, h, "198.51.100.99, 203.0.113.7")
	if status != http.StatusTooManyRequests {
		t.Fatalf("malicious leftmost value split the bucket: status = %d body=%v", status, body)
	}
	if n := sessionRows(t, h); n != before {
		t.Fatalf("denied proxied request wrote an identity: sessions = %d, want %d", n, before)
	}
	// A different real client is still distinguishable and keeps its own budget.
	status, body = postSession(t, h, "198.51.100.1, 203.0.113.88")
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("distinct proxied client was merged with another: status = %d body=%v", status, body)
	}
	if body["sessionId"] == nil {
		t.Fatalf("distinct proxied client did not receive a session: %v", body)
	}
}

// browser is one independent client: its own cookie jar and CSRF token. Two
// browsers are two players even when they share one egress address.
type browser struct {
	client *http.Client
	csrf   string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &browser{client: &http.Client{Jar: jar}}
}

// do performs a request as this browser, optionally claiming an egress address.
func (b *browser) do(t *testing.T, base, method, path string, body any, key, forwarded string) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, base+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.csrf != "" {
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s %s returned non-JSON (%d): %s", method, path, resp.StatusCode, raw)
		}
	}
	return resp.StatusCode, out
}

// TestSameEgressKeepsDistinctPlayerIdentities is the H01 remainder for
// "同出口多身份独立": several players arriving through one trusted proxy share a
// single egress address. They must remain separate identities — separate
// sessions, memberships and six-digit room numbers — while the address budget
// itself stays shared, which is the documented boundary rather than a defect.
func TestSameEgressKeepsDistinctPlayerIdentities(t *testing.T) {
	const egress = "203.0.113.7"
	loopback := netip.MustParsePrefix("127.0.0.0/8")
	h := newHarnessWith(t, func(c *httpapi.Config) {
		c.TrustProxy = true
		c.TrustedProxies = []netip.Prefix{loopback}
		// Exactly four first arrivals fit the shared address budget, so the fifth
		// demonstrates the shared bucket without depending on a production default.
		c.Limiter.SessionCreatePerIP = httpapi.Rate{PerMinute: 1, BurstTokens: 4}
	})

	owner := newBrowser(t)
	status, body := owner.do(t, h.base, http.MethodPost, "/api/v1/sessions", map[string]any{"displayName": "venue-host"}, "", egress)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("owner session status = %d body=%v", status, body)
	}
	owner.csrf, _ = body["csrfToken"].(string)
	ownerID, _ := body["sessionId"].(string)
	if ownerID == "" || owner.csrf == "" {
		t.Fatalf("owner session incomplete: %v", body)
	}

	status, created := owner.do(t, h.base, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name":            "venue room",
		"kind":            "friend",
		"modeConfig":      map[string]any{"modeId": "classic", "config": map[string]any{}},
		"maxPlayers":      8,
		"maxSpectators":   0,
		"visibility":      "invite",
		"allowSpectators": false,
	}, idem("venue-room"), egress)
	if status != http.StatusCreated {
		t.Fatalf("create room status = %d body=%v", status, created)
	}
	roomID, _ := created["roomId"].(string)
	if roomID == "" {
		if rm, ok := created["room"].(map[string]any); ok {
			roomID, _ = rm["roomId"].(string)
		}
	}
	invite, _ := created["inviteCode"].(string)
	if roomID == "" || invite == "" {
		t.Fatalf("create room returned no roomId/inviteCode: %v", created)
	}

	// Three more players from the same egress address.
	sessions := map[string]bool{ownerID: true}
	memberships := map[string]bool{}
	numbers := map[any]bool{}
	codes := map[string]bool{}
	for _, name := range []string{"uma-alpha", "uma-beta", "uma-gamma"} {
		b := newBrowser(t)
		st, sb := b.do(t, h.base, http.MethodPost, "/api/v1/sessions", map[string]any{"displayName": name}, "", egress)
		if st != http.StatusCreated && st != http.StatusOK {
			t.Fatalf("%s session status = %d body=%v", name, st, sb)
		}
		b.csrf, _ = sb["csrfToken"].(string)
		sid, _ := sb["sessionId"].(string)
		if sid == "" || b.csrf == "" {
			t.Fatalf("%s session incomplete: %v", name, sb)
		}
		if sessions[sid] {
			t.Fatalf("%s was merged into another player's session %s", name, sid)
		}
		sessions[sid] = true
		if got, _ := sb["displayName"].(string); got != name {
			t.Fatalf("%s session display name = %q, want %q", name, got, name)
		}

		st, jb := b.do(t, h.base, http.MethodPost, "/api/v1/rooms/"+roomID+"/memberships", map[string]any{
			"inviteCode":    invite,
			"requestedRole": "player",
		}, idem("venue-join-"+name), egress)
		if st != http.StatusCreated {
			t.Fatalf("%s join status = %d body=%v", name, st, jb)
		}
		mid, _ := jb["membershipId"].(string)
		if mid == "" {
			t.Fatalf("%s join returned no membershipId: %v", name, jb)
		}
		if memberships[mid] {
			t.Fatalf("%s reused another player's membership %s", name, mid)
		}
		memberships[mid] = true
		if got, _ := jb["displayName"].(string); got != name {
			t.Fatalf("%s membership display name = %q, want %q", name, got, name)
		}
		code, _ := jb["publicCode"].(string)
		if len(code) != 6 {
			t.Fatalf("%s publicCode = %q, want six digits", name, code)
		}
		if codes[code] {
			t.Fatalf("%s reused room number %s", name, code)
		}
		codes[code] = true
		numbers[jb["playerNumber"]] = true
		t.Logf("%s -> session %s membership %s number %s", name, sid, mid, code)
	}
	if len(memberships) != 3 {
		t.Fatalf("distinct memberships = %d, want 3", len(memberships))
	}

	// The address budget itself is deliberately shared: a fifth first arrival from
	// the same egress is refused, and the refusal writes no identity.
	before := sessionRows(t, h)
	fifth := newBrowser(t)
	st, fb := fifth.do(t, h.base, http.MethodPost, "/api/v1/sessions", map[string]any{"displayName": "uma-delta"}, "", egress)
	if st != http.StatusTooManyRequests {
		t.Fatalf("fifth arrival from the same egress status = %d, want 429; body=%v", st, fb)
	}
	if n := sessionRows(t, h); n != before {
		t.Fatalf("denied arrival wrote an identity: sessions = %d, want %d", n, before)
	}
}

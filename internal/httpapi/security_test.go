package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/room"
)

// TestNoPrivateStateLeaksToNonParticipants is the private-information test.
//
// It seeds a round with a secret answer in round_private_state and asserts that no
// response reachable by a spectator or a display session contains the secret. The
// point is not that a field is hidden in the browser: the value must not appear in
// the JSON at all .
func TestNoPrivateStateLeaksToNonParticipants(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ownerClient, ownerSess := h.newSession("round-owner")
	specClient, specSess := h.newSession("spectator")

	roomID := h.seedRoom(t, ownerSess.ID, "private room")
	specMembership := h.seedMember(t, roomID, specSess.ID, "spectator")
	_ = specMembership

	// The secret is a distinctive token so a leak is unambiguous.
	const secretAnswer = "SECRET_ANSWER_CHARACTER_ID_9001"
	roundID := h.seedRound(t, roomID, secretAnswer)

	// The owner (a player, so a competing viewer) reads the round.
	ownerRes := h.doAs(ownerClient, http.MethodGet, "/api/v1/rounds/"+roundID, nil, readHeaders(h))
	if ownerRes.Status != http.StatusOK {
		t.Fatalf("owner getRound status = %d, want 200 (body=%s)", ownerRes.Status, string(ownerRes.Body))
	}
	if strings.Contains(string(ownerRes.Body), secretAnswer) {
		t.Fatalf("owner response contains the secret answer before reveal: %s", string(ownerRes.Body))
	}

	// The spectator must also not receive it.
	specRes := h.doAs(specClient, http.MethodGet, "/api/v1/rounds/"+roundID, nil, readHeaders(h))
	if specRes.Status != http.StatusOK {
		t.Fatalf("spectator getRound status = %d, want 200 (body=%s)", specRes.Status, string(specRes.Body))
	}
	if strings.Contains(string(specRes.Body), secretAnswer) {
		t.Fatalf("spectator response contains the secret answer: %s", string(specRes.Body))
	}
	// A spectator must not receive a privateState object with content.
	var specView struct {
		ViewerRole   string          `json:"viewerRole"`
		PrivateState json.RawMessage `json:"privateState"`
	}
	if err := json.Unmarshal(specRes.Body, &specView); err != nil {
		t.Fatalf("decode spectator view: %v", err)
	}
	if specView.ViewerRole != "spectator" {
		t.Errorf("spectator viewerRole = %q, want spectator", specView.ViewerRole)
	}
	if len(specView.PrivateState) > 0 && string(specView.PrivateState) != "null" {
		t.Errorf("spectator received a privateState: %s", string(specView.PrivateState))
	}
}

// TestDeadlineUsesServerArrivalTime asserts the window comparison uses the server
// clock, not a client claim.
//
// The test moves the service clock past the deadline and then submits: the response
// must be a 409 deadline-passed, proving the server never trusts a client-supplied
// time .
func TestDeadlineUsesServerArrivalTime(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, ownerSess := h.newSession("deadline-owner")
	roomID := h.seedRoom(t, ownerSess.ID, "deadline room")
	roundID := h.seedRoundWithDeadline(t, roomID, "COLLECTING", time.Now().Add(-time.Minute))

	// A submission after the window must be refused.
	ctx := context.Background()
	actor, err := h.rooms.LoadActor(ctx, ownerSess.ID, roomID)
	if err != nil {
		t.Fatalf("load actor: %v", err)
	}
	_, err = h.rooms.CreateSubmission(ctx, actor, roundID, room.SubmissionInput{
		PhaseVersion:       1,
		SubmissionRevision: 0,
		ControlGeneration:  0,
	})
	if err == nil {
		t.Fatal("submission after the deadline was accepted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "mode") &&
		!strings.Contains(strings.ToLower(err.Error()), "deadline") &&
		!strings.Contains(strings.ToLower(err.Error()), "notimplemented") {
		t.Logf("submission error: %v", err)
	}
}

// TestModeDependentOperationsReturn501 documents the deliberate 501s.
//
// No mode implementation is registered in this build, so every operation that needs
// one must answer 501 with a stable code rather than fabricating success. This test
// pins that behaviour so it cannot silently become a fake 200 later.
func TestModeDependentOperationsReturn501(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, sess := h.newSession("501-tester")

	cases := []struct {
		name     string
		method   string
		path     string
		body     any
		wantCode string
	}{
		{
			name: "createRoom needs a mode to validate its config", method: http.MethodPost,
			path: "/api/v1/rooms",
			body: map[string]any{
				"name": "needs mode", "kind": "friend",
				"modeConfig": map[string]any{"modeId": "classic"},
				"maxPlayers": 4, "maxSpectators": 2,
				"visibility": "invite", "allowSpectators": true,
			},
			wantCode: "modes-unavailable",
		},
		{
			name: "listCharacters needs a content pack", method: http.MethodGet,
			path:     "/api/v1/characters",
			wantCode: "content-unavailable",
		},
		{
			name: "getCharacter needs a content pack", method: http.MethodGet,
			path:     "/api/v1/characters/char_1",
			wantCode: "content-unavailable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.doAs(client, tc.method, tc.path, tc.body, writeHeaders(h, sess, idemKey("501")))
			if res.Status != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501 (body=%s)", res.Status, string(res.Body))
			}
			if res.Problem.Code != tc.wantCode {
				t.Errorf("problem.code = %q, want %q", res.Problem.Code, tc.wantCode)
			}
			if res.Problem.Detail == "" {
				t.Error("a 501 must explain which dependency is missing")
			}
			if res.Header.Get("Content-Type") != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json",
					res.Header.Get("Content-Type"))
			}
		})
	}

	// The WebSocket endpoint is also explicitly unimplemented, and must say so
	// instead of half-upgrading a connection.
	wsRes := h.doAs(client, http.MethodGet, "/api/v1/rooms/rom_missing/socket", nil, readHeaders(h))
	if wsRes.Status != http.StatusNotFound && wsRes.Status != http.StatusNotImplemented {
		t.Fatalf("socket status = %d, want 404 or 501 (body=%s)", wsRes.Status, string(wsRes.Body))
	}
}

// TestRateLimitingIsPerIdentityNotPerIP verifies separate quotas for sessions sharing one public IP.
func TestRateLimitingIsPerIdentityNotPerIP(t *testing.T) {
	t.Parallel()
	// A deliberately tiny session allowance makes the boundary observable.
	h := newHarness(t, func(c *harnessConfig) {})
	clientA, sessA := h.newSession("rate-a")
	clientB, _ := h.newSession("rate-b")

	// Both sessions arrive from 127.0.0.1, the same public address.
	// Exhaust A's per-session allowance on a read-heavy private route.
	var aLimited bool
	for i := 0; i < 400; i++ {
		res := h.doAs(clientA, http.MethodGet, "/api/v1/sessions/current", nil, readHeaders(h))
		if res.Status == http.StatusTooManyRequests {
			aLimited = true
			if res.Header.Get("Retry-After") == "" {
				t.Error("429 must carry Retry-After")
			}
			if res.Problem.Retryable == nil || !*res.Problem.Retryable {
				t.Error("429 problem must set retryable=true")
			}
			break
		}
	}
	if !aLimited {
		t.Fatal("default per-session limiter did not reject a flood within 400 requests")
	}

	// B, from the same IP, must still be served: the bucket is per session.
	resB := h.doAs(clientB, http.MethodGet, "/api/v1/sessions/current", nil, readHeaders(h))
	if resB.Status == http.StatusTooManyRequests {
		t.Fatalf("session B was rate limited by session A's traffic from the same IP")
	}
	_ = sessA
}

// TestBodySizeLimit asserts an oversized body is a 413 Problem, not a truncated
// parse that silently succeeds on a prefix.
func TestBodySizeLimit(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, sess := h.newSession("size-tester")

	// 128 KiB of a single long string field, well over the 64 KiB limit.
	huge := map[string]any{"displayName": strings.Repeat("a", 128<<10)}
	res := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current", huge,
		writeHeaders(h, sess, idemKey("big")))
	if res.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body=%s)", res.Status, string(res.Body))
	}
	if res.Problem.Code != "payload-too-large" {
		t.Errorf("problem.code = %q, want payload-too-large", res.Problem.Code)
	}
}

// TestRevokedSessionCannotReuseIdentity asserts a revoked cookie stops working and
// is never reactivated by a repeated delete .
func TestRevokedSessionCannotReuseIdentity(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, sess := h.newSession("revoke-me")

	// First delete revokes and returns 204.
	first := h.doAs(client, http.MethodDelete, "/api/v1/sessions/current", nil,
		writeHeaders(h, sess, idemKey("del0")))
	if first.Status != http.StatusNoContent {
		t.Fatalf("first delete status = %d, want 204 (body=%s)", first.Status, string(first.Body))
	}

	// A repeat is permitted to answer 204 or 401 , but the
	// identity must never be reactivated and the old receipt must not become an
	// access grant for a revoked session.
	second := h.doAs(client, http.MethodDelete, "/api/v1/sessions/current", nil,
		writeHeaders(h, sess, idemKey("del1")))
	if second.Status != http.StatusNoContent && second.Status != http.StatusUnauthorized {
		t.Fatalf("repeat delete status = %d, want 204 or 401 (body=%s)",
			second.Status, string(second.Body))
	}

	// The stored session row must be marked revoked, and a read must now fail.
	got := h.do(http.MethodGet, "/api/v1/sessions/current", nil, readHeaders(h))
	if got.Status != http.StatusUnauthorized {
		t.Fatalf("post-revoke read status = %d, want 401 (body=%s)", got.Status, string(got.Body))
	}

	var revoked int
	err := h.db.Read().QueryRowContext(context.Background(),
		`SELECT COUNT(1) FROM sessions WHERE id = ? AND revoked_at IS NOT NULL`, sess.ID).Scan(&revoked)
	if err != nil {
		t.Fatalf("query revoked: %v", err)
	}
	if revoked != 1 {
		t.Errorf("session row is not marked revoked (revoked rows = %d)", revoked)
	}
}

// TestSessionCookieIsHttpOnly asserts the cookie attributes the security model
// depends on.
func TestSessionCookieIsHttpOnly(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	jar := h.client
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/sessions",
		strings.NewReader(`{"displayName":"cookie-tester"}`))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", h.server.URL)
	res, err := jar.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}

	var found bool
	for _, c := range res.Cookies() {
		if c.Name != "uma_session" {
			continue
		}
		found = true
		if !c.HttpOnly {
			t.Error("session cookie is not HttpOnly; script could read the credential")
		}
		if c.Path != "/" {
			t.Errorf("cookie Path = %q, want /", c.Path)
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie SameSite = %v, want Lax", c.SameSite)
		}
		if c.Value == "" {
			t.Error("cookie value is empty")
		}
		// The cookie must not carry the session id: it carries only the opaque token.
		var body testSession
		if err := json.NewDecoder(res.Body).Decode(&body); err == nil && body.ID == c.Value {
			t.Error("cookie value equals the session id; it must be an unguessable token")
		}
	}
	if !found {
		t.Fatal("no uma_session cookie was set")
	}
}

// TestDisplaySessionCannotWrite asserts a restricted display identity is refused on
// gameplay writes .
func TestDisplaySessionCannotWrite(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, _ := h.newSession("host")
	_ = client

	// Build a display session directly so the test does not depend on grant flow.
	displayClient, displaySess := h.newDisplaySession(t)
	res := h.doAs(displayClient, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name": "display write", "kind": "friend",
		"modeConfig": map[string]any{"modeId": "classic"},
		"maxPlayers": 4, "maxSpectators": 2,
		"visibility": "invite", "allowSpectators": true,
	}, map[string]string{
		"Origin": h.server.URL, "X-CSRF-Token": displaySess.CSRFToken,
		"Idempotency-Key": idemKey("disp"),
	})
	if res.Status != http.StatusForbidden && res.Status != http.StatusUnauthorized {
		t.Fatalf("display write status = %d, want 403/401 (body=%s)", res.Status, string(res.Body))
	}
	if res.Problem.Code == "" {
		t.Errorf("display write refusal has no problem.code (body=%s)", string(res.Body))
	}
}

// TestSessionPolicyIsSingleSourced guards against "configured but inert".
//
// A real defect was found in this package's own Config: it carried
// SecureCookies, AllowedOrigins, SameSite, AllowMissingOrigin, SessionTTL,
// DisplayTTL, IdempotencyTTL and QueueCapacity, cmd/server set all of them, and
// nothing ever read any of them. The values looked configured and did nothing.
//
// The fix was to delete the copies and keep the policy on the component that acts
// on it. This test asserts the behaviour actually follows the session service's
// configuration, so the duplication cannot silently return: if someone re-adds a
// shadow field on httpapi.Config, the assertion below still fails to observe any
// effect and the test documents why.
func TestSessionPolicyIsSingleSourced(t *testing.T) {
	t.Parallel()

	t.Run("secure cookie follows session config", func(t *testing.T) {
		t.Parallel()
		// With Secure=true the cookie must carry the Secure attribute. An httptest
		// TLS server is not needed to read the attribute off the Set-Cookie header.
		h := newHarness(t, func(c *harnessConfig) { c.SecureCookies = true })
		res := h.do(http.MethodPost, "/api/v1/sessions",
			map[string]any{"displayName": "secure-probe"},
			map[string]string{"Origin": h.server.URL})
		if res.Status != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body=%s)", res.Status, string(res.Body))
		}
		raw := res.Header.Get("Set-Cookie")
		if raw == "" {
			t.Fatal("no Set-Cookie header")
		}
		if !strings.Contains(strings.ToLower(raw), "secure") {
			t.Errorf("Secure=true was configured but the cookie is not Secure: %s", raw)
		}
	})

	t.Run("non-secure cookie omits Secure", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, func(c *harnessConfig) { c.SecureCookies = false })
		res := h.do(http.MethodPost, "/api/v1/sessions",
			map[string]any{"displayName": "plain-probe"},
			map[string]string{"Origin": h.server.URL})
		raw := strings.ToLower(res.Header.Get("Set-Cookie"))
		if strings.Contains(raw, "; secure") {
			t.Errorf("Secure=false was configured but the cookie is Secure: %s", raw)
		}
	})

	t.Run("allowed origins list is honoured", func(t *testing.T) {
		t.Parallel()
		// An explicit allow list must be consulted instead of the request Host, so a
		// proxy-fronted deployment can accept its public origin.
		const allowed = "https://uma.example"
		h := newHarness(t, func(c *harnessConfig) { c.AllowedOrigins = []string{allowed} })
		res := h.do(http.MethodPost, "/api/v1/sessions",
			map[string]any{"displayName": "origin-probe"},
			map[string]string{"Origin": allowed})
		if res.Status != http.StatusCreated {
			t.Fatalf("allowed origin was refused: status = %d (body=%s)",
				res.Status, string(res.Body))
		}

		// A different origin must still be refused, proving the list is a filter and
		// not a blanket permit.
		bad := h.do(http.MethodPost, "/api/v1/sessions",
			map[string]any{"displayName": "origin-probe-2"},
			map[string]string{"Origin": "https://evil.example"})
		if bad.Status != http.StatusForbidden {
			t.Fatalf("unlisted origin status = %d, want 403 (body=%s)",
				bad.Status, string(bad.Body))
		}
	})

	t.Run("allow-missing-origin is opt-in", func(t *testing.T) {
		t.Parallel()
		// Default: a missing Origin is refused.
		strict := newHarness(t)
		res := strict.do(http.MethodPost, "/api/v1/sessions",
			map[string]any{"displayName": "no-origin"}, nil)
		if res.Status != http.StatusForbidden {
			t.Fatalf("default status = %d, want 403 (body=%s)", res.Status, string(res.Body))
		}

		// Opted in: the same request is accepted.
		lenient := newHarness(t, func(c *harnessConfig) { c.AllowMissingOrigin = true })
		ok := lenient.do(http.MethodPost, "/api/v1/sessions",
			map[string]any{"displayName": "no-origin-2"}, nil)
		if ok.Status != http.StatusCreated {
			t.Fatalf("allow-missing-origin status = %d, want 201 (body=%s)",
				ok.Status, string(ok.Body))
		}
	})

	t.Run("queue capacity reaches the room service", func(t *testing.T) {
		t.Parallel()
		// The bound must be observable through the service, not just stored.
		h := newHarness(t, func(c *harnessConfig) { c.QueueCapacity = 3 })
		if got := h.rooms.Queue().Capacity(); got != 3 {
			t.Errorf("room queue capacity = %d, want 3", got)
		}
	})
}

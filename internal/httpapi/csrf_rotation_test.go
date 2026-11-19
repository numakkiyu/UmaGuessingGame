package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestCsrfRotationKeepsIdentityAndInvalidatesOldToken verifies rotation without the old secret, successful writes with the new secret, and rejection of the old secret.
func TestCsrfRotationKeepsIdentityAndInvalidatesOldToken(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) { c.AllowMissingOrigin = true })
	client, sess := h.newSession("rotate")

	rec := h.doAs(client, http.MethodPost, "/api/v1/sessions/current/csrf", nil, nil)
	if rec.Status != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200; body=%s", rec.Status, rec.Body)
	}
	var rotated struct {
		CSRFToken string `json:"csrfToken"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(rec.Body, &rotated); err != nil {
		t.Fatalf("decode rotation body: %v (body=%s)", err, rec.Body)
	}
	if rotated.CSRFToken == "" {
		t.Fatal("rotation returned no csrfToken")
	}
	if rotated.CSRFToken == sess.CSRFToken {
		t.Fatal("rotation returned the same token")
	}
	if rotated.SessionID != sess.ID {
		t.Errorf("rotation changed the session id: got %q, want %q", rotated.SessionID, sess.ID)
	}

	// 2. The new token works for a write.
	fresh := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current", map[string]any{
		"displayName": "after rotation",
	}, map[string]string{
		"X-CSRF-Token":    rotated.CSRFToken,
		"Idempotency-Key": "rotate-fresh-token-01",
	})
	if fresh.Status != http.StatusOK {
		t.Fatalf("write with the new token status = %d, want 200; body=%s", fresh.Status, fresh.Body)
	}

	// 3. The old token is rejected.
	stale := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current", map[string]any{
		"displayName": "stale token",
	}, map[string]string{
		"X-CSRF-Token":    sess.CSRFToken,
		"Idempotency-Key": "rotate-stale-token-01",
	})
	if stale.Status != http.StatusForbidden {
		t.Fatalf("write with the stale token status = %d, want 403; body=%s", stale.Status, stale.Body)
	}
}

// TestCsrfRotationRequiresSession proves the endpoint is not a way to obtain a
// token without a session.
func TestCsrfRotationRequiresSession(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) { c.AllowMissingOrigin = true })
	rec := h.do(http.MethodPost, "/api/v1/sessions/current/csrf", nil, nil)
	if rec.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session; body=%s", rec.Status, rec.Body)
	}
}

// TestCsrfRotationRejectsCrossOrigin proves Origin is still enforced even though
// the CSRF token itself is exempt.
func TestCsrfRotationRejectsCrossOrigin(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) { c.AllowMissingOrigin = true })
	client, _ := h.newSession("rotate")
	rec := h.doAs(client, http.MethodPost, "/api/v1/sessions/current/csrf", nil,
		map[string]string{"Origin": "http://evil.example"})
	if rec.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a cross-origin rotation; body=%s", rec.Status, rec.Body)
	}
}

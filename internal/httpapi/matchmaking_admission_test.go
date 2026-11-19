package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

func TestMatchProofRequiresConfiguredAdmissionBeforeWork(t *testing.T) {
	gate, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "site", Hostnames: []string{"uma.example"}, Operations: []string{"createMatchmakingTicket"}}, "test-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(stubMode{})
	h := newHarness(t, func(c *harnessConfig) {
		c.AllowMissingOrigin = true
		c.Admission = gate
		c.Modes = reg
		c.Content = matchHTTPContent{}
	})
	client, sess := h.newSession("player")
	headers := map[string]string{"X-CSRF-Token": sess.CSRFToken}
	refused := h.doAs(client, http.MethodPost, "/api/v1/matchmaking-proofs", map[string]any{"poolId": "classic-duel"}, headers)
	if refused.Status != 403 || refused.Problem.Code != "admission-required" {
		t.Fatalf("%d %s", refused.Status, refused.Body)
	}
	var count int
	if err = h.db.Read().QueryRow(`SELECT COUNT(*) FROM matchmaking_proofs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("issued proof before verification: %d %v", count, err)
	}
	grant := httptest.NewRecorder()
	gate.Grant(grant, sess.ID, time.Now().Add(time.Hour))
	client.Jar.SetCookies(mustURL(t, h.server.URL), grant.Result().Cookies())
	// A provider grant permits admission preparation, never admission without work.
	bad := h.doAs(client, http.MethodPost, "/api/v1/matchmaking-tickets", map[string]any{"poolId": "classic-duel", "proof": map[string]string{"challengeId": "missing", "nonce": "0"}}, map[string]string{"X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": "match-no-work-001"})
	if bad.Status != 403 || bad.Problem.Code != "matchmaking-proof-required" {
		t.Fatalf("provider grant bypassed work: %d %s", bad.Status, bad.Body)
	}
	body := matchTicketBody(t, h, client, sess, "classic-duel")
	good := h.doAs(client, http.MethodPost, "/api/v1/matchmaking-tickets", body, map[string]string{"X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": "match-with-work-01"})
	if good.Status != 201 {
		t.Fatalf("%d %s", good.Status, good.Body)
	}
}

func TestMatchHeartbeatNeedsSessionAndCSRFButNoGameReceipt(t *testing.T) {
	h := newMatchmakingHarness(t)
	client, sess := h.newSession("player")
	body := matchTicketBody(t, h, client, sess, "classic-duel")
	made := h.doAs(client, http.MethodPost, "/api/v1/matchmaking-tickets", body, map[string]string{"X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": "match-lease-create01"})
	if made.Status != 201 {
		t.Fatalf("%d %s", made.Status, made.Body)
	}
	var ticket struct {
		ID string `json:"ticketId"`
	}
	if err := json.Unmarshal(made.Body, &ticket); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/matchmaking-tickets/" + ticket.ID + "/heartbeat"
	denied := h.doAs(client, http.MethodPost, path, map[string]any{}, nil)
	if denied.Status != 403 {
		t.Fatalf("lease renewed without csrf: %d", denied.Status)
	}
	ok := h.doAs(client, http.MethodPost, path, map[string]any{}, map[string]string{"X-CSRF-Token": sess.CSRFToken})
	if ok.Status != 200 {
		t.Fatalf("lease requires a game receipt or rejects empty object: %d %s", ok.Status, ok.Body)
	}
	other, sess2 := h.newSession("other")
	denied = h.doAs(other, http.MethodPost, path, map[string]any{}, map[string]string{"X-CSRF-Token": sess2.CSRFToken})
	if denied.Status != 404 {
		t.Fatalf("foreign lease accessible: %d %s", denied.Status, denied.Body)
	}
}

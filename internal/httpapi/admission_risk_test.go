package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

func TestAdmissionStatusDoesNotCreateIdentityAndSeesSessionLimits(t *testing.T) {
	entry, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "public-site", Hostnames: []string{"uma.example"}, RiskThreshold: 3}, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *harnessConfig) {
		c.Admission = entry
		c.Limiter.SessionPerMinute = httpapi.Rate{PerMinute: 1, BurstTokens: 2}
	})
	check := func(required bool) {
		t.Helper()
		r := h.do("GET", "/api/v1/sessions/current/admission", nil, nil)
		var state admission.Status
		if r.Status != 200 || json.Unmarshal(r.Body, &state) != nil || !state.Enabled || state.Required != required {
			t.Fatalf("status=%d %s", r.Status, r.Body)
		}
	}
	check(false)
	var n int
	_ = h.db.Read().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 0 {
		t.Fatal("status minted identity")
	}
	client, sess := h.newSession("过闸训练员")
	h.useClient(client)
	grant := httptest.NewRecorder()
	entry.Grant(grant, sess.ID, time.Now().Add(time.Hour))
	client.Jar.SetCookies(mustURL(t, h.server.URL+"/api/v1"), grant.Result().Cookies())
	for i := 0; i < 2; i++ {
		if r := h.do("GET", "/api/v1/rooms/rom_missing", nil, nil); r.Status != 404 {
			t.Fatal(string(r.Body))
		}
	}
	for i := 0; i < 3; i++ {
		r := h.do("GET", "/api/v1/rooms/rom_missing", nil, nil)
		if r.Status != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" {
			t.Fatalf("limit lost %d", r.Status)
		}
	}
	if r := h.do("GET", "/api/v1/sessions/current", nil, nil); r.Status != 200 {
		t.Fatalf("flood blocked identity recovery: %d %s", r.Status, r.Body)
	}
	check(true) // Recovery status has its own budget even when ordinary reads are exhausted.
	other, _ := h.newSession("同出口的另一位")
	h.useClient(other)
	check(false)
	h.useClient(client)
	for i := 0; i < 3; i++ {
		r := h.do("POST", "/api/v1/sessions/current/admission", map[string]string{"token": "ignored"}, nil)
		if r.Status != 403 {
			t.Fatal("csrf was bypassed")
		}
	}
	check(true)
}

func TestAdmissionStatusDoesNotSpendSharedInvitationBudget(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) { c.Limiter.PublicPerIP = httpapi.Rate{PerMinute: 1, BurstTokens: 1} })
	for i := 0; i < 80; i++ {
		r := h.do("GET", "/api/v1/sessions/current/admission", nil, nil)
		if r.Status != 200 {
			t.Fatalf("venue status polling spent invitation budget at %d: %d", i, r.Status)
		}
	}
	var n int
	_ = h.db.Read().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 0 {
		t.Fatal("anonymous polling created sessions")
	}
}

func TestAdaptiveGateAllowsCancellationButStillBlocksAcceptanceAndHeartbeat(t *testing.T) {
	entry, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "site", Hostnames: []string{"uma.example"}}, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(stubMode{})
	h := newHarness(t, func(c *harnessConfig) {
		c.Admission = entry
		c.Modes = reg
		c.Content = matchHTTPContent{}
		c.AllowMissingOrigin = true
	})
	client, sess := h.newSession("排队训练员")
	grant := httptest.NewRecorder()
	entry.Grant(grant, sess.ID, time.Now().Add(time.Hour))
	client.Jar.SetCookies(mustURL(t, h.server.URL+"/api/v1"), grant.Result().Cookies())
	made := h.doAs(client, "POST", "/api/v1/matchmaking-tickets", matchTicketBody(t, h, client, sess, "classic-duel"), writeHeaders(h, sess, idemKey("entry-ticket")))
	var ticket struct {
		ID string `json:"ticketId"`
	}
	if made.Status != 201 || json.Unmarshal(made.Body, &ticket) != nil {
		t.Fatalf("create: %d %s", made.Status, made.Body)
	}
	for i := 0; i < 6; i++ {
		entry.ObserveLimit(sess.ID)
	}
	for _, path := range []string{"/api/v1/matchmaking-tickets/" + ticket.ID + "/heartbeat", "/api/v1/matchmaking-tickets/" + ticket.ID + "/responses"} {
		denied := h.doAs(client, "POST", path, map[string]any{"accept": true}, writeHeaders(h, sess, idemKey("blocked")))
		if denied.Status != 403 || denied.Problem.Code != "admission-risk-required" {
			t.Fatalf("unsafe action passed: %d %s", denied.Status, denied.Body)
		}
	}
	cancelled := h.doAs(client, "DELETE", "/api/v1/matchmaking-tickets/"+ticket.ID, nil, writeHeaders(h, sess, idemKey("cancel-risk")))
	if cancelled.Status != 204 {
		t.Fatalf("could not cancel after risk: %d %s", cancelled.Status, cancelled.Body)
	}
	if !entry.RiskRequired(sess.ID) {
		t.Fatal("exit cleared risk")
	}
}

func TestAdaptiveGateAllowsOwnDepartureButNotRemovingAnotherMember(t *testing.T) {
	entry, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "site", Hostnames: []string{"uma.example"}}, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(stubMode{})
	h := newHarness(t, func(c *harnessConfig) { c.Admission = entry; c.Modes = reg; c.AllowMissingOrigin = true })
	client, sess := h.newSession("主持")
	grant := httptest.NewRecorder()
	entry.Grant(grant, sess.ID, time.Now().Add(time.Hour))
	client.Jar.SetCookies(mustURL(t, h.server.URL+"/api/v1"), grant.Result().Cookies())
	created, made := h.createRoom(client, sess, "退出房间")
	if made.Status != 201 {
		t.Fatalf("%d %s", made.Status, made.Body)
	}
	id := created["room"].(map[string]any)["roomId"].(string)
	var mid string
	if err = h.db.Read().QueryRow("SELECT id FROM memberships WHERE room_id=? AND session_id=?", id, sess.ID).Scan(&mid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		entry.ObserveLimit(sess.ID)
	}
	denied := h.doAs(client, "DELETE", "/api/v1/rooms/"+id+"/memberships/mem_other", nil, writeHeaders(h, sess, idemKey("kick-risk")))
	if denied.Status != 403 || denied.Problem.Code != "admission-risk-required" {
		t.Fatalf("removal skipped risk: %d %s", denied.Status, denied.Body)
	}
	left := h.doAs(client, "DELETE", "/api/v1/rooms/"+id+"/memberships/"+mid, nil, writeHeaders(h, sess, idemKey("leave-risk")))
	if left.Status != 204 {
		t.Fatalf("could not leave: %d %s", left.Status, left.Body)
	}
	repeat := h.doAs(client, "DELETE", "/api/v1/rooms/"+id+"/memberships/"+mid, nil, writeHeaders(h, sess, idemKey("leave-again-risk")))
	if repeat.Status != 204 {
		t.Fatalf("departure retry: %d %s", repeat.Status, repeat.Body)
	}
	denied = h.doAs(client, "POST", "/api/v1/rounds/rnd_missing/surrenders", map[string]any{"phaseVersion": 1}, writeHeaders(h, sess, idemKey("give-up-risk")))
	if denied.Problem.Code == "admission-risk-required" {
		t.Fatal("surrender blocked by entry verification")
	}
}

func TestAdaptiveGateAllowsDecliningAnOwnedMatchWithoutDisablingCSRF(t *testing.T) {
	entry, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "site", Hostnames: []string{"uma.example"}}, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(stubMode{})
	h := newHarness(t, func(c *harnessConfig) {
		c.Admission = entry
		c.Modes = reg
		c.Content = matchHTTPContent{}
		c.AllowMissingOrigin = true
	})
	clients := make([]*http.Client, 2)
	sessions := make([]testSession, 2)
	ids := make([]string, 2)
	for i := range clients {
		clients[i], sessions[i] = h.newSession("训练员")
		grant := httptest.NewRecorder()
		entry.Grant(grant, sessions[i].ID, time.Now().Add(time.Hour))
		clients[i].Jar.SetCookies(mustURL(t, h.server.URL+"/api/v1"), grant.Result().Cookies())
		made := h.doAs(clients[i], "POST", "/api/v1/matchmaking-tickets", matchTicketBody(t, h, clients[i], sessions[i], "classic-duel"), writeHeaders(h, sessions[i], idemKey("decline-create")))
		var body struct {
			ID string `json:"ticketId"`
		}
		if made.Status != 201 || json.Unmarshal(made.Body, &body) != nil {
			t.Fatalf("%d %s", made.Status, made.Body)
		}
		ids[i] = body.ID
	}
	for i := 0; i < 6; i++ {
		entry.ObserveLimit(sessions[0].ID)
	}
	path := "/api/v1/matchmaking-tickets/" + ids[0] + "/responses"
	noCSRF := h.doAs(clients[0], "POST", path, map[string]any{"accept": false}, map[string]string{"Idempotency-Key": idemKey("decline-no-csrf")})
	if noCSRF.Status != 403 || noCSRF.Problem.Code == "admission-risk-required" {
		t.Fatalf("csrf not checked: %d %s", noCSRF.Status, noCSRF.Body)
	}
	foreign := h.doAs(clients[1], "DELETE", "/api/v1/matchmaking-tickets/"+ids[0], nil, writeHeaders(h, sessions[1], idemKey("foreign-cancel")))
	if foreign.Status != 404 {
		t.Fatalf("foreign cancellation: %d %s", foreign.Status, foreign.Body)
	}
	declined := h.doAs(clients[0], "POST", path, map[string]any{"accept": false}, writeHeaders(h, sessions[0], idemKey("decline-owned")))
	if declined.Status != 201 {
		t.Fatalf("could not decline: %d %s", declined.Status, declined.Body)
	}
	var rooms int
	if err = h.db.Read().QueryRow("SELECT COUNT(*) FROM rooms").Scan(&rooms); err != nil || rooms != 0 {
		t.Fatalf("decline created room: %d %v", rooms, err)
	}
}

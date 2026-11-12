package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
)

func TestSharedIPSessionArrivalIsBoundedWithoutMergingPeople(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) {
		c.Limiter.SessionCreatePerIP = httpapi.Rate{PerMinute: 1, BurstTokens: 8}
	})
	identities := map[string]bool{}
	var firstClient *http.Client
	for i := 0; i < 8; i++ {
		client, sess := h.newSession("同名训练员")
		if identities[sess.ID] {
			t.Fatal("shared IP merged different anonymous players")
		}
		identities[sess.ID] = true
		if firstClient == nil {
			firstClient = client
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	h.useClient(&http.Client{Jar: jar, Timeout: 10 * time.Second})
	// Neither an invented browser signal nor an untrusted forwarded IP can
	// mint another identity after the configured shared-network burst is spent.
	for _, signal := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		denied := h.do(http.MethodPost, "/api/v1/sessions", map[string]string{"displayName": "重复入场"}, map[string]string{
			"Origin": h.server.URL, "X-Browser-Signal": signal, "X-Forwarded-For": "203.0.113.99",
		})
		if denied.Status != http.StatusTooManyRequests || denied.Header.Get("Retry-After") == "" {
			t.Fatalf("creation flood not bounded: %d %s", denied.Status, denied.Body)
		}
	}
	var count int
	if err := h.db.Read().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 8 {
		t.Fatalf("denied creation wrote identity: count=%d err=%v", count, err)
	}
	h.useClient(firstClient)
	read := h.do(http.MethodGet, "/api/v1/sessions/current", nil, nil)
	if read.Status != http.StatusOK {
		t.Fatalf("new-arrival flood blocked existing player: %d %s", read.Status, read.Body)
	}
}

func TestCreateSessionRestoresExistingCookie(t *testing.T) {
	h := newHarness(t)
	client, sess := h.newSession("特别周")
	h.useClient(client)
	before := client.Jar.Cookies(mustURL(t, h.server.URL))[0].Value
	result := h.do("POST", "/api/v1/sessions", map[string]string{"displayName": "另一个昵称"}, map[string]string{"Origin": h.server.URL})
	if result.Status != 200 {
		t.Fatalf("restore: %d %s", result.Status, result.Body)
	}
	var restored testSession
	if err := json.Unmarshal(result.Body, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ID != sess.ID || restored.DisplayName != "特别周" || restored.CSRFToken == "" {
		t.Fatal("restore replaced identity")
	}
	after := client.Jar.Cookies(mustURL(t, h.server.URL))[0].Value
	if before != after {
		t.Fatal("cookie replaced")
	}
	var count int
	if err := h.db.Read().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("sessions=%d err=%v", count, err)
	}
}
func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	u, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func TestEntryGateIsServerEnforcedAndBoundToIdentity(t *testing.T) {
	entry, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "public-site", Hostnames: []string{"uma.example"}, Operations: []string{"createRoom"}}, "private-test-secret", false)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *harnessConfig) { c.Admission = entry; c.Modes, _ = modes.NewRegistry(classic.New()) })
	client, first := h.newSession("同名")
	h.useClient(client)
	body := map[string]any{"name": "一起开跑", "kind": "friend", "modeConfig": map[string]any{"modeId": "classic"}, "maxPlayers": 4, "maxSpectators": 0, "visibility": "invite", "allowSpectators": false}
	headers := map[string]string{"Origin": h.server.URL, "X-CSRF-Token": first.CSRFToken, "Idempotency-Key": "entry-once-room-0001"}
	refused := h.do("POST", "/api/v1/rooms", body, headers)
	if refused.Status != 403 || refused.Problem.Code != "admission-required" {
		t.Fatalf("gate=%d %s", refused.Status, refused.Body)
	}
	var n int
	_ = h.db.Read().QueryRow("SELECT COUNT(*) FROM rooms").Scan(&n)
	if n != 0 {
		t.Fatal("mutated before verification")
	}
	grant := httptest.NewRecorder()
	entry.Grant(grant, first.ID, time.Now().Add(time.Hour))
	u := mustURL(t, h.server.URL+"/api/v1/rooms")
	client.Jar.SetCookies(u, grant.Result().Cookies())
	allowed := h.do("POST", "/api/v1/rooms", body, headers)
	if allowed.Status != http.StatusCreated {
		t.Fatal("grant not accepted")
	}
	// The protocol boundary is tested in admission/service_test; this test issues
	// its own signed grant to exercise real routing, cookies and authentication.
	other, second := h.newSession("同名")
	other.Jar.SetCookies(u, grant.Result().Cookies())
	h.useClient(other)
	headers["X-CSRF-Token"] = second.CSRFToken
	headers["Idempotency-Key"] = "entry-other-room-0001"
	rejected := h.do("POST", "/api/v1/rooms", body, headers)
	if rejected.Problem.Code != "admission-required" {
		t.Fatal("grant crossed sessions")
	}
	config := h.do("GET", "/api/v1/config", nil, nil)
	if strings.Contains(string(config.Body), "private-test-secret") || strings.Contains(string(config.Body), "secretEnv") {
		t.Fatal("secret exposed")
	}
}
func TestAdmissionRequiresCsrfAndRestoresLostResponse(t *testing.T) {
	entry, err := admission.New(deployment.Turnstile{Enabled: true, SiteKey: "public-site", Hostnames: []string{"uma.example"}}, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *harnessConfig) { c.Admission = entry; c.Modes, _ = modes.NewRegistry(classic.New()) })
	client, sess := h.newSession("训练员")
	h.useClient(client)
	denied := h.do("POST", "/api/v1/sessions/current/admission", map[string]string{"token": "unused"}, map[string]string{"Origin": h.server.URL})
	if denied.Status != 403 {
		t.Fatal("missing csrf accepted")
	}
	grant := httptest.NewRecorder()
	entry.Grant(grant, sess.ID, time.Now().Add(time.Hour))
	client.Jar.SetCookies(mustURL(t, h.server.URL+"/api/v1/rooms"), grant.Result().Cookies())
	restored := h.do(http.MethodPost, "/api/v1/sessions/current/admission", map[string]string{"token": "already-consumed"}, map[string]string{"Origin": h.server.URL, "X-CSRF-Token": sess.CSRFToken})
	if restored.Status != 200 {
		t.Fatalf("lost response not restored: %d %s", restored.Status, restored.Body)
	}
}

func (h *harness) useClient(client *http.Client) { h.client = client }

func TestSameNicknameHasStableDifferentRoomNumbers(t *testing.T) {
	registry, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = registry })
	owner, host := h.newSession("主持")
	first, a := h.newSession("特别周")
	second, b := h.newSession("特别周")
	created, res := h.createRoom(owner, host, "同名辨识")
	if res.Status != 201 {
		t.Fatal(string(res.Body))
	}
	snapshot := created["room"].(map[string]any)
	id, code := snapshot["roomId"].(string), created["inviteCode"].(string)
	if a.ID == b.ID {
		t.Fatal("same nickname shared identity")
	}
	for i, p := range []struct {
		client *http.Client
		sess   testSession
	}{{first, a}, {second, b}} {
		result := h.doAs(p.client, "POST", "/api/v1/rooms/"+id+"/memberships", map[string]any{"inviteCode": code, "requestedRole": "player"}, writeHeaders(h, p.sess, idemKey(string(rune('a'+i)))))
		if result.Status != 201 {
			t.Fatalf("join: %d %s", result.Status, result.Body)
		}
	}
	read := func(client *http.Client) map[string]any {
		t.Helper()
		res := h.doAs(client, "GET", "/api/v1/rooms/"+id, nil, nil)
		var body map[string]any
		if res.Status != 200 || json.Unmarshal(res.Body, &body) != nil {
			t.Fatalf("read: %s", res.Body)
		}
		return body
	}
	one, two := read(first)["viewer"].(map[string]any), read(second)["viewer"].(map[string]any)
	if one["displayName"] == two["displayName"] || one["playerNumber"] == two["playerNumber"] {
		t.Fatalf("ambiguous: %v %v", one, two)
	}
	mid := one["membershipId"].(string)
	number := one["playerNumber"]
	left := h.doAs(first, "DELETE", "/api/v1/rooms/"+id+"/memberships/"+mid, nil, writeHeaders(h, a, idemKey("leave")))
	if left.Status != 204 {
		t.Fatalf("leave: %d %s", left.Status, left.Body)
	}
	joined := h.doAs(first, "POST", "/api/v1/rooms/"+id+"/memberships", map[string]any{"inviteCode": code, "requestedRole": "player"}, writeHeaders(h, a, idemKey("rejoin")))
	if joined.Status != 201 {
		t.Fatalf("rejoin: %d %s", joined.Status, joined.Body)
	}
	again := read(first)["viewer"].(map[string]any)
	if again["membershipId"] != mid || again["playerNumber"] != number || again["displayName"] != one["displayName"] {
		t.Fatal("rejoin changed seat")
	}
	// Public numbers are unique in SQLite too, not merely different UI positions.
	_, err := h.db.Write().Exec("UPDATE memberships SET entry_number=? WHERE id=?", two["playerNumber"], mid)
	if err == nil {
		t.Fatal("database accepted duplicate room number")
	}
}

func TestMembershipPatchDistinguishesOmittedAndNullTeam(t *testing.T) {
	registry, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = registry })
	client, sess := h.newSession("同名选手")
	data, res := h.createRoom(client, sess, "队伍修改")
	if res.Status != 201 {
		t.Fatal(string(res.Body))
	}
	rm := data["room"].(map[string]any)
	id := rm["roomId"].(string)
	mid := rm["viewer"].(map[string]any)["membershipId"].(string)
	team := h.doAs(client, "POST", "/api/v1/rooms/"+id+"/teams", map[string]any{"label": "胡萝卜队", "colorToken": "green"}, writeHeaders(h, sess, idemKey("team")))
	if team.Status != 201 {
		t.Fatalf("team: %s", team.Body)
	}
	var created map[string]any
	if err := json.Unmarshal(team.Body, &created); err != nil {
		t.Fatal(err)
	}
	teamID := created["teamId"]
	for i, step := range []struct {
		body map[string]any
		want any
	}{{map[string]any{"teamId": teamID}, teamID}, {map[string]any{"ready": true}, teamID}, {map[string]any{"teamId": nil}, nil}} {
		read := h.doAs(client, "GET", "/api/v1/rooms/"+id, nil, nil)
		var snapshot map[string]any
		if err := json.Unmarshal(read.Body, &snapshot); err != nil {
			t.Fatal(err)
		}
		step.body["expectedRoomVersion"] = snapshot["roomVersion"]
		changed := h.doAs(client, "PATCH", "/api/v1/rooms/"+id+"/memberships/"+mid, step.body, writeHeaders(h, sess, idemKey(string(rune('x'+i)))))
		var member map[string]any
		if changed.Status != 200 || json.Unmarshal(changed.Body, &member) != nil {
			t.Fatalf("patch %d: %d %s", i, changed.Status, changed.Body)
		}
		if member["teamId"] != step.want {
			t.Fatalf("patch %d: team=%v want=%v", i, member["teamId"], step.want)
		}
	}
}

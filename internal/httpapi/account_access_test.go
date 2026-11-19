package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

func accountPolicy(form string) deployment.AccountAccess {
	return deployment.AccountAccess{Enabled: true, PublicURL: "https://game.example", RequireLogin: map[string]bool{form: true}, Providers: []deployment.AccountProvider{{ID: "github", Enabled: true, Protocol: "oauth2", ClientID: "test-client", ClientSecretEnv: "TEST_ACCOUNT_SECRET"}}}
}
func TestAccountRequiredAcrossResources(t *testing.T) {
	t.Setenv("TEST_ACCOUNT_SECRET", "test-secret")
	for _, form := range []string{"friend", "event", "matchmaking"} {
		t.Run(form, func(t *testing.T) {
			h := newHarness(t, func(c *harnessConfig) { c.Policy.Accounts = accountPolicy(form) })
			client, sess := h.newSession("同名训练员")
			_, err := h.db.Write().ExecContext(context.Background(), `INSERT INTO rooms(id,owner_session_id,name,status,config_json,capacity,created_at,updated_at) VALUES('rom_account',?,'test','lobby',?,8,0,0)`, sess.ID, `{"kind":"`+form+`","modeConfig":{"modeId":"classic"}}`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.db.Write().Exec(`INSERT INTO memberships(id,room_id,session_id,role,joined_at) VALUES('mem_account','rom_account',?,'player',0)`, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Private reads, joins, management, recovery and both push handshakes must gate.
			for _, path := range []string{"/api/v1/rooms/rom_account", "/api/v1/rooms/rom_account/events", "/api/v1/rooms/rom_account/stream", "/api/v1/rooms/rom_account/socket"} {
				res := h.doAs(client, http.MethodGet, path, nil, readHeaders(h))
				if res.Status != 401 || res.Problem.Code != "account-required" {
					t.Fatalf("anonymous bypass %s: %d %s", path, res.Status, res.Body)
				}
			}
			for _, path := range []string{"/api/v1/rooms/rom_account/memberships", "/api/v1/rooms/rom_account/rounds", "/api/v1/memberships/mem_account/recovery-grants"} {
				res := h.doAs(client, http.MethodPost, path, map[string]any{}, writeHeaders(h, sess, idemKey("account")))
				if res.Status != 401 || res.Problem.Code != "account-required" {
					t.Fatalf("anonymous write bypass %s: %d %s", path, res.Status, res.Body)
				}
			}
			roomBody := map[string]any{"name": "test", "kind": form, "modeConfig": map[string]string{"modeId": "classic"}, "maxPlayers": 2, "maxSpectators": 0, "visibility": "invite", "allowSpectators": false}
			res := h.doAs(client, http.MethodPost, "/api/v1/rooms", roomBody, writeHeaders(h, sess, idemKey("new")))
			if res.Status != 401 || res.Problem.Code != "account-required" {
				t.Fatalf("creation gate: %d %s", res.Status, res.Body)
			}
			if form == "matchmaking" {
				for _, path := range []string{"/api/v1/matchmaking-proofs", "/api/v1/matchmaking-tickets", "/api/v1/matchmaking-tickets/tkt_missing/heartbeat", "/api/v1/matchmaking-tickets/tkt_missing/responses"} {
					res = h.doAs(client, http.MethodPost, path, map[string]any{}, writeHeaders(h, sess, idemKey("queue")))
					if res.Status != 401 || res.Problem.Code != "account-required" {
						t.Fatalf("queue gate: %s %d %s", path, res.Status, res.Body)
					}
				}
			}
			// A different play form remains open to the original anonymous session.
			other := "friend"
			if form == "friend" {
				other = "event"
			}
			_, err = h.db.Write().Exec(`INSERT INTO rooms(id,owner_session_id,name,status,config_json,capacity,created_at,updated_at) VALUES('rom_other',?,'test','lobby',?,8,0,0)`, sess.ID, `{"kind":"`+other+`","modeConfig":{"modeId":"classic"}}`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.db.Write().Exec(`INSERT INTO memberships(id,room_id,session_id,role,joined_at) VALUES('mem_other','rom_other',?,'player',0)`, sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			res = h.doAs(client, http.MethodGet, "/api/v1/rooms/rom_other", nil, readHeaders(h))
			if res.Status != 200 {
				t.Fatalf("independent mode blocked: %d %s", res.Status, res.Body)
			}
			token := "test-account-opaque-secret"
			hash := sha256.Sum256([]byte(token))
			now := time.Now()
			_, err = h.db.Write().Exec(`INSERT INTO accounts(id,created_at) VALUES('7c4b03e5-e141-42ac-b657-27f049f32cab',?)`, now.UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.db.Write().Exec(`INSERT INTO account_sessions(id,account_id,token_hash,created_at,idle_expires_at,absolute_expires_at,renewed_at) VALUES('account-session','7c4b03e5-e141-42ac-b657-27f049f32cab',?,?,?,?,?)`, hex.EncodeToString(hash[:]), now.UnixMilli(), now.Add(time.Hour).UnixMilli(), now.Add(2*time.Hour).UnixMilli(), now.UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			headers := readHeaders(h)
			headers["Cookie"] = "__Host-uma_account=" + token
			res = h.doAs(client, http.MethodGet, "/api/v1/rooms/rom_account", nil, headers)
			if res.Status != 200 {
				t.Fatalf("authenticated player refused: %d %s", res.Status, res.Body)
			}
			res = h.doAs(client, http.MethodGet, "/api/v1/account", nil, headers)
			if strings.Contains(string(res.Body), token) || strings.Contains(string(res.Body), "test-secret") {
				t.Fatal("credential exposed")
			}
			var st struct {
				CSRF string `json:"csrfToken"`
			}
			if json.Unmarshal(res.Body, &st) != nil || st.CSRF == "" {
				t.Fatal("missing account CSRF")
			}
			headers["X-Account-CSRF"] = "wrong"
			res = h.doAs(client, http.MethodDelete, "/api/v1/account/session", nil, headers)
			if res.Status != 403 {
				t.Fatal("account logout CSRF bypass")
			}
			headers["X-Account-CSRF"] = st.CSRF
			headers["Origin"] = "https://evil.example"
			res = h.doAs(client, http.MethodDelete, "/api/v1/account/session", nil, headers)
			if res.Status != 403 {
				t.Fatal("account logout Origin bypass")
			}
			headers["Origin"] = h.server.URL
			res = h.doAs(client, http.MethodDelete, "/api/v1/account/session", nil, headers)
			if res.Status != 204 {
				t.Fatalf("logout failed %d %s", res.Status, res.Body)
			}
			res = h.doAs(client, http.MethodGet, "/api/v1/rooms/rom_account", nil, headers)
			if res.Status != 401 {
				t.Fatal("revoked account or game session accepted")
			}
		})
	}
}
func TestAccountDisabledPreservesAnonymousPlay(t *testing.T) {
	reg, err := modes.NewRegistry(stubMode{})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	client, sess := h.newSession("训练员")
	_, res := h.createRoom(client, sess, "friend")
	if res.Status != 201 {
		t.Fatal(string(res.Body))
	}
	res = h.doAs(client, http.MethodGet, "/api/v1/account", nil, readHeaders(h))
	if res.Status != 200 || string(res.Body) != `{"authenticated":false}` {
		t.Fatal(string(res.Body))
	}
	res = h.doAs(client, http.MethodPost, "/api/v1/account/logins/github", map[string]string{"returnTo": "/friends"}, readHeaders(h))
	if res.Status != 404 {
		t.Fatal("disabled provider accepted login")
	}
}

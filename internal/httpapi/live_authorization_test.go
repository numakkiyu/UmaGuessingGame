package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/realtime"
	"github.com/bhcn/umaguessinggame/internal/session"
	"github.com/coder/websocket"
)

func TestLiveSocketStopsWhenIdentityChanges(t *testing.T) {
	for _, change := range []string{"seat-transfer", "session-expired", "session-revoked", "role-changed", "membership-revoked", "recovery", "logout"} {
		t.Run(change, func(t *testing.T) {
			registry, _ := modes.NewRegistry(classic.New())
			h := newHarness(t, func(c *harnessConfig) { c.Modes = registry })
			cfg := realtime.Config{RevalidateInterval: 20 * time.Millisecond}
			if change == "recovery" || change == "logout" {
				cfg.RevalidateInterval = time.Minute
			}
			hub, err := realtime.NewHub(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = hub.Close() })
			dispatcher, err := realtime.NewDispatcher(h.db, hub, cfg)
			if err != nil {
				t.Fatal(err)
			}
			dispatcher.Start()
			t.Cleanup(dispatcher.Stop)
			srv := httpapi.New(httpapi.Deps{Rooms: h.rooms, Sessions: session.New(h.db, session.Config{}), Realtime: hub, Config: httpapi.Config{RealtimeConfig: cfg}})
			ts := httptest.NewServer(srv.Handler())
			t.Cleanup(ts.Close)
			h.server = ts
			client, sess := h.newSession("连接测试")
			created, res := h.createRoom(client, sess, "权限变更")
			if res.Status != 201 {
				t.Fatal(string(res.Body))
			}
			rid := created["room"].(map[string]any)["roomId"].(string)
			u, _ := url.Parse(ts.URL)
			headers := http.Header{"Origin": {ts.URL}}
			for _, c := range client.Jar.Cookies(u) {
				headers.Add("Cookie", c.String())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/rooms/"+rid+"/socket", &websocket.DialOptions{HTTPHeader: headers})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if _, _, err = conn.Read(ctx); err != nil {
				t.Fatal(err)
			}
			var sid string
			if err := h.db.Read().QueryRow(`SELECT owner_session_id FROM rooms WHERE id=?`, rid).Scan(&sid); err != nil {
				t.Fatal(err)
			}
			var query string
			switch change {
			case "logout":
				res := h.doAs(client, "DELETE", "/api/v1/sessions/current", nil, writeHeaders(h, sess, idemKey("logout")))
				if res.Status != 204 {
					t.Fatalf("logout: %d %s", res.Status, res.Body)
				}
			case "recovery":
				var mid string
				if err := h.db.Read().QueryRow(`SELECT id FROM memberships WHERE room_id=?`, rid).Scan(&mid); err != nil {
					t.Fatal(err)
				}
				grant := h.doAs(client, "POST", "/api/v1/memberships/"+mid+"/recovery-grants", map[string]any{"reason": "换设备"}, writeHeaders(h, sess, idemKey("recover")))
				if grant.Status != 201 {
					t.Fatal(string(grant.Body))
				}
				var code map[string]any
				_ = json.Unmarshal(grant.Body, &code)
				jar, _ := cookiejar.New(nil)
				fresh := &http.Client{Jar: jar}
				res := h.doAs(fresh, "POST", "/api/v1/session-recoveries", map[string]any{"grantCode": code["grantCode"]}, readHeaders(h))
				if res.Status != 201 {
					t.Fatalf("recovery: %d %s", res.Status, res.Body)
				}
				res = h.doAs(fresh, "GET", "/api/v1/rooms/"+rid, nil, readHeaders(h))
				if res.Status != 200 {
					t.Fatalf("new device lost access: %d", res.Status)
				}
			case "seat-transfer":
				fresh, err := session.New(h.db, session.Config{}).Create(ctx, "新设备")
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.db.Write().Exec(`UPDATE memberships SET session_id=? WHERE room_id=?`, fresh.Session.ID, rid)
				if err != nil {
					t.Fatal(err)
				}
			case "session-expired":
				query = `UPDATE sessions SET expires_at=1 WHERE id=?`
			case "session-revoked":
				query = `UPDATE sessions SET revoked_at=1 WHERE id=?`
			case "role-changed":
				query = `UPDATE memberships SET role='spectator' WHERE session_id=?`
			case "membership-revoked":
				query = `UPDATE memberships SET revoked_at=1 WHERE session_id=?`
			}
			if query != "" {
				if _, err := h.db.Write().Exec(query, sid); err != nil {
					t.Fatal(err)
				}
			}
			readCtx, stop := context.WithTimeout(context.Background(), 750*time.Millisecond)
			defer stop()
			for {
				_, raw, err := conn.Read(readCtx)
				if err != nil {
					t.Fatalf("identity change did not deliver revocation: %v", err)
				}
				var message struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(raw, &message); err != nil {
					t.Fatal(err)
				}
				if message.Type == "session.revoked" {
					break
				}
			}
			if _, _, err := conn.Read(readCtx); err == nil {
				t.Fatal("revoked connection remained open")
			}
		})
	}
}

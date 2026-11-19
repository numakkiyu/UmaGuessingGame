package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestDisplayOnlyReadsNeverUsePlayerIdentity(t *testing.T) {
	h := newHarness(t)
	player, owner := h.newSession("主持")
	roomID := h.seedRoom(t, owner.ID, "display-bound")
	roundID := h.seedRound(t, roomID, "SECRET-ANSWER")
	created := h.doAs(player, "POST", "/api/v1/events", map[string]any{"roomId": roomID, "name": "观赛活动", "tiePolicy": "shared-rank"}, writeHeaders(h, owner, idemKey("disp-event")))
	if created.Status != 201 {
		t.Fatal(string(created.Body))
	}
	var event map[string]any
	if err := json.Unmarshal(created.Body, &event); err != nil {
		t.Fatal(err)
	}
	eventID := event["eventId"].(string)
	paths := []string{"/rooms/" + roomID, "/rounds/" + roundID, "/events/" + eventID, "/events/" + eventID + "/standings"}
	for _, path := range paths {
		res := h.doAs(player, "GET", "/api/v1"+path+"?view=display", nil, nil)
		if res.Status != 401 {
			t.Fatalf("player-only %s got %d", path, res.Status)
		}
	}
	display, ds := h.newDisplaySession(t)
	h.seedMember(t, roomID, ds.ID, "display")
	u, _ := url.Parse(h.server.URL)
	player.Jar.SetCookies(u, display.Jar.Cookies(u))
	for _, path := range paths {
		res := h.doAs(player, "GET", "/api/v1"+path+"?view=display", nil, nil)
		if res.Status != 200 {
			t.Fatalf("dual-cookie %s: %d %s", path, res.Status, res.Body)
		}
		if strings.Contains(string(res.Body), "SECRET-ANSWER") {
			t.Fatal("private answer leaked")
		}
	}
	res := h.doAs(player, "GET", "/api/v1/rounds/"+roundID+"?view=display", nil, nil)
	var projection map[string]any
	_ = json.Unmarshal(res.Body, &projection)
	if projection["viewerRole"] != "display" {
		t.Fatalf("wrong projection: %s", res.Body)
	}
	if p, ok := projection["privateState"]; ok && p != nil {
		t.Fatalf("privateState in display: %s", res.Body)
	}
	res = h.doAs(player, "GET", "/api/v1/rounds/"+roundID, nil, nil)
	_ = json.Unmarshal(res.Body, &projection)
	if res.Status != 200 || projection["viewerRole"] == "display" {
		t.Fatalf("normal player read changed: %s", res.Body)
	}
	other := h.seedRoom(t, owner.ID, "other-display-room")
	res = h.doAs(display, "GET", "/api/v1/rooms/"+other+"?view=display", nil, nil)
	if res.Status != 403 && res.Status != 404 {
		t.Fatalf("cross-room display read: %d", res.Status)
	}
}

func TestDisplayPairingBindsEventBeforeConsuming(t *testing.T) {
	h := newHarness(t)
	host, owner := h.newSession("主持")
	roomID := h.seedRoom(t, owner.ID, "pair-bound")
	created := h.doAs(host, "POST", "/api/v1/events", map[string]any{"roomId": roomID, "name": "活动", "tiePolicy": "shared-rank"}, writeHeaders(h, owner, idemKey("pair-event")))
	if created.Status != 201 {
		t.Fatal(string(created.Body))
	}
	var event map[string]any
	_ = json.Unmarshal(created.Body, &event)
	id := event["eventId"].(string)
	grant := h.doAs(host, "POST", "/api/v1/events/"+id+"/display-grants", map[string]any{}, writeHeaders(h, owner, idemKey("pair-grant")))
	if grant.Status != 201 {
		t.Fatal(string(grant.Body))
	}
	var g map[string]any
	_ = json.Unmarshal(grant.Body, &g)
	code := g["grantCode"]
	wrong := h.do("POST", "/api/v1/display-sessions", map[string]any{"grantCode": code, "eventId": "evs_wrong"}, readHeaders(h))
	if wrong.Status != 409 || wrong.Problem.Code != "grant-event-mismatch" {
		t.Fatalf("wrong event: %d %s", wrong.Status, wrong.Body)
	}
	right := h.do("POST", "/api/v1/display-sessions", map[string]any{"grantCode": code, "eventId": id}, readHeaders(h))
	if right.Status != 201 {
		t.Fatalf("code was consumed on mismatch: %d %s", right.Status, right.Body)
	}
	read := h.do("GET", "/api/v1/events/"+id+"?view=display", nil, nil)
	if read.Status != 200 {
		t.Fatalf("paired cookie cannot restore: %d %s", read.Status, read.Body)
	}
	again := h.do("POST", "/api/v1/display-sessions", map[string]any{"grantCode": code, "eventId": id}, readHeaders(h))
	if again.Status != 409 || again.Problem.Code != "grant-already-used" {
		t.Fatalf("reused: %d", again.Status)
	}
	// A display identity never authorises an interactive write.
	write := h.do("POST", "/api/v1/rooms", map[string]any{}, readHeaders(h))
	if write.Status != http.StatusUnauthorized && write.Status != http.StatusForbidden {
		t.Fatalf("display write: %d", write.Status)
	}
}

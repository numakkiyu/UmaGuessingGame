package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
)

func TestRecoveryHTTPKeepsOriginalMembershipAndRevokesOldRoomAccess(t *testing.T) {
	reg, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	owner, host := h.newSession("房主")
	guest, player := h.newSession("换设备的玩家")
	created, response := h.createRoom(owner, host, "好友对局")
	if response.Status != 201 {
		t.Fatal(string(response.Body))
	}
	roomID := created["room"].(map[string]any)["roomId"].(string)
	joined := h.doAs(guest, "POST", "/api/v1/rooms/"+roomID+"/memberships", map[string]any{"inviteCode": created["inviteCode"], "requestedRole": "player"}, writeHeaders(h, player, idemKey("join-recover")))
	if joined.Status != 201 {
		t.Fatal(string(joined.Body))
	}
	var member map[string]any
	_ = json.Unmarshal(joined.Body, &member)
	id := member["membershipId"].(string)
	grant := h.doAs(owner, "POST", "/api/v1/memberships/"+id+"/recovery-grants", map[string]any{"reason": "换设备继续游玩"}, writeHeaders(h, host, idemKey("recover-code")))
	if grant.Status != 201 {
		t.Fatal(string(grant.Body))
	}
	var code map[string]any
	_ = json.Unmarshal(grant.Body, &code)
	jar, _ := cookiejar.New(nil)
	fresh := &http.Client{Jar: jar}
	restored := h.doAs(fresh, "POST", "/api/v1/session-recoveries", map[string]any{"grantCode": code["grantCode"]}, readHeaders(h))
	if restored.Status != 201 {
		t.Fatalf("redeem: %d %s", restored.Status, restored.Body)
	}
	res := h.doAs(fresh, "GET", "/api/v1/rooms/"+roomID, nil, readHeaders(h))
	if res.Status != 200 {
		t.Fatalf("recovered cannot read room: %d %s", res.Status, res.Body)
	}
	var snapshot struct {
		Viewer struct {
			MembershipID string `json:"membershipId"`
		} `json:"viewer"`
	}
	_ = json.Unmarshal(res.Body, &snapshot)
	if snapshot.Viewer.MembershipID != id {
		t.Fatalf("membership replaced: %+v", snapshot)
	}
	old := h.doAs(guest, "GET", "/api/v1/rooms/"+roomID, nil, readHeaders(h))
	if old.Status != 404 {
		t.Fatalf("old browser retained room: %d", old.Status)
	}
	replay := h.doAs(fresh, "POST", "/api/v1/session-recoveries", map[string]any{"grantCode": code["grantCode"]}, readHeaders(h))
	if replay.Status != 409 {
		t.Fatalf("code reuse: %d", replay.Status)
	}
	// Invalid fallback cursors are rejected before switching to event-stream framing.
	invalid := h.doAs(fresh, "GET", "/api/v1/rooms/"+roomID+"/stream?cursor=c1%0Aevent%3Aforged", nil, readHeaders(h))
	if invalid.Status != 400 {
		t.Fatalf("unsafe stream cursor: %d %s", invalid.Status, invalid.Body)
	}
}

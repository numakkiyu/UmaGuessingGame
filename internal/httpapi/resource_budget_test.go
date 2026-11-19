package httpapi_test

import (
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
)

func TestOutsiderCannotSpendAnotherRoomsCommandBudget(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) {
		c.Limiter.RoomPerMinute = httpapi.Rate{PerMinute: 1, BurstTokens: 1}
	})
	owner, host := h.newSession("房主")
	outsider, attacker := h.newSession("未入场玩家")
	rid := h.seedRoom(t, host.ID, "保持可用")
	path := "/api/v1/rooms/" + rid
	bad := h.doAs(outsider, "PATCH", path, map[string]any{"name": "篡改", "expectedRoomVersion": 1}, writeHeaders(h, attacker, idemKey("foreign")))
	if bad.Status != 404 {
		t.Fatalf("foreign command: %d %s", bad.Status, bad.Body)
	}
	good := h.doAs(owner, "PATCH", path, map[string]any{"name": "正常修改", "expectedRoomVersion": 1}, writeHeaders(h, host, idemKey("own")))
	if good.Status != 200 {
		t.Fatalf("outsider consumed room allowance: %d %s", good.Status, good.Body)
	}
	again := h.doAs(owner, "PATCH", path, map[string]any{"name": "超过限额", "expectedRoomVersion": 2}, writeHeaders(h, host, idemKey("own-again")))
	if again.Status != 429 {
		t.Fatalf("authorized room quota not enforced: %d", again.Status)
	}
}

package httpapi_test

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"net/http"
	"testing"
)

// Simulate the crash boundary after a business commit but before the separate
// HTTP receipt. Resource identity must survive even if that receipt is absent.
func TestCreationRecoversWithoutHTTPReceipt(t *testing.T) {
	reg, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	browser, owner := h.newSession("主持")
	body := map[string]any{"name": "娱乐杯活动房", "kind": "event", "modeConfig": map[string]any{"modeId": "classic"}, "maxPlayers": 3, "maxSpectators": 2, "visibility": "invite", "allowSpectators": true}
	roomKey := idemKey("recover-room")
	first := h.doAs(browser, "POST", "/api/v1/rooms", body, writeHeaders(h, owner, roomKey))
	if first.Status != http.StatusCreated {
		t.Fatal(string(first.Body))
	}
	var original map[string]any
	if err := json.Unmarshal(first.Body, &original); err != nil {
		t.Fatal(err)
	}
	originalRoom := original["room"].(map[string]any)["roomId"].(string)
	if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	retry := h.doAs(browser, "POST", "/api/v1/rooms", body, writeHeaders(h, owner, roomKey))
	var repeated map[string]any
	_ = json.Unmarshal(retry.Body, &repeated)
	if retry.Status != http.StatusCreated || repeated["room"].(map[string]any)["roomId"] != originalRoom {
		t.Fatalf("room retry: %d %s", retry.Status, retry.Body)
	}
	eventBody := map[string]any{"roomId": originalRoom, "name": "娱乐杯", "tiePolicy": "shared-rank"}
	eventKey := idemKey("recover-event")
	created := h.doAs(browser, "POST", "/api/v1/events", eventBody, writeHeaders(h, owner, eventKey))
	if created.Status != http.StatusCreated {
		t.Fatal(string(created.Body))
	}
	var event map[string]any
	_ = json.Unmarshal(created.Body, &event)
	if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	recovered := h.doAs(browser, "POST", "/api/v1/events", eventBody, writeHeaders(h, owner, eventKey))
	var again map[string]any
	_ = json.Unmarshal(recovered.Body, &again)
	if recovered.Status != http.StatusCreated || again["eventId"] != event["eventId"] {
		t.Fatalf("event retry: %d %s", recovered.Status, recovered.Body)
	}
	for table := range map[string]bool{"rooms": true, "event_sessions": true} {
		var count int
		if err := h.db.Read().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s duplicate: %d", table, count)
		}
	}
	// The persistent identity must bind the original body even after receipts vanish.
	if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	body["maxPlayers"] = 4
	different := h.doAs(browser, "POST", "/api/v1/rooms", body, writeHeaders(h, owner, roomKey))
	if different.Status != http.StatusConflict {
		t.Fatalf("changed body: %d %s", different.Status, different.Body)
	}
	eventBody["name"] = "另一场活动"
	changed := h.doAs(browser, "POST", "/api/v1/events", eventBody, writeHeaders(h, owner, eventKey))
	if changed.Status != http.StatusConflict {
		t.Fatalf("changed event body: %d %s", changed.Status, changed.Body)
	}
}

func TestPublishedEventTiePolicySurvivesReadAndReceiptRecovery(t *testing.T) {
	for _, policy := range []string{"shared-rank", "announced-tiebreak"} {
		t.Run(policy, func(t *testing.T) {
			reg, _ := modes.NewRegistry(classic.New())
			h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
			browser, owner := h.newSession("主持")
			made := h.doAs(browser, "POST", "/api/v1/rooms", map[string]any{"name": "活动房", "kind": "event", "modeConfig": map[string]any{"modeId": "classic"}, "maxPlayers": 3, "maxSpectators": 0, "visibility": "invite", "allowSpectators": false}, writeHeaders(h, owner, idemKey("policy-room")))
			if made.Status != http.StatusCreated {
				t.Fatal(string(made.Body))
			}
			var room map[string]any
			_ = json.Unmarshal(made.Body, &room)
			roomID := room["room"].(map[string]any)["roomId"].(string)
			body := map[string]any{"roomId": roomID, "name": "同分安排", "tiePolicy": policy}
			key := idemKey("policy-event")
			created := h.doAs(browser, "POST", "/api/v1/events", body, writeHeaders(h, owner, key))
			if created.Status != http.StatusCreated {
				t.Fatal(string(created.Body))
			}
			var first map[string]any
			_ = json.Unmarshal(created.Body, &first)
			id := first["eventId"].(string)
			check := func(label string, data []byte) {
				var event map[string]any
				if err := json.Unmarshal(data, &event); err != nil {
					t.Fatal(err)
				}
				if event["tiePolicy"] != policy || event["eventId"] != id {
					t.Fatalf("%s: %s", label, data)
				}
			}
			check("created", created.Body)
			read := h.doAs(browser, "GET", "/api/v1/events/"+id, nil, nil)
			if read.Status != http.StatusOK {
				t.Fatal(string(read.Body))
			}
			check("read", read.Body)
			if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=?`, owner.ID); err != nil {
				t.Fatal(err)
			}
			replay := h.doAs(browser, "POST", "/api/v1/events", body, writeHeaders(h, owner, key))
			if replay.Status != http.StatusCreated {
				t.Fatal(string(replay.Body))
			}
			check("recovered", replay.Body)
		})
	}
}

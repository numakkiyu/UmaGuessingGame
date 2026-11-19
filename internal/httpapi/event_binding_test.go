package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
)

func TestHTTPEventBindingCannotCrossOwnedRooms(t *testing.T) {
	reg, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	browser, owner := h.newSession("主持")
	rules := map[string]any{"modeId": "classic", "maxWrongGuesses": 8, "winPolicy": "window-attempts", "durationSeconds": 0, "shadowEnabled": false}
	type programme struct {
		room, event, segment string
		version              float64
	}
	makeProgramme := func(name string) programme {
		id := h.seedRoom(t, owner.ID, name)
		created := h.doAs(browser, "POST", "/api/v1/events", map[string]any{"roomId": id, "name": "测试活动", "tiePolicy": "shared-rank"}, writeHeaders(h, owner, idemKey("event-"+name)))
		if created.Status != http.StatusCreated {
			t.Fatal(string(created.Body))
		}
		var event map[string]any
		_ = json.Unmarshal(created.Body, &event)
		eventID := event["eventId"].(string)
		made := h.doAs(browser, "POST", "/api/v1/events/"+eventID+"/segments", map[string]any{"modeConfig": rules, "label": "经典题", "scoreBudget": 60, "roundCount": 2, "competitive": true}, writeHeaders(h, owner, idemKey("segment-"+name)))
		if made.Status != http.StatusCreated {
			t.Fatal(string(made.Body))
		}
		var seg map[string]any
		_ = json.Unmarshal(made.Body, &seg)
		current := h.doAs(browser, "GET", "/api/v1/events/"+eventID, nil, readHeaders(h))
		_ = json.Unmarshal(current.Body, &event)
		return programme{id, eventID, seg["segmentId"].(string), event["eventVersion"].(float64)}
	}
	one, two := makeProgramme("A"), makeProgramme("B")
	bound := h.doAs(browser, "POST", "/api/v1/rooms/"+two.room+"/rounds", map[string]any{"modeConfig": rules, "segmentId": one.segment}, writeHeaders(h, owner, idemKey("cross")))
	if bound.Status != http.StatusNotFound || bound.Problem.Code != "segment-not-found" {
		t.Fatalf("cross-room bind: %d %s", bound.Status, bound.Body)
	}
	changed := map[string]any{"modeId": "classic", "maxWrongGuesses": 4, "winPolicy": "window-attempts", "durationSeconds": 0, "shadowEnabled": false}
	wrongRules := h.doAs(browser, "POST", "/api/v1/rooms/"+one.room+"/rounds", map[string]any{"modeConfig": changed, "segmentId": one.segment}, writeHeaders(h, owner, idemKey("rules")))
	if wrongRules.Status != http.StatusConflict || wrongRules.Problem.Code != "segment-rules-mismatch" {
		t.Fatalf("rule replacement: %d %s", wrongRules.Status, wrongRules.Body)
	}
	correction := h.doAs(browser, "POST", "/api/v1/events/"+one.event+"/score-corrections", map[string]any{"segmentId": two.segment, "subjectId": "unrelated", "newScoreUnits": 1, "reason": "核对另一项成绩", "expectedEventVersion": one.version}, writeHeaders(h, owner, idemKey("cross-correction")))
	if correction.Status != http.StatusNotFound || correction.Problem.Code != "segment-not-found" {
		t.Fatalf("cross-event correction: %d %s", correction.Status, correction.Body)
	}
	var rounds, corrections int
	if err := h.db.Read().QueryRow(`SELECT COUNT(*) FROM rounds`).Scan(&rounds); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Read().QueryRow(`SELECT COUNT(*) FROM score_corrections`).Scan(&corrections); err != nil {
		t.Fatal(err)
	}
	if rounds != 0 || corrections != 0 {
		t.Fatalf("rejected writes left effects: rounds=%d corrections=%d", rounds, corrections)
	}
}

package e2e

import (
	"net/http"
	"testing"
)

// TestSettledSegmentAppearsInStandings proves the event standings actually receive
// a settled round's scores.
//
// Before this, nothing wrote segment_results: the round settled into round_results,
// the segment flipped to completed, and GET /events/{id}/standings returned an empty
// list. A completed programme item with no standings is the inconsistency asserted
// against here, and it needs the real content pack and a real mode to be meaningful.
func TestSettledSegmentAppearsInStandings(t *testing.T) {
	h := newHarness(t)
	h.createSession("standings-host")

	roomID := h.createRoom("classic")

	status, event := h.do(http.MethodPost, "/api/v1/events", map[string]any{
		"roomId": roomID, "name": "standings event", "tiePolicy": "shared-rank",
	}, "standings-event-0001")
	if status != http.StatusCreated {
		t.Fatalf("create event = %d; body=%v", status, event)
	}
	eventID, _ := event["eventId"].(string)
	if eventID == "" {
		t.Fatal("create event returned no eventId")
	}

	status, segment := h.do(http.MethodPost, "/api/v1/events/"+eventID+"/segments", map[string]any{
		"label":       "first item",
		"modeConfig":  map[string]any{"modeId": "classic", "config": map[string]any{}},
		"scoreBudget": 100,
		"roundCount":  1,
		"competitive": true,
	}, "standings-segment-01")
	if status != http.StatusCreated {
		t.Fatalf("create segment = %d; body=%v", status, segment)
	}
	segmentID, _ := segment["segmentId"].(string)
	if segmentID == "" {
		t.Fatal("create segment returned no segmentId")
	}

	// Bind the round to the segment. Omitting segmentId is legal but leaves the
	// segment unbound, so this is what makes the round part of the programme.
	status, round := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{
		"modeConfig": map[string]any{"modeId": "classic", "config": map[string]any{}},
		"segmentId":  segmentID,
	}, "standings-round-0001")
	if status != http.StatusCreated {
		t.Fatalf("create round = %d; body=%v", status, round)
	}
	roundID, _ := round["roundId"].(string)
	if roundID == "" {
		t.Fatal("create round returned no roundId")
	}

	h.transition(roundID, "briefing", 1)
	h.transition(roundID, "collecting", 2)
	h.transition(roundID, "revealed", 3)
	h.transition(roundID, "settled", 4)

	status, standings := h.do(http.MethodGet, "/api/v1/events/"+eventID+"/standings", nil, "")
	if status != http.StatusOK {
		t.Fatalf("standings = %d; body=%v", status, standings)
	}
	items, _ := standings["items"].([]any)
	if len(items) == 0 {
		t.Fatal("a settled segment must produce at least one standings row")
	}

	// The programme label must reach the wire. Without it a console can only show the
	// id's tail, which reads like a database key rather than a schedule item.
	var sawName bool
	statusEvt, evt := h.do(http.MethodGet, "/api/v1/events/"+eventID, nil, "")
	if statusEvt != http.StatusOK {
		t.Fatalf("read event = %d; body=%v", statusEvt, evt)
	}
	details, _ := evt["segmentDetails"].([]any)
	for _, d := range details {
		row, _ := d.(map[string]any)
		if id, _ := row["segmentId"].(string); id == segmentID {
			if name, _ := row["name"].(string); name != "first item" {
				t.Errorf("the segment must report its label, got %q", name)
			} else {
				sawName = true
			}
		}
	}
	if !sawName {
		t.Error("the segment label is missing from segmentDetails")
	}

	found := false
	for _, it := range items {
		row, _ := it.(map[string]any)
		segments, _ := row["segments"].([]any)
		for _, s := range segments {
			seg, _ := s.(map[string]any)
			if id, _ := seg["segmentId"].(string); id == segmentID {
				found = true
				maxUnits, _ := seg["maxUnits"].(float64)
				if maxUnits <= 0 {
					t.Errorf("the segment row must carry a positive max, got %v", seg["maxUnits"])
				}
			}
		}
	}
	if !found {
		t.Errorf("the settled segment %q is missing from the standings", segmentID)
	}
}

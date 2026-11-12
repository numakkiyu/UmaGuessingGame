package e2e

import (
	"net/http"
	"testing"
)

// TestTeamRoundScoresPerTeam pins that a draft team round is bound to TEAMS, not to
// individual members.
//
// The mechanism lives in the room layer: a membership with role team-captain AND a
// team id becomes a team subject, and the mode then scores that team. Nothing tested
// it end to end, so a regression would silently turn team rounds back into per-player
// rounds — the standings would look plausible while aggregating the wrong thing.
func TestTeamRoundScoresPerTeam(t *testing.T) {
	h := newHarness(t)
	h.createSession("team-host")

	roomID := h.createRoom("draft")

	// Create a team while the room is still in lobby.
	status, team := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/teams", map[string]any{
		"label": "blue team", "colorToken": "blue",
	}, "team-create-000001")
	if status != http.StatusCreated {
		t.Fatalf("create team = %d; body=%v", status, team)
	}
	teamID, _ := team["teamId"].(string)
	if teamID == "" {
		t.Fatal("create team returned no teamId")
	}

	// Put the caller on that team and make them its captain.
	_, room := h.do(http.MethodGet, "/api/v1/rooms/"+roomID, nil, "")
	// GET /rooms/{id} returns the room fields at the top level.
	version, _ := room["roomVersion"].(float64)
	viewer, _ := room["viewer"].(map[string]any)
	membershipID, _ := viewer["membershipId"].(string)
	if membershipID == "" {
		t.Fatal("room viewer has no membershipId")
	}
	status, patched := h.do(http.MethodPatch,
		"/api/v1/rooms/"+roomID+"/memberships/"+membershipID, map[string]any{
			"expectedRoomVersion": int64(version),
			"teamId":              teamID,
			"requestedRole":       "team-captain",
		}, "team-captain-0001")
	if status != http.StatusOK {
		t.Fatalf("set captain = %d; body=%v", status, patched)
	}

	status, round := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{
		"modeConfig": map[string]any{"modeId": "draft", "config": map[string]any{"variant": "teams"}},
	}, "team-round-000001")
	if status != http.StatusCreated {
		t.Fatalf("create round = %d; body=%v", status, round)
	}
	roundID, _ := round["roundId"].(string)

	status, view := h.do(http.MethodGet, "/api/v1/rounds/"+roundID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("read round = %d; body=%v", status, view)
	}
	publicState, _ := view["publicState"].(map[string]any)
	subjects, _ := publicState["subjects"].([]any)
	if len(subjects) != 1 {
		t.Fatalf("a team round must have one subject per team, got %d: %v", len(subjects), subjects)
	}
	first, _ := subjects[0].(map[string]any)
	// The decisive assertion: the roster entry is keyed by TEAM. That is what makes
	// the standings aggregate per team instead of per player.
	if id, _ := first["subjectId"].(string); id != teamID {
		t.Errorf("subject id = %q, want the team id %q; entry=%v", id, teamID, first)
	}
	if kind, _ := first["subjectKind"].(string); kind != "" && kind != "team" {
		t.Errorf("subject kind = %q, want team or omitted", kind)
	}
}

// TestTeamStandingsCarryTheTeamLabel proves a team row in the standings shows the
// team's name.
//
// The name lookup used to key only by membership id (mem_...), while a team round is
// scored by team id (tem_...). Every team row therefore had an empty displayName: the
// scores and segment counts were correct, so the table looked functional while being
// unreadable — the host could not tell which team was which.
func TestTeamStandingsCarryTheTeamLabel(t *testing.T) {
	h := newHarness(t)
	h.createSession("label-host")

	roomID := h.createRoom("draft")

	status, team := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/teams", map[string]any{
		"label": "blue team", "colorToken": "blue",
	}, "label-team-000001")
	if status != http.StatusCreated {
		t.Fatalf("create team = %d; body=%v", status, team)
	}
	teamID, _ := team["teamId"].(string)

	_, room := h.do(http.MethodGet, "/api/v1/rooms/"+roomID, nil, "")
	version, _ := room["roomVersion"].(float64)
	viewer, _ := room["viewer"].(map[string]any)
	membershipID, _ := viewer["membershipId"].(string)

	status, patched := h.do(http.MethodPatch,
		"/api/v1/rooms/"+roomID+"/memberships/"+membershipID, map[string]any{
			"expectedRoomVersion": int64(version),
			"teamId":              teamID,
			"requestedRole":       "team-captain",
		}, "label-captain-0001")
	if status != http.StatusOK {
		t.Fatalf("set captain = %d; body=%v", status, patched)
	}

	status, event := h.do(http.MethodPost, "/api/v1/events", map[string]any{
		"roomId": roomID, "name": "label event", "tiePolicy": "shared-rank",
	}, "label-event-00001")
	if status != http.StatusCreated {
		t.Fatalf("create event = %d; body=%v", status, event)
	}
	eventID, _ := event["eventId"].(string)

	status, segment := h.do(http.MethodPost, "/api/v1/events/"+eventID+"/segments", map[string]any{
		"label":       "item one",
		"modeConfig":  map[string]any{"modeId": "draft", "config": map[string]any{}},
		"scoreBudget": 100,
		"roundCount":  1,
		"competitive": true,
	}, "label-segment-001")
	if status != http.StatusCreated {
		t.Fatalf("create segment = %d; body=%v", status, segment)
	}
	segmentID, _ := segment["segmentId"].(string)

	status, round := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{
		"modeConfig": map[string]any{"modeId": "draft", "config": map[string]any{}},
		"segmentId":  segmentID,
	}, "label-round-00001")
	if status != http.StatusCreated {
		t.Fatalf("create round = %d; body=%v", status, round)
	}
	roundID, _ := round["roundId"].(string)

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
		t.Fatal("a settled segment must produce a standings row")
	}
	for _, it := range items {
		row, _ := it.(map[string]any)
		id, _ := row["subjectId"].(string)
		if id != teamID {
			continue
		}
		name, _ := row["displayName"].(string)
		if name != "blue team" {
			t.Errorf("the team row must carry the team label, got %q", name)
		}
		return
	}
	t.Errorf("the team %q is missing from the standings: %v", teamID, items)
}

// TestRoundViewCarriesSubjectNames proves a team round tells the client what to call
// each subject.
//
// A mode only ever sees Subject{ID, Kind}, so it cannot resolve a membership or team
// label — that knowledge lives in the room layer. Without subjectNames a console can
// render only opaque ids ("tem_2cc002ada4...") or must rebuild the mapping from the
// member list, duplicating the rule for which id a team round uses.
func TestRoundViewCarriesSubjectNames(t *testing.T) {
	h := newHarness(t)
	h.createSession("names-host")

	roomID := h.createRoom("draft")

	status, team := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/teams", map[string]any{
		"label": "blue team", "colorToken": "blue",
	}, "names-team-000001")
	if status != http.StatusCreated {
		t.Fatalf("create team = %d; body=%v", status, team)
	}
	teamID, _ := team["teamId"].(string)

	_, room := h.do(http.MethodGet, "/api/v1/rooms/"+roomID, nil, "")
	version, _ := room["roomVersion"].(float64)
	viewer, _ := room["viewer"].(map[string]any)
	membershipID, _ := viewer["membershipId"].(string)

	status, patched := h.do(http.MethodPatch,
		"/api/v1/rooms/"+roomID+"/memberships/"+membershipID, map[string]any{
			"expectedRoomVersion": int64(version),
			"teamId":              teamID,
			"requestedRole":       "team-captain",
		}, "names-captain-0001")
	if status != http.StatusOK {
		t.Fatalf("set captain = %d; body=%v", status, patched)
	}

	status, round := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{
		"modeConfig": map[string]any{"modeId": "draft", "config": map[string]any{}},
	}, "names-round-00001")
	if status != http.StatusCreated {
		t.Fatalf("create round = %d; body=%v", status, round)
	}
	roundID, _ := round["roundId"].(string)

	status, view := h.do(http.MethodGet, "/api/v1/rounds/"+roundID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("read round = %d; body=%v", status, view)
	}
	names, _ := view["subjectNames"].(map[string]any)
	if len(names) == 0 {
		t.Fatal("the round view must carry subjectNames")
	}
	label, _ := names[teamID].(string)
	if label != "blue team" {
		t.Errorf("subjectNames[%q] = %q, want %q", teamID, label, "blue team")
	}
}

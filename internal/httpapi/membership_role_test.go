package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestRequestedRoleContractMatchesImplementation pins that the contract's
// MembershipPatch.requestedRole enum lists exactly the roles this endpoint accepts.
//
// The enum used to declare only player/spectator while the handler accepted any
// valid role except host and display — so team-captain worked but was undocumented,
// and a client generated from the contract could not express it.
//
// The two lists must agree in BOTH directions: a role the contract offers must be
// assignable, and host/display must never appear because granting them through a
// role change would turn a participation edit into a path to control.
func TestRequestedRoleContractMatchesImplementation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.json"))
	if err != nil {
		t.Fatalf("contract not readable: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	patch, ok := doc.Components.Schemas["MembershipPatch"]
	if !ok {
		t.Fatal("contract has no MembershipPatch schema")
	}
	enum := patch.Properties["requestedRole"].Enum
	if len(enum) == 0 {
		t.Fatal("MembershipPatch.requestedRole declares no enum")
	}

	declared := map[string]bool{}
	for _, role := range enum {
		declared[role] = true
	}
	for _, role := range []string{"player", "team-captain", "spectator"} {
		if !declared[role] {
			t.Errorf("the contract must allow requestedRole %q", role)
		}
	}
	// These two are granted through their own endpoints; a role change must never
	// reach them.
	for _, role := range []string{"host", "display"} {
		if declared[role] {
			t.Errorf("the contract must NOT allow requestedRole %q", role)
		}
	}
}

// TestAssignTeamCaptainIsAccepted proves the role the contract now declares is
// actually assignable over HTTP, and that host is refused.
func TestAssignTeamCaptainIsAccepted(t *testing.T) {
	h := newMatchmakingHarness(t)
	client, sess := h.newSession("manager")
	csrf := sess.CSRFToken

	roomRec := h.doAs(client, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name": "roles", "kind": "friend",
		"modeConfig": map[string]any{"modeId": "classic", "config": map[string]any{}},
		"maxPlayers": 8, "maxSpectators": 0,
		"visibility": "invite", "allowSpectators": false,
	}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "role-room-000001"})
	if roomRec.Status != http.StatusCreated {
		t.Fatalf("create room = %d; body=%s", roomRec.Status, roomRec.Body)
	}
	var roomBody struct {
		Room struct {
			RoomID      string `json:"roomId"`
			RoomVersion int64  `json:"roomVersion"`
		} `json:"room"`
	}
	if err := json.Unmarshal(roomRec.Body, &roomBody); err != nil {
		t.Fatalf("decode room: %v", err)
	}
	roomID := roomBody.Room.RoomID

	teamRec := h.doAs(client, http.MethodPost, "/api/v1/rooms/"+roomID+"/teams", map[string]any{
		"label": "blue team", "colorToken": "blue",
	}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "role-team-000001"})
	if teamRec.Status != http.StatusCreated {
		t.Fatalf("create team = %d; body=%s", teamRec.Status, teamRec.Body)
	}
	var teamBody struct {
		TeamID string `json:"teamId"`
	}
	if err := json.Unmarshal(teamRec.Body, &teamBody); err != nil {
		t.Fatalf("decode team: %v", err)
	}

	readRoom := func() map[string]any {
		rec := h.doAs(client, http.MethodGet, "/api/v1/rooms/"+roomID, nil, nil)
		if rec.Status != http.StatusOK {
			t.Fatalf("read room = %d; body=%s", rec.Status, rec.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body, &out); err != nil {
			t.Fatalf("decode room: %v", err)
		}
		return out
	}
	room := readRoom()
	viewer, _ := room["viewer"].(map[string]any)
	membershipID, _ := viewer["membershipId"].(string)
	version, _ := room["roomVersion"].(float64)

	// team-captain must be accepted.
	ok := h.doAs(client, http.MethodPatch,
		"/api/v1/rooms/"+roomID+"/memberships/"+membershipID, map[string]any{
			"expectedRoomVersion": int64(version),
			"teamId":              teamBody.TeamID,
			"requestedRole":       "team-captain",
		}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "role-patch-000001"})
	if ok.Status != http.StatusOK {
		t.Fatalf("assign team-captain = %d; body=%s", ok.Status, ok.Body)
	}

	// host must be refused: a role change is not a path to control.
	room2 := readRoom()
	version2, _ := room2["roomVersion"].(float64)
	denied := h.doAs(client, http.MethodPatch,
		"/api/v1/rooms/"+roomID+"/memberships/"+membershipID, map[string]any{
			"expectedRoomVersion": int64(version2),
			"requestedRole":       "host",
		}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "role-patch-000002"})
	if denied.Status != http.StatusUnprocessableEntity && denied.Status != http.StatusBadRequest {
		t.Errorf("assigning host = %d, want 422 or 400; body=%s", denied.Status, denied.Body)
	}
}

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/room"
)

type entryRules struct {
	room.ContentIndex
	baseline json.RawMessage
}

func (s entryRules) Version() string        { return "rules-test" }
func (s entryRules) DatasetVersion() string { return "rules-test" }
func (s entryRules) AssetBaseURL() string   { return "/assets/" }
func (s entryRules) Section(name string) (json.RawMessage, bool) {
	return s.baseline, name == "rules.track"
}
func (s entryRules) ModeAdmission(name string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{Available: true}, name == "strategy-race"
}

func TestRoomInvitationVisibleOnlyToManager(t *testing.T) {
	registry, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = registry })
	owner, host := h.newSession("房主")
	guest, player := h.newSession("来宾")
	created, response := h.createRoom(owner, host, "周末约局")
	if response.Status != http.StatusCreated {
		t.Fatalf("create: %s", response.Body)
	}
	snapshot := created["room"].(map[string]any)
	id, code := snapshot["roomId"].(string), created["inviteCode"].(string)
	if snapshot["modeId"] != "classic" || snapshot["inviteCode"] != code {
		t.Fatalf("manager snapshot: %v", snapshot)
	}
	denied := h.doAs(guest, http.MethodGet, "/api/v1/rooms/"+id, nil, readHeaders(h))
	if denied.Status != http.StatusNotFound {
		t.Fatalf("outsider read: %d", denied.Status)
	}
	joined := h.doAs(guest, http.MethodPost, "/api/v1/rooms/"+id+"/memberships", map[string]any{"inviteCode": code, "requestedRole": "player"}, writeHeaders(h, player, idemKey("join")))
	if joined.Status != http.StatusCreated {
		t.Fatalf("join: %s", joined.Body)
	}
	for _, client := range []struct {
		manager bool
		name    string
	}{{true, "owner"}, {false, "player"}} {
		browser := owner
		if !client.manager {
			browser = guest
		}
		res := h.doAs(browser, http.MethodGet, "/api/v1/rooms/"+id, nil, readHeaders(h))
		var body map[string]any
		_ = json.Unmarshal(res.Body, &body)
		if res.Status != http.StatusOK || body["modeId"] != "classic" {
			t.Fatalf("%s snapshot: %s", client.name, res.Body)
		}
		if client.manager && body["inviteCode"] != code || !client.manager && body["inviteCode"] != nil {
			t.Fatalf("invitation permission: %s", res.Body)
		}
	}
}

func TestDefaultRoundUsesSavedConfiguration(t *testing.T) {
	registry, _ := modes.NewRegistry(strategyrace.New())
	baseline, err := os.ReadFile("../../data/manual/rules-track.json")
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *harnessConfig) { c.Modes = registry; c.Content = entryRules{baseline: baseline} })
	browser, owner := h.newSession("练习员")
	made := h.doAs(browser, http.MethodPost, "/api/v1/rooms", map[string]any{"name": "目标练习", "kind": "solo", "modeConfig": map[string]any{"modeId": "strategy-race", "practice": true}, "maxPlayers": 1, "maxSpectators": 0, "visibility": "invite", "allowSpectators": false}, writeHeaders(h, owner, idemKey("make")))
	if made.Status != http.StatusCreated {
		t.Fatalf("create: %s", made.Body)
	}
	var creation struct {
		Room struct {
			RoomID string `json:"roomId"`
		} `json:"room"`
	}
	_ = json.Unmarshal(made.Body, &creation)
	res := h.doAs(browser, http.MethodPost, "/api/v1/rooms/"+creation.Room.RoomID+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey("start")))
	if res.Status != http.StatusCreated {
		t.Fatalf("start saved configuration: %s", res.Body)
	}
	var round struct {
		ModeID string `json:"modeId"`
	}
	_ = json.Unmarshal(res.Body, &round)
	if round.ModeID != "strategy-race" {
		t.Fatalf("wrong game: %s", res.Body)
	}
	explicitNull := h.doAs(browser, http.MethodPost, "/api/v1/rooms/"+creation.Room.RoomID+"/rounds", map[string]any{"modeConfig": nil}, writeHeaders(h, owner, idemKey("null")))
	if explicitNull.Status != http.StatusBadRequest {
		t.Fatalf("null must not silently use defaults: %d %s", explicitNull.Status, explicitNull.Body)
	}
	event := h.doAs(browser, http.MethodPost, "/api/v1/rooms/"+creation.Room.RoomID+"/rounds", map[string]any{"segmentId": "seg_missing"}, writeHeaders(h, owner, idemKey("segment")))
	if event.Status != http.StatusBadRequest {
		t.Fatalf("segment must be explicit: %d %s", event.Status, event.Body)
	}
}

func TestDefaultRoundCannotBypassDeploymentPolicy(t *testing.T) {
	registry, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) {
		c.Modes = registry
		c.Policy = deployment.Policy{Forms: map[string]bool{"friend": false}}
	})
	browser, owner := h.newSession("房主")
	id := h.seedRoom(t, owner.ID, "已有的房间")
	_, err := h.db.Write().Exec(`UPDATE rooms SET config_json = ? WHERE id = ?`, `{"kind":"friend","modeConfig":{"modeId":"classic"},"maxPlayers":8}`, id)
	if err != nil {
		t.Fatal(err)
	}
	res := h.doAs(browser, http.MethodPost, "/api/v1/rooms/"+id+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey("blocked")))
	if res.Status != http.StatusForbidden || res.Problem.Code != "play-disabled" {
		t.Fatalf("default bypassed policy: %d %s", res.Status, res.Body)
	}
	// Existing room reads must still work when new games are disabled.
	read := h.doAs(browser, http.MethodGet, "/api/v1/rooms/"+id, nil, readHeaders(h))
	if read.Status != http.StatusOK {
		t.Fatalf("existing room recovery denied: %s", read.Body)
	}
}

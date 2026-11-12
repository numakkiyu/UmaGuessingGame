package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// bluffContent serves the bluff fact section from the real reviewed-facts document.
// The document exists but carries no admitted fact, which is exactly the state the
// test asserts on.
type bluffContent struct {
	facts      json.RawMessage
	characters json.RawMessage
}

func (c bluffContent) Section(name string) (json.RawMessage, bool) {
	switch name {
	case bluff.FactsSection:
		return c.facts, true
	case "characters.index":
		return c.characters, true
	}
	return nil, false
}
func (bluffContent) Version() string                             { return "bluff-gate-pack" }
func (bluffContent) DatasetVersion() string                      { return "bluff-gate-data" }
func (bluffContent) AssetBaseURL() string                        { return "/assets" }
func (bluffContent) Character(string) (room.CharacterCard, bool) { return room.CharacterCard{}, false }
func (bluffContent) Characters(string, int, string) ([]room.CharacterCard, string, error) {
	return nil, "", nil
}
func (bluffContent) PathNeighbors(string) ([]room.Neighbor, bool) { return nil, false }

// ModeAdmission reports what the compiler would: the mode is registered but carries
// no usable item, so it is unavailable and the count is zero.
func (bluffContent) ModeAdmission(id string) (room.ModeAdmission, bool) {
	if id != bluff.ModeID {
		return room.ModeAdmission{}, false
	}
	return room.ModeAdmission{
		Mode: bluff.ModeID, Available: false, QuestionCount: 0,
		Section: bluff.FactsSection, Reason: "no reviewed fact with a citation",
	}, true
}

// TestBluffRefusesToOpenWithoutReviewedFacts is the HTTP-level gate for G08.
func TestBluffRefusesToOpenWithoutReviewedFacts(t *testing.T) {
	// The reviewed-facts document is the real one: if it ever gains an admitted
	// fact, this test should be revisited rather than silently keep passing.
	facts, err := os.ReadFile(filepath.Join("..", "..", "data", "manual", "bluff-facts.review.json"))
	if err != nil {
		t.Fatalf("read reviewed facts: %v", err)
	}
	characters := json.RawMessage(`{"characters":[
		{"id":"1001","nameZh":"特别周"},
		{"id":"1002","nameZh":"无声铃鹿"},
		{"id":"1003","nameZh":"东海帝皇"}
	]}`)
	reg, _ := modes.NewRegistry(bluff.New())
	h := newHarness(t, func(c *harnessConfig) {
		c.Modes = reg
		c.Content = bluffContent{facts: facts, characters: characters}
	})

	host, owner := h.newSession("主持")
	made := h.doAs(host, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name": "假档案发布会", "kind": "friend",
		"modeConfig":      map[string]any{"modeId": "bluff", "config": map[string]any{}},
		"maxPlayers":      8,
		"maxSpectators":   0,
		"allowSpectators": false,
		"visibility":      "invite",
	}, writeHeaders(h, owner, idemKey("bluff-room")))
	if made.Status != http.StatusCreated {
		t.Fatalf("create room: %d %s", made.Status, made.Body)
	}
	var created struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	if err := json.Unmarshal(made.Body, &created); err != nil {
		t.Fatal(err)
	}
	rid := created.Room.ID

	for i := 0; i < 2; i++ {
		c, sess := h.newSession("训练员" + string(rune('A'+i)))
		joined := h.doAs(c, http.MethodPost, "/api/v1/rooms/"+rid+"/memberships", map[string]any{
			"inviteCode": created.Code, "requestedRole": "player",
		}, writeHeaders(h, sess, idemKey("bluff-join")))
		if joined.Status != http.StatusCreated {
			t.Fatalf("join %d: %d %s", i, joined.Status, joined.Body)
		}
	}

	// Opening the round must fail with a structured, attributable refusal.
	opened := h.doAs(host, http.MethodPost, "/api/v1/rooms/"+rid+"/rounds", map[string]any{},
		writeHeaders(h, owner, idemKey("bluff-open")))
	if opened.Status != http.StatusUnprocessableEntity {
		t.Fatalf("opening bluff without reviewed facts = %d, want 422; body=%s",
			opened.Status, opened.Body)
	}
	var problem struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(opened.Body, &problem); err != nil {
		t.Fatalf("refusal is not a structured problem: %v (%s)", err, opened.Body)
	}
	if problem.Code != "round-preparation-failed" {
		t.Fatalf("refusal code = %q, want round-preparation-failed", problem.Code)
	}
	// The detail must name the real cause, not a generic "something went wrong".
	if !strings.Contains(problem.Detail, "bluff") {
		t.Fatalf("refusal detail does not name the mode: %q", problem.Detail)
	}
	// The refusal must not be a server error: nothing is broken, the content simply
	// does not clear review.
	if opened.Status >= 500 {
		t.Fatalf("a content gate must not surface as a server error: %d", opened.Status)
	}

	// The lobby and the open attempt must agree: a mode reporting itself unavailable
	// cannot be started, which is the invariant this project kept breaking.
	modesResp := h.doAs(host, http.MethodGet, "/api/v1/modes", nil, readHeaders(h))
	if modesResp.Status != http.StatusOK {
		t.Fatalf("list modes: %d %s", modesResp.Status, modesResp.Body)
	}
	var listed struct {
		Items []struct {
			ModeID       string `json:"modeId"`
			Availability string `json:"availability"`
		} `json:"items"`
	}
	if err := json.Unmarshal(modesResp.Body, &listed); err != nil {
		t.Fatalf("decode modes: %v", err)
	}
	var found bool
	for _, item := range listed.Items {
		if item.ModeID != bluff.ModeID {
			continue
		}
		found = true
		if item.Availability == "available" {
			t.Fatalf("bluff reports available while refusing to open a round")
		}
	}
	if !found {
		t.Fatalf("bluff is missing from the mode list entirely")
	}
}

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
)

func TestHTTPPathNeighborsUsesPreparedInformationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  map[string]any
		allowed bool
	}{
		{"default-closed", map[string]any{"modeId": "co-race-path", "config": map[string]any{}}, false},
		{"closed-with-search-override", map[string]any{"modeId": "co-race-path", "informationStyle": "closed", "config": map[string]any{"searchAllowed": true}}, false},
		{"legacy-open", map[string]any{"modeId": "co-race-path", "config": map[string]any{"openBook": true}}, true},
		{"open", map[string]any{"modeId": "co-race-path", "informationStyle": "open", "config": map[string]any{}}, true},
		{"open-search-disabled", map[string]any{"modeId": "co-race-path", "informationStyle": "open", "config": map[string]any{"searchAllowed": false}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pack := loadRealPackSource(t)
			reg, _ := modes.NewRegistry(coracepath.New())
			h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = pack })
			_, roundID, _, _, player, _ := openModeRoom(t, h, coracepath.ModeID, "ps", tc.config)
			view := readRound(t, h, player, roundID)
			var pub struct {
				Start struct {
					CharacterID string `json:"characterId"`
				} `json:"start"`
				Config struct {
					OpenBook bool `json:"openBook"`
				} `json:"config"`
				Edges []json.RawMessage `json:"edges"`
			}
			if err := json.Unmarshal(view.Public, &pub); err != nil {
				t.Fatal(err)
			}
			response := h.doAs(player, http.MethodGet, "/api/v1/rounds/"+roundID+"/path-neighbors?characterId="+pub.Start.CharacterID, nil, readHeaders(h))
			if tc.allowed {
				if response.Status != http.StatusOK || !pub.Config.OpenBook || len(pub.Edges) == 0 {
					t.Fatalf("open search not available: %d %s", response.Status, response.Body)
				}
			} else if response.Status != http.StatusForbidden {
				t.Fatalf("search must fail closed: %d %s", response.Status, response.Body)
			}
			if !pub.Config.OpenBook && len(pub.Edges) != 0 {
				t.Fatal("closed question leaks edges")
			}
		})
	}
}

func TestHTTPPathMissingEdgeHasStructuredStep(t *testing.T) {
	pack := loadRealPackSource(t)
	reg, _ := modes.NewRegistry(coracepath.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = pack })
	_, roundID, _, _, player, sess := openModeRoom(t, h, coracepath.ModeID, "err")
	view := readRound(t, h, player, roundID)
	var pub struct {
		Start struct {
			CharacterID string `json:"characterId"`
		} `json:"start"`
		Nodes []struct {
			CharacterID string `json:"characterId"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(view.Public, &pub); err != nil {
		t.Fatal(err)
	}
	neighbors, _ := pack.PathNeighbors(pub.Start.CharacterID)
	adj := map[string]bool{pub.Start.CharacterID: true}
	for _, n := range neighbors {
		adj[n.CharacterID] = true
	}
	var bad string
	for _, node := range pub.Nodes {
		if !adj[node.CharacterID] {
			bad = node.CharacterID
			break
		}
	}
	if bad == "" {
		t.Fatal("test pack needs a non-edge from start")
	}
	response := submitAction(t, h, player, sess, roundID, view, map[string]any{"kind": "path.route", "characterIds": []string{pub.Start.CharacterID, bad}}, "path-bad")
	var failure struct {
		Code        string `json:"code"`
		FieldErrors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fieldErrors"`
	}
	if err := json.Unmarshal(response.Body, &failure); err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusUnprocessableEntity || failure.Code != "missing_edge" || len(failure.FieldErrors) != 1 || failure.FieldErrors[0].Field != "modeAction.characterIds[1]" || failure.FieldErrors[0].Message != "missing_edge" {
		t.Fatalf("missing structured attempted-step error: %d %s", response.Status, response.Body)
	}
}

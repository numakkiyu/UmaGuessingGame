package coracepath

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

func TestSharedPracticeRoutes(t *testing.T) {
	var fixtures struct {
		Nodes []string   `json:"nodes"`
		Edges [][]string `json:"edges"`
		Cases []struct {
			Name       string
			Path       []string
			Goal       string
			Accepted   bool
			ScoreUnits int
		} `json:"cases"`
	}
	raw, err := os.ReadFile("../../../web/src/lib/domain/testdata/path-routes.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	nodes, edges := []Node{}, []Edge{}
	for _, id := range fixtures.Nodes {
		nodes = append(nodes, Node{CharacterID: id, Name: id})
	}
	for _, pair := range fixtures.Edges {
		edges = append(edges, Edge{From: pair[0], To: pair[1], RaceID: "r"})
	}
	adj := adjacency(nodes, edges)
	for _, f := range fixtures.Cases {
		t.Run(f.Name, func(t *testing.T) {
			pub := publicPrepared{Config: Config{MaxEdges: 6}, Start: Node{CharacterID: "a"}, Goal: Node{CharacterID: f.Goal}, Nodes: nodes, ShortestEdges: bfsDistances(adj, "a")[f.Goal]}
			dec, err := New().Apply(modes.ApplyInput{Public: modeutil.Marshal(pub), Private: modeutil.Marshal(privatePrepared{Edges: edges}), Action: modes.ModeAction{Kind: ActionPath, Payload: modeutil.Marshal(pathPayload{Kind: ActionPath, CharacterIDs: f.Path})}})
			if err != nil {
				t.Fatal(err)
			}
			if dec.Accepted != f.Accepted {
				t.Fatalf("accepted %v, want %v: %s", dec.Accepted, f.Accepted, dec.Detail)
			}
			if f.Accepted {
				var st state
				if err := json.Unmarshal(dec.State, &st); err != nil {
					t.Fatal(err)
				}
				if st.ScoreUnits != f.ScoreUnits {
					t.Fatalf("score %d want %d", st.ScoreUnits, f.ScoreUnits)
				}
			}
		})
	}
}

func TestPracticeGraphWhitelistsAndDeduplicatesEvidence(t *testing.T) {
	content := fakeContent{"co-race-path.graph": json.RawMessage(`{"nodes":[{"characterId":"a","name":"甲","imagePath":"https://untrusted.example/x","seed":"NEVER_EXPORT"},{"characterId":"b","name":"乙"}],"edges":[{"from":"a","to":"b","raceId":"r1","raceName":"锦标","date":"2026-01-01","secret":"NEVER_EXPORT"},{"from":"b","to":"a","raceId":"r1","raceName":"锦标","date":"2026-01-01"},{"from":"b","to":"a","raceId":"r2","raceName":"另一场","date":"2026-01-02"}],"shortestPath":"NEVER_EXPORT"}`)}
	nodes, edges, err := PracticeGraph(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 || nodes[0].ImagePath != "/assets/avatars/a.webp" {
		t.Fatal(nodes, edges)
	}
	raw, _ := json.Marshal(struct {
		Nodes []Node
		Edges []Edge
	}{nodes, edges})
	if strings.Contains(string(raw), "NEVER_EXPORT") || strings.Contains(string(raw), "untrusted") {
		t.Fatal("extra data exported")
	}
	for _, source := range []string{
		`{"nodes":[{"characterId":"a","name":"甲"},{"characterId":"b","name":"乙"}],"edges":[{"from":"a","to":"b","raceId":"r1"}]}`,
		`{"nodes":[{"characterId":"a","name":"甲"},{"characterId":"b","name":"乙"}],"edges":[{"from":"a","to":"foreign","raceId":"r1","raceName":"锦标","date":"2026-01-01"}]}`,
	} {
		if _, _, err := PracticeGraph(fakeContent{"co-race-path.graph": json.RawMessage(source)}); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
}

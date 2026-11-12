package coracepath

import (
	"fmt"
	"sort"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// PracticeGraph exports public co-appearance facts, never a prepared round.
func PracticeGraph(src modes.ContentSource) ([]Node, []Edge, error) {
	g, err := loadGraph(src)
	if err != nil {
		return nil, nil, err
	}
	known := map[string]bool{}
	for _, n := range g.Nodes {
		if n.CharacterID == "" || n.Name == "" || known[n.CharacterID] {
			return nil, nil, fmt.Errorf("coracepath: invalid practice node")
		}
		known[n.CharacterID] = true
	}
	edges := []Edge{}
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if !known[e.From] || !known[e.To] || e.From == e.To || e.RaceID == "" || e.RaceName == "" || e.Date == "" {
			return nil, nil, fmt.Errorf("coracepath: practice edge lacks recorded evidence")
		}
		if e.From > e.To {
			e.From, e.To = e.To, e.From
		}
		key := edgeKey(e.From, e.To) + "\x00" + e.RaceID
		if seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, e)
	}
	if len(edges) == 0 {
		return nil, nil, fmt.Errorf("coracepath: no practice co-appearance")
	}
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.RaceID < b.RaceID
	})
	for i := range g.Nodes {
		g.Nodes[i].ImagePath = "/assets/avatars/" + g.Nodes[i].CharacterID + ".webp"
	}
	return g.Nodes, edges, nil
}

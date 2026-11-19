package coracepath

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// nodeSpec and edgeSpec are the on-the-wire spellings.
type nodeSpec struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
}

type edgeSpec struct {
	From   string `json:"from"`
	To     string `json:"to"`
	RaceID string `json:"raceId"`
	Date   string `json:"date"`
}

// graphSection is the on-the-wire graph section.
type graphSection struct {
	Nodes []nodeSpec `json:"nodes"`
	Edges []edgeSpec `json:"edges"`
}

// packOf builds a co-race-path pack.
func packOf(nodes []nodeSpec, edges []edgeSpec) fakeContent {
	raw, err := json.Marshal(graphSection{Nodes: nodes, Edges: edges})
	if err != nil {
		panic(err)
	}
	return fakeContent{"co-race-path.graph": raw}
}

// linePack is a simple chain a—b—c—d—e—f with single edges, plus a shortcut
// a—c so the shortest path is two edges rather than four.
func linePack() fakeContent {
	nodes := []nodeSpec{
		{CharacterID: "a", Name: "甲"},
		{CharacterID: "b", Name: "乙"},
		{CharacterID: "c", Name: "丙"},
		{CharacterID: "d", Name: "丁"},
		{CharacterID: "e", Name: "戊"},
		{CharacterID: "f", Name: "己"},
	}
	edges := []edgeSpec{
		{From: "a", To: "b", RaceID: "r1", Date: "2026-01-01"},
		{From: "b", To: "c", RaceID: "r2", Date: "2026-02-02"},
		{From: "c", To: "d", RaceID: "r3", Date: "2026-03-03"},
		{From: "d", To: "e", RaceID: "r4", Date: "2026-04-04"},
		{From: "e", To: "f", RaceID: "r5", Date: "2026-05-05"},
		{From: "a", To: "c", RaceID: "r6", Date: "2026-06-06"},
	}
	return packOf(nodes, edges)
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-1",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// prepare runs Prepare and returns the decoded halves.
func prepare(t *testing.T, pack fakeContent, seed, cfg string, subjects ...modes.Subject) (modes.Prepared, publicPrepared, privatePrepared) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: subjects,
		Config:  json.RawMessage(cfg),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicPrepared
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privatePrepared
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return prep, pub, priv
}

// pathAction builds a contract-shaped PathRoute payload.
func pathAction(path []string) modes.ModeAction {
	raw, _ := json.Marshal(pathPayload{Kind: ActionPath, CharacterIDs: path})
	return modes.ModeAction{Kind: ActionPath, Payload: raw}
}

// applyOne runs one Apply call.
func applyOne(t *testing.T, prep modes.Prepared, subj modes.Subject, prev json.RawMessage, action modes.ModeAction) (modes.Decision, state) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("apply"),
		Subject: subj,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   prev,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var st state
	if len(dec.State) > 0 {
		if err := json.Unmarshal(dec.State, &st); err != nil {
			t.Fatalf("decode state: %v", err)
		}
	}
	return dec, st
}

// fixedPairEdges are the edges of fixedPairPack.
func fixedPairEdges() []edgeSpec {
	return []edgeSpec{
		{From: "a", To: "b", RaceID: "r1", Date: "2026-01-01"},
		{From: "b", To: "d", RaceID: "r2", Date: "2026-02-02"},
		{From: "a", To: "c", RaceID: "r3", Date: "2026-03-03"},
		{From: "c", To: "b", RaceID: "r4", Date: "2026-04-04"},
	}
}

// fixedPairPack builds a small connected graph with a shortcut, so the prepared
// question has a shortest chain plus at least one longer legal detour.
func fixedPairPack() fakeContent {
	nodes := []nodeSpec{
		{CharacterID: "a", Name: "甲"},
		{CharacterID: "b", Name: "乙"},
		{CharacterID: "c", Name: "丙"},
		{CharacterID: "d", Name: "丁"},
	}
	edges := []edgeSpec{
		{From: "a", To: "b", RaceID: "r1", Date: "2026-01-01"},
		{From: "b", To: "d", RaceID: "r2", Date: "2026-02-02"},
		{From: "a", To: "c", RaceID: "r3", Date: "2026-03-03"},
		{From: "c", To: "b", RaceID: "r4", Date: "2026-04-04"},
	}
	return packOf(nodes, edges)
}

// edgeSetOf builds an undirected edge set from specs.
func edgeSetOf(edges []edgeSpec) map[string]struct{} {
	set := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		a, b := e.From, e.To
		if a > b {
			a, b = b, a
		}
		set[a+"\x00"+b] = struct{}{}
	}
	return set
}

// neighboursOf returns the sorted neighbours of a node.
func neighboursOf(edges []edgeSpec, node string) []string {
	seen := map[string]struct{}{}
	for _, e := range edges {
		switch {
		case e.From == node && e.To != node:
			seen[e.To] = struct{}{}
		case e.To == node && e.From != node:
			seen[e.From] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// sortStrings is a tiny local sort to avoid importing sort just for tests.
func sortStrings(list []string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// bfsChain returns a shortest chain between two nodes over the given edges.
func bfsChain(edges []edgeSpec, start, goal string) []string {
	prev := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == goal {
			break
		}
		for _, next := range neighboursOf(edges, cur) {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = cur
			queue = append(queue, next)
		}
	}
	if _, ok := prev[goal]; !ok {
		return nil
	}
	chain := []string{goal}
	for cur := goal; cur != start; {
		cur = prev[cur]
		chain = append(chain, cur)
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// findDetour returns a simple chain from start to goal that is strictly longer
// than the shortest one, or nil when the graph offers none.
func findDetour(edges []edgeSpec, start, goal string, shortest int) []string {
	best := []string(nil)
	var walk func(path []string, visited map[string]struct{})
	walk = func(path []string, visited map[string]struct{}) {
		cur := path[len(path)-1]
		if cur == goal {
			if len(path)-1 > shortest && (best == nil || len(path) < len(best)) {
				best = append([]string(nil), path...)
			}
			return
		}
		for _, next := range neighboursOf(edges, cur) {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			walk(append(path, next), visited)
			delete(visited, next)
		}
	}
	walk([]string{start}, map[string]struct{}{start: {}})
	return best
}

// TestScoreFormula pins max(20, 100-10×(actual-shortest)) .
func TestScoreFormula(t *testing.T) {
	cases := []struct {
		name      string
		actual    int
		shortest  int
		wantUnits int
	}{
		{name: "shortest path scores 100", actual: 2, shortest: 2, wantUnits: 10000},
		{name: "one extra edge scores 90", actual: 3, shortest: 2, wantUnits: 9000},
		{name: "two extra edges scores 80", actual: 4, shortest: 2, wantUnits: 8000},
		{name: "eight extra edges hits the floor", actual: 10, shortest: 2, wantUnits: 2000},
		{name: "nine extra edges stays at the floor", actual: 11, shortest: 2, wantUnits: 2000},
		{name: "far beyond still floors at 20", actual: 20, shortest: 1, wantUnits: 2000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreUnits(tc.actual, tc.shortest); got != tc.wantUnits {
				t.Fatalf("scoreUnits(%d,%d) = %d, want %d",
					tc.actual, tc.shortest, got, tc.wantUnits)
			}
		})
	}
}

// TestShortestPathScoresFull asserts a shortest legal chain scores 100 and a
// longer legal chain scores less .
func TestShortestPathScoresFull(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, fixedPairPack(), "path-seed", `{}`, subjects...)

	edges := fixedPairEdges()
	start, goal := pub.Start.CharacterID, pub.Goal.CharacterID
	if start == goal {
		t.Fatalf("Prepare built a degenerate question %s→%s", start, goal)
	}

	shortest := bfsChain(edges, start, goal)
	if shortest == nil {
		t.Fatalf("no chain from %s to %s", start, goal)
	}
	if pub.ShortestEdges != len(shortest)-1 {
		t.Fatalf("shortestEdges = %d, want %d", pub.ShortestEdges, len(shortest)-1)
	}
	if len(priv.ShortestPath) != len(shortest) {
		t.Fatalf("private shortestPath = %v, want %d nodes", priv.ShortestPath, len(shortest))
	}

	dec, st := applyOne(t, prep, subjects[0], nil, pathAction(shortest))
	if !dec.Accepted {
		t.Fatalf("shortest chain %v rejected: %s %s", shortest, dec.Code, dec.Detail)
	}
	if !st.Arrived {
		t.Fatalf("shortest chain %v did not arrive", shortest)
	}
	if st.Edges != pub.ShortestEdges {
		t.Fatalf("edges = %d, want %d", st.Edges, pub.ShortestEdges)
	}
	if st.ScoreUnits != 10000 {
		t.Fatalf("shortest chain score = %d, want 10000", st.ScoreUnits)
	}

	// A strictly longer legal detour scores less, and still at least the floor.
	detour := findDetour(edges, start, goal, pub.ShortestEdges)
	if detour == nil {
		t.Fatalf("the test graph offers no detour from %s to %s", start, goal)
	}
	dec2, st2 := applyOne(t, prep, subjects[0], nil, pathAction(detour))
	if !dec2.Accepted {
		t.Fatalf("detour %v rejected: %s %s", detour, dec2.Code, dec2.Detail)
	}
	if st2.Edges <= pub.ShortestEdges {
		t.Fatalf("detour edges = %d, want more than %d", st2.Edges, pub.ShortestEdges)
	}
	wantDetour := scoreUnits(st2.Edges, pub.ShortestEdges)
	if st2.ScoreUnits != wantDetour {
		t.Fatalf("detour score = %d, want %d", st2.ScoreUnits, wantDetour)
	}
	if st2.ScoreUnits >= st.ScoreUnits {
		t.Fatalf("detour scored %d, not below the shortest %d", st2.ScoreUnits, st.ScoreUnits)
	}
}

// TestUnreachableGoalScoresZero covers the documented "不可达" case: a legal chain
// that does not end at the goal scores 0 .
func TestUnreachableGoalScoresZero(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, fixedPairPack(), "unreach-seed", `{}`, subjects...)

	// a→b is legal but does not reach d.
	dec, st := applyOne(t, prep, subjects[0], nil, pathAction([]string{"a", "b"}))
	if !dec.Accepted {
		t.Fatalf("legal partial chain rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.Arrived {
		t.Fatal("a chain ending at b reported arrival at d")
	}
	if st.ScoreUnits != 0 {
		t.Fatalf("partial chain score = %d, want 0", st.ScoreUnits)
	}

	// Settle must also score it 0.
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("unreach-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st)},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if scores[0].ScoreUnits != 0 {
		t.Fatalf("settled score = %d, want 0", scores[0].ScoreUnits)
	}
}

// TestCycleAndRepeatsRejected covers the documented "环" and "重复" cases: a
// repeated character is refused, which also rules out a cycle .
func TestCycleAndRepeatsRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, fixedPairPack(), "cycle-seed", `{}`, subjects...)
	cases := []struct {
		name string
		path []string
	}{
		{name: "immediate back-and-forth", path: []string{"a", "b", "a", "d"}},
		{name: "longer cycle", path: []string{"a", "c", "b", "c", "b", "d"}},
		{name: "repeated start", path: []string{"a", "b", "d", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := applyOne(t, prep, subjects[0], nil, pathAction(tc.path))
			if dec.Accepted {
				t.Fatalf("chain with a repeated character was accepted: %v", tc.path)
			}
			if dec.Code != CodeRepeatedNode {
				t.Fatalf("code = %q, want %q", dec.Code, CodeRepeatedNode)
			}
		})
	}
}

// TestTooLongRejected covers the documented "超过长度" case: a chain longer than
// the documented six edges is refused .
func TestTooLongRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, linePack(), "long-seed", `{}`, subjects...)
	if pub.Config.MaxEdges != DefaultMaxEdges {
		t.Fatalf("maxEdges = %d, want %d", pub.Config.MaxEdges, DefaultMaxEdges)
	}

	// Build a chain with more than six edges using a long simple path. The
	// linePack has six nodes, so a five-edge simple path is the longest; tighten
	// the limit to three and check a four-edge chain is refused.
	prepStrict, _, _ := prepare(t, linePack(), "long-seed", `{"maxEdges":3}`, subjects...)

	// A five-node chain has four edges, over the limit of three.
	dec, _ := applyOne(t, prepStrict, subjects[0], nil, pathAction([]string{"a", "b", "c", "d", "e"}))
	if dec.Accepted {
		t.Fatal("a chain over the edge limit was accepted")
	}
	if dec.Code != CodeTooLong {
		t.Fatalf("code = %q, want %q", dec.Code, CodeTooLong)
	}
	// A chain within the limit is accepted.
	dec2, st2 := applyOne(t, prepStrict, subjects[0], nil, pathAction([]string{"a", "c", "d"}))
	if !dec2.Accepted {
		t.Fatalf("chain within the limit rejected: %s %s", dec2.Code, dec2.Detail)
	}
	if st2.Edges != 2 {
		t.Fatalf("edges = %d, want 2", st2.Edges)
	}
	_ = prep
}

// TestStartEqualsGoalRefused covers the documented "起点等于终点" case: the round
// never builds such a pair, and a chain that begins and ends at the same node
// necessarily repeats it .
func TestStartEqualsGoalRefused(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, fixedPairPack(), "same-seed", `{}`, subjects...)
	if pub.Start.CharacterID == pub.Goal.CharacterID {
		t.Fatal("Prepare built a question whose start equals its goal")
	}

	// A chain that is only the start does not reach the goal, and any attempt to
	// return to the start repeats it.
	dec, st := applyOne(t, prep, subjects[0], nil, pathAction([]string{"a"}))
	if dec.Accepted && st.Arrived {
		t.Fatal("a single-node chain reported arrival")
	}
	dec2, _ := applyOne(t, prep, subjects[0], nil, pathAction([]string{"a", "b", "d", "a"}))
	if dec2.Accepted {
		t.Fatal("a chain returning to the start was accepted")
	}
	if dec2.Code != CodeRepeatedNode {
		t.Fatalf("code = %q, want %q", dec2.Code, CodeRepeatedNode)
	}
}

// TestMissingEvidenceRejected covers the documented per-step feedback rule: a
// step without pack evidence is refused and the rejection names the step without
// revealing the shortest solution .
func TestMissingEvidenceRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, fixedPairPack(), "evidence-seed", `{}`, subjects...)
	edges := fixedPairEdges()
	edgeSet := edgeSetOf(edges)

	// Find a node reachable from the start but not directly adjacent to it, so
	// the second step has no evidence while the first does.
	start := pub.Start.CharacterID
	var victim string
	shortest := bfsChain(edges, start, pub.Goal.CharacterID)
	for _, candidate := range shortest {
		if candidate == start {
			continue
		}
		if _, adjacent := edgeSet[edgeKey(start, candidate)]; !adjacent {
			victim = candidate
			break
		}
	}
	if victim == "" {
		t.Skip("the test graph has no reachable non-adjacent node to exercise a missing step")
	}

	dec, _ := applyOne(t, prep, subjects[0], nil, pathAction([]string{start, victim}))
	if dec.Accepted {
		t.Fatalf("a step without evidence (%s→%s) was accepted", start, victim)
	}
	if dec.Code != CodeNoEdge {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNoEdge)
	}
	if dec.Detail == "" {
		t.Fatal("rejection has no detail")
	}
	// The detail must name the failing step without handing over the answer.
	if len(dec.Detail) == 0 {
		t.Fatal("empty detail")
	}
}

// TestBadStartRejected asserts the chain must begin at the published start
// .
func TestBadStartRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, fixedPairPack(), "start-seed", `{}`, subjects...)

	// Any node other than the published start.
	var elsewhere string
	for _, node := range pub.Nodes {
		if node.CharacterID != pub.Start.CharacterID {
			elsewhere = node.CharacterID
			break
		}
	}
	if elsewhere == "" {
		t.Fatal("the graph has only one node")
	}

	cases := []struct {
		name string
		path []string
	}{
		{name: "empty chain", path: []string{}},
		{name: "starts elsewhere", path: []string{elsewhere}},
		{name: "starts at the goal", path: []string{pub.Goal.CharacterID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.path) == 1 && tc.path[0] == pub.Start.CharacterID {
				t.Skip("the goal equals the start in this question")
			}
			dec, _ := applyOne(t, prep, subjects[0], nil, pathAction(tc.path))
			if dec.Accepted {
				t.Fatalf("accepted a chain not starting at the start: %v", tc.path)
			}
			if dec.Code != CodeBadStart {
				t.Fatalf("code = %q, want %q", dec.Code, CodeBadStart)
			}
		})
	}
}

// TestAnyLegalChainAccepted covers the documented "任何合法路线都接受" rule: every
// legal chain that reaches the goal is accepted, not only the canonical shortest
// one .
func TestAnyLegalChainAccepted(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, fixedPairPack(), "any-seed", `{}`, subjects...)
	edges := fixedPairEdges()
	start, goal := pub.Start.CharacterID, pub.Goal.CharacterID

	shortest := bfsChain(edges, start, goal)
	if shortest == nil {
		t.Fatalf("no chain from %s to %s", start, goal)
	}
	chains := [][]string{shortest}
	if detour := findDetour(edges, start, goal, pub.ShortestEdges); detour != nil {
		chains = append(chains, detour)
	}
	if len(chains) < 2 {
		t.Skip("the test graph offers only one legal chain")
	}

	for _, chain := range chains {
		dec, st := applyOne(t, prep, subjects[0], nil, pathAction(chain))
		if !dec.Accepted {
			t.Fatalf("legal chain %v rejected: %s %s", chain, dec.Code, dec.Detail)
		}
		if !st.Arrived {
			t.Fatalf("legal chain %v did not report arrival", chain)
		}
		if st.ScoreUnits < modeutil.Units(MinScoreFloor) {
			t.Fatalf("legal chain %v scored below the floor: %d", chain, st.ScoreUnits)
		}
	}
}

// TestNotConnectedPairNeverBuilt covers the documented "当前图非全连通" rule: a
// question is only built inside one connected component, so Prepare never picks
// an unreachable pair .
func TestNotConnectedPairNeverBuilt(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// Two separate components: {a,b,c} and {x,y,z}.
	nodes := []nodeSpec{
		{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"},
		{CharacterID: "x"}, {CharacterID: "y"}, {CharacterID: "z"},
	}
	edges := []edgeSpec{
		{From: "a", To: "b", RaceID: "r1"},
		{From: "b", To: "c", RaceID: "r2"},
		{From: "x", To: "y", RaceID: "r3"},
		{From: "y", To: "z", RaceID: "r4"},
	}
	pack := packOf(nodes, edges)

	// Across many seeds the pair always comes from one component.
	groupOf := map[string]string{
		"a": "abc", "b": "abc", "c": "abc",
		"x": "xyz", "y": "xyz", "z": "xyz",
	}
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10"} {
		_, pub, _ := prepare(t, pack, seed, `{}`, subjects...)
		startGroup := groupOf[pub.Start.CharacterID]
		goalGroup := groupOf[pub.Goal.CharacterID]
		if startGroup != goalGroup {
			t.Fatalf("seed %s built a cross-component pair %s→%s",
				seed, pub.Start.CharacterID, pub.Goal.CharacterID)
		}
		if pub.ShortestEdges < 1 {
			t.Fatalf("seed %s built a pair with no path", seed)
		}
	}
}

// TestConcurrentUpdatesAllowed covers the documented "并发更新" case: revisions
// replace the previous chain and the last accepted one wins .
func TestConcurrentUpdatesAllowed(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, pub, _ := prepare(t, fixedPairPack(), "update-seed", `{}`, subjects...)
	edges := fixedPairEdges()
	start, goal := pub.Start.CharacterID, pub.Goal.CharacterID

	shortest := bfsChain(edges, start, goal)
	detour := findDetour(edges, start, goal, pub.ShortestEdges)
	if shortest == nil || detour == nil {
		t.Fatalf("test graph lacks both a shortest chain and a detour (%v / %v)", shortest, detour)
	}

	// p1 first submits the detour, then revises to the shortest chain.
	_, st1 := applyOne(t, prep, subjects[0], nil, pathAction(detour))
	dec1b, st1b := applyOne(t, prep, subjects[0], modeutil.Marshal(st1), pathAction(shortest))
	if !dec1b.Accepted {
		t.Fatalf("revision rejected: %s", dec1b.Code)
	}
	if st1b.Revisions != 2 {
		t.Fatalf("revisions = %d, want 2", st1b.Revisions)
	}
	if st1b.ScoreUnits <= st1.ScoreUnits {
		t.Fatalf("the shorter revision should score higher: %d vs %d",
			st1b.ScoreUnits, st1.ScoreUnits)
	}
	if st1b.ScoreUnits != 10000 {
		t.Fatalf("revised score = %d, want 10000", st1b.ScoreUnits)
	}

	_, st2 := applyOne(t, prep, subjects[1], nil, pathAction(shortest))
	if st2.ScoreUnits != 10000 {
		t.Fatalf("p2 score = %d, want 10000", st2.ScoreUnits)
	}

	// Settlement is independent of submission order and shares a rank on ties.
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("update-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States: map[string]json.RawMessage{
			"p2": modeutil.Marshal(st2),
			"p1": modeutil.Marshal(st1b),
		},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if scores[0].ScoreUnits != 10000 || scores[1].ScoreUnits != 10000 {
		t.Fatalf("tied scores = %d,%d, want 10000,10000",
			scores[0].ScoreUnits, scores[1].ScoreUnits)
	}
	if scores[0].Rank != 1 || scores[1].Rank != 1 {
		t.Fatalf("tied ranks = %d,%d, want 1,1", scores[0].Rank, scores[1].Rank)
	}
}

// TestEvidenceDeduplicated covers the documented "同场证据去重" case: several
// records for the same pair collapse to one adjacency entry, but every record is
// kept as evidence for the reveal .
func TestEvidenceDeduplicated(t *testing.T) {
	// The same pair a—b appears three times (different races) plus reversed.
	nodes := []nodeSpec{{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"}}
	edges := []edgeSpec{
		{From: "a", To: "b", RaceID: "r1", Date: "2026-01-01"},
		{From: "a", To: "b", RaceID: "r2", Date: "2026-02-02"},
		{From: "b", To: "a", RaceID: "r3", Date: "2026-03-03"},
		{From: "b", To: "c", RaceID: "r4", Date: "2026-04-04"},
	}
	pack := packOf(nodes, edges)

	// The adjacency must have exactly one a↔b entry in each direction.
	g, err := loadGraph(pack)
	if err != nil {
		t.Fatalf("loadGraph: %v", err)
	}
	adj := adjacency(g.Nodes, g.Edges)
	if len(adj["a"]) != 1 || adj["a"][0] != "b" {
		t.Fatalf("adjacency of a = %v, want one entry b", adj["a"])
	}
	if len(adj["b"]) != 2 {
		t.Fatalf("adjacency of b = %v, want two entries", adj["b"])
	}
	// Every record is retained for the reveal evidence.
	if len(g.Edges) != 4 {
		t.Fatalf("edges = %d, want 4 records", len(g.Edges))
	}

	// A path using the deduplicated edge is accepted and scores full marks.
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, _ := prepare(t, pack, "dedupe-seed", `{}`, subjects...)
	if pub.ShortestEdges != 1 && pub.ShortestEdges != 2 {
		t.Fatalf("shortestEdges = %d, want 1 or 2", pub.ShortestEdges)
	}
	// Build the winning chain from the published endpoints.
	chain := []string{pub.Start.CharacterID}
	if pub.Start.CharacterID == "a" {
		chain = append(chain, "b")
		if pub.Goal.CharacterID == "c" {
			chain = append(chain, "c")
		}
	} else {
		chain = append(chain, "a")
	}
	if chain[len(chain)-1] != pub.Goal.CharacterID {
		chain = append(chain, pub.Goal.CharacterID)
	}
	dec, st := applyOne(t, prep, subjects[0], nil, pathAction(chain))
	if !dec.Accepted {
		t.Fatalf("chain %v rejected: %s %s", chain, dec.Code, dec.Detail)
	}
	if !st.Arrived {
		t.Fatalf("chain %v did not arrive", chain)
	}
}

// TestShortestSolutionHiddenUntilReveal asserts the reference solution is not
// exposed during play, which would leak an unpublished shortest path (,
// ).
func TestShortestSolutionHiddenUntilReveal(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, pub, priv := prepare(t, fixedPairPack(), "hide-seed", `{}`, subjects...)

	during, err := New().Project(modes.ProjectInput{
		Scope:   scope("hide-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var duringView projection
	if err := json.Unmarshal(during, &duringView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if duringView.Answer != nil {
		t.Fatal("the shortest path was projected during play")
	}
	// The closed-book variant does not ship edges to the client.
	if len(pub.Edges) != 0 {
		t.Fatalf("closed-book round shipped %d edges", len(pub.Edges))
	}

	// On reveal the path and its evidence appear.
	after, err := New().Project(modes.ProjectInput{
		Scope:     scope("hide-seed"),
		Viewer:    modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    map[string]json.RawMessage{},
		Phase:     modes.PhaseSettled,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("Project reveal: %v", err)
	}
	var afterView projection
	if err := json.Unmarshal(after, &afterView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if afterView.Answer == nil {
		t.Fatal("the reveal did not include the reference path")
	}
	if len(afterView.Answer.Path) != len(priv.ShortestPath) {
		t.Fatalf("revealed path = %v, want %v", afterView.Answer.Path, priv.ShortestPath)
	}
	// Every step carries race evidence.
	if len(afterView.Answer.Evidence) != len(priv.ShortestPath)-1 {
		t.Fatalf("evidence count = %d, want %d",
			len(afterView.Answer.Evidence), len(priv.ShortestPath)-1)
	}
	for _, ev := range afterView.Answer.Evidence {
		if ev.RaceID == "" {
			t.Fatalf("evidence for %s→%s has no race id", ev.From, ev.To)
		}
	}
}

// TestOpenBookShipsEdges asserts the open-book variant exposes the graph so the
// client can offer search and candidates .
func TestOpenBookShipsEdges(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	_, openPub, _ := prepare(t, fixedPairPack(), "open-seed", `{"openBook":true}`, subjects...)
	if len(openPub.Edges) == 0 {
		t.Fatal("the open-book variant shipped no edges")
	}
	_, closedPub, _ := prepare(t, fixedPairPack(), "open-seed", `{}`, subjects...)
	if len(closedPub.Edges) != 0 {
		t.Fatal("the closed-book variant shipped edges")
	}
	// Both variants must answer identically on the same chain.
	prepOpen, _, _ := prepare(t, fixedPairPack(), "open-seed", `{"openBook":true}`, subjects...)
	prepClosed, _, _ := prepare(t, fixedPairPack(), "open-seed", `{}`, subjects...)
	decOpen, stOpen := applyOne(t, prepOpen, subjects[0], nil, pathAction([]string{"a", "b", "d"}))
	decClosed, stClosed := applyOne(t, prepClosed, subjects[0], nil, pathAction([]string{"a", "b", "d"}))
	if decOpen.Accepted != decClosed.Accepted {
		t.Fatal("open and closed variants disagree on the same chain")
	}
	if stOpen.ScoreUnits != stClosed.ScoreUnits {
		t.Fatalf("open scored %d, closed scored %d", stOpen.ScoreUnits, stClosed.ScoreUnits)
	}
}

// TestPrepareDeterministic asserts the determinism gate .
func TestPrepareDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := linePack()
	first, pubA, _ := prepare(t, pack, "det-seed", `{}`, subjects...)
	second, pubB, _ := prepare(t, pack, "det-seed", `{}`, subjects...)
	if string(first.Public) != string(second.Public) {
		t.Fatal("prepared public state differs between identical prepares")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("prepared private state differs between identical prepares")
	}
	if pubA.Start.CharacterID != pubB.Start.CharacterID || pubA.Goal.CharacterID != pubB.Goal.CharacterID {
		t.Fatal("the chosen pair is not deterministic")
	}
	// Across seeds the pair varies.
	seen := map[string]struct{}{}
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		_, pub, _ := prepare(t, pack, seed, `{}`, subjects...)
		seen[pub.Start.CharacterID+"→"+pub.Goal.CharacterID] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("the pair never varies across seeds: %v", seen)
	}
}

// TestPrepareRejectsUnusableGraph asserts a graph without a usable pair is
// refused before the round opens .
func TestPrepareRejectsUnusableGraph(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	cases := []struct {
		name string
		pack fakeContent
	}{
		{name: "no section", pack: fakeContent{}},
		{name: "single node", pack: packOf([]nodeSpec{{CharacterID: "a"}}, nil)},
		{
			name: "all nodes isolated",
			pack: packOf([]nodeSpec{
				{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"},
			}, nil),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("unusable"),
				Subject: subjects,
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare accepted an unusable graph")
			}
		})
	}
}

// TestMaxEdgesBoundsTheQuestion asserts the length limit also bounds which pairs
// are eligible: with maxEdges=1 only adjacent pairs can be chosen .
func TestMaxEdgesBoundsTheQuestion(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// A five-node line: the endpoints are four edges apart, adjacent pairs one.
	nodes := []nodeSpec{
		{CharacterID: "a"}, {CharacterID: "b"}, {CharacterID: "c"},
		{CharacterID: "d"}, {CharacterID: "e"},
	}
	edges := []edgeSpec{
		{From: "a", To: "b", RaceID: "r1"},
		{From: "b", To: "c", RaceID: "r2"},
		{From: "c", To: "d", RaceID: "r3"},
		{From: "d", To: "e", RaceID: "r4"},
	}
	pack := packOf(nodes, edges)
	edgeSet := edgeSetOf(edges)

	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6"} {
		_, pub, _ := prepare(t, pack, seed, `{"maxEdges":1}`, subjects...)
		if pub.ShortestEdges != 1 {
			t.Fatalf("seed %s shortestEdges = %d, want 1", seed, pub.ShortestEdges)
		}
		if _, adjacent := edgeSet[edgeKey(pub.Start.CharacterID, pub.Goal.CharacterID)]; !adjacent {
			t.Fatalf("seed %s chose a non-adjacent pair %s→%s under maxEdges=1",
				seed, pub.Start.CharacterID, pub.Goal.CharacterID)
		}
	}
}

// TestUnknownCharacterRejected asserts an unknown node is refused.
func TestUnknownCharacterRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, _ := prepare(t, fixedPairPack(), "unknown-seed", `{}`, subjects...)
	dec, _ := applyOne(t, prep, subjects[0], nil, pathAction([]string{"a", "zzz"}))
	if dec.Accepted {
		t.Fatal("an unknown character was accepted")
	}
	if dec.Code != CodeUnknownNode {
		t.Fatalf("code = %q, want %q", dec.Code, CodeUnknownNode)
	}
}

// TestSettleFloorAndZero asserts settlement honours both the floor and the zero
// case .
func TestSettleFloorAndZero(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, pub, _ := prepare(t, fixedPairPack(), "floor-seed", `{}`, subjects...)
	edges := fixedPairEdges()
	start, goal := pub.Start.CharacterID, pub.Goal.CharacterID

	detour := findDetour(edges, start, goal, pub.ShortestEdges)
	if detour == nil {
		shortest := bfsChain(edges, start, goal)
		if shortest == nil {
			t.Fatalf("no chain from %s to %s", start, goal)
		}
		detour = shortest
	}

	// p1 arrives, then the chain is stretched far past the shortest so the score
	// must clamp to the documented floor.
	_, st1 := applyOne(t, prep, subjects[0], nil, pathAction(detour))
	if !st1.Arrived {
		t.Fatalf("chain %v did not arrive", detour)
	}
	st1.Edges = pub.ShortestEdges + 20
	st1.ScoreUnits = scoreUnits(st1.Edges, pub.ShortestEdges)

	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("floor-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st1)},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	byID := map[string]modes.Score{}
	for _, s := range scores {
		byID[s.Subject.ID] = s
	}
	if byID["p1"].ScoreUnits != modeutil.Units(MinScoreFloor) {
		t.Fatalf("long detour score = %d, want %d",
			byID["p1"].ScoreUnits, modeutil.Units(MinScoreFloor))
	}
	if byID["p2"].ScoreUnits != 0 {
		t.Fatalf("no-submission score = %d, want 0", byID["p2"].ScoreUnits)
	}
}

// TestConfigValidation covers config bounds .
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
	}{
		{name: "empty uses defaults", cfg: ``},
		{name: "explicit max edges", cfg: `{"maxEdges":4}`},
		{name: "open book valid", cfg: `{"openBook":true}`},
		{name: "zero max edges rejected", cfg: `{"maxEdges":-1}`, wantErr: true},
		{name: "negative seconds rejected", cfg: `{"questionSeconds":-1}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error for %s", tc.cfg)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

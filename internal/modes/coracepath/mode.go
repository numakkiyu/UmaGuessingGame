// Package coracepath implements G05 同场之路 .
//
// Characters are graph nodes and an edge means the two prototypes appeared in the
// same raceId. A player submits a character chain from the start to the goal; the
// chain may not repeat a character and defaults to at most six edges. A legal
// chain that reaches the goal scores max(20, 100-10×(actualEdges-shortestEdges));
// anything else scores 0.
package coracepath

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier.
const ModeID = "co-race-path"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Graph and scoring parameters .
const (
	// DefaultMaxEdges is the default chain length limit.
	DefaultMaxEdges = 6
	// QuestionSeconds is the per-question window.
	QuestionSeconds = 180
	// MinScoreFloor is the lowest score a legal arrival can earn.
	MinScoreFloor = 20
	// EdgePenalty is subtracted per extra edge beyond the shortest path.
	EdgePenalty = 10
)

// Action kinds.
//
// The value is the contract discriminator (ModeAction → PathRoute), not an
// internal name .
const (
	// ActionPath submits a character chain from start to goal.
	ActionPath = "path.route"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionPath} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeUnknownNode    = "unknown_character"
	CodeBadStart       = "bad_start"
	CodeNoEdge         = "missing_edge"
	CodeRepeatedNode   = "repeated_character"
	CodeTooLong        = "too_long"
	CodeNotConnected   = "not_connected"
)

// Config is the round configuration.
type Config struct {
	// MaxEdges bounds the chain length; 0 means the documented default of 6.
	MaxEdges int `json:"maxEdges"`
	// QuestionSeconds is the per-question window.
	QuestionSeconds int `json:"questionSeconds"`
	// OpenBook reports whether the round supplies search and candidates.
	OpenBook bool `json:"openBook"`
}

// decodeConfig normalises the configuration.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{MaxEdges: DefaultMaxEdges, QuestionSeconds: QuestionSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("coracepath: invalid config: %w", err)
		}
		var form struct {
			InformationStyle string `json:"informationStyle"`
			OpenBook         *bool  `json:"openBook"`
		}
		if err := json.Unmarshal(raw, &form); err != nil {
			return Config{}, fmt.Errorf("coracepath: invalid information style: %w", err)
		}
		if form.InformationStyle != "" {
			if form.InformationStyle != "open" && form.InformationStyle != "closed" {
				return Config{}, fmt.Errorf("coracepath: unknown informationStyle")
			}
			open := form.InformationStyle == "open"
			if form.OpenBook != nil && *form.OpenBook != open {
				return Config{}, fmt.Errorf("coracepath: openBook conflicts with informationStyle")
			}
			cfg.OpenBook = open
		}
	}
	if cfg.MaxEdges == 0 {
		cfg.MaxEdges = DefaultMaxEdges
	}
	if cfg.MaxEdges < 1 || cfg.MaxEdges > DefaultMaxEdges {
		return Config{}, fmt.Errorf("coracepath: maxEdges %d must be between 1 and %d", cfg.MaxEdges, DefaultMaxEdges)
	}
	if cfg.QuestionSeconds == 0 {
		cfg.QuestionSeconds = QuestionSeconds
	}
	if cfg.QuestionSeconds < 0 {
		return Config{}, fmt.Errorf("coracepath: negative questionSeconds %d", cfg.QuestionSeconds)
	}
	return cfg, nil
}

// Node is one character in the graph.
type Node struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	ImagePath   string `json:"imagePath"`
}

// Edge is one evidenced same-race co-appearance. Edges are undirected: the
// documented rule is "corresponding prototypes appeared in the same raceId",
// which has no direction.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	RaceID   string `json:"raceId"`
	RaceName string `json:"raceName"`
	Date     string `json:"date"`
}

// key returns the canonical undirected pair key.
func edgeKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

// graph is the decoded pack section.
type graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// loadGraph reads the co-race-path section.
func loadGraph(src modes.ContentSource) (graph, error) {
	raw, err := modeutil.Section(src, "co-race-path.graph", "coRacePath.graph", "graph")
	if err != nil {
		return graph{}, err
	}
	fm, err := modeutil.DecodeFieldMap(raw)
	if err != nil {
		return graph{}, fmt.Errorf("coracepath: decode section: %w", err)
	}
	var out graph
	if nodeRecords, ok := fm.List("nodes", "characters"); ok {
		for _, rec := range nodeRecords {
			id := rec.Str("characterId", "id", "character_id")
			if id == "" {
				continue
			}
			out.Nodes = append(out.Nodes, Node{
				CharacterID: id,
				Name:        rec.Str("name", "nameZh", "name_zh"),
				ImagePath:   rec.Str("imagePath", "image_path", "image_local_path"),
			})
		}
	}
	if edgeRecords, ok := fm.List("edges"); ok {
		for _, rec := range edgeRecords {
			from := rec.Str("from", "a", "source")
			to := rec.Str("to", "b", "target")
			if from == "" || to == "" || from == to {
				continue
			}
			out.Edges = append(out.Edges, Edge{
				From:     from,
				To:       to,
				RaceID:   rec.Str("raceId", "race_id"),
				RaceName: rec.Str("raceName", "race_name"),
				Date:     rec.Str("date", "raceDate", "race_date"),
			})
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].CharacterID < out.Nodes[j].CharacterID })
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].From != out.Edges[j].From {
			return out.Edges[i].From < out.Edges[j].From
		}
		if out.Edges[i].To != out.Edges[j].To {
			return out.Edges[i].To < out.Edges[j].To
		}
		return out.Edges[i].RaceID < out.Edges[j].RaceID
	})
	return out, nil
}

// adjacency builds the undirected neighbour lists in sorted order so every
// traversal is deterministic.
func adjacency(nodes []Node, edges []Edge) map[string][]string {
	adj := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		adj[node.CharacterID] = nil
	}
	seen := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		key := edgeKey(edge.From, edge.To)
		if _, dup := seen[key]; dup {
			// Duplicate same-race evidence collapses to one adjacency entry, but
			// the evidence list keeps every record for the reveal.
			continue
		}
		seen[key] = struct{}{}
		adj[edge.From] = append(adj[edge.From], edge.To)
		adj[edge.To] = append(adj[edge.To], edge.From)
	}
	for id := range adj {
		sort.Strings(adj[id])
	}
	return adj
}

// bfsDistances returns the shortest edge distance from source to every reachable
// node. Unreachable nodes are absent from the map.
func bfsDistances(adj map[string][]string, source string) map[string]int {
	dist := map[string]int{source: 0}
	queue := []string{source}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adj[current] {
			if _, seen := dist[next]; seen {
				continue
			}
			dist[next] = dist[current] + 1
			queue = append(queue, next)
		}
	}
	return dist
}

// connectedComponents returns a component id per node, so a question can only be
// built from a start and goal inside one connected component .
func connectedComponents(nodes []Node, adj map[string][]string) map[string]int {
	component := make(map[string]int, len(nodes))
	next := 0
	// Iterate nodes in sorted order so component ids are stable.
	for _, node := range nodes {
		if _, done := component[node.CharacterID]; done {
			continue
		}
		queue := []string{node.CharacterID}
		component[node.CharacterID] = next
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, neighbor := range adj[current] {
				if _, done := component[neighbor]; done {
					continue
				}
				component[neighbor] = next
				queue = append(queue, neighbor)
			}
		}
		next++
	}
	return component
}

// publicPrepared is the non-secret prepared state.
type publicPrepared struct {
	ModeID       string `json:"modeId"`
	RulesVersion string `json:"rulesVersion"`
	Config       Config `json:"config"`
	Start        Node   `json:"start"`
	Goal         Node   `json:"goal"`
	// ShortestEdges is the number of edges on a shortest path. It is public
	// because the score formula is published, but the shortest path itself is
	// not: forbids leaking an unpublished shortest solution.
	ShortestEdges int `json:"shortestEdges"`
	// Nodes and Edges are the searchable material. In the open-book variant the
	// client gets the full graph; in the closed-book variant it gets only the
	// nodes so it can render names.
	Nodes    []Node        `json:"nodes"`
	Edges    []Edge        `json:"edges,omitempty"`
	Subjects []rosterEntry `json:"subjects"`
}

// rosterEntry is one participant.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// privatePrepared carries the reference solution and the full edge set, kept
// server-side.
//
// The edge set lives here as well as in the public payload because the
// closed-book variant does not ship edges to the client, and Apply must still
// validate every step against the same evidence. Both variants therefore
// validate identically while only the open-book variant exposes the graph.
type privatePrepared struct {
	// ShortestPath is one shortest chain. It is never projected before reveal.
	ShortestPath []string `json:"shortestPath"`
	// Edges is the complete evidenced edge set of this round's graph.
	Edges []Edge `json:"edges"`
}

// state is one subject's participant state.
type state struct {
	Path []string `json:"path"`
	// Revisions counts accepted submissions.
	Revisions int `json:"revisions"`
	// Edges is the submitted chain length in edges.
	Edges int `json:"edges"`
	// Arrived reports whether the last accepted chain reached the goal.
	Arrived bool `json:"arrived"`
	// Submitted reports whether any chain was accepted.
	Submitted bool `json:"submitted"`
	// ScoreUnits is the last accepted chain's score.
	ScoreUnits int `json:"scoreUnits"`
}

// Mode implements modes.Mode for G05.
type Mode struct{}

// New returns the co-race-path mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "同场之路" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "用同场参赛关系把起点角色连到终点角色，链越短分越高。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"open", "closed"}
}

// RoundWindow implements modes.Mode.
//
// 本模式没有题数配置，按单题窗口计。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.QuestionSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return 1 }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 8 }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields: []string{"characterId", "raceId", "edges"},
		// One usable path graph is enough to pose a round; the player count is enforced
		// by the room, not here. This used to be 2, which conflated the two.
		MinEntities: 1,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode.
//
// The start and goal are chosen inside one connected component, never at random
// across the whole graph, because the graph is not fully connected .
func (*Mode) Prepare(ctx context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	if err := in.Scope.Validate(); err != nil {
		return modes.Prepared{}, err
	}
	cfg, err := decodeConfig(in.Config)
	if err != nil {
		return modes.Prepared{}, err
	}
	if err := ctx.Err(); err != nil {
		return modes.Prepared{}, err
	}
	g, err := loadGraph(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if len(g.Nodes) < 2 {
		return modes.Prepared{}, fmt.Errorf("coracepath: graph has %d nodes, need at least 2", len(g.Nodes))
	}
	adj := adjacency(g.Nodes, g.Edges)

	// Build the candidate (start, goal) pairs from each connected component with
	// at least two nodes, then pick one deterministically from the seed.
	type pair struct{ start, goal string }
	candidates := make([]pair, 0, len(g.Nodes))
	component := connectedComponents(g.Nodes, adj)
	byComponent := make(map[int][]string, 8)
	for _, node := range g.Nodes {
		id := node.CharacterID
		if len(adj[id]) == 0 {
			// An isolated node can never be part of a question.
			continue
		}
		byComponent[component[id]] = append(byComponent[component[id]], id)
	}
	// Iterate component ids in ascending order for determinism.
	compIDs := make([]int, 0, len(byComponent))
	for id := range byComponent {
		compIDs = append(compIDs, id)
	}
	sort.Ints(compIDs)
	for _, compID := range compIDs {
		members := byComponent[compID]
		sort.Strings(members)
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				dist := bfsDistances(adj, members[i])
				d, ok := dist[members[j]]
				if !ok || d < 1 {
					continue
				}
				// The chain must be solvable within the length limit.
				if d > cfg.MaxEdges {
					continue
				}
				candidates = append(candidates, pair{start: members[i], goal: members[j]})
			}
		}
	}
	if len(candidates) == 0 {
		return modes.Prepared{}, fmt.Errorf(
			"coracepath: no start/goal pair is connected within %d edges", cfg.MaxEdges,
		)
	}
	chosen := candidates[modeutil.PickIndex(in.Scope.Seed, "coracepath.pair", len(candidates))]

	nodeByID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		nodeByID[node.CharacterID] = node
	}
	dist := bfsDistances(adj, chosen.start)
	shortest := dist[chosen.goal]
	shortestPath := shortestChain(adj, chosen.start, chosen.goal)

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("coracepath: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < 1 || len(roster) > 8 {
		return modes.Prepared{}, fmt.Errorf("coracepath: %d subjects outside [1,8]", len(roster))
	}

	pub := publicPrepared{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Config:        cfg,
		Start:         nodeByID[chosen.start],
		Goal:          nodeByID[chosen.goal],
		ShortestEdges: shortest,
		Nodes:         g.Nodes,
		Subjects:      roster,
	}
	// The closed-book variant hides the edge list; the open-book variant ships
	// it so the client can offer search and candidates .
	if cfg.OpenBook {
		pub.Edges = g.Edges
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(privatePrepared{ShortestPath: shortestPath, Edges: g.Edges}),
		QuestionCount: 1,
	}, nil
}

// shortestChain returns one shortest chain from source to goal. Neighbours are
// sorted, so the chain is deterministic.
func shortestChain(adj map[string][]string, source, goal string) []string {
	if source == goal {
		return []string{source}
	}
	prev := map[string]string{source: ""}
	queue := []string{source}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == goal {
			break
		}
		for _, next := range adj[current] {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = current
			queue = append(queue, next)
		}
	}
	if _, ok := prev[goal]; !ok {
		return nil
	}
	chain := []string{goal}
	for current := goal; current != source; {
		current = prev[current]
		chain = append(chain, current)
	}
	// Reverse into source→goal order.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// pathPayload is the body of ActionPath, matching the contract's PathRoute schema:
// {kind, characterIds[]}.
type pathPayload struct {
	Kind         string   `json:"kind"`
	CharacterIDs []string `json:"characterIds"`
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionPath {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("coracepath: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload pathPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"coracepath: path payload must carry a characterIds array"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionPath {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"coracepath: payload kind %q does not match action %q", payload.Kind, ActionPath,
		)), nil
	}

	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return modes.Decision{}, fmt.Errorf("coracepath: decode public state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("coracepath: decode subject state: %w", err)
		}
	}

	// The full edge set always comes from the private blob: Apply must not do
	// I/O, and the closed-book variant does not ship edges publicly.
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return modes.Decision{}, fmt.Errorf("coracepath: decode private state: %w", err)
		}
	}

	known := make(map[string]struct{}, len(pub.Nodes))
	for _, node := range pub.Nodes {
		known[node.CharacterID] = struct{}{}
	}
	// Every node in the submitted chain must be a real character.
	for _, id := range payload.CharacterIDs {
		if _, ok := known[id]; !ok {
			return modes.Rejected(CodeUnknownNode,
				fmt.Sprintf("coracepath: character %q is not in this round's graph", id)), nil
		}
	}
	// The chain must start at the start and end at the goal. A start equal to
	// the goal is refused by construction (Prepare never builds such a pair).
	if len(payload.CharacterIDs) == 0 || payload.CharacterIDs[0] != pub.Start.CharacterID {
		return modes.Rejected(CodeBadStart, fmt.Sprintf(
			"coracepath: the chain must start at %q", pub.Start.CharacterID,
		)), nil
	}
	// A repeated character is refused: forbids repeating nodes, which also
	// rules out cycles.
	seenNodes := make(map[string]int, len(payload.CharacterIDs))
	for i, id := range payload.CharacterIDs {
		if first, dup := seenNodes[id]; dup {
			return modes.Rejected(CodeRepeatedNode, fmt.Sprintf(
				"coracepath: character %q appears at positions %d and %d", id, first, i,
			)), nil
		}
		seenNodes[id] = i
	}

	edgesUsed := len(payload.CharacterIDs) - 1
	if edgesUsed > pub.Config.MaxEdges {
		return modes.Rejected(CodeTooLong, fmt.Sprintf(
			"coracepath: chain uses %d edges, over the limit of %d", edgesUsed, pub.Config.MaxEdges,
		)), nil
	}

	// Validate every consecutive pair against the round's evidenced edge set.
	edgeSet := make(map[string]struct{}, len(priv.Edges))
	for _, edge := range priv.Edges {
		edgeSet[edgeKey(edge.From, edge.To)] = struct{}{}
	}
	for i := 0; i+1 < len(payload.CharacterIDs); i++ {
		if _, ok := edgeSet[edgeKey(payload.CharacterIDs[i], payload.CharacterIDs[i+1])]; !ok {
			// The feedback says which step lacks evidence without revealing the
			// shortest solution .
			decision := modes.Rejected(CodeNoEdge, fmt.Sprintf(
				"coracepath: step %d (%q → %q) has no same-race evidence in this pack",
				i+1, payload.CharacterIDs[i], payload.CharacterIDs[i+1],
			))
			decision.Issues = []modes.ActionIssue{{Field: fmt.Sprintf("modeAction.characterIds[%d]", i+1), Code: CodeNoEdge}}
			return decision, nil
		}
	}

	arrived := payload.CharacterIDs[len(payload.CharacterIDs)-1] == pub.Goal.CharacterID
	units := 0
	if arrived {
		units = scoreUnits(edgesUsed, pub.ShortestEdges)
	}

	st.Path = payload.CharacterIDs
	st.Revisions++
	st.Edges = edgesUsed
	st.Arrived = arrived
	st.Submitted = true
	st.ScoreUnits = units

	feedback := modeutil.Marshal(struct {
		Edges         int  `json:"edges"`
		ShortestEdges int  `json:"shortestEdges"`
		Arrived       bool `json:"arrived"`
		ScoreUnits    int  `json:"scoreUnits"`
		Revisions     int  `json:"revisions"`
	}{
		Edges:         edgesUsed,
		ShortestEdges: pub.ShortestEdges,
		Arrived:       arrived,
		ScoreUnits:    units,
		Revisions:     st.Revisions,
	})

	// Revisions are allowed until the deadline .
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// scoreUnits computes max(20, 100-10×(actual-shortest)) display points in units
// .
func scoreUnits(actual, shortest int) int {
	display := 100 - EdgePenalty*(actual-shortest)
	if display < MinScoreFloor {
		display = MinScoreFloor
	}
	return modeutil.Units(display)
}

// projection is the viewer-specific state.
type projection struct {
	ModeID        string        `json:"modeId"`
	RulesVersion  string        `json:"rulesVersion"`
	Phase         modes.Phase   `json:"phase"`
	Config        Config        `json:"config"`
	Start         Node          `json:"start"`
	Goal          Node          `json:"goal"`
	ShortestEdges int           `json:"shortestEdges"`
	Nodes         []Node        `json:"nodes"`
	Edges         []Edge        `json:"edges,omitempty"`
	Subjects      []progress    `json:"subjects"`
	Self          *selfPath     `json:"self,omitempty"`
	Answer        *answerReveal `json:"answer,omitempty"`
}

// progress is the coarse per-subject progress.
type progress struct {
	SubjectID  string `json:"subjectId"`
	Submitted  bool   `json:"submitted"`
	Revisions  int    `json:"revisions"`
	Edges      int    `json:"edges"`
	Arrived    bool   `json:"arrived"`
	ScoreUnits int    `json:"scoreUnits"`
}

// selfPath is the caller's own state.
type selfPath struct {
	SubjectID  string         `json:"subjectId"`
	Path       []string       `json:"path"`
	Submitted  bool           `json:"submitted"`
	Revisions  int            `json:"revisions"`
	Edges      int            `json:"edges"`
	Arrived    bool           `json:"arrived"`
	ScoreUnits int            `json:"scoreUnits"`
	Evidence   []EdgeEvidence `json:"evidence,omitempty"`
}

// answerReveal is the reference solution with its per-edge evidence. It is
// released only on reveal .
type answerReveal struct {
	Path     []string       `json:"path"`
	Evidence []EdgeEvidence `json:"evidence"`
}

// EdgeEvidence is the race evidence for one step of the reference solution.
type EdgeEvidence struct {
	From     string `json:"from"`
	To       string `json:"to"`
	RaceID   string `json:"raceId"`
	RaceName string `json:"raceName"`
	Date     string `json:"date"`
}

// Project implements modes.Mode.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("coracepath: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, fmt.Errorf("coracepath: decode private state: %w", err)
		}
	}

	out := projection{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Phase:         in.Phase,
		Config:        pub.Config,
		Start:         pub.Start,
		Goal:          pub.Goal,
		ShortestEdges: pub.ShortestEdges,
		Nodes:         pub.Nodes,
		Edges:         pub.Edges,
		Subjects:      make([]progress, 0, len(pub.Subjects)),
	}
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("coracepath: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		out.Subjects = append(out.Subjects, progress{
			SubjectID:  entry.SubjectID,
			Submitted:  st.Submitted,
			Revisions:  st.Revisions,
			Edges:      st.Edges,
			Arrived:    st.Arrived,
			ScoreUnits: st.ScoreUnits,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfPath{
				SubjectID:  entry.SubjectID,
				Path:       st.Path,
				Submitted:  st.Submitted,
				Revisions:  st.Revisions,
				Edges:      st.Edges,
				Arrived:    st.Arrived,
				ScoreUnits: st.ScoreUnits,
			}
		}
	}

	// The shortest path and its evidence stay secret until reveal, so an
	// unpublished solution never leaks early .
	if in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal()) {
		reveal := &answerReveal{Path: priv.ShortestPath}
		reveal.Evidence = routeEvidence(priv.ShortestPath, priv.Edges)
		if out.Self != nil {
			out.Self.Evidence = routeEvidence(out.Self.Path, priv.Edges)
		}
		out.Answer = reveal
	}
	return modeutil.Marshal(out), nil
}

func routeEvidence(path []string, edges []Edge) []EdgeEvidence {
	var evidence []EdgeEvidence
	for i := 0; i+1 < len(path); i++ {
		from, to := path[i], path[i+1]
		for _, edge := range edges {
			if edgeKey(edge.From, edge.To) == edgeKey(from, to) {
				evidence = append(evidence, EdgeEvidence{From: from, To: to, RaceID: edge.RaceID, RaceName: edge.RaceName, Date: edge.Date})
				break
			}
		}
	}
	return evidence
}

// Settle implements modes.Mode.
//
// A legal arrival scores max(20, 100-10×(actual-shortest)); anything else is 0
// . Ties share a rank.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("coracepath: decode public state: %w", err)
		}
	}
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("coracepath: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		units := 0
		if st.Submitted && st.Arrived {
			units = scoreUnits(st.Edges, pub.ShortestEdges)
		}
		explanation := modeutil.Marshal(struct {
			Submitted     bool `json:"submitted"`
			Arrived       bool `json:"arrived"`
			Edges         int  `json:"edges"`
			ShortestEdges int  `json:"shortestEdges"`
			Revisions     int  `json:"revisions"`
		}{
			Submitted:     st.Submitted,
			Arrived:       st.Arrived,
			Edges:         st.Edges,
			ShortestEdges: pub.ShortestEdges,
			Revisions:     st.Revisions,
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: modeutil.Units(100),
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}

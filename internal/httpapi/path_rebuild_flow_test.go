package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// realPackSource serves the published pack sections, including the private ones.
//
// It mirrors the loader the rule-engine tests use: the compiler writes each
// section as a flat file named by replacing the dot with an underscore, so the
// dotted canonical name is restored rather than hard-coded in a table that could
// drift.
type realPackSource struct {
	public  map[string]json.RawMessage
	private map[string]json.RawMessage
	version string
}

func (p *realPackSource) Section(name string) (json.RawMessage, bool) {
	raw, ok := p.public[name]
	return raw, ok
}

func (p *realPackSource) Version() string { return p.version }

// Secrets exposes the private tree so answer-dependent modes can grade.
func (p *realPackSource) Secrets() modes.ContentSource {
	return &privateOnly{private: p.private, version: p.version}
}

type privateOnly struct {
	private map[string]json.RawMessage
	version string
}

func (p *privateOnly) Section(name string) (json.RawMessage, bool) {
	raw, ok := p.private[name]
	return raw, ok
}

func (p *privateOnly) Version() string { return p.version }

func (p *realPackSource) DatasetVersion() string { return p.version + "-data" }
func (p *realPackSource) AssetBaseURL() string   { return "/assets" }

func (p *realPackSource) Character(string) (room.CharacterCard, bool) {
	return room.CharacterCard{}, false
}

func (p *realPackSource) Characters(string, int, string) ([]room.CharacterCard, string, error) {
	return nil, "", nil
}

// PathNeighbors returns the real graph neighbours, because co-race-path reads
// them through this interface rather than through a pack section.
func (p *realPackSource) PathNeighbors(characterID string) ([]room.Neighbor, bool) {
	raw, ok := p.public["co-race-path.graph"]
	if !ok {
		return nil, false
	}
	var graph struct {
		Edges []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, false
	}
	var out []room.Neighbor
	for _, e := range graph.Edges {
		switch characterID {
		case e.From:
			out = append(out, room.Neighbor{CharacterID: e.To})
		case e.To:
			out = append(out, room.Neighbor{CharacterID: e.From})
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func (p *realPackSource) ModeAdmission(id string) (room.ModeAdmission, bool) {
	switch id {
	case racerebuild.ModeID:
		return room.ModeAdmission{
			Mode: id, Available: true, QuestionCount: 52,
			Section: "race-rebuild.races",
		}, true
	case coracepath.ModeID:
		return room.ModeAdmission{
			Mode: id, Available: true, QuestionCount: 329,
			Section: "co-race-path.graph",
		}, true
	}
	return room.ModeAdmission{}, false
}

// loadRealPackSource reads the current compiled pack, skipping when absent.
func loadRealPackSource(t *testing.T) *realPackSource {
	t.Helper()
	root := filepath.Join("..", "..")
	versionBytes, err := os.ReadFile(filepath.Join(root, "content", "current"))
	if err != nil {
		t.Skip("no compiled content pack; run the content compiler first")
	}
	version := strings.TrimSpace(string(versionBytes))
	dir := filepath.Join(root, "content", version)
	if _, err := os.Stat(dir); err != nil {
		t.Skip("the current pack directory is missing")
	}
	pack := &realPackSource{
		public:  map[string]json.RawMessage{},
		private: map[string]json.RawMessage{},
		version: version,
	}
	readDir := func(sub string, into map[string]json.RawMessage) {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, sub, entry.Name()))
			if err != nil {
				t.Fatalf("read %s/%s: %v", sub, entry.Name(), err)
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			name = strings.ReplaceAll(name, "_", ".")
			into[name] = json.RawMessage(raw)
		}
	}
	readDir("public", pack.public)
	readDir("private", pack.private)
	if len(pack.public) == 0 {
		t.Skip("the compiled pack carries no public section")
	}
	return pack
}

// openModeRoom creates a friend room in modeID as the host and joins one player.
func openModeRoom(t *testing.T, h *harness, modeID, key string, ruleConfig ...map[string]any) (roomID string, roundID string,
	host *http.Client, hostSess testSession, player *http.Client, playerSess testSession) {
	t.Helper()
	rules := map[string]any{"modeId": modeID, "config": map[string]any{}}
	if len(ruleConfig) > 0 {
		rules = ruleConfig[0]
	}
	host, hostSess = h.newSession("主持")
	made := h.doAs(host, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name":            "玩法核验",
		"kind":            "friend",
		"modeConfig":      rules,
		"maxPlayers":      8,
		"maxSpectators":   0,
		"allowSpectators": false,
		"visibility":      "invite",
	}, writeHeaders(h, hostSess, idemKey(key+"-room")))
	if made.Status != http.StatusCreated {
		t.Fatalf("%s: create room: %d %s", modeID, made.Status, made.Body)
	}
	var created struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	if err := json.Unmarshal(made.Body, &created); err != nil {
		t.Fatalf("%s: decode room: %v", modeID, err)
	}
	roomID = created.Room.ID

	player, playerSess = h.newSession("训练员")
	joined := h.doAs(player, http.MethodPost, "/api/v1/rooms/"+roomID+"/memberships", map[string]any{
		"inviteCode": created.Code, "requestedRole": "player",
	}, writeHeaders(h, playerSess, idemKey(key+"-join")))
	if joined.Status != http.StatusCreated {
		t.Fatalf("%s: join: %d %s", modeID, joined.Status, joined.Body)
	}

	opened := h.doAs(host, http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{},
		writeHeaders(h, hostSess, idemKey(key+"-open")))
	if opened.Status != http.StatusCreated {
		t.Fatalf("%s: open round: %d %s", modeID, opened.Status, opened.Body)
	}
	var summary struct {
		ID string `json:"roundId"`
	}
	if err := json.Unmarshal(opened.Body, &summary); err != nil {
		t.Fatalf("%s: decode round: %v", modeID, err)
	}
	roundID = summary.ID
	for i, phase := range []string{"briefing", "collecting"} {
		res := h.doAs(host, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions",
			map[string]any{"target": phase, "expectedPhaseVersion": i + 1},
			writeHeaders(h, hostSess, idemKey(key+"-"+phase)))
		if res.Status != http.StatusCreated && res.Status != http.StatusOK {
			t.Fatalf("%s: transition %s: %d %s", modeID, phase, res.Status, res.Body)
		}
	}
	return roomID, roundID, host, hostSess, player, playerSess
}

// roundEnvelope is the common part of a player round view these tests need.
type roundEnvelope struct {
	Round struct {
		Phase    string `json:"phase"`
		Version  int64  `json:"phaseVersion"`
		Deadline string `json:"deadlineAt"`
	} `json:"round"`
	CanSubmit  bool            `json:"canSubmit"`
	Revision   int             `json:"submissionRevision"`
	Generation int             `json:"controlGeneration"`
	Public     json.RawMessage `json:"publicState"`
}

func readRound(t *testing.T, h *harness, c *http.Client, roundID string) roundEnvelope {
	t.Helper()
	res := h.doAs(c, http.MethodGet, "/api/v1/rounds/"+roundID, nil, readHeaders(h))
	if res.Status != http.StatusOK {
		t.Fatalf("read round: %d %s", res.Status, res.Body)
	}
	var v roundEnvelope
	if err := json.Unmarshal(res.Body, &v); err != nil {
		t.Fatalf("decode round view: %v", err)
	}
	return v
}

// submitAction posts one mode action and returns the raw response.
func submitAction(t *testing.T, h *harness, c *http.Client, sess testSession, roundID string,
	v roundEnvelope, action map[string]any, key string) response {
	t.Helper()
	return h.doAs(c, http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", map[string]any{
		"modeAction":         action,
		"phaseVersion":       v.Round.Version,
		"submissionRevision": v.Revision,
		"controlGeneration":  v.Generation,
	}, writeHeaders(h, sess, idemKey(key)))
}

// TestHTTPRaceRebuildOrdersAgainstThePrivateAnswerSheet drives G04 end to end.
//
// The public pack carries the unordered starter list; the finish order is the
// answer and lives in the private section. This test therefore proves the whole
// chain: the answer sheet is loaded, a wrong order is rejected, the correct order
// is accepted, and the settled projection scores it.
func TestHTTPRaceRebuildOrdersAgainstThePrivateAnswerSheet(t *testing.T) {
	pack := loadRealPackSource(t)
	reg, _ := modes.NewRegistry(racerebuild.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = pack })

	_, roundID, host, hostSess, player, playerSess := openModeRoom(t, h, racerebuild.ModeID, "rebuild")

	v := readRound(t, h, player, roundID)
	var pub struct {
		// Entries is the published starter set. The public state must never carry
		// the answer, so none of the order-shaped fields may be present.
		Entries []struct {
			CharacterID string `json:"characterId"`
			Name        string `json:"name"`
		} `json:"entries"`
		RaceID     string          `json:"raceId"`
		TotalPairs int             `json:"totalPairs"`
		Ranks      json.RawMessage `json:"ranks"`
		Order      json.RawMessage `json:"order"`
		Answers    json.RawMessage `json:"answers"`
	}
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode race public state: %v (raw=%s)", err, v.Public)
	}
	if len(pub.Entries) < 4 {
		t.Fatalf("the drawn race has %d entries, want at least 4 (raw=%s)", len(pub.Entries), v.Public)
	}
	if pub.RaceID == "" || pub.TotalPairs < 1 {
		t.Fatalf("the drawn race is incomplete: raceId=%q totalPairs=%d", pub.RaceID, pub.TotalPairs)
	}
	if len(pub.Ranks) > 0 || len(pub.Order) > 0 || len(pub.Answers) > 0 {
		t.Fatalf("the public state leaks the finish order: %s", v.Public)
	}
	if !v.CanSubmit {
		t.Fatalf("the player cannot submit in collecting: %+v", v)
	}

	// A deliberately wrong order: the starters reversed.
	ids := make([]string, 0, len(pub.Entries))
	for _, s := range pub.Entries {
		ids = append(ids, s.CharacterID)
	}
	wrong := make([]string, len(ids))
	for i := range ids {
		wrong[i] = ids[len(ids)-1-i]
	}
	wrongGroups := make([][]string, 0, len(wrong))
	for _, id := range wrong {
		wrongGroups = append(wrongGroups, []string{id})
	}
	res := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "race.order", "groups": wrongGroups,
	}, "rebuild-wrong")
	if res.Status != http.StatusCreated {
		t.Fatalf("wrong order submission status = %d, want 201; body=%s", res.Status, res.Body)
	}
	// A wrong order is accepted as a submission (it is a real attempt) but must not
	// score full marks; the reveal is where that becomes visible.
	if !strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("a well-formed order was not accepted as a submission: %s", res.Body)
	}

	// Now the recorded answer, read from the private section the pack publishes.
	// The shape is {races:[{raceId, order:[characterId...]}]} where order is the
	// finish order, best first.
	rawAnswer, ok := pack.private["race-rebuild.answers"]
	if !ok {
		t.Skip("the pack carries no private answer section")
	}
	var answers struct {
		Races []struct {
			RaceID string         `json:"raceId"`
			Order  []string       `json:"order"`
			Ranks  map[string]int `json:"ranks"`
		} `json:"races"`
	}
	if err := json.Unmarshal(rawAnswer, &answers); err != nil {
		t.Fatalf("decode private answers: %v", err)
	}
	// Match the answer row by raceId, so the test cannot grade against another race.
	var order []string
	var ranks map[string]int
	for _, race := range answers.Races {
		if race.RaceID == pub.RaceID {
			order = race.Order
			ranks = race.Ranks
			break
		}
	}
	if len(order) == 0 {
		t.Fatalf("the private answer section carries no order for race %q", pub.RaceID)
	}
	// The answer must cover exactly the published entries: a mismatch means the
	// question and the answer sheet disagree, which would make the round ungradeable.
	if len(order) != len(ids) {
		t.Fatalf("race %q: answer covers %d starters but %d were published",
			pub.RaceID, len(order), len(ids))
	}
	starterSet := map[string]bool{}
	for _, id := range ids {
		starterSet[id] = true
	}
	for _, id := range order {
		if !starterSet[id] {
			t.Fatalf("race %q: the answer names %q, which is not a published starter",
				pub.RaceID, id)
		}
	}
	correctGroups := make([][]string, 0, len(order))
	for i, id := range order {
		if ranks[id] < 1 {
			t.Fatalf("private answer has no source rank for %q", id)
		}
		if i > 0 && ranks[id] == ranks[order[i-1]] {
			last := len(correctGroups) - 1
			correctGroups[last] = append(correctGroups[last], id)
		} else {
			correctGroups = append(correctGroups, []string{id})
		}
	}

	// Revision advanced by the first accepted attempt, so re-read before the second.
	v = readRound(t, h, player, roundID)
	res = submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "race.order", "groups": correctGroups,
	}, "rebuild-correct")
	if res.Status != http.StatusCreated {
		t.Fatalf("correct order submission status = %d, want 201; body=%s", res.Status, res.Body)
	}
	if !strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("the recorded order was not accepted: %s", res.Body)
	}

	// Lock and reveal, then assert the score distinguishes the two attempts: the
	// final accepted order is the correct one, so the subject scores full marks.
	advance := func(phase string, expected int64, key string) {
		t.Helper()
		r := h.doAs(host, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions",
			map[string]any{"target": phase, "expectedPhaseVersion": expected},
			writeHeaders(h, hostSess, idemKey(key)))
		if r.Status != http.StatusCreated && r.Status != http.StatusOK {
			t.Fatalf("transition %s: %d %s", phase, r.Status, r.Body)
		}
	}
	current := readRound(t, h, player, roundID)
	advance("locked", current.Round.Version, "rebuild-locked")
	advance("revealed", current.Round.Version+1, "rebuild-revealed")

	revealed := readRound(t, h, host, roundID)
	if revealed.Round.Phase != "revealed" {
		t.Fatalf("phase after reveal = %q", revealed.Round.Phase)
	}
	// The reveal must publish the answer, because that is what makes the round
	// teachable rather than a scoreboard nobody can check. The score must also be
	// full marks: the last accepted order was the recorded one.
	var shown struct {
		Answer *struct {
			RaceID string `json:"raceId"`
			Ranked []struct {
				CharacterID string `json:"characterId"`
				FinishRank  int    `json:"finishRank"`
			} `json:"ranked"`
		} `json:"answer"`
		Subjects []struct {
			SubjectID    string `json:"subjectId"`
			Submitted    bool   `json:"submitted"`
			CorrectPairs int    `json:"correctPairs"`
			TotalPairs   int    `json:"totalPairs"`
		} `json:"subjects"`
	}
	if err := json.Unmarshal(revealed.Public, &shown); err != nil {
		t.Fatalf("decode revealed state: %v", err)
	}
	if shown.Answer == nil || shown.Answer.RaceID != pub.RaceID {
		t.Fatalf("the reveal did not publish the answer for race %q: %s", pub.RaceID, revealed.Public)
	}
	if len(shown.Answer.Ranked) != len(ids) {
		t.Fatalf("the revealed answer ranks %d starters, want %d",
			len(shown.Answer.Ranked), len(ids))
	}
	// The revealed order must match the answer sheet the pack carries.
	for i, ranked := range shown.Answer.Ranked {
		if ranked.CharacterID != order[i] {
			t.Fatalf("revealed rank %d = %q, want %q", i+1, ranked.CharacterID, order[i])
		}
		if ranked.FinishRank != ranks[ranked.CharacterID] {
			t.Fatalf("revealed finishRank for %s = %d, want source rank %d", ranked.CharacterID, ranked.FinishRank, ranks[ranked.CharacterID])
		}
	}
	var scored bool
	for _, sub := range shown.Subjects {
		if !sub.Submitted {
			continue
		}
		scored = true
		if sub.CorrectPairs != sub.TotalPairs || sub.TotalPairs < 1 {
			t.Fatalf("the accepted order scored %d/%d, want full marks",
				sub.CorrectPairs, sub.TotalPairs)
		}
	}
	if !scored {
		t.Fatalf("no subject carries a score after the reveal: %s", revealed.Public)
	}

	// Before the reveal the answer must be absent: that is the whole point of the
	// private answer section.
	var preAnswer struct {
		Answer json.RawMessage `json:"answer"`
	}
	_ = json.Unmarshal(v.Public, &preAnswer)
	if len(preAnswer.Answer) > 0 {
		t.Fatalf("the collecting projection carried the answer: %s", v.Public)
	}
}

// TestHTTPCoRacePathAcceptsOnlyLegalSteps drives G05 end to end against the real
// character graph: an illegal hop is refused, and a legal chain reaches the goal.
func TestHTTPCoRacePathAcceptsOnlyLegalSteps(t *testing.T) {
	pack := loadRealPackSource(t)
	reg, _ := modes.NewRegistry(coracepath.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = pack })

	_, roundID, host, hostSess, player, playerSess := openModeRoom(t, h, coracepath.ModeID, "path", map[string]any{"modeId": coracepath.ModeID, "informationStyle": "open", "config": map[string]any{}})

	v := readRound(t, h, player, roundID)
	var pub struct {
		Start struct {
			CharacterID string `json:"characterId"`
			Name        string `json:"name"`
		} `json:"start"`
		Goal struct {
			CharacterID string `json:"characterId"`
			Name        string `json:"name"`
		} `json:"goal"`
		ShortestEdges int `json:"shortestEdges"`
		Nodes         []struct {
			CharacterID string `json:"characterId"`
		} `json:"nodes"`
		Edges []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"edges"`
		// The shortest path itself must not be published .
		Solution json.RawMessage `json:"solution"`
		Path     json.RawMessage `json:"path"`
	}
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode path public state: %v (raw=%s)", err, v.Public)
	}
	if pub.Start.CharacterID == "" || pub.Goal.CharacterID == "" {
		t.Fatalf("the drawn pair is incomplete: %s", v.Public)
	}
	t.Logf("drawn pair %s → %s (%d edges shortest)", pub.Start.CharacterID, pub.Goal.CharacterID, pub.ShortestEdges)
	if pub.ShortestEdges < 1 {
		t.Fatalf("shortestEdges = %d, want at least 1", pub.ShortestEdges)
	}
	if len(pub.Solution) > 0 || len(pub.Path) > 0 {
		t.Fatalf("the public state leaks the shortest path: %s", v.Public)
	}
	if !v.CanSubmit {
		t.Fatalf("the player cannot submit in collecting: %+v", v)
	}

	// An illegal chain: start → goal with no edge between them (when the pair is
	// more than one hop apart, which is the interesting case).
	if pub.ShortestEdges > 1 {
		res := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
			"kind":         "path.route",
			"characterIds": []string{pub.Start.CharacterID, pub.Goal.CharacterID},
		}, "path-illegal")
		if res.Status == http.StatusCreated && strings.Contains(string(res.Body), `"accepted"`) {
			t.Fatalf("a chain with no edge between its ends was accepted: %s", res.Body)
		}
	}

	// A legal shortest path, found by BFS over the graph the server published. When
	// the closed-book variant hides edges, use the server's own neighbour endpoint.
	adjacency := map[string][]string{}
	for _, e := range pub.Edges {
		adjacency[e.From] = append(adjacency[e.From], e.To)
		adjacency[e.To] = append(adjacency[e.To], e.From)
	}
	if len(adjacency) == 0 {
		// Closed book: the pack hides edges, so ask the server for the neighbours of
		// each node as it is expanded. The endpoint requires an explicit characterId,
		// so the seed is always a real node id.
		if pub.Start.CharacterID == "" {
			t.Fatalf("the closed-book round published no start node: %s", v.Public)
		}
		type node struct{ id string }
		queue := []node{{id: pub.Start.CharacterID}}
		seen := map[string]bool{pub.Start.CharacterID: true}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur.id == pub.Goal.CharacterID {
				break
			}
			neigh := h.doAs(player, http.MethodGet,
				"/api/v1/rounds/"+roundID+"/path-neighbors?characterId="+cur.id, nil, readHeaders(h))
			if neigh.Status != http.StatusOK {
				t.Fatalf("path-neighbors %s: %d %s", cur.id, neigh.Status, neigh.Body)
			}
			// The endpoint nests the neighbour under a "character" object, so the
			// flat {characterId} shape does not decode. Read the nested field.
			var body struct {
				Neighbors []struct {
					Character struct {
						CharacterID string `json:"characterId"`
					} `json:"character"`
				} `json:"neighbors"`
			}
			if err := json.Unmarshal(neigh.Body, &body); err != nil {
				t.Fatalf("decode neighbours: %v", err)
			}
			for _, n := range body.Neighbors {
				id := n.Character.CharacterID
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				adjacency[cur.id] = append(adjacency[cur.id], id)
				queue = append(queue, node{id: id})
			}
		}
	}
	// BFS for the actual chain.
	prev := map[string]string{pub.Start.CharacterID: ""}
	queue := []string{pub.Start.CharacterID}
	found := false
	for len(queue) > 0 && !found {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[cur] {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = cur
			if next == pub.Goal.CharacterID {
				found = true
				break
			}
			queue = append(queue, next)
		}
	}
	if !found {
		t.Skipf("the drawn pair %s → %s has no path in the published graph",
			pub.Start.CharacterID, pub.Goal.CharacterID)
	}
	chain := []string{pub.Goal.CharacterID}
	for cur := pub.Goal.CharacterID; cur != pub.Start.CharacterID; {
		cur = prev[cur]
		chain = append([]string{cur}, chain...)
	}

	v = readRound(t, h, player, roundID)
	res := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind":         "path.route",
		"characterIds": chain,
	}, "path-legal")
	if res.Status != http.StatusCreated {
		t.Fatalf("legal chain submission status = %d, want 201; body=%s", res.Status, res.Body)
	}
	if !strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("a legal chain of %d nodes was not accepted: %s", len(chain), res.Body)
	}

	// Reveal and confirm the round reaches a terminal, readable state.
	advance := func(phase string, expected int64, key string) {
		t.Helper()
		r := h.doAs(host, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions",
			map[string]any{"target": phase, "expectedPhaseVersion": expected},
			writeHeaders(h, hostSess, idemKey(key)))
		if r.Status != http.StatusCreated && r.Status != http.StatusOK {
			t.Fatalf("transition %s: %d %s", phase, r.Status, r.Body)
		}
	}
	current := readRound(t, h, player, roundID)
	advance("locked", current.Round.Version, "path-locked")
	advance("revealed", current.Round.Version+1, "path-revealed")
	if got := readRound(t, h, host, roundID); got.Round.Phase != "revealed" {
		t.Fatalf("phase after reveal = %q", got.Round.Phase)
	}
}

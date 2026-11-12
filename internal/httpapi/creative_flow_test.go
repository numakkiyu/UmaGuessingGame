package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// creativeContent serves the two sections G12 reads from the authored pool and the
// public name index. It is a synthetic pack so the test does not depend on the
// scraped content snapshot's review status.
type creativeContent struct{ prompts, characters json.RawMessage }

func (c creativeContent) Section(name string) (json.RawMessage, bool) {
	switch name {
	case creative.PromptsSection:
		return c.prompts, true
	case creative.CharactersSection:
		return c.characters, true
	}
	return nil, false
}
func (creativeContent) Version() string        { return "creative-test-pack" }
func (creativeContent) DatasetVersion() string { return "creative-test-data" }
func (creativeContent) AssetBaseURL() string   { return "/assets" }
func (creativeContent) Character(string) (room.CharacterCard, bool) {
	return room.CharacterCard{}, false
}
func (creativeContent) Characters(string, int, string) ([]room.CharacterCard, string, error) {
	return nil, "", nil
}
func (creativeContent) PathNeighbors(string) ([]room.Neighbor, bool) { return nil, false }
func (c creativeContent) ModeAdmission(id string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{
		Mode: id, Available: true, QuestionCount: 12, Section: creative.PromptsSection,
	}, id == creative.ModeID
}

// creativeView is the projection fields this test asserts on.
type creativeView struct {
	Round struct {
		Phase    string `json:"phase"`
		Version  int64  `json:"phaseVersion"`
		Deadline string `json:"deadlineAt"`
	} `json:"round"`
	CanSubmit  bool `json:"canSubmit"`
	Revision   int  `json:"submissionRevision"`
	Generation int  `json:"controlGeneration"`
	Public     struct {
		MaxCodePoints int    `json:"maxCodePoints"`
		TextRules     string `json:"textRules"`
		Prompt        *struct {
			CharacterName string `json:"characterName"`
			Scene         string `json:"scene"`
			Constraint    string `json:"constraint"`
		} `json:"prompt"`
		Self *struct {
			SubjectID  string `json:"subjectId"`
			WorkID     string `json:"workId"`
			WorkText   string `json:"workText"`
			Submitted  bool   `json:"submitted"`
			Voted      bool   `json:"voted"`
			VoteWorkID string `json:"voteWorkId"`
			VoteKind   string `json:"voteKind"`
		} `json:"self"`
		Ballot []struct {
			WorkID string `json:"workId"`
			Text   string `json:"text"`
		} `json:"ballot"`
		BallotHiddenReason string `json:"ballotHiddenReason"`
		Progress           []struct {
			SubjectID string `json:"subjectId"`
			Submitted bool   `json:"submitted"`
			Voted     bool   `json:"voted"`
		} `json:"progress"`
		Result *struct {
			Works []struct {
				WorkID        string `json:"workId"`
				TeamVotes     int    `json:"teamVotes"`
				AudienceVotes int    `json:"audienceVotes"`
				Total         int    `json:"total"`
				Rank          int    `json:"rank"`
			} `json:"works"`
			TeamVotes     int `json:"teamVotes"`
			AudienceVotes int `json:"audienceVotes"`
			ValidWorks    int `json:"validWorks"`
		} `json:"result"`
		Authors map[string]string `json:"authors"`
	} `json:"publicState"`
}

// TestHTTPCreativePitchVoteAndResult walks G12 through the real HTTP surface:
// write a pitch, see the anonymous ballot, cast a ballot, then read the tally and
// the authorship reveal. It is the HTTP-level counterpart to the rule-layer tests,
// which cannot observe the room layer's phase clock, the anonymity of the ballot
// before submission, or the settled projection.
func TestHTTPCreativePitchVoteAndResult(t *testing.T) {
	prompts, err := os.ReadFile(filepath.Join("..", "..", "data", "manual", "creative-prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The name index is the same public projection classic reads; only the display
	// name matters here, so a small fixture keeps the test independent of the pack.
	// The pool must be at least as large as the roster, and the roster includes the
	// host, who owns the room and therefore holds a play seat.
	characters := json.RawMessage(`{"characters":[
		{"id":"1001","nameZh":"特别周"},
		{"id":"1002","nameZh":"无声铃鹿"},
		{"id":"1003","nameZh":"东海帝皇"},
		{"id":"1004","nameZh":"丸善斯基"},
		{"id":"1005","nameZh":"小栗帽"},
		{"id":"1006","nameZh":"鲁道夫象征"}
	]}`)
	reg, _ := modes.NewRegistry(creative.New())
	h := newHarness(t, func(c *harnessConfig) {
		c.Modes = reg
		c.Content = creativeContent{prompts: prompts, characters: characters}
	})

	host, owner := h.newSession("主持")
	made := h.doAs(host, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name": "企划擂台", "kind": "friend",
		"modeConfig":      map[string]any{"modeId": "creative", "config": map[string]any{"writeSeconds": 90, "voteSeconds": 30}},
		"maxPlayers":      8,
		"maxSpectators":   0,
		"allowSpectators": false,
		"visibility":      "invite",
	}, writeHeaders(h, owner, idemKey("creative-room")))
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

	clients := make([]*http.Client, 0, 3)
	sessions := make([]testSession, 0, 3)
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		c, sess := h.newSession(fmt.Sprintf("训练员%d", i))
		clients = append(clients, c)
		sessions = append(sessions, sess)
		joined := h.doAs(c, http.MethodPost, "/api/v1/rooms/"+rid+"/memberships", map[string]any{
			"inviteCode": created.Code, "requestedRole": "player",
		}, writeHeaders(h, sess, idemKey("creative-join")))
		if joined.Status != http.StatusCreated {
			t.Fatalf("join %d: %d %s", i, joined.Status, joined.Body)
		}
		var member struct {
			ID string `json:"membershipId"`
		}
		if err := json.Unmarshal(joined.Body, &member); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, member.ID)
	}

	opened := h.doAs(host, http.MethodPost, "/api/v1/rooms/"+rid+"/rounds", map[string]any{},
		writeHeaders(h, owner, idemKey("creative-open")))
	if opened.Status != http.StatusCreated {
		t.Fatalf("open round: %d %s", opened.Status, opened.Body)
	}
	var summary struct {
		ID string `json:"roundId"`
	}
	if err := json.Unmarshal(opened.Body, &summary); err != nil {
		t.Fatal(err)
	}
	roundID := summary.ID
	path := "/api/v1/rounds/" + roundID

	advance := func(phase string, expected int64, key string) {
		t.Helper()
		res := h.doAs(host, http.MethodPost, path+"/transitions", map[string]any{
			"target": phase, "expectedPhaseVersion": expected,
		}, writeHeaders(h, owner, idemKey(key)))
		if res.Status != http.StatusCreated && res.Status != http.StatusOK {
			t.Fatalf("transition %s: %d %s", phase, res.Status, res.Body)
		}
	}
	advance("briefing", 1, "creative-briefing")
	advance("collecting", 2, "creative-collecting")

	read := func(c *http.Client) creativeView {
		t.Helper()
		res := h.doAs(c, http.MethodGet, path, nil, readHeaders(h))
		if res.Status != http.StatusOK {
			t.Fatalf("read round: %d %s", res.Status, res.Body)
		}
		var v creativeView
		if err := json.Unmarshal(res.Body, &v); err != nil {
			t.Fatalf("decode round: %v", err)
		}
		return v
	}

	// ---- writing phase ----
	// Each participant gets their own draw, and the ballot stays hidden while the
	// writing window is open, because showing unfinished pitches would let a team
	// copy a stronger one.
	draws := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		v := read(clients[i])
		if v.Public.Prompt == nil || v.Public.Self == nil {
			t.Fatalf("player %d has no own draw: %+v", i, v.Public)
		}
		if v.Public.Prompt.CharacterName == "" || v.Public.Prompt.Scene == "" || v.Public.Prompt.Constraint == "" {
			t.Fatalf("player %d draw is incomplete: %+v", i, *v.Public.Prompt)
		}
		if v.Public.MaxCodePoints != creative.MaxCodePoints {
			t.Fatalf("player %d published limit = %d, want %d", i, v.Public.MaxCodePoints, creative.MaxCodePoints)
		}
		if v.Public.TextRules == "" || strings.Contains(v.Public.TextRules, "NFC") ||
			strings.Contains(v.Public.TextRules, "Unicode") || strings.Contains(v.Public.TextRules, "码点") {
			// The rule text must be player-facing: the normalization requirement is a
			// client capability, not vocabulary a player should have to decode.
			t.Fatalf("player %d text rules leak implementation terms: %q", i, v.Public.TextRules)
		}
		if len(v.Public.Ballot) != 0 || v.Public.BallotHiddenReason == "" {
			t.Fatalf("player %d saw the ballot before submitting: %+v", i, v.Public.Ballot)
		}
		if v.Public.Result != nil || len(v.Public.Authors) != 0 {
			t.Fatalf("player %d saw a tally during writing", i)
		}
		draws = append(draws, v.Public.Prompt.CharacterName)
	}
	if draws[0] == draws[1] || draws[1] == draws[2] || draws[0] == draws[2] {
		t.Fatalf("two teams drew the same character: %v", draws)
	}

	pitches := []string{
		"雨天校门口，她借出一把伞，把自己的迟到讲成笑话。",
		"食堂只剩最后一份胡萝卜蛋糕，她分给了刚认识的学妹。",
		"训练场上没人看见的时候，她一个人把散落的栏架摆回原位。",
	}
	for i := 0; i < 3; i++ {
		v := read(clients[i])
		res := h.doAs(clients[i], http.MethodPost, path+"/submissions", map[string]any{
			"modeAction":         map[string]any{"kind": "creative.work", "text": pitches[i]},
			"phaseVersion":       v.Round.Version,
			"submissionRevision": v.Revision,
			"controlGeneration":  v.Generation,
		}, writeHeaders(h, sessions[i], idemKey(fmt.Sprintf("creative-work-%d", i))))
		if res.Status != http.StatusCreated {
			t.Fatalf("submit pitch %d: %d %s", i, res.Status, res.Body)
		}
		if !strings.Contains(string(res.Body), `"accepted"`) {
			t.Fatalf("pitch %d was not accepted: %s", i, res.Body)
		}
	}

	// Submitting must not expose peers while writing remains open.
	for i := range clients {
		if len(read(clients[i]).Public.Ballot) != 0 {
			t.Fatal("unfinished ballot exposed")
		}
	}
	advance("locked", 3, "creative-review")
	if read(clients[0]).Round.Deadline != "" {
		t.Fatal("review must not run a timer")
	}
	advance("creative-voting", 4, "creative-open-voting")
	// ---- the ballot becomes visible to authors, still anonymous ----
	for i := 0; i < 3; i++ {
		v := read(clients[i])
		if v.Public.Self == nil || !v.Public.Self.Submitted || v.Public.Self.WorkText != pitches[i] {
			t.Fatalf("player %d own pitch was not persisted: %+v", i, v.Public.Self)
		}
		if len(v.Public.Ballot) != 3 {
			t.Fatalf("player %d ballot has %d entries, want 3", i, len(v.Public.Ballot))
		}
		for _, entry := range v.Public.Ballot {
			if entry.WorkID == "" || entry.Text == "" {
				t.Fatalf("player %d ballot entry is incomplete: %+v", i, entry)
			}
		}
		// Anonymity is structural: the wire must carry no author field at all.
		if strings.Contains(string(mustJSON(t, v.Public.Ballot)), "subjectId") ||
			strings.Contains(string(mustJSON(t, v.Public.Ballot)), "authorId") {
			t.Fatalf("player %d ballot carries an author field", i)
		}
	}

	// ---- voting ----
	// Each of the three authors votes for a different peer's work. A ballot for
	// one's own work is refused, which is what keeps the vote meaningful.
	ownWork := map[int]string{}
	for i := 0; i < 3; i++ {
		ownWork[i] = read(clients[i]).Public.Self.WorkID
	}
	v := read(clients[0])
	ownAttempt := h.doAs(clients[0], http.MethodPost, path+"/submissions", map[string]any{
		"modeAction":         map[string]any{"kind": "creative.vote", "optionId": ownWork[0]},
		"phaseVersion":       v.Round.Version,
		"submissionRevision": v.Revision,
		"controlGeneration":  v.Generation,
	}, writeHeaders(h, sessions[0], idemKey("creative-own-vote")))
	if ownAttempt.Status == http.StatusCreated && strings.Contains(string(ownAttempt.Body), `"accepted"`) {
		t.Fatalf("voting for one's own work was accepted: %s", ownAttempt.Body)
	}

	// 0 → 1, 1 → 2, 2 → 0: a three-way ring so every work receives exactly one vote.
	//
	// The revision is per SLOT: the vote slot starts at 0 even though the work slot
	// already advanced to 1 for the same subject. The round view reports one
	// revision, so a client must address the vote slot from its own first
	// submission — which is what the round page does.
	for i := 0; i < 3; i++ {
		target := (i + 1) % 3
		v := read(clients[i])
		res := h.doAs(clients[i], http.MethodPost, path+"/submissions", map[string]any{
			"modeAction":         map[string]any{"kind": "creative.vote", "optionId": ownWork[target]},
			"phaseVersion":       v.Round.Version,
			"submissionRevision": 0,
			"controlGeneration":  v.Generation,
		}, writeHeaders(h, sessions[i], idemKey(fmt.Sprintf("creative-vote-%d", i))))
		if res.Status != http.StatusCreated {
			t.Fatalf("vote %d: %d %s", i, res.Status, res.Body)
		}
		if !strings.Contains(string(res.Body), `"accepted"`) {
			t.Fatalf("vote %d was not accepted: %s", i, res.Body)
		}
	}

	// A second ballot from the same identity is refused even from a fresh request:
	// the vote is bound to the membership, not to a connection or a device.
	v = read(clients[0])
	repeat := h.doAs(clients[0], http.MethodPost, path+"/submissions", map[string]any{
		"modeAction":         map[string]any{"kind": "creative.vote", "optionId": ownWork[2]},
		"phaseVersion":       v.Round.Version,
		"submissionRevision": 1,
		"controlGeneration":  v.Generation,
	}, writeHeaders(h, sessions[0], idemKey("creative-vote-repeat")))
	if repeat.Status == http.StatusCreated && strings.Contains(string(repeat.Body), `"accepted"`) {
		t.Fatalf("a second ballot from the same identity was accepted: %s", repeat.Body)
	}

	// A pitch cannot change once somebody has voted on it.
	v = read(clients[0])
	late := h.doAs(clients[0], http.MethodPost, path+"/submissions", map[string]any{
		"modeAction":         map[string]any{"kind": "creative.work", "text": "改写后的企划"},
		"phaseVersion":       v.Round.Version,
		"submissionRevision": 1,
		"controlGeneration":  v.Generation,
	}, writeHeaders(h, sessions[0], idemKey("creative-late-work")))
	if late.Status == http.StatusCreated && strings.Contains(string(late.Body), `"accepted"`) {
		t.Fatalf("a pitch was changed after voting started: %s", late.Body)
	}

	// ---- result ----
	advance("locked", 5, "creative-locked")
	advance("revealed", 6, "creative-revealed")

	settled := read(clients[0])
	if settled.Public.Result == nil {
		t.Fatalf("no tally after the reveal: %+v", settled.Public)
	}
	res := settled.Public.Result
	if res.ValidWorks != 3 {
		t.Fatalf("validWorks = %d, want 3", res.ValidWorks)
	}
	// Three team votes (every voter pitched), no audience, one per work.
	if res.TeamVotes != 3 || res.AudienceVotes != 0 {
		t.Fatalf("vote pools = team %d / audience %d, want 3/0", res.TeamVotes, res.AudienceVotes)
	}
	if len(res.Works) != 3 {
		t.Fatalf("tally rows = %d, want 3", len(res.Works))
	}
	for _, w := range res.Works {
		if w.Total != 1 || w.TeamVotes != 1 || w.AudienceVotes != 0 {
			t.Fatalf("work %s tally = %+v, want one team vote", w.WorkID, w)
		}
		// A three-way tie shares the top rank rather than inventing an order.
		if w.Rank != 1 {
			t.Fatalf("work %s rank = %d, want 1 (shared on a tie)", w.WorkID, w.Rank)
		}
	}
	// Authorship is published only now, and it maps each work back to its author.
	// The reveal covers the whole roster, which includes the host: owning a room
	// grants a play seat, so the host is a fourth participant with a fourth work id.
	if len(settled.Public.Authors) != 3 {
		t.Fatalf("authors reveal has %d entries, want 3 submitted works",
			len(settled.Public.Authors))
	}
	for i := 0; i < 3; i++ {
		if settled.Public.Authors[ownWork[i]] != ids[i] {
			t.Fatalf("author of work %s = %q, want %q", ownWork[i], settled.Public.Authors[ownWork[i]], ids[i])
		}
	}

	// Every participant sees the same tally: the reveal is not viewer-dependent.
	other := read(clients[1])
	if other.Public.Result == nil || other.Public.Result.TeamVotes != res.TeamVotes ||
		other.Public.Result.ValidWorks != res.ValidWorks || len(other.Public.Result.Works) != len(res.Works) {
		t.Fatalf("participants disagree about the tally: %+v vs %+v", other.Public.Result, res)
	}
	for i := range res.Works {
		if other.Public.Result.Works[i].WorkID != res.Works[i].WorkID ||
			other.Public.Result.Works[i].Total != res.Works[i].Total {
			t.Fatalf("tally row %d differs between viewers", i)
		}
	}
}

// mustJSON marshals a value for substring assertions; a marshal failure is a test
// bug rather than a product failure.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

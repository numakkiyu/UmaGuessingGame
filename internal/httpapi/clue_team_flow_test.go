package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/cluepuzzle"
	"github.com/bhcn/umaguessinggame/internal/room"
)

type clueContent struct{ tradeContent }

func (c clueContent) Section(name string) (json.RawMessage, bool) {
	return c.raw, name == cluepuzzle.DeckSection
}
func (clueContent) ModeAdmission(id string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{Mode: id, Available: true, QuestionCount: 12, Section: cluepuzzle.DeckSection}, id == cluepuzzle.ModeID
}

type clueView struct {
	Round struct {
		ID      string `json:"roundId"`
		Version int    `json:"phaseVersion"`
	} `json:"round"`
	CanSubmit  bool `json:"canSubmit"`
	Revision   int  `json:"submissionRevision"`
	Generation int  `json:"controlGeneration"`
	Public     struct {
		Slots []struct {
			Index int `json:"slotIndex"`
		} `json:"slots"`
		Clues []struct {
			ID   string `json:"constraintId"`
			Text string `json:"text"`
		} `json:"privateClues"`
		TeamDraft map[string]string `json:"teamDraft"`
		Self      *struct {
			Assignment map[string]string `json:"assignment"`
		} `json:"self"`
		Answer json.RawMessage `json:"answer"`
	} `json:"publicState"`
	Private struct {
		ModeID      string   `json:"modeId"`
		Clues       []string `json:"clues"`
		Assignments []any    `json:"draftAssignments"`
	} `json:"privateState"`
}

func TestHTTPClueTeamPrivateInformationAndRotation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "manual", "cluepuzzle-deck.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(cluepuzzle.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = clueContent{tradeContent{raw: raw}} })
	host, owner := h.newSession("主持")
	made := h.doAs(host, "POST", "/api/v1/rooms", map[string]any{"name": "情报游玩会", "kind": "event", "modeConfig": map[string]any{"modeId": "clue-puzzle", "config": map[string]any{"cards": 5}}, "maxPlayers": 8, "maxSpectators": 2, "allowSpectators": true, "visibility": "invite"}, writeHeaders(h, owner, idemKey("clue-room")))
	if made.Status != 201 {
		t.Fatalf("room: %d %s", made.Status, made.Body)
	}
	var created struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	json.Unmarshal(made.Body, &created)
	rid := created.Room.ID
	clients := []*http.Client{}
	sessions := []testSession{}
	ids := []string{}
	for i := 0; i < 6; i++ {
		c, sess := h.newSession(fmt.Sprintf("同名训练员%d", i))
		clients = append(clients, c)
		sessions = append(sessions, sess)
		joined := h.doAs(c, "POST", "/api/v1/rooms/"+rid+"/memberships", map[string]any{"inviteCode": created.Code, "requestedRole": "player"}, writeHeaders(h, sess, idemKey("clue-join")))
		var member struct {
			ID string `json:"membershipId"`
		}
		json.Unmarshal(joined.Body, &member)
		if joined.Status != 201 {
			t.Fatalf("join: %s", joined.Body)
		}
		ids = append(ids, member.ID)
	}
	open := func(key string) response {
		return h.doAs(host, "POST", "/api/v1/rooms/"+rid+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey(key)))
	}
	if res := open("clue-unassigned"); res.Status != 422 || !strings.Contains(string(res.Body), "team-roster-invalid") {
		t.Fatalf("unassigned: %d %s", res.Status, res.Body)
	}
	for team := 0; team < 2; team++ {
		teamID := fmt.Sprintf("clue-team-%d", team)
		if _, err := h.db.Write().Exec(`INSERT INTO teams(id,room_id,label,created_at) VALUES(?,?,?,1)`, teamID, rid, fmt.Sprintf("队%d", team)); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 3; j++ {
			role := "player"
			if j == 0 {
				role = "team-captain"
			}
			if _, err := h.db.Write().Exec(`UPDATE memberships SET team_id=?,role=? WHERE id=?`, teamID, role, ids[team*3+j]); err != nil {
				t.Fatal(err)
			}
		}
	}
	opened := open("clue-open")
	if opened.Status != 201 {
		t.Fatalf("open: %d %s", opened.Status, opened.Body)
	}
	var summary struct {
		ID string `json:"roundId"`
	}
	json.Unmarshal(opened.Body, &summary)
	roundID := summary.ID
	for i, phase := range []string{"briefing", "collecting"} {
		res := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": phase, "expectedPhaseVersion": i + 1}, writeHeaders(h, owner, idemKey("clue-"+phase)))
		if res.Status != 201 && res.Status != 200 {
			t.Fatalf("phase: %d %s", res.Status, res.Body)
		}
	}
	read := func(client *http.Client) clueView {
		t.Helper()
		res := h.doAs(client, "GET", "/api/v1/rounds/"+roundID, nil, nil)
		if res.Status != 200 {
			t.Fatalf("read: %s", res.Body)
		}
		var v clueView
		if err := json.Unmarshal(res.Body, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	seen := map[string]bool{}
	for i, c := range clients {
		v := read(c)
		if len(v.Public.Slots) != 5 || len(v.Public.Clues) == 0 || len(v.Private.Clues) != len(v.Public.Clues) || v.Private.ModeID != "clue-puzzle" || len(v.Public.Answer) > 0 || v.CanSubmit != (i%3 == 0) {
			t.Fatalf("own view %d: %+v", i, v)
		}
		for j, clue := range v.Public.Clues {
			if clue.Text != v.Private.Clues[j] || strings.Contains(clue.Text, "dc-") {
				t.Fatal("raw identifier or wrong private DTO")
			}
			if i < 3 {
				if seen[clue.ID] {
					t.Fatal("same-team private clue reused")
				}
				seen[clue.ID] = true
			}
		}
	}
	v := read(host)
	if len(v.Public.Clues) > 0 || v.Public.Self != nil || v.CanSubmit || v.Private.ModeID != "" {
		t.Fatal("host reads member secrets")
	}
	var secret string
	if err := h.db.Read().QueryRow(`SELECT state_json FROM round_private_state WHERE round_id=? AND scope='round'`, roundID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Solution map[string]string `json:"solution"`
	}
	json.Unmarshal([]byte(secret), &stored)
	assignments := []map[string]string{}
	for slot, card := range stored.Solution {
		assignments = append(assignments, map[string]string{"slotId": slot, "characterId": card})
	}
	original := read(clients[0])
	body := map[string]any{"phaseVersion": original.Round.Version, "submissionRevision": original.Revision, "controlGeneration": original.Generation, "modeAction": map[string]any{"kind": "clue.assignment", "assignments": assignments}}
	path := "/api/v1/rounds/" + roundID + "/submissions"
	first := h.doAs(clients[0], "POST", path, body, writeHeaders(h, sessions[0], idemKey("clue-answer")))
	if first.Status != 201 {
		t.Fatalf("submit: %d %s", first.Status, first.Body)
	}
	var second int
	for i := 1; i < 3; i++ {
		if read(clients[i]).CanSubmit {
			second = i
		}
	}
	if second == 0 || read(clients[0]).CanSubmit {
		t.Fatal("representative did not rotate")
	}
	for i := 0; i < 3; i++ {
		v := read(clients[i])
		if v.Revision != 1 || v.Public.Self == nil || !reflect.DeepEqual(v.Public.Self.Assignment, stored.Solution) || !reflect.DeepEqual(v.Public.TeamDraft, stored.Solution) {
			t.Fatal("same-team assignment not shared")
		}
	}
	for i := 3; i < 6; i++ {
		if v := read(clients[i]); len(v.Public.TeamDraft) > 0 || len(v.Public.Self.Assignment) > 0 || v.Revision != 0 {
			t.Fatal("opponent assignment leaked")
		}
	}
	if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE route_scope NOT LIKE 'gameplay:%'`); err != nil {
		t.Fatal(err)
	}
	replay := h.doAs(clients[0], "POST", path, body, writeHeaders(h, sessions[0], idemKey("clue-answer")))
	if replay.Status != 201 || read(clients[second]).Revision != 1 {
		t.Fatalf("handoff replay: %d %s", replay.Status, replay.Body)
	}
	body["submissionRevision"] = 1
	rejected := h.doAs(clients[0], "POST", path, body, writeHeaders(h, sessions[0], idemKey("clue-old-control")))
	if rejected.Status != 409 {
		t.Fatalf("old controller: %d %s", rejected.Status, rejected.Body)
	}
	// The other team can submit independently, without changing this team's head.
	v = read(clients[3])
	body["submissionRevision"] = v.Revision
	body["controlGeneration"] = v.Generation
	// Swap two cards so the second team's unique-solution puzzle is incomplete.
	assignments[0]["characterId"], assignments[1]["characterId"] = assignments[1]["characterId"], assignments[0]["characterId"]
	res := h.doAs(clients[3], "POST", path, body, writeHeaders(h, sessions[3], idemKey("clue-other-team")))
	if res.Status != 201 {
		t.Fatalf("other team: %d %s", res.Status, res.Body)
	}
	if read(clients[second]).Revision != 1 {
		t.Fatal("opponent changed team head")
	}
	for i, phase := range []string{"locked", "revealed", "settled"} {
		res := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": phase, "expectedPhaseVersion": 4 + i - 1}, writeHeaders(h, owner, idemKey("clue-finish-"+phase)))
		if res.Status != 201 && res.Status != 200 {
			t.Fatalf("finish: %d %s", res.Status, res.Body)
		}
	}
	res = h.doAs(host, "GET", "/api/v1/rounds/"+roundID, nil, nil)
	var ended struct {
		Results []struct {
			Subject string `json:"subjectId"`
			Units   int    `json:"scoreUnits"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body, &ended); err != nil {
		t.Fatal(err)
	}
	if len(ended.Results) != 2 {
		t.Fatalf("results: %s", res.Body)
	}
	for _, r := range ended.Results {
		if r.Subject == "clue-team-0" && r.Units != 10000 || r.Subject == "clue-team-1" && r.Units >= 10000 {
			t.Fatalf("cross-team scoring: %+v", ended.Results)
		}
	}
	if v := read(host); len(v.Public.Answer) == 0 || len(v.Public.Clues) > 0 {
		t.Fatal("terminal public answer or private clues wrong")
	}

	// A later cancelled round must not publish a solution or another member's clues.
	opened = open("clue-cancel-open")
	if opened.Status != 201 {
		t.Fatalf("cancel round open: %d %s", opened.Status, opened.Body)
	}
	json.Unmarshal(opened.Body, &summary)
	roundID = summary.ID
	for i, phase := range []string{"briefing", "collecting", "cancel"} {
		res := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": phase, "expectedPhaseVersion": i + 1}, writeHeaders(h, owner, idemKey("clue-cancel-"+phase)))
		if res.Status != 200 && res.Status != 201 {
			t.Fatalf("cancel phase: %d %s", res.Status, res.Body)
		}
	}
	for _, client := range append([]*http.Client{host}, clients...) {
		v := read(client)
		if len(v.Public.Answer) > 0 {
			t.Fatal("cancelled solution leaked")
		}
	}
	res = h.doAs(host, "GET", "/api/v1/rounds/"+roundID, nil, nil)
	var cancelled struct {
		Results []any `json:"results"`
	}
	json.Unmarshal(res.Body, &cancelled)
	if len(cancelled.Results) > 0 {
		t.Fatal("cancelled round has scores")
	}

}

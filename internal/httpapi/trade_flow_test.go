package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
	"github.com/bhcn/umaguessinggame/internal/room"
)

type tradeContent struct{ raw json.RawMessage }

func (c tradeContent) Section(name string) (json.RawMessage, bool) {
	return c.raw, name == trade.CardsSection || name == trade.ObjectivesSection
}
func (tradeContent) Version() string                             { return "trade-test-pack" }
func (tradeContent) DatasetVersion() string                      { return "trade-test-data" }
func (tradeContent) AssetBaseURL() string                        { return "/assets" }
func (tradeContent) Character(string) (room.CharacterCard, bool) { return room.CharacterCard{}, false }
func (tradeContent) Characters(string, int, string) ([]room.CharacterCard, string, error) {
	return nil, "", nil
}
func (tradeContent) PathNeighbors(string) ([]room.Neighbor, bool) { return nil, false }
func (tradeContent) ModeAdmission(id string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{Mode: id, Available: true, QuestionCount: 16, Section: trade.CardsSection}, id == trade.ModeID
}

type tradeView struct {
	Round struct {
		Phase    string    `json:"phase"`
		Version  int64     `json:"phaseVersion"`
		Deadline time.Time `json:"deadlineAt"`
	} `json:"round"`
	Public struct {
		CurrentRound int                 `json:"currentRound"`
		Hands        map[string][]string `json:"hands"`
		Trades       []struct {
			ID     string `json:"tradeId"`
			Status string `json:"status"`
		} `json:"trades"`
		Objectives []struct {
			ID string `json:"objectiveId"`
		} `json:"selfObjectives"`
		Self struct {
			ID   string   `json:"subjectId"`
			Hand []string `json:"hand"`
		} `json:"self"`
	} `json:"publicState"`
	Controllers []any `json:"teamControllers"`
}

func TestHTTPTradeUsesIndividualHandsAndAuthoritativeProposalState(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "manual", "trade-content.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(trade.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = tradeContent{raw: raw} })
	now := time.Now().UTC()
	h.rooms.SetClock(func() time.Time { return now })
	host, owner := h.newSession("主持")
	created := h.doAs(host, "POST", "/api/v1/rooms", map[string]any{"name": "档案游玩会", "kind": "event", "modeConfig": map[string]any{"modeId": "trade", "rounds": 3, "roundSeconds": 60}, "maxPlayers": 8, "maxSpectators": 0, "allowSpectators": false, "visibility": "invite"}, writeHeaders(h, owner, idemKey("trade-room")))
	if created.Status != 201 {
		t.Fatalf("create: %d %s", created.Status, created.Body)
	}
	var made struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	_ = json.Unmarshal(created.Body, &made)
	rid := made.Room.ID
	clients := []*http.Client{}
	sessions := []testSession{}
	ids := []string{}
	for i := 0; i < 3; i++ {
		c, sess := h.newSession(fmt.Sprintf("训练员%d", i))
		clients = append(clients, c)
		sessions = append(sessions, sess)
		joined := h.doAs(c, "POST", "/api/v1/rooms/"+rid+"/memberships", map[string]any{"inviteCode": made.Code, "requestedRole": "player"}, writeHeaders(h, sess, idemKey("trade-join")))
		if joined.Status != 201 {
			t.Fatal(string(joined.Body))
		}
		var member struct {
			ID string `json:"membershipId"`
		}
		_ = json.Unmarshal(joined.Body, &member)
		ids = append(ids, member.ID)
	}
	// Grouped event members still receive individual cards and secret objectives.
	if _, err = h.db.Write().Exec(`INSERT INTO teams(id,room_id,label,created_at) VALUES('trade-team',?,'胡萝卜队',?)`, rid, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err = h.db.Write().Exec(`UPDATE memberships SET team_id='trade-team' WHERE room_id=? AND role='player'`, rid); err != nil {
		t.Fatal(err)
	}
	opened := h.doAs(host, "POST", "/api/v1/rooms/"+rid+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey("trade-open")))
	if opened.Status != 201 {
		t.Fatalf("open: %d %s", opened.Status, opened.Body)
	}
	var summary struct {
		ID string `json:"roundId"`
	}
	_ = json.Unmarshal(opened.Body, &summary)
	roundID := summary.ID
	for i, phase := range []string{"briefing", "collecting"} {
		res := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": phase, "expectedPhaseVersion": i + 1}, writeHeaders(h, owner, idemKey("trade-"+phase)))
		if res.Status != 200 && res.Status != 201 {
			t.Fatalf("phase: %d %s", res.Status, res.Body)
		}
	}
	read := func(i int) tradeView {
		t.Helper()
		res := h.doAs(clients[i], "GET", "/api/v1/rounds/"+roundID, nil, nil)
		if res.Status != 200 {
			t.Fatal(string(res.Body))
		}
		var v tradeView
		if err := json.Unmarshal(res.Body, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	first, second := read(0), read(1)
	for _, target := range []string{"locked", "revealed", "settled"} {
		early := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": target, "expectedPhaseVersion": first.Round.Version}, writeHeaders(h, owner, idemKey("trade-early-"+target)))
		if early.Status != 409 {
			t.Fatalf("host skipped windows: %s %d %s", target, early.Status, early.Body)
		}
	}
	if len(first.Public.Hands) != 3 || len(first.Public.Self.Hand) != 4 || len(first.Public.Objectives) != 2 || first.Public.Self.ID != ids[0] || len(first.Controllers) != 0 {
		t.Fatalf("wrong individual topology: %+v", first)
	}
	if first.Public.Objectives[0].ID == second.Public.Objectives[0].ID {
		t.Fatal("secret objectives reused across players")
	}
	if first.Round.Deadline.Sub(now) < time.Minute || first.Round.Deadline.Sub(now) > time.Minute+time.Second {
		t.Fatalf("window=%s", first.Round.Deadline.Sub(now))
	}
	// Simulate a process dying after the business commit but before its separate
	// HTTP receipt. The transaction's gameplay result must survive that boundary.
	recoverHTTP := func(client *http.Client, sess testSession, path string, body any, key string, original response) {
		t.Helper()
		if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=? AND route_scope NOT LIKE 'gameplay:%'`, sess.ID); err != nil {
			t.Fatal(err)
		}
		replayed := h.doAs(client, "POST", path, body, writeHeaders(h, sess, idemKey(key)))
		var before, after any
		_ = json.Unmarshal(original.Body, &before)
		_ = json.Unmarshal(replayed.Body, &after)
		if replayed.Status != original.Status || !reflect.DeepEqual(before, after) {
			t.Fatalf("gameplay recovery: %d %s; want %s", replayed.Status, replayed.Body, original.Body)
		}
	}
	propose := func(key string) string {
		t.Helper()
		a, b := read(0), read(1)
		body := map[string]any{"phaseVersion": a.Round.Version, "recipientMembershipId": ids[1], "offeredInstanceId": a.Public.Self.Hand[0], "requestedInstanceId": b.Public.Self.Hand[0]}
		res := h.doAs(clients[0], "POST", "/api/v1/rounds/"+roundID+"/trades", body, writeHeaders(h, sessions[0], idemKey(key)))
		if res.Status != 201 {
			t.Fatalf("propose: %d %s", res.Status, res.Body)
		}
		var result struct {
			ID string `json:"tradeId"`
		}
		_ = json.Unmarshal(res.Body, &result)
		recoverHTTP(clients[0], sessions[0], "/api/v1/rounds/"+roundID+"/trades", body, key, res)
		body["requestedInstanceId"] = "different-card"
		if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=? AND route_scope NOT LIKE 'gameplay:%'`, sessions[0].ID); err != nil {
			t.Fatal(err)
		}
		conflict := h.doAs(clients[0], "POST", "/api/v1/rounds/"+roundID+"/trades", body, writeHeaders(h, sessions[0], idemKey(key)))
		if conflict.Status != 409 {
			t.Fatalf("changed trade body: %d %s", conflict.Status, conflict.Body)
		}
		return result.ID
	}
	respond := func(id string, accept bool, key string) response {
		return h.doAs(clients[1], "POST", "/api/v1/rounds/"+roundID+"/trades/"+id+"/responses", map[string]any{"accept": accept}, writeHeaders(h, sessions[1], idemKey(key)))
	}
	id := propose("trade-reject")
	rejected := respond(id, false, "trade-no")
	if rejected.Status != 201 {
		t.Fatalf("reject: %d %s", rejected.Status, rejected.Body)
	}
	recoverHTTP(clients[1], sessions[1], "/api/v1/rounds/"+roundID+"/trades/"+id+"/responses", map[string]any{"accept": false}, "trade-no", rejected)
	if read(0).Public.Trades[0].Status != "rejected" {
		t.Fatal("rejection left proposal pending")
	}
	if duplicate := respond(id, true, "trade-after-no"); duplicate.Status != 409 {
		t.Fatalf("rejected trade reopened: %d %s", duplicate.Status, duplicate.Body)
	}
	beforeA, beforeB := read(0).Public.Self.Hand[0], read(1).Public.Self.Hand[0]
	id = propose("trade-accept")
	acceptedID := id
	paused := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": "pause", "expectedPhaseVersion": read(0).Round.Version}, writeHeaders(h, owner, idemKey("trade-pause")))
	if paused.Status != 201 && paused.Status != 200 {
		t.Fatalf("pause: %d %s", paused.Status, paused.Body)
	}
	now = now.Add(2 * time.Minute)
	if blocked := respond(id, true, "trade-paused"); blocked.Status != 409 {
		t.Fatalf("paused trade accepted: %d", blocked.Status)
	}
	resumed := h.doAs(host, "POST", "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": "resume", "expectedPhaseVersion": read(0).Round.Version}, writeHeaders(h, owner, idemKey("trade-resume")))
	if resumed.Status != 201 && resumed.Status != 200 {
		t.Fatalf("resume: %d %s", resumed.Status, resumed.Body)
	}
	accepted := respond(id, true, "trade-yes")
	if accepted.Status != 201 {
		t.Fatalf("accept: %d %s", accepted.Status, accepted.Body)
	}
	recoverHTTP(clients[1], sessions[1], "/api/v1/rounds/"+roundID+"/trades/"+id+"/responses", map[string]any{"accept": true}, "trade-yes", accepted)
	afterA, afterB := read(0), read(1)
	contains := func(values []string, key string) bool {
		for _, value := range values {
			if value == key {
				return true
			}
		}
		return false
	}
	if !contains(afterA.Public.Self.Hand, beforeB) || !contains(afterB.Public.Self.Hand, beforeA) || contains(afterA.Public.Self.Hand, beforeA) {
		t.Fatal("public hands did not swap")
	}
	for i, expected := range []string{beforeB, beforeA} {
		var state struct {
			Hand       []string          `json:"hand"`
			Objectives []json.RawMessage `json:"objectives"`
		}
		var raw string
		if err = h.db.Read().QueryRow(`SELECT state_json FROM participant_round_state WHERE round_id=? AND subject_id=?`, roundID, ids[i]).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal([]byte(raw), &state)
		if !contains(state.Hand, expected) {
			t.Fatal("stored individual hand did not swap")
		}
		if len(state.Objectives) != 2 {
			t.Fatal("exchanging a card discarded the player's secret objectives")
		}
	}
	id = propose("trade-next") // Possession validation must use the newly exchanged cards.
	now = read(0).Round.Deadline.Add(time.Millisecond)
	if _, err = h.rooms.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := read(0)
	if next.Public.CurrentRound != 2 || next.Round.Phase != "collecting" {
		t.Fatal("round did not advance")
	}
	if expired := respond(id, true, "trade-expired"); expired.Status != 409 {
		t.Fatalf("old proposal revived: %d %s", expired.Status, expired.Body)
	}
	for i := 0; i < 2; i++ {
		now = read(0).Round.Deadline.Add(time.Millisecond)
		if _, err = h.rooms.SweepExpired(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	final := read(0)
	if final.Round.Phase != "locked" || final.Public.CurrentRound != 3 {
		t.Fatalf("final state: %+v", final.Round)
	}
	recoverHTTP(clients[1], sessions[1], "/api/v1/rounds/"+roundID+"/trades/"+acceptedID+"/responses", map[string]any{"accept": true}, "trade-yes", accepted)
	if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE session_id=? AND route_scope NOT LIKE 'gameplay:%'`, sessions[1].ID); err != nil {
		t.Fatal(err)
	}
	changed := h.doAs(clients[1], "POST", "/api/v1/rounds/"+roundID+"/trades/"+acceptedID+"/responses", map[string]any{"accept": false}, writeHeaders(h, sessions[1], idemKey("trade-yes")))
	if changed.Status != 409 {
		t.Fatalf("changed original response: %d %s", changed.Status, changed.Body)
	}
	stopped := h.doAs(clients[0], "POST", "/api/v1/rounds/"+roundID+"/trades", map[string]any{"phaseVersion": final.Round.Version, "recipientMembershipId": ids[1], "offeredInstanceId": final.Public.Self.Hand[0], "requestedInstanceId": read(1).Public.Self.Hand[0]}, writeHeaders(h, sessions[0], idemKey("trade-terminal")))
	if stopped.Status != 409 && stopped.Status != 422 {
		t.Fatalf("terminal trade accepted: %d %s", stopped.Status, stopped.Body)
	}
}

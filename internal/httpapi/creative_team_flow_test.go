package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestHTTPCreativeTeamPersonalAndSpectatorVotes(t *testing.T) {
	prompts, err := os.ReadFile("../../data/manual/creative-prompts.json")
	if err != nil {
		t.Fatal(err)
	}
	content := creativeContent{prompts: prompts, characters: json.RawMessage(`{"characters":[{"id":"1001","nameZh":"特别周"},{"id":"1002","nameZh":"无声铃鹿"}]}`)}
	reg, _ := modes.NewRegistry(creative.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = content })
	host, owner := h.newSession("主持")
	made := h.doAs(host, "POST", "/api/v1/rooms", map[string]any{"name": "创意场", "kind": "event", "modeConfig": map[string]any{"modeId": "creative"}, "maxPlayers": 6, "maxSpectators": 2, "allowSpectators": true, "visibility": "invite"}, writeHeaders(h, owner, idemKey("creative-team-room")))
	if made.Status != 201 {
		t.Fatalf("create %d %s", made.Status, made.Body)
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
	for i := 0; i < 5; i++ {
		c, s := h.newSession("特别周")
		role := "player"
		if i == 4 {
			role = "spectator"
		}
		joined := h.doAs(c, "POST", "/api/v1/rooms/"+rid+"/memberships", map[string]any{"inviteCode": created.Code, "requestedRole": role}, writeHeaders(h, s, idemKey("creative-team-join")))
		if joined.Status != 201 {
			t.Fatalf("join %d %s", joined.Status, joined.Body)
		}
		var m struct {
			ID string `json:"membershipId"`
		}
		json.Unmarshal(joined.Body, &m)
		clients = append(clients, c)
		sessions = append(sessions, s)
		ids = append(ids, m.ID)
	}
	for team := 0; team < 2; team++ {
		tid := fmt.Sprintf("creative-team-%d", team)
		if _, err := h.db.Write().Exec(`INSERT INTO teams(id,room_id,label,created_at) VALUES(?,?,?,1)`, tid, rid, tid); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 2; j++ {
			role := "player"
			if j == 0 {
				role = "team-captain"
			}
			if _, err := h.db.Write().Exec(`UPDATE memberships SET team_id=?,role=? WHERE id=?`, tid, role, ids[team*2+j]); err != nil {
				t.Fatal(err)
			}
		}
	}
	opened := h.doAs(host, "POST", "/api/v1/rooms/"+rid+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey("creative-team-open")))
	if opened.Status != 201 {
		t.Fatalf("open %d %s", opened.Status, opened.Body)
	}
	var r struct {
		ID string `json:"roundId"`
	}
	json.Unmarshal(opened.Body, &r)
	path := "/api/v1/rounds/" + r.ID
	read := func(c *http.Client) creativeView {
		t.Helper()
		res := h.doAs(c, "GET", path, nil, nil)
		if res.Status != 200 {
			t.Fatalf("read %d %s", res.Status, res.Body)
		}
		var v creativeView
		if err := json.Unmarshal(res.Body, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	transition := func(target string) {
		t.Helper()
		v := read(host)
		res := h.doAs(host, "POST", path+"/transitions", map[string]any{"target": target, "expectedPhaseVersion": v.Round.Version}, writeHeaders(h, owner, idemKey("creative-team-"+target)))
		if res.Status != 201 {
			t.Fatalf("transition %s %d %s", target, res.Status, res.Body)
		}
	}
	submit := func(i int, action map[string]any, revision int, key string, body map[string]any) response {
		t.Helper()
		if body == nil {
			v := read(clients[i])
			gen := v.Generation
			if action["kind"] == "creative.vote" {
				gen = 0
			}
			body = map[string]any{"phaseVersion": v.Round.Version, "submissionRevision": revision, "controlGeneration": gen, "modeAction": action}
		}
		return h.doAs(clients[i], "POST", path+"/submissions", body, writeHeaders(h, sessions[i], key))
	}
	transition("briefing")
	if res := submit(0, map[string]any{"kind": "creative.work", "text": "雨天借伞"}, 0, idemKey("creative-team-early"), nil); res.Status != 409 {
		t.Fatalf("briefing write %d", res.Status)
	}
	transition("collecting")
	if res := submit(1, map[string]any{"kind": "creative.work", "text": "队友越权"}, 0, idemKey("creative-team-controller"), nil); res.Status != 409 {
		t.Fatalf("non-controller wrote %d %s", res.Status, res.Body)
	}
	for _, i := range []int{0, 2} {
		res := submit(i, map[string]any{"kind": "creative.work", "text": fmt.Sprintf("雨天的企划%d", i)}, 0, idemKey("creative-team-work"), nil)
		if res.Status != 201 {
			t.Fatalf("write %d %s", res.Status, res.Body)
		}
	}
	workA := read(clients[0]).Public.Self.WorkID
	workB := read(clients[2]).Public.Self.WorkID
	if len(read(clients[0]).Public.Ballot) > 0 || len(read(clients[4]).Public.Ballot) > 0 {
		t.Fatal("writing ballot leaked")
	}
	// The real deadline freezes review, with no automatic approval and no clock.
	svc := room.New(h.db, room.Config{}, room.Options{Epoch: "creative-restart", Modes: reg, Content: content})
	defer svc.Close()
	d, _ := time.Parse(time.RFC3339Nano, read(host).Round.Deadline)
	svc.SetClock(func() time.Time { return d.Add(time.Millisecond) })
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if read(host).Round.Deadline != "" || read(host).Round.Phase != "locked" {
		t.Fatal("writing timer skipped review")
	}
	if n, err := svc.SweepExpired(context.Background()); err != nil || n != 0 {
		t.Fatal("review auto advanced")
	}
	ordinary := h.doAs(clients[0], "GET", path, nil, nil)
	manager := h.doAs(host, "GET", path, nil, nil)
	var a, b map[string]any
	json.Unmarshal(ordinary.Body, &a)
	json.Unmarshal(manager.Body, &b)
	if _, ok := a["publicState"].(map[string]any)["reviewOptions"]; ok {
		t.Fatal("player saw review")
	}
	if len(b["publicState"].(map[string]any)["reviewOptions"].([]any)) != 2 {
		t.Fatal("manager review missing")
	}
	v := read(clients[0])
	denied := h.doAs(clients[0], "POST", path+"/transitions", map[string]any{"target": "creative-voting", "expectedPhaseVersion": v.Round.Version}, writeHeaders(h, sessions[0], idemKey("creative-team-forged-review")))
	if denied.Status != 403 {
		t.Fatal("player opened ballot")
	}
	transition("creative-voting")
	if read(clients[1]).Generation != 0 || !read(clients[1]).CanSubmit || !read(clients[4]).CanSubmit {
		t.Fatal("personal/spectator vote controls missing")
	}
	for _, i := range []int{0, 1} {
		if res := submit(i, map[string]any{"kind": "creative.vote", "optionId": workA}, 0, idemKey("creative-team-own"), nil); res.Status != 422 {
			t.Fatalf("own team vote %d %s", res.Status, res.Body)
		}
	}
	// Keep the exact first command for replay after the phase and HTTP receipt change.
	v = read(clients[1])
	key := idemKey("creative-team-personal-replay")
	body := map[string]any{"phaseVersion": v.Round.Version, "submissionRevision": 0, "controlGeneration": 0, "modeAction": map[string]any{"kind": "creative.vote", "optionId": workB}}
	for i := 0; i < 5; i++ {
		target := workB
		if i >= 2 {
			target = workA
		}
		keyNow := idemKey("creative-team-ballot")
		var original map[string]any
		if i == 1 {
			keyNow = key
			original = body
		}
		res := submit(i, map[string]any{"kind": "creative.vote", "optionId": target}, 0, keyNow, original)
		if res.Status != 201 {
			t.Fatalf("vote %d %d %s", i, res.Status, res.Body)
		}
	}
	if read(clients[1]).CanSubmit || readSlotRevisions(t, h, clients[1], r.ID).SubmissionRevisionsBySlot["vote"] != 1 {
		t.Fatal("vote confirmation not personal")
	}
	if res := submit(4, map[string]any{"kind": "creative.vote", "optionId": workB}, 1, idemKey("creative-team-repeat"), nil); res.Status != 422 {
		t.Fatal("spectator voted twice")
	}
	transition("locked")
	transition("revealed")
	transition("settled")
	result := read(clients[4]).Public.Result
	if result == nil || result.TeamVotes != 4 || result.AudienceVotes != 1 {
		t.Fatalf("separate actual pools %+v", result)
	}
	if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE route_scope NOT LIKE 'gameplay:%'`); err != nil {
		t.Fatal(err)
	}
	if res := submit(1, nil, 0, key, body); res.Status != 201 {
		t.Fatalf("old body replay %d %s", res.Status, res.Body)
	}
	if got := read(clients[4]).Public.Result; got.TeamVotes != 4 || got.AudienceVotes != 1 {
		t.Fatal("replay duplicated vote")
	}
}

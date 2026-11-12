package httpapi_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestHTTPNestedRulesPreparePersistAndRestartWithSameClock(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "manual", "trade-content.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(trade.New())
	content := tradeContent{raw: raw}
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = content })
	clock := time.Now().UTC().Truncate(time.Millisecond)
	h.rooms.SetClock(func() time.Time { return clock })
	host, owner := h.newSession("特别周")
	other, guest := h.newSession("特别周")
	third, thirdSession := h.newSession("小栗帽")
	res := h.doAs(host, "POST", "/api/v1/rooms", map[string]any{"name": "交易约局", "kind": "friend", "modeConfig": map[string]any{"modeId": "trade", "config": map[string]any{"rounds": 2, "roundSeconds": 35}}, "maxPlayers": 3, "maxSpectators": 0, "allowSpectators": false, "visibility": "invite"}, writeHeaders(h, owner, idemKey("config-room")))
	if res.Status != 201 {
		t.Fatalf("create %d %s", res.Status, res.Body)
	}
	var made struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	json.Unmarshal(res.Body, &made)
	joined := h.doAs(other, "POST", "/api/v1/rooms/"+made.Room.ID+"/memberships", map[string]any{"inviteCode": made.Code, "requestedRole": "player"}, writeHeaders(h, guest, idemKey("config-join")))
	if joined.Status != 201 {
		t.Fatalf("join %s", joined.Body)
	}
	joined = h.doAs(third, "POST", "/api/v1/rooms/"+made.Room.ID+"/memberships", map[string]any{"inviteCode": made.Code, "requestedRole": "player"}, writeHeaders(h, thirdSession, idemKey("config-third")))
	if joined.Status != 201 {
		t.Fatalf("third join %s", joined.Body)
	}
	opened := h.doAs(host, "POST", "/api/v1/rooms/"+made.Room.ID+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey("config-open")))
	if opened.Status != 201 {
		t.Fatalf("open %s", opened.Body)
	}
	var r struct {
		ID string `json:"roundId"`
	}
	json.Unmarshal(opened.Body, &r)
	path := "/api/v1/rounds/" + r.ID
	type configView struct {
		Round struct {
			Phase    string     `json:"phase"`
			Version  int        `json:"phaseVersion"`
			Deadline *time.Time `json:"deadlineAt"`
		} `json:"round"`
		Public struct {
			Config struct {
				Rounds  int `json:"rounds"`
				Seconds int `json:"roundSeconds"`
			} `json:"config"`
			Current int `json:"currentRound"`
		} `json:"publicState"`
	}
	read := func() configView {
		t.Helper()
		res := h.doAs(host, "GET", path, nil, nil)
		if res.Status != 200 {
			t.Fatalf("get %s", res.Body)
		}
		var v configView
		if err := json.Unmarshal(res.Body, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, target := range []string{"briefing", "collecting"} {
		v := read()
		res := h.doAs(host, "POST", path+"/transitions", map[string]any{"target": target, "expectedPhaseVersion": v.Round.Version}, writeHeaders(h, owner, idemKey("config-"+target)))
		if res.Status != 201 && res.Status != 200 {
			t.Fatalf("transition %s", res.Body)
		}
		if target == "collecting" {
			var receipt struct {
				AcceptedAt time.Time `json:"acceptedAt"`
			}
			if err := json.Unmarshal(res.Body, &receipt); err != nil {
				t.Fatal(err)
			}
			clock = receipt.AcceptedAt // Authority is queue arrival, never fixture wall time.
		}
	}
	v := read()
	if v.Public.Config.Rounds != 2 || v.Public.Config.Seconds != 35 || v.Round.Deadline == nil || !v.Round.Deadline.Equal(clock.Add(35*time.Second)) {
		t.Fatalf("rules/timer diverged %+v", v)
	}
	var stored string
	if err := h.db.Read().QueryRow(`SELECT config_json FROM rounds WHERE id=?`, r.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var flat map[string]any
	if err := json.Unmarshal([]byte(stored), &flat); err != nil {
		t.Fatal(err)
	}
	if flat["modeId"] != "trade" || flat["rounds"] != float64(2) || flat["roundSeconds"] != float64(35) || flat["config"] != nil {
		t.Fatalf("not the actual prepared rules: %s", stored)
	}
	restored := room.New(h.db, room.Config{}, room.Options{Modes: reg, Content: content, Epoch: "restarted"})
	defer restored.Close()
	restored.SetClock(func() time.Time { return clock })
	clock = clock.Add(36 * time.Second)
	if _, err := restored.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	v = read()
	if v.Public.Current != 2 || v.Round.Phase != "collecting" || !v.Round.Deadline.Equal(clock.Add(35*time.Second)) {
		t.Fatalf("restart lost rules %+v", v)
	}
	clock = clock.Add(36 * time.Second)
	if _, err := restored.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v := read(); v.Round.Phase != "locked" {
		t.Fatalf("configured two turns not respected %+v", v)
	}
	var after string
	if err := h.db.Read().QueryRow(`SELECT config_json FROM rounds WHERE id=?`, r.ID).Scan(&after); err != nil || after != stored {
		t.Fatalf("stored rules rewritten: %v", err)
	}
}

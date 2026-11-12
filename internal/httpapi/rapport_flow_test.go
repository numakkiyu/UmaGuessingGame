package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/rapport"
	"github.com/bhcn/umaguessinggame/internal/room"
)

type rapportContent struct{ tradeContent }

func (c rapportContent) Section(name string) (json.RawMessage, bool) {
	return c.raw, name == rapport.QuestionsSection
}
func (rapportContent) ModeAdmission(id string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{Mode: id, Available: true, QuestionCount: 12, Section: rapport.QuestionsSection}, id == rapport.ModeID
}

type rapportView struct {
	Round struct {
		ID       string     `json:"roundId"`
		Version  int        `json:"phaseVersion"`
		Deadline *time.Time `json:"deadlineAt"`
	} `json:"round"`
	CanSubmit       bool  `json:"canSubmit"`
	Revision        int   `json:"submissionRevision"`
	Generation      int   `json:"controlGeneration"`
	TeamControllers []any `json:"teamControllers"`
	Public          struct {
		Current       int             `json:"currentQuestion"`
		QuestionCount int             `json:"questionCount"`
		Self          json.RawMessage `json:"self"`
		Questions     []struct {
			ID      string `json:"questionId"`
			Options []struct {
				ID string `json:"optionId"`
			} `json:"options"`
			Own     string          `json:"selfOptionId"`
			Chooser string          `json:"chooserId"`
			Reveal  json.RawMessage `json:"reveal"`
		} `json:"questions"`
	} `json:"publicState"`
	Private struct {
		Mode   string  `json:"modeId"`
		Choice *string `json:"selectedOptionId"`
	} `json:"privateState"`
	Results []struct {
		Subject string `json:"subjectId"`
		Units   int    `json:"scoreUnits"`
	} `json:"results"`
}

func TestHTTPRapportIndividualSeatsTeamsAndRecovery(t *testing.T) {
	for _, teamPlay := range []bool{false, true} {
		t.Run(fmt.Sprint("teams-", teamPlay), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "data", "manual", "rapport-questions.json"))
			if err != nil {
				t.Fatal(err)
			}
			reg, _ := modes.NewRegistry(rapport.New())
			h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = rapportContent{tradeContent{raw: raw}} })
			host, owner := h.newSession("主持")
			kind := "friend"
			if teamPlay {
				kind = "event"
			}
			made := h.doAs(host, "POST", "/api/v1/rooms", map[string]any{"name": "默契场", "kind": kind, "modeConfig": map[string]any{"modeId": "rapport", "config": map[string]any{"questions": 2}}, "maxPlayers": 6, "maxSpectators": 2, "allowSpectators": true, "visibility": "invite"}, writeHeaders(h, owner, idemKey("rapport-room")))
			if made.Status != 201 {
				t.Fatalf("create %d %s", made.Status, made.Body)
			}
			var created struct {
				Room struct {
					ID     string `json:"roomId"`
					Viewer struct {
						ID string `json:"membershipId"`
					} `json:"viewer"`
				} `json:"room"`
				Code string `json:"inviteCode"`
			}
			json.Unmarshal(made.Body, &created)
			rid := created.Room.ID
			clients := []*http.Client{}
			sessions := []testSession{}
			ids := []string{}
			if !teamPlay {
				clients = append(clients, host)
				sessions = append(sessions, owner)
				ids = append(ids, created.Room.Viewer.ID)
			}
			count := 2
			if teamPlay {
				count = 4
			}
			for len(ids) < count {
				c, s := h.newSession("特别周")
				joined := h.doAs(c, "POST", "/api/v1/rooms/"+rid+"/memberships", map[string]any{"inviteCode": created.Code, "requestedRole": "player"}, writeHeaders(h, s, idemKey("rapport-join")))
				if joined.Status != 201 {
					t.Fatalf("join %s", joined.Body)
				}
				var m struct {
					ID string `json:"membershipId"`
				}
				json.Unmarshal(joined.Body, &m)
				clients = append(clients, c)
				sessions = append(sessions, s)
				ids = append(ids, m.ID)
			}
			if teamPlay {
				for team := 0; team < 2; team++ {
					tid := fmt.Sprintf("rapport-team-%d", team)
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
			}
			opened := h.doAs(host, "POST", "/api/v1/rooms/"+rid+"/rounds", map[string]any{}, writeHeaders(h, owner, idemKey("rapport-open")))
			if opened.Status != 201 {
				t.Fatalf("open %d %s", opened.Status, opened.Body)
			}
			var r struct {
				ID string `json:"roundId"`
			}
			json.Unmarshal(opened.Body, &r)
			path := "/api/v1/rounds/" + r.ID
			read := func(c *http.Client) rapportView {
				t.Helper()
				res := h.doAs(c, "GET", path, nil, nil)
				if res.Status != 200 {
					t.Fatalf("read %s", res.Body)
				}
				var v rapportView
				if err := json.Unmarshal(res.Body, &v); err != nil {
					t.Fatal(err)
				}
				return v
			}
			transition := func(target string) {
				t.Helper()
				v := read(host)
				res := h.doAs(host, "POST", path+"/transitions", map[string]any{"target": target, "expectedPhaseVersion": v.Round.Version}, writeHeaders(h, owner, idemKey("rapport-"+target)))
				if res.Status != 201 && res.Status != 200 {
					t.Fatalf("transition %s %s", target, res.Body)
				}
			}
			submit := func(i int, option, key string, body map[string]any) response {
				t.Helper()
				if body == nil {
					v := read(clients[i])
					body = map[string]any{"phaseVersion": v.Round.Version, "submissionRevision": v.Revision, "controlGeneration": v.Generation, "modeAction": map[string]any{"kind": "rapport.answer", "optionId": option}}
				}
				return h.doAs(clients[i], "POST", path+"/submissions", body, writeHeaders(h, sessions[i], key))
			}
			transition("briefing")
			v := read(clients[0])
			if res := submit(0, v.Public.Questions[0].Options[0].ID, idemKey("rapport-early"), nil); res.Status != 409 {
				t.Fatalf("early %d %s", res.Status, res.Body)
			}
			transition("collecting")
			before := read(host)
			blocked := h.doAs(host, "POST", path+"/transitions", map[string]any{"target": "locked", "expectedPhaseVersion": before.Round.Version}, writeHeaders(h, owner, idemKey("rapport-no-skip")))
			if blocked.Status != 409 {
				t.Fatalf("host skipped question: %d %s", blocked.Status, blocked.Body)
			}
			initial := read(clients[0])
			if initial.Round.Deadline == nil || time.Until(*initial.Round.Deadline) > 21*time.Second || time.Until(*initial.Round.Deadline) < 18*time.Second {
				t.Fatal("not a per-question clock")
			}
			for _, c := range clients {
				v := read(c)
				if !v.CanSubmit || len(v.TeamControllers) > 0 || len(v.Public.Questions) != 1 || v.Private.Mode != "rapport" {
					t.Fatalf("not individual %+v", v)
				}
			}
			if teamPlay {
				v := read(host)
				if v.CanSubmit || len(v.Public.Self) > 0 || v.Private.Mode != "" {
					t.Fatal("host gets player state")
				}
			}
			q := initial.Public.Questions[0]
			body := map[string]any{"phaseVersion": initial.Round.Version, "submissionRevision": initial.Revision, "controlGeneration": initial.Generation, "modeAction": map[string]any{"kind": "rapport.answer", "optionId": q.Options[0].ID}}
			key := idemKey("rapport-saved")
			if res := submit(0, q.Options[0].ID, key, body); res.Status != 201 {
				t.Fatalf("first %s", res.Body)
			}
			own := read(clients[0])
			other := read(clients[1])
			if own.CanSubmit || own.Private.Choice == nil || *own.Private.Choice != q.Options[0].ID || other.Private.Choice != nil || len(other.Public.Questions[0].Reveal) > 0 || !other.CanSubmit {
				t.Fatal("private isolation / personal answer")
			}
			if !own.Round.Deadline.Equal(*initial.Round.Deadline) {
				t.Fatal("partial answer renewed time")
			}
			for i := 1; i < len(clients); i++ {
				v := read(clients[i])
				pick := v.Public.Questions[0].Options[0].ID
				if teamPlay && i >= 2 && v.Public.Questions[0].Chooser != ids[i] {
					pick = v.Public.Questions[0].Options[1].ID
				}
				res := submit(i, pick, idemKey("rapport-all"), nil)
				if res.Status != 201 {
					t.Fatalf("answer %d %s", i, res.Body)
				}
			}
			after := read(clients[0])
			if after.Public.Current != 2 || !after.CanSubmit || after.Private.Choice != nil || len(after.Public.Questions[0].Reveal) == 0 {
				t.Fatal("all answered did not advance")
			}
			if _, err := h.db.Write().Exec(`DELETE FROM idempotency_receipts WHERE route_scope NOT LIKE 'gameplay:%'`); err != nil {
				t.Fatal(err)
			}
			if res := submit(0, q.Options[0].ID, key, body); res.Status != 201 {
				t.Fatalf("recovery %d %s", res.Status, res.Body)
			}
			if read(clients[0]).Public.Current != 2 || read(clients[0]).Revision != 1 {
				t.Fatal("recovery answered next question")
			}
			// Recreate the service over the same SQLite file, then expire the current
			// question. Missing preferences must not fabricate a hit or shrink the divisor.
			restored := room.New(h.db, room.Config{}, room.Options{Epoch: "restarted", Modes: reg, Content: rapportContent{tradeContent{raw: raw}}})
			defer restored.Close()
			restored.SetClock(func() time.Time { return after.Round.Deadline.Add(time.Millisecond) })
			if teamPlay {
				transition("pause")
				if n, err := restored.SweepExpired(context.Background()); err != nil || n != 0 {
					t.Fatalf("paused round advanced: %d %v", n, err)
				}
				if res := submit(0, read(clients[0]).Public.Questions[1].Options[0].ID, idemKey("rapport-paused"), nil); res.Status != 409 {
					t.Fatalf("paused answer: %d %s", res.Status, res.Body)
				}
				if read(clients[0]).Public.Current != 2 {
					t.Fatal("pause changed question")
				}
				transition("resume")
				after = read(clients[0])
			}
			if _, err := restored.SweepExpired(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"revealed", "settled"} {
				transition(target)
			}
			ended := read(clients[0])
			if len(ended.Results) != 2 {
				t.Fatalf("results %+v", ended.Results)
			}
			for _, score := range ended.Results {
				expected := 5000
				if teamPlay && score.Subject == "rapport-team-1" {
					expected = 0
				}
				if score.Units != expected {
					t.Fatalf("fixed denominator/independent score %+v", ended.Results)
				}
			}
		})
	}
}

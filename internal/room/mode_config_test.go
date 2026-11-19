package room_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/modes/audio"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/modes/cluepuzzle"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
	"github.com/bhcn/umaguessinggame/internal/modes/draft"
	"github.com/bhcn/umaguessinggame/internal/modes/memory"
	"github.com/bhcn/umaguessinggame/internal/modes/pedigree"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
	"github.com/bhcn/umaguessinggame/internal/modes/rapport"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestNestedRulesValidatedByEveryMode(t *testing.T) {
	cases := []struct {
		mode    modes.Mode
		invalid string
	}{
		{classic.New(), `{"maxWrongGuesses":-1}`}, {aptitudegrid.New(), `{"rowAxis":"unknown"}`},
		{draft.New(), `{"planningSeconds":-1}`}, {racerebuild.New(), `{"questionSeconds":-1}`},
		{coracepath.New(), `{"questionSeconds":-1}`}, {sortgame.New(), `{"cardCount":-1}`},
		{memory.New(), `{"viewSeconds":-1}`}, {bluff.New(), `{"writeSeconds":-1}`},
		{cluepuzzle.New(), `{"cards":-1}`}, {strategyrace.New(), `{"decisionSeconds":-1}`},
		{rapport.New(), `{"questions":-1}`}, {creative.New(), `{"writeSeconds":-1}`},
		{trade.New(), `{"rounds":-1}`}, {pedigree.New(), `{"slotCount":-1}`}, {audio.New(), `{"clips":-1}`},
	}
	for _, tc := range cases {
		t.Run(tc.mode.ID(), func(t *testing.T) {
			if err := tc.mode.ValidateConfig(json.RawMessage(tc.invalid)); err == nil {
				t.Fatal("invalid fixture accepted directly")
			}
			db := newDB(t)
			reg, _ := modes.NewRegistry(tc.mode)
			svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "config-test"})
			defer svc.Close()
			_, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES('s','h','训练员','c','player',0,9999999999999)`)
			if err != nil {
				t.Fatal(err)
			}
			raw := json.RawMessage(fmt.Sprintf(`{"modeId":%q,"config":%s}`, tc.mode.ID(), tc.invalid))
			_, err = svc.CreateRoom(context.Background(), "s", room.RoomCreateInput{Name: "规则测试", Kind: "friend", ModeConfig: raw, MaxPlayers: 2, Visibility: "invite"})
			if err == nil || problem.As(err).Status != 400 {
				t.Fatalf("nested invalid rule not rejected: %v", err)
			}
		})
	}
}

func TestRuleEnvelopeRejectsContradictionsAndMetadataOverrides(t *testing.T) {
	cases := []struct {
		name, raw string
		valid     bool
	}{
		{"flat", `{"modeId":"sort","key":"birthday","questionSeconds":35}`, true},
		{"nested", `{"modeId":"sort","config":{"key":"birthday","questionSeconds":35}}`, true},
		{"mixed", `{"modeId":"sort","informationStyle":"open","questionSeconds":35,"config":{"key":"birthday"}}`, true},
		{"same", `{"modeId":"sort","questionSeconds":35,"config":{"questionSeconds":35}}`, true},
		{"escaped same", `{"modeId":"sort","key":"birthday","config":{"key":"birth\u0064ay"}}`, true},
		{"different", `{"modeId":"sort","questionSeconds":35,"config":{"questionSeconds":40}}`, false},
		{"type conflict", `{"modeId":"sort","questionSeconds":35,"config":{"questionSeconds":"35"}}`, false},
		{"different numeric spelling", `{"modeId":"sort","questionSeconds":35,"config":{"questionSeconds":35.0}}`, false},
		{"mode override", `{"modeId":"sort","config":{"modeId":"memory"}}`, false},
		{"style override", `{"modeId":"sort","config":{"informationStyle":"open"}}`, false},
		{"rules override", `{"modeId":"sort","config":{"rulesVersion":"other"}}`, false},
		{"pack override", `{"modeId":"sort","config":{"contentPackId":"other"}}`, false},
		{"recursive envelope", `{"modeId":"sort","config":{"config":{}}}`, false},
		{"null envelope", `{"modeId":"sort","config":null}`, false},
		{"array envelope", `{"modeId":"sort","config":[]}`, false},
		{"number envelope", `{"modeId":"sort","config":1}`, false},
		{"root null", `null`, false}, {"root array", `[]`, false},
	}
	db := newDB(t)
	reg, _ := modes.NewRegistry(sortgame.New())
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "config-shapes"})
	defer svc.Close()
	if _, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES('s','h','训练员','c','player',0,9999999999999)`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateRoom(context.Background(), "s", room.RoomCreateInput{Name: "规则", Kind: "friend", ModeConfig: json.RawMessage(tc.raw), MaxPlayers: 2, Visibility: "invite"})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

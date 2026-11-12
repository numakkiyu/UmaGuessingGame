package aptitudegrid_test

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
)

// duelPublic 构造一份 duel 形态的最小公开状态。
// 字段名与 mode.go 的 preparedEnvelope / boardState 一致。
func duelPublic(t *testing.T, turnSubject string) json.RawMessage {
	t.Helper()
	body := map[string]any{
		"modeId":       "aptitude-grid",
		"rulesVersion": aptitudegrid.RulesVersion,
		"round": map[string]any{
			"config":   map[string]any{"variant": "duel"},
			"subjects": []map[string]any{{"subjectId": "mem_a"}, {"subjectId": "mem_b"}},
		},
		"board": map[string]any{
			"claims":        []any{},
			"closed":        false,
			"turnSubjectId": turnSubject,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal duel public: %v", err)
	}
	return raw
}

func readTurn(t *testing.T, raw json.RawMessage) (string, bool) {
	t.Helper()
	var view struct {
		Board struct {
			TurnSubjectID string `json:"turnSubjectId"`
			Closed        bool   `json:"closed"`
			Claims        []any  `json:"claims"`
		} `json:"board"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode after expire: %v", err)
	}
	return view.Board.TurnSubjectID, view.Board.Closed
}

// TestDuelTurnWindowIsPerTurn duel 的回合窗口是 30 秒，挑战形态没有回合时钟。
func TestDuelTurnWindowIsPerTurn(t *testing.T) {
	t.Parallel()
	mode := aptitudegrid.New()

	duel := mode.TurnWindow(json.RawMessage(`{"variant":"duel"}`))
	if duel <= 0 {
		t.Fatalf("duel TurnWindow = %v, want a positive per-turn clock", duel)
	}

	challenge := mode.TurnWindow(json.RawMessage(`{"variant":"challenge"}`))
	if challenge != 0 {
		t.Fatalf("challenge TurnWindow = %v, want 0 (not turn-based)", challenge)
	}
}

// TestExpireTurnPassesToNextSubject 超时只换手，不送格。
func TestExpireTurnPassesToNextSubject(t *testing.T) {
	t.Parallel()
	mode := aptitudegrid.New()

	public := duelPublic(t, "mem_a")
	next, more, err := mode.ExpireTurn(public)
	if err != nil {
		t.Fatalf("expire turn: %v", err)
	}
	if !more {
		t.Fatal("more = false after expiring a turn on an open board")
	}

	turn, closed := readTurn(t, next)
	if turn != "mem_b" {
		t.Fatalf("turn after expiry = %q, want mem_b (the clock passes the turn)", turn)
	}
	if closed {
		t.Fatal("board must stay open after a turn timeout")
	}

	// 关键：超时不等于送一个格子。
	var view struct {
		Board struct {
			Claims []any `json:"claims"`
		} `json:"board"`
	}
	if err := json.Unmarshal(next, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view.Board.Claims) != 0 {
		t.Fatalf("claims after expiry = %d, want 0 (a timeout must not claim a cell)",
			len(view.Board.Claims))
	}
}

// TestExpireTurnRotatesBack 两人局连续超时应当回到第一个人。
func TestExpireTurnRotatesBack(t *testing.T) {
	t.Parallel()
	mode := aptitudegrid.New()

	public := duelPublic(t, "mem_a")
	first, _, err := mode.ExpireTurn(public)
	if err != nil {
		t.Fatalf("first expire: %v", err)
	}
	second, _, err := mode.ExpireTurn(first)
	if err != nil {
		t.Fatalf("second expire: %v", err)
	}
	if turn, _ := readTurn(t, second); turn != "mem_a" {
		t.Fatalf("turn after two expiries = %q, want mem_a", turn)
	}
}

// TestExpireTurnStopsOnClosedBoard 棋盘关闭后不再换手。
func TestExpireTurnStopsOnClosedBoard(t *testing.T) {
	t.Parallel()
	mode := aptitudegrid.New()

	body := map[string]any{
		"modeId": "aptitude-grid",
		"round": map[string]any{
			"config":   map[string]any{"variant": "duel"},
			"subjects": []map[string]any{{"subjectId": "mem_a"}, {"subjectId": "mem_b"}},
		},
		"board": map[string]any{"claims": []any{}, "closed": true, "turnSubjectId": "mem_a"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, more, err := mode.ExpireTurn(raw)
	if err != nil {
		t.Fatalf("expire on closed board: %v", err)
	}
	if more {
		t.Fatal("more = true on a closed board; the room layer would keep renewing the clock")
	}
}

// 编译期断言：模式确实满足可选接口。
var _ modes.TurnExpirer = aptitudegrid.New()

func TestAbsentDuelPlayersCannotKeepRoundOpenForever(t *testing.T) {
	mode := aptitudegrid.New()
	public := duelPublic(t, "mem_a")
	for turn := 1; turn <= 3; turn++ {
		next, more, err := mode.ExpireTurn(public)
		if err != nil {
			t.Fatal(err)
		}
		public = next
		if more != (turn < 3) {
			t.Fatalf("turn %d: more=%v; the second missed own turn must end the duel", turn, more)
		}
	}
	_, closed := readTurn(t, public)
	if !closed {
		t.Fatal("consecutive timeouts left the board open")
	}
	next, more, err := mode.ExpireTurn(public)
	if err != nil || more || string(next) != string(public) {
		t.Fatalf("closed board changed on repeat expiry: more=%v err=%v", more, err)
	}
}

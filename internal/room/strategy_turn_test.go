package room_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

const (
	strategyModeID = "strategy-race"
	// 两个队员，8 回合，每回合 20 秒 —— 与默认基线一致。
	strategyCfg = `{"modeId":"strategy-race","config":{"totalTurns":8,"decisionSeconds":20}}`
)

// newStrategyService 建一个注册了真实 strategy-race 模式的服务，时钟由调用方控制。
func newStrategyService(t *testing.T) (*storage.DB, *room.Service, *time.Time) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.Config{Dir: t.TempDir(), ReadConns: 2})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reg, err := modes.NewRegistry(strategyrace.New())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test", Modes: reg})
	t.Cleanup(svc.Close)

	// 受控时钟：测试自己推进 now。
	clock := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return clock })
	return db, svc, &clock
}

// seedStrategyRound 直接写一条 collecting 的策略赛回合。
//
// 不调用 Prepare：Prepare 需要真实内容包（"rules.track" 没有内置兜底），
// 而这条测试要验的是「时钟推进回合」，与题包无关。公开状态按 strategyrace
// 的 publicState 字段手写，只填 ExpireTurn 用得到的部分（config/subjects/committed）。
func seedStrategyRound(t *testing.T, db *storage.DB, now time.Time, deadline time.Time) (roomID, roundID string) {
	t.Helper()
	ctx := context.Background()

	public := map[string]any{
		"modeId":       strategyModeID,
		"rulesVersion": strategyrace.RulesVersion,
		"config": map[string]any{
			"totalTurns": 8, "decisionSeconds": 20,
		},
		"subjects": []map[string]any{
			{"subjectId": "mem_a", "subjectKind": "team"},
			{"subjectId": "mem_b", "subjectKind": "team"},
		},
		// track 是投影必需的：resolveRace 要按它算位置与体力。
		// 数值取 的基线，与线上题包一致，避免测出一个假轨道。
		"track": map[string]any{
			"totalTurns": 8, "decisionSeconds": 20,
			"startPosition": 0, "startStamina": 6, "staminaCap": 8,
			"followMinGap": 1, "followMaxGap": 3,
			"defaultAction": "steady",
			"actions": []map[string]any{
				{"action": "steady", "label": "稳进", "positionGain": 3, "staminaDelta": 0},
				{"action": "sprint", "label": "冲刺", "positionGain": 5, "staminaDelta": -2, "minStamina": 2},
				{"action": "recover", "label": "恢复", "positionGain": 1, "staminaDelta": 2},
				{"action": "follow", "label": "跟跑", "positionGain": 4, "alternateGain": 2, "staminaDelta": 0, "leaderGap": true},
			},
			"terrainChanges": false, "randomEvents": false,
		},
		"competitive": true,
		"committed":   []any{},
	}
	publicJSON, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public: %v", err)
	}
	privateJSON := []byte(`{"modeId":"strategy-race","note":"no hidden answer"}`)

	roomID = "rom_turn_timeout"
	roundID = "rnd_turn_timeout"
	ms := now.UnixMilli()

	// rooms 的真实列：没有 kind；owner_session_id 必填，所以先落一条会话。
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES ('ses_owner', 'h', 'Owner', 'c', 'player', ?, ?)`,
		ms, ms+3600_000); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rooms (id, owner_session_id, name, status, config_json, room_version, created_at, updated_at)
		VALUES (?, 'ses_owner', 'turn-timeout', 'playing', ?, 1, ?, ?)`,
		roomID, strategyCfg, ms, ms); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at,
		                    seed, config_json, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, 'ds-test', 'pack-test', 'collecting', 1, ?, 'seed-1', ?, ?, ?)`,
		roundID, roomID, strategyModeID, strategyrace.RulesVersion,
		deadline.UnixMilli(), strategyCfg, ms, ms); err != nil {
		t.Fatalf("seed round: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'public', ?)`,
		roundID, string(publicJSON)); err != nil {
		t.Fatalf("seed public state: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'round', ?)`,
		roundID, string(privateJSON)); err != nil {
		t.Fatalf("seed private state: %v", err)
	}
	return roomID, roundID
}

// readCurrentTurn 用模式的真实投影读当前回合号。
//
// 不能直接读 round_private_state 里的 state_json：那是存储形态，只有
// config/subjects/committed；currentTurn 是 Project 从 committed 推导出来的
// 投影字段，界面看到的是它。读存储会恒得到 0。
func readCurrentTurn(t *testing.T, db *storage.DB, roundID string) int {
	t.Helper()
	ctx := context.Background()

	var raw string
	if err := db.Read().QueryRowContext(ctx,
		`SELECT state_json FROM round_private_state WHERE round_id = ? AND scope = 'public'`,
		roundID).Scan(&raw); err != nil {
		t.Fatalf("read public state: %v", err)
	}
	projected, err := strategyrace.New().Project(modes.ProjectInput{
		Scope:  modes.Scope{ModeID: strategyModeID, RulesVersion: strategyrace.RulesVersion},
		Viewer: modes.Viewer{Subject: modes.Subject{ID: "mem_a", Kind: modes.SubjectTeam}},
		Public: json.RawMessage(raw),
		Phase:  modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("project round: %v", err)
	}
	var view struct {
		CurrentTurn int `json:"currentTurn"`
	}
	if err := json.Unmarshal(projected, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	return view.CurrentTurn
}

// readDeadline 读回合截止时间（毫秒）。
func readDeadline(t *testing.T, db *storage.DB, roundID string) (int64, bool) {
	t.Helper()
	var deadline *int64
	if err := db.Read().QueryRowContext(context.Background(),
		`SELECT deadline_at FROM rounds WHERE id = ?`, roundID).Scan(&deadline); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	if deadline == nil {
		return 0, false
	}
	return *deadline, true
}

// TestStrategyTurnExpiresWithDefaultAction 是本项的核心断言。
//
// 两个队都不提交，只把时钟推过 20 秒，回合必须前进到第 2 回合，
// 且第 1 回合里两支队伍都被记成默认行动（steady）。
func TestStrategyTurnExpiresWithDefaultAction(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)

	start := *clock
	_, roundID := seedStrategyRound(t, db, start, start.Add(20*time.Second))

	if got := readCurrentTurn(t, db, roundID); got != 1 {
		t.Fatalf("currentTurn before expiry = %d, want 1", got)
	}

	// 推进 21 秒：第 1 回合的 20 秒窗口已经过去。
	*clock = start.Add(21 * time.Second)
	advanced, err := svc.SweepExpired(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if advanced == 0 {
		t.Fatal("sweep advanced nothing; the turn clock did not fire")
	}

	if got := readCurrentTurn(t, db, roundID); got != 2 {
		t.Fatalf("currentTurn after expiry = %d, want 2 (turn 1 must close on the clock, not on submissions)", got)
	}

	// 第 2 回合的截止时间必须被重新给满一个窗口。
	deadline, ok := readDeadline(t, db, roundID)
	if !ok {
		t.Fatal("turn 2 has no deadline; the next turn must get its own clock")
	}
	want := (*clock).Add(20 * time.Second).UnixMilli()
	if deadline != want {
		t.Fatalf("turn 2 deadline = %d, want %d (one full turn window from now)", deadline, want)
	}

	// 回合仍然在 collecting：回合推进不等于整局收卷。
	var status string
	if err := db.Read().QueryRowContext(context.Background(),
		`SELECT status FROM rounds WHERE id = ?`, roundID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "collecting" {
		t.Fatalf("round status after one turn expiry = %q, want collecting (the race is not over)", status)
	}
}

// TestStrategyTurnExpiryIsIdempotent 重复扫描不能把同一回合补两遍。
func TestStrategyTurnExpiryIsIdempotent(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)

	start := *clock
	_, roundID := seedStrategyRound(t, db, start, start.Add(20*time.Second))

	*clock = start.Add(21 * time.Second)
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	turnAfterFirst := readCurrentTurn(t, db, roundID)

	// 同一时刻再扫一次：第 2 回合的 deadline 还没到，不应再推进。
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if got := readCurrentTurn(t, db, roundID); got != turnAfterFirst {
		t.Fatalf("second sweep moved currentTurn %d -> %d; a repeated sweep must not close a turn twice",
			turnAfterFirst, got)
	}
}

// TestStrategyPausedTurnDoesNotExpire 暂停期间时钟不走。
func TestStrategyPausedTurnDoesNotExpire(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)

	start := *clock
	_, roundID := seedStrategyRound(t, db, start, start.Add(20*time.Second))

	// 打上暂停标记。
	if _, err := db.Write().ExecContext(context.Background(),
		`UPDATE rounds SET paused_at = ? WHERE id = ?`, start.UnixMilli(), roundID); err != nil {
		t.Fatalf("pause round: %v", err)
	}

	*clock = start.Add(60 * time.Second)
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatalf("sweep while paused: %v", err)
	}
	if got := readCurrentTurn(t, db, roundID); got != 1 {
		t.Fatalf("currentTurn while paused = %d, want 1 (a paused round must not consume turns)", got)
	}
}

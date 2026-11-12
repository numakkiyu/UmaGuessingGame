package room_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

func newTradeService(t *testing.T) (*storage.DB, *room.Service, *time.Time) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.Config{Dir: t.TempDir(), ReadConns: 2})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reg, err := modes.NewRegistry(trade.New())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test", Modes: reg})
	t.Cleanup(svc.Close)

	clock := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return clock })
	return db, svc, &clock
}

// seedTradeRound writes one collecting trade round with a pending proposal open,
// so the sweep has something to expire as well as something to advance.
//
// Prepare needs the real card and objective pools, which this test deliberately
// avoids: what is under test is the clock, not the pack. The public state carries
// only the fields ExpireTurn and Project read.
func seedTradeRound(t *testing.T, db *storage.DB, now time.Time, deadline time.Time) (roomID, roundID string) {
	t.Helper()
	ctx := context.Background()

	public := map[string]any{
		"modeId":       trade.ModeID,
		"rulesVersion": trade.RulesVersion,
		"config": map[string]any{
			"rounds": 3, "roundSeconds": 60, "handSize": 5,
		},
		"subjects": []map[string]any{
			{"subjectId": "mem_a", "subjectKind": "player"},
			{"subjectId": "mem_b", "subjectKind": "player"},
			{"subjectId": "mem_c", "subjectKind": "player"},
		},
		"hands": map[string][]string{
			"mem_a": {"i-a1", "i-a2"},
			"mem_b": {"i-b1", "i-b2"},
			"mem_c": {"i-c1", "i-c2"},
		},
		"instances": []map[string]any{
			{"instanceId": "i-a1", "cardId": "speed", "label": "速度"},
			{"instanceId": "i-a2", "cardId": "stamina", "label": "耐力"},
			{"instanceId": "i-b1", "cardId": "power", "label": "力量"},
			{"instanceId": "i-b2", "cardId": "guts", "label": "根性"},
			{"instanceId": "i-c1", "cardId": "wit", "label": "智慧"},
			{"instanceId": "i-c2", "cardId": "speed", "label": "速度"},
		},
		// One open proposal, opened in round 1, must be expired by the round boundary.
		"trades": []map[string]any{{
			"tradeId": "trd_1", "senderId": "mem_a", "recipientId": "mem_b",
			"offeredInstanceId": "i-a1", "requestedInstanceId": "i-b1",
			"status": "pending", "round": 1, "phaseVersion": 1,
		}},
		"currentRound": 1,
		"objectives":   []any{},
		"cardKinds":    []any{},
		"scoring":      "按手里的卡判定秘密任务",
	}
	publicJSON, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public: %v", err)
	}
	privateJSON := []byte(`{"modeId":"trade","note":"objectives live per subject"}`)

	roomID = "rom_trade_rounds"
	roundID = "rnd_trade_rounds"
	ms := now.UnixMilli()

	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES ('ses_owner', 'h', 'Owner', 'c', 'player', ?, ?)`,
		ms, ms+3600_000); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rooms (id, owner_session_id, name, status, config_json, room_version, created_at, updated_at)
		VALUES (?, 'ses_owner', 'trade-rounds', 'playing', '{}', 1, ?, ?)`,
		roomID, ms, ms); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at,
		                    seed, config_json, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, 'ds-test', 'pack-test', 'collecting', 1, ?, 'seed-1', ?, ?, ?)`,
		roundID, roomID, trade.ModeID, trade.RulesVersion,
		deadline.UnixMilli(), `{"rounds":3,"roundSeconds":60,"handSize":5}`, ms, ms); err != nil {
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

// readTradeBoard projects the stored public state through the real mode, because
// currentRound and trade statuses are projection facts, not storage columns.
func readTradeBoard(t *testing.T, db *storage.DB, roundID string) (currentRound int, tradeStatuses []string) {
	t.Helper()
	var raw string
	if err := db.Read().QueryRowContext(context.Background(),
		`SELECT state_json FROM round_private_state WHERE round_id = ? AND scope = 'public'`,
		roundID).Scan(&raw); err != nil {
		t.Fatalf("read public state: %v", err)
	}
	projected, err := trade.New().Project(modes.ProjectInput{
		Scope:  modes.Scope{ModeID: trade.ModeID, RulesVersion: trade.RulesVersion},
		Viewer: modes.Viewer{Subject: modes.Subject{ID: "mem_a", Kind: modes.SubjectPlayer}},
		Public: json.RawMessage(raw),
		Phase:  modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("project round: %v", err)
	}
	var view struct {
		CurrentRound int `json:"currentRound"`
		Trades       []struct {
			Status string `json:"status"`
		} `json:"trades"`
	}
	if err := json.Unmarshal(projected, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	for _, tr := range view.Trades {
		tradeStatuses = append(tradeStatuses, tr.Status)
	}
	return view.CurrentRound, tradeStatuses
}

// TestTradeRoundsAdvanceWithoutAnyPlayerAction is the room-level G13 natural
// three-round assertion: nobody acts, the clock advances, and the room layer drives
// the mode's own turn expiry until the configured third round closes the match.
func TestTradeRoundsAdvanceWithoutAnyPlayerAction(t *testing.T) {
	t.Parallel()
	db, svc, clock := newTradeService(t)
	ctx := context.Background()

	start := *clock
	_, roundID := seedTradeRound(t, db, start, start.Add(60*time.Second))

	if got, _ := readTradeBoard(t, db, roundID); got != 1 {
		t.Fatalf("currentRound before any sweep = %d, want 1", got)
	}

	for want := 2; want <= 3; want++ {
		// Push the clock one second past the open round's window.
		*clock = clock.Add(61 * time.Second)
		if _, err := svc.SweepExpired(ctx); err != nil {
			t.Fatalf("sweep before round %d: %v", want, err)
		}
		got, statuses := readTradeBoard(t, db, roundID)
		if got != want {
			t.Fatalf("after sweep currentRound = %d, want %d", got, want)
		}
		// The proposal opened in round 1 must be expired at the first boundary and
		// must stay expired: a round boundary cannot revive a dead proposal.
		for _, status := range statuses {
			if status == "pending" || status == "accepted" {
				t.Fatalf("round %d left a live proposal: %v", want, statuses)
			}
		}
	}

	// The third round is the last: the sweep must close the round rather than open a
	// fourth trading round.
	*clock = clock.Add(61 * time.Second)
	if _, err := svc.SweepExpired(ctx); err != nil {
		t.Fatalf("sweep after the final round: %v", err)
	}
	summary, err := svc.CurrentRoundSummary(ctx, "rom_trade_rounds")
	if err != nil {
		t.Fatalf("load round: %v", err)
	}
	if summary == nil {
		t.Fatal("the round disappeared after its final trading round")
	}
	if summary.Status != modes.PhaseLocked {
		t.Fatalf("status after the final trading round = %q, want %q",
			summary.Status, modes.PhaseLocked)
	}
	if got, _ := readTradeBoard(t, db, roundID); got != 3 {
		t.Fatalf("a fourth trading round was opened: currentRound = %d", got)
	}
}

func TestTradeHistoricalEarlyLockStopsOnceWithoutSettlement(t *testing.T) {
	db, svc, clock := newTradeService(t)
	_, roundID := seedTradeRound(t, db, *clock, clock.Add(time.Second))
	if _, err := db.Write().Exec(`UPDATE rounds SET status='locked' WHERE id=?`, roundID); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(2 * time.Second)
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	var version int64
	var deadline *int64
	if err := db.Read().QueryRow(`SELECT phase_version,deadline_at FROM rounds WHERE id=?`, roundID).Scan(&version, &deadline); err != nil {
		t.Fatal(err)
	}
	if deadline != nil {
		t.Fatal("historical incomplete round kept a deadline")
	}
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := db.Read().QueryRow(`SELECT phase_version FROM rounds WHERE id=?`, roundID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != version {
		t.Fatal("historical incomplete round repeated settlement")
	}
}

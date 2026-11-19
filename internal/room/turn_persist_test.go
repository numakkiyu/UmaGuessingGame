package room

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/storage"
)

// TestUpdateRoundPublicStatePersistsTurnAdvance 写回后从库里能读到新的持有者。
func TestUpdateRoundPublicStatePersistsTurnAdvance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := storage.Open(ctx, storage.Config{Dir: t.TempDir(), ReadConns: 1})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer db.Close()

	now := int64(1_700_000_000_000)
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES ('ses_w', 'h', 'W', 'c', 'player', ?, ?)`, now, now+3600_000); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rooms (id, owner_session_id, name, status, config_json, room_version, created_at, updated_at)
		VALUES ('rom_w', 'ses_w', 'w', 'playing', '{}', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at,
		                    seed, config_json, created_at, updated_at)
		VALUES ('rnd_w', 'rom_w', 1, 'aptitude-grid', 'v', 'ds', 'pack',
		        'collecting', 1, NULL, 'seed', '{}', ?, ?)`, now, now); err != nil {
		t.Fatalf("seed round: %v", err)
	}

	before := `{"board":{"claims":[],"closed":false,"turnSubjectId":"mem_a"}}`
	after := `{"board":{"claims":[],"closed":false,"turnSubjectId":"mem_b"}}`
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES ('rnd_w', 'public', ?)`,
		before); err != nil {
		t.Fatalf("seed public: %v", err)
	}

	// 走真实的写回路径。
	tx, err := db.Write().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := updateRoundPublicStateQ(ctx, tx, "rnd_w", json.RawMessage(after)); err != nil {
		tx.Rollback()
		t.Fatalf("update public state: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 从库里读回来：换手必须已经落库。
	var raw string
	if err := db.Read().QueryRowContext(ctx,
		`SELECT state_json FROM round_private_state WHERE round_id = 'rnd_w' AND scope = 'public'`,
	).Scan(&raw); err != nil {
		t.Fatalf("read public: %v", err)
	}
	var view struct {
		Board struct {
			TurnSubjectID string `json:"turnSubjectId"`
		} `json:"board"`
	}
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Board.TurnSubjectID != "mem_b" {
		t.Fatalf("库里 turnSubjectId = %q，want mem_b；换手没有落库", view.Board.TurnSubjectID)
	}
}

// TestUpdateRoundPublicStateSkipsEmpty 空 body 不应清空已有状态。
//
// 这是同一个函数的边界：模式返回 nil（无变化）时，写回必须是无操作，
// 否则会把公开状态整块抹掉——比不换手更糟。
func TestUpdateRoundPublicStateSkipsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := storage.Open(ctx, storage.Config{Dir: t.TempDir(), ReadConns: 1})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer db.Close()

	now := int64(1_700_000_000_000)
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES ('ses_e', 'h', 'E', 'c', 'player', ?, ?)`, now, now+3600_000); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rooms (id, owner_session_id, name, status, config_json, room_version, created_at, updated_at)
		VALUES ('rom_e', 'ses_e', 'e', 'playing', '{}', 1, ?, ?)`, now, now); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at,
		                    seed, config_json, created_at, updated_at)
		VALUES ('rnd_e', 'rom_e', 1, 'aptitude-grid', 'v', 'ds', 'pack',
		        'collecting', 1, NULL, 'seed', '{}', ?, ?)`, now, now); err != nil {
		t.Fatalf("seed round: %v", err)
	}
	original := `{"board":{"turnSubjectId":"mem_a"}}`
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES ('rnd_e', 'public', ?)`,
		original); err != nil {
		t.Fatalf("seed public: %v", err)
	}

	tx, err := db.Write().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := updateRoundPublicStateQ(ctx, tx, "rnd_e", nil); err != nil {
		tx.Rollback()
		t.Fatalf("update with nil: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var raw string
	if err := db.Read().QueryRowContext(ctx,
		`SELECT state_json FROM round_private_state WHERE round_id = 'rnd_e' AND scope = 'public'`,
	).Scan(&raw); err != nil {
		t.Fatalf("read public: %v", err)
	}
	if raw != original {
		t.Fatalf("空 body 改动了状态：%q → %q", original, raw)
	}
}

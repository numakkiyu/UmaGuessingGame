package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestBackupCanBeOpenedAndReadBack is the missing half of the backup test: it
// proves the artifact is a usable database, not just a non-empty file.
func TestBackupCanBeOpenedAndReadBack(t *testing.T) {
	ctx := context.Background()
	source := newTestDB(t)

	// Seed something that spans more than one table, so a partial copy shows up.
	seedSession(t, source, "sess-backup-a")
	seedSession(t, source, "sess-backup-b")
	if _, err := source.Write().ExecContext(ctx,
		`INSERT INTO rooms (id, owner_session_id, name, status, created_at, updated_at)
		 VALUES ('rom_backup', 'sess-backup-a', '备份房', 'lobby', 0, 0)`); err != nil {
		t.Fatalf("seed room: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "backup", "runtime.db")
	if err := source.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// (1) The artifact must be openable as a database — not merely non-empty.
	restored, err := sql.Open("sqlite", dest+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open backup as sqlite: %v", err)
	}
	t.Cleanup(func() { restored.Close() })

	// (2) Integrity of the restored copy itself, not of the source.
	rows, err := restored.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		t.Fatalf("integrity_check on backup: %v", err)
	}
	var results []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan integrity: %v", err)
		}
		results = append(results, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("integrity rows: %v", err)
	}
	rows.Close()
	if len(results) != 1 || results[0] != "ok" {
		t.Fatalf("backup integrity_check = %v, want [ok]", results)
	}

	// (3) The data must actually be there, across the tables we seeded.
	var sessions, rooms int
	if err := restored.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions in backup: %v", err)
	}
	if err := restored.QueryRowContext(ctx, `SELECT COUNT(*) FROM rooms`).Scan(&rooms); err != nil {
		t.Fatalf("count rooms in backup: %v", err)
	}
	if sessions < 2 {
		t.Fatalf("backup has %d sessions, want at least the 2 seeded", sessions)
	}
	if rooms != 1 {
		t.Fatalf("backup has %d rooms, want 1", rooms)
	}
	var name string
	if err := restored.QueryRowContext(ctx, `SELECT name FROM rooms WHERE id = 'rom_backup'`).Scan(&name); err != nil {
		t.Fatalf("read seeded room from backup: %v", err)
	}
	if name != "备份房" {
		t.Fatalf("backup room name = %q, want 备份房 (non-ASCII must survive the copy)", name)
	}

	// (4) A backup that can only be read is not a restore. It must accept writes,
	// otherwise an operator who swaps it in finds the server unable to start a game.
	if _, err := restored.ExecContext(ctx,
		`INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		 VALUES ('sess-after-restore', 'h', '恢复后新增', 'c', 'player', 0, 9999999999999)`); err != nil {
		t.Fatalf("write into restored backup: %v", err)
	}
	var after int
	if err := restored.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE id = 'sess-after-restore'`).Scan(&after); err != nil {
		t.Fatalf("count after write: %v", err)
	}
	if after != 1 {
		t.Fatalf("write into restored backup did not persist")
	}
}

// TestBackupRestoredThroughOpenServesTheSameData is the operator-level check: put
// the backup where the server expects its data dir and open it with the real
// Open() path, so migrations and pragmas run against the restored copy too.
func TestBackupRestoredThroughOpenServesTheSameData(t *testing.T) {
	ctx := context.Background()
	source := newTestDB(t)
	seedSession(t, source, "sess-open-a")

	dir := t.TempDir()
	dest := filepath.Join(dir, "runtime.db")
	if err := source.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	// Open the restored directory exactly like the server would.
	restored, err := Open(ctx, Config{Dir: dir, ReadConns: 1})
	if err != nil {
		t.Fatalf("Open restored dir: %v", err)
	}
	t.Cleanup(func() { restored.Close() })

	var n int
	if err := restored.Read().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE id = 'sess-open-a'`).Scan(&n); err != nil {
		t.Fatalf("query restored db: %v", err)
	}
	if n != 1 {
		t.Fatalf("restored session missing (count = %d)", n)
	}

	// And the restored copy must still be a working write target.
	if _, err := restored.Write().ExecContext(ctx,
		`INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		 VALUES ('sess-open-b', 'h', '恢复后', 'c', 'player', 0, 9999999999999)`); err != nil {
		t.Fatalf("write after restore: %v", err)
	}
}

// TestBackupTargetDirectoryIsCreated documents that Backup creates the parent
// directory: operators point it at a fresh backup path, and a missing parent is
// not a reason to lose the backup.
func TestBackupTargetDirectoryIsCreated(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	dest := filepath.Join(t.TempDir(), "nested", "deeper", "runtime.db")
	if err := db.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup into missing dirs: %v", err)
	}
	if info, err := os.Stat(dest); err != nil || info.Size() == 0 {
		t.Fatalf("backup not created at nested path: err=%v", err)
	}
}

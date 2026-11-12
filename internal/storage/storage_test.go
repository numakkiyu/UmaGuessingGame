package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAppliesMigrationsAndPragmas(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	ver, err := db.SQLiteVersion(ctx)
	if err != nil {
		t.Fatalf("SQLiteVersion: %v", err)
	}
	if ver == "" {
		t.Fatal("empty sqlite_version")
	}
	// The document baseline is 3.54.1; the driver must embed that or newer.
	if strings.HasPrefix(ver, "3.5") && ver < "3.54.1" {
		t.Errorf("embedded sqlite %s is older than the 3.54.1 baseline", ver)
	}

	for _, table := range []string{
		"schema_migrations", "sessions", "rooms", "memberships", "teams",
		"room_grants", "control_leases", "rounds", "round_private_state",
		"participant_round_state", "submissions", "submission_heads",
		"classic_guesses", "event_sessions", "event_segments", "round_results",
		"segment_results", "score_corrections", "idempotency_receipts",
		"outbox_events", "matchmaking_tickets", "audit_events",
		"asset_cache_entries", "recovery_grants", "display_grants",
	} {
		var name string
		err := db.Write().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}

	var count int
	if err := db.Write().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	// The count must equal the number of embedded migration files, not a hard-coded
	// constant: adding a migration is a normal change and should not require editing
	// this assertion (which is exactly what broke when 0002 was added).
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	want := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			want++
		}
	}
	if count != want {
		t.Errorf("schema_migrations rows = %d, want %d", count, want)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var count int
	if err := db.Write().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	// Idempotence means the second run adds nothing; the absolute count is whatever
	// the embedded set contains.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	want := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			want++
		}
	}
	if count != want {
		t.Errorf("migrations re-applied: rows = %d, want %d", count, want)
	}
}

func TestMigrateDetectsChecksumDrift(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	// Simulate an edited applied migration.
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE schema_migrations SET checksum = 'deadbeef' WHERE version = '0001_init.sql'`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	err := db.Migrate(ctx)
	if !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("Migrate error = %v, want ErrMigrationChecksum", err)
	}
}

// TestForeignKeysEnforced proves the PRAGMA reached the connection, not just the DSN.
func TestForeignKeysEnforced(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	_, err := db.Write().ExecContext(ctx,
		`INSERT INTO rooms (id, owner_session_id, name, status, created_at, updated_at)
		 VALUES ('r1', 'no-such-session', 'x', 'lobby', 0, 0)`)
	if err == nil {
		t.Fatal("expected foreign key violation for unknown owner_session_id")
	}
}

// TestClassicGuessUniqueness proves re-guessing the same character is rejected by SQL.
func TestClassicGuessUniqueness(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedRoomRound(t, db)

	insert := func(attempt int) error {
		_, err := db.Write().ExecContext(ctx,
			`INSERT INTO classic_guesses (round_id, subject_id, attempt, character_id, correct, feedback_json, received_at)
			 VALUES ('rd1', 's1', ?, 'char-a', 0, '{}', 0)`, attempt)
		return err
	}
	if err := insert(1); err != nil {
		t.Fatalf("first guess: %v", err)
	}
	if err := insert(2); err == nil {
		t.Fatal("expected unique violation for duplicate character in same round")
	}
}

// TestSingleCurrentRoundEnforced proves a room cannot hold two live rounds.
func TestSingleCurrentRoundEnforced(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedRoomRound(t, db)

	_, err := db.Write().ExecContext(ctx,
		`INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
			content_pack_version, status, seed, created_at, updated_at)
		 VALUES ('rd2', 'room1', 2, 'classic', 'v', 'd', 'c', 'collecting', 's', 0, 0)`)
	if err == nil {
		t.Fatal("expected partial unique violation for a second live round in one room")
	}
}

// TestSettledRoundAllowsNext proves finishing a round frees the slot for a rematch.
func TestSettledRoundAllowsNext(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedRoomRound(t, db)

	if _, err := db.Write().ExecContext(ctx,
		`UPDATE rounds SET status = 'settled' WHERE id = 'rd1'`); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
			content_pack_version, status, seed, created_at, updated_at)
		 VALUES ('rd2', 'room1', 2, 'classic', 'v', 'd', 'c', 'collecting', 's', 0, 0)`); err != nil {
		t.Fatalf("new round after settle: %v", err)
	}
}

// TestSingleActiveMatchmakingTicket proves a session cannot hold two live tickets.
func TestSingleActiveMatchmakingTicket(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedSession(t, db, "sess1")

	ins := func(id string) error {
		_, err := db.Write().ExecContext(ctx,
			`INSERT INTO matchmaking_tickets (id, session_id, mode_id, status, created_at, expires_at, updated_at)
			 VALUES (?, 'sess1', 'classic', 'queueing', 0, 0, 0)`, id)
		return err
	}
	if err := ins("t1"); err != nil {
		t.Fatalf("first ticket: %v", err)
	}
	if err := ins("t2"); err == nil {
		t.Fatal("expected unique violation for a second active ticket")
	}
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status='cancelled' WHERE id='t1'`); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := ins("t2"); err != nil {
		t.Fatalf("ticket after cancel: %v", err)
	}
}

// TestSingleActiveEventPerRoom proves two programmes cannot run on one room.
func TestSingleActiveEventPerRoom(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedRoomRound(t, db)

	ins := func(id string) error {
		_, err := db.Write().ExecContext(ctx,
			`INSERT INTO event_sessions (id, room_id, host_session_id, name, status, created_at, updated_at)
			 VALUES (?, 'room1', 's1', 'cup', 'running', 0, 0)`, id)
		return err
	}
	if err := ins("ev1"); err != nil {
		t.Fatalf("first event: %v", err)
	}
	if err := ins("ev2"); err == nil {
		t.Fatal("expected unique violation for a second active event")
	}
}

// TestWritePoolSerialises proves concurrent writers do not observe SQLITE_BUSY
// under the single-writer configuration.
func TestWritePoolSerialises(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedSession(t, db, "sess-w")

	const workers = 16
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			_, err := db.Write().ExecContext(ctx,
				`INSERT INTO audit_events (id, actor_id, action, created_at) VALUES (?, 'sess-w', 'test', ?)`,
				"a"+string(rune('a'+n)), time.Now().UnixMilli())
			errCh <- err
		}(i)
	}
	for i := 0; i < workers; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent write %d: %v", i, err)
		}
	}
	var count int
	if err := db.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != workers {
		t.Errorf("audit rows = %d, want %d", count, workers)
	}
}

func TestIntegrityCheckAndBackup(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedSession(t, db, "sess-b")

	res, err := db.IntegrityCheck(ctx)
	if err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
	if len(res) != 1 || res[0] != "ok" {
		t.Fatalf("integrity_check = %v, want [ok]", res)
	}

	dest := filepath.Join(t.TempDir(), "backup", "runtime.db")
	if err := db.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("backup is empty")
	}
	// Backup must refuse to clobber an existing file.
	if err := db.Backup(ctx, dest); err == nil {
		t.Fatal("expected error when backup target exists")
	}
}

func TestOpenRejectsEmptyDir(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); err == nil {
		t.Fatal("expected error for empty data dir")
	}
}

// seedSession inserts a minimal valid session row.
func seedSession(t *testing.T, db *DB, id string) {
	t.Helper()
	_, err := db.Write().ExecContext(context.Background(),
		`INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		 VALUES (?, ?, 'tester', 'csrf', 'player', 0, ?)`,
		id, "hash-"+id, time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// seedRoomRound inserts a session, room and live round for constraint tests.
func seedRoomRound(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	seedSession(t, db, "s1")
	if _, err := db.Write().ExecContext(ctx,
		`INSERT INTO rooms (id, owner_session_id, name, status, created_at, updated_at)
		 VALUES ('room1', 's1', 'room', 'lobby', 0, 0)`); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
			content_pack_version, status, seed, created_at, updated_at)
		 VALUES ('rd1', 'room1', 1, 'classic', 'v', 'd', 'c', 'collecting', 's', 0, 0)`); err != nil {
		t.Fatalf("seed round: %v", err)
	}
}

var _ = sql.ErrNoRows

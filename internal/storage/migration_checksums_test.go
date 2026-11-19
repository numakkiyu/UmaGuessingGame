package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCommentRevisionDigestsMatchEmbeddedMigrations(t *testing.T) {
	for name, revision := range migrationCommentRevisions {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		current := hex.EncodeToString(sum[:])
		if current != revision.current {
			t.Fatalf("%s: digest changed without a compatibility decision", name)
		}
		for _, tc := range []struct {
			name, applied, current string
			want                   bool
		}{
			{name, revision.previous, current, true},
			{name, current, current, true},
			{name, "unknown", current, false},
			{name, revision.previous, "changed-file", false},
			{"other.sql", revision.previous, current, false},
		} {
			if got := acceptsMigrationChecksum(tc.name, tc.applied, tc.current); got != tc.want {
				t.Errorf("%s: checksum acceptance=%v, want %v", tc.name, got, tc.want)
			}
		}
	}
}

func TestMigrateAcceptsCommentRevisionsWithoutReapplyingSQL(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedRoomRound(t, db)
	for name, revision := range migrationCommentRevisions {
		if _, err := db.Write().ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE version=?`, revision.previous, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("existing database rejected: %v", err)
	}
	var rooms int
	if err := db.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM rooms`).Scan(&rooms); err != nil || rooms == 0 {
		t.Fatalf("existing rooms lost: count=%d, err=%v", rooms, err)
	}
	for name, revision := range migrationCommentRevisions {
		var applied string
		if err := db.Read().QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, name).Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied != revision.previous {
			t.Fatalf("%s: historical checksum was overwritten", name)
		}
	}
}

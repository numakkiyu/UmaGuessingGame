// Package storage owns the SQLite connection pools, migrations and repositories.
//
// Concurrency model : one write connection, a small read pool,
// WAL journal, foreign keys on, busy_timeout bounded below the HTTP timeout.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed all:migrations
var migrationsFS embed.FS

// ErrMigrationChecksum reports that an applied migration file changed on disk.
var ErrMigrationChecksum = errors.New("storage: migration checksum mismatch")

// Config describes the runtime database location and pool sizing.
// DefaultReadConns is the read pool size used when Config.ReadConns is unset.
//
// It is exported so the command-line flag default can reference it: the value used
// to be written twice (8 in this package, 4 in cmd/server) and the flag silently won,
// so raising the baseline here had no effect on a real deployment.
const DefaultReadConns = 8

type Config struct {
	// Dir is the directory holding the database, WAL and SHM files.
	Dir string
	// ReadConns is the read pool size. Zero means DefaultReadConns.
	//
	//
	// Baseline is 8, raised from 4 after a 100-player load test: with 4 read
	// connections serving 30+ concurrent readers, requests waited out busy_timeout
	// and failed. 8 keeps a single small instance responsive without changing the
	// write model — writes still go through exactly one connection, so ordering and
	// SQLITE_BUSY semantics are untouched.
	//
	// This is a starting point, not a capacity claim: deployments with larger
	// audiences should raise it via UMA_READ_CONNS and re-measure.
	ReadConns int
	// BusyTimeout is applied to every connection. Baseline 1000ms per
	BusyTimeout time.Duration
	// Synchronous selects the durability mode. FULL is the production default.
	Synchronous string
}

func (c Config) withDefaults() Config {
	if c.ReadConns <= 0 {
		c.ReadConns = DefaultReadConns
	}
	if c.BusyTimeout <= 0 {
		c.BusyTimeout = 1000 * time.Millisecond
	}
	if c.Synchronous == "" {
		c.Synchronous = "FULL"
	}
	return c
}

// DB holds the write pool and the read pool.
//
// Exactly one write connection is intentional: SQLite allows a single writer and
// serialising it in-process turns lock contention into a bounded queue instead of
// SQLITE_BUSY retries.
type DB struct {
	write *sql.DB
	read  *sql.DB
	path  string
}

// Open prepares the runtime directory, opens both pools, verifies PRAGMAs and
// applies pending migrations.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	cfg = cfg.withDefaults()
	if cfg.Dir == "" {
		return nil, errors.New("storage: empty data directory")
	}
	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create data dir: %w", err)
	}
	path := filepath.Join(cfg.Dir, "runtime.db")
	dsn := buildDSN(path, cfg)

	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open write pool: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)

	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("storage: open read pool: %w", err)
	}
	read.SetMaxOpenConns(cfg.ReadConns)
	read.SetMaxIdleConns(cfg.ReadConns)
	read.SetConnMaxLifetime(0)

	db := &DB{write: write, read: read, path: path}
	if err := db.verify(ctx, cfg); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func buildDSN(path string, cfg Config) string {
	q := url.Values{}
	// Every pooled connection must be initialised identically; the _pragma values
	// below are applied by the driver on each new connection.
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", cfg.BusyTimeout.Milliseconds()))
	q.Add("_pragma", fmt.Sprintf("synchronous(%s)", cfg.Synchronous))
	q.Add("_pragma", "temp_store(MEMORY)")
	return "file:" + path + "?" + q.Encode()
}

// verify confirms the connection-level PRAGMAs actually took effect. A driver
// that silently ignores them would leave the durability and FK guarantees false.
func (db *DB) verify(ctx context.Context, cfg Config) error {
	var journal, sync string
	var fk, busy int
	row := db.write.QueryRowContext(ctx, "PRAGMA journal_mode")
	if err := row.Scan(&journal); err != nil {
		return fmt.Errorf("storage: read journal_mode: %w", err)
	}
	if !strings.EqualFold(journal, "wal") {
		return fmt.Errorf("storage: journal_mode is %q, want wal", journal)
	}
	if err := db.write.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("storage: read foreign_keys: %w", err)
	}
	if fk != 1 {
		return errors.New("storage: foreign_keys is off")
	}
	if err := db.write.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		return fmt.Errorf("storage: read busy_timeout: %w", err)
	}
	if busy != int(cfg.BusyTimeout.Milliseconds()) {
		return fmt.Errorf("storage: busy_timeout is %d, want %d", busy, cfg.BusyTimeout.Milliseconds())
	}
	if err := db.write.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync); err != nil {
		return fmt.Errorf("storage: read synchronous: %w", err)
	}
	return nil
}

// SQLiteVersion returns the version compiled into the driver. The system sqlite3
// CLI version is not evidence for the embedded engine .
func (db *DB) SQLiteVersion(ctx context.Context) (string, error) {
	var v string
	if err := db.write.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

// Migrate applies embedded migrations in filename order inside one transaction
// each, recording a checksum so an edited applied migration is detected.
func (db *DB) Migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("storage: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	if _, err := db.write.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			checksum TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		) WITHOUT ROWID`); err != nil {
		return fmt.Errorf("storage: ensure schema_migrations: %w", err)
	}

	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("storage: read %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])

		var applied string
		err = db.write.QueryRowContext(ctx,
			`SELECT checksum FROM schema_migrations WHERE version = ?`, name).Scan(&applied)
		switch {
		case err == nil:
			if !acceptsMigrationChecksum(name, applied, checksum) {
				return fmt.Errorf("%w: %s (db %s, file %s)",
					ErrMigrationChecksum, name, shorten(applied), shorten(checksum))
			}
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("storage: read migration state %s: %w", name, err)
		}

		tx, err := db.write.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("storage: begin migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (?, ?, ?)`,
			name, checksum, time.Now().UTC().UnixMilli()); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("storage: commit %s: %w", name, err)
		}
	}
	return nil
}

// Write returns the single-writer pool.
func (db *DB) Write() *sql.DB { return db.write }

// Read returns the reader pool. Long streaming responses must not hold a read
// transaction open .
func (db *DB) Read() *sql.DB { return db.read }

// Path is the absolute database file path.
func (db *DB) Path() string { return db.path }

// Ping checks both pools.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.write.PingContext(ctx); err != nil {
		return fmt.Errorf("storage: write pool: %w", err)
	}
	if err := db.read.PingContext(ctx); err != nil {
		return fmt.Errorf("storage: read pool: %w", err)
	}
	return nil
}

// IntegrityCheck runs PRAGMA integrity_check and returns the raw result rows.
func (db *DB) IntegrityCheck(ctx context.Context) ([]string, error) {
	rows, err := db.write.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Backup writes a consistent copy using VACUUM INTO, which is safe while the
// database is live. Copying only runtime.db is not a valid backup .
func (db *DB) Backup(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("storage: backup dir: %w", err)
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("storage: backup target already exists: %s", dest)
	}
	// VACUUM INTO requires a literal path, so bind it as a parameter is not
	// possible; escape single quotes defensively.
	safe := strings.ReplaceAll(dest, "'", "''")
	_, err := db.write.ExecContext(ctx, "VACUUM INTO '"+safe+"'")
	if err != nil {
		return fmt.Errorf("storage: vacuum into: %w", err)
	}
	return nil
}

// shorten trims a hash for log output without panicking on short input.
func shorten(s string) string {
	const n = 12
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Close releases both pools.
func (db *DB) Close() error {
	var errs []error
	if db.read != nil {
		if err := db.read.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if db.write != nil {
		if err := db.write.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// IsBusy reports whether an error is SQLite's "database is locked" family, i.e. the
// statement waited out busy_timeout because other connections held the resource.
//
// The driver returns these as plain errors whose text contains the SQLite message,
// and modernc.org/sqlite does not expose a typed code for every case, so matching on
// the message is the portable option. It is deliberately narrow: only the busy and
// locked spellings count, so a real fault is never downgraded to "retry later".
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy")
}

// Read returns the read-only pool.

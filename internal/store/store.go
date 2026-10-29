// Package store owns the SQLite database: connection, embedded migrations and
// repositories. A single connection is used, which keeps PRAGMA settings
// deterministic and serialises writes; the workload is small and local.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store is the database handle.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (and migrates) the database at path. Use ":memory:" for tests.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn, err := dsnFor(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func dsnFor(path string) (string, error) {
	if path == ":memory:" {
		return "file:aiptimize-memory?mode=memory&cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", nil
	}
	if path == "" {
		return "", errors.New("database path must not be empty")
	}
	p := filepath.ToSlash(path)
	u := url.URL{Scheme: "file", Path: p}
	q := u.Query()
	q.Set("_pragma", "busy_timeout(5000)")
	// modernc.org/sqlite accepts repeated _pragma parameters; url.Values keeps
	// them in sorted order, which is harmless for independent PRAGMAs.
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous(NORMAL)")
	// Use a plain "file:<path>?query" string: URL.String would treat a Windows
	// drive letter as a URI authority.
	return "file:" + p + "?" + q.Encode(), nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// DB exposes the handle for packages that need it (kept intentionally small).
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		base := filepath.Base(name)
		ver, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %q: bad version prefix", base)
		}
		var exists int
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, ver).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", ver, err)
		}
		if exists > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", base, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, ver, nowString()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", base, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", base, err)
		}
	}
	return nil
}

// Prune removes expired sessions and old telemetry/events. It is safe to call
// repeatedly and is invoked once per day by the service loop.
func (s *Store) Prune(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(now)); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM telemetry WHERE ts_utc < ?`, formatTime(now.AddDate(0, 0, -90))); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE ts_utc < ?`, formatTime(now.AddDate(-1, 0, 0))); err != nil {
		return err
	}
	return nil
}

// Checkpoint flushes the WAL, e.g. before a backup copy.
func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func nowString() string { return formatTime(time.Now()) }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

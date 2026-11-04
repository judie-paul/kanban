// Package database opens the SQLite database, applies migrations and provides
// a few helpers shared by the domain packages.
package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no C compiler needed)
)

// DBTX is satisfied by both *sql.DB and *sql.Tx so that helper functions
// (for example activity.Record) can run inside or outside a transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open opens (creating if needed) the SQLite database at path and configures
// it for a small multi-user web application:
//
//   - foreign keys ON (SQLite has them off by default)
//   - WAL journal so readers do not block the single writer
//   - busy_timeout so concurrent writers wait instead of failing immediately
//   - BEGIN IMMEDIATE for transactions, which avoids "database is locked"
//     errors caused by read->write lock upgrades.
func Open(path string) (*sql.DB, error) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows one writer at a time; a handful of connections is plenty
	// for concurrent readers while keeping lock contention low.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// Migrate applies every *.sql file in migrationsFS (sorted by name) that has
// not been applied yet. Each migration runs in its own transaction and is
// recorded in the schema_migrations table.
func Migrate(ctx context.Context, db *sql.DB, migrationsFS fs.FS, logger *slog.Logger) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), ".sql")
		var exists int
		err := db.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := fs.ReadFile(migrationsFS, file)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, Now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if logger != nil {
			logger.Info("applied migration", "version", version)
		}
	}
	return nil
}

// WithTx runs fn inside a transaction, committing on success and rolling back
// on error or panic. All multi-statement writes go through this helper so a
// change and its audit records are saved together or not at all.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
		if err != nil {
			tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// TimeLayout is the single format used for every timestamp column. It is
// fixed-width UTC, so string comparison and ORDER BY sort chronologically.
const TimeLayout = "2006-01-02T15:04:05Z"

// DateLayout is used for calendar dates such as task due dates.
const DateLayout = "2006-01-02"

// nowFunc is swappable in tests.
var nowFunc = func() time.Time { return time.Now() }

// Now returns the current UTC time formatted for storage.
func Now() string { return nowFunc().UTC().Format(TimeLayout) }

// Today returns the current UTC date formatted for storage.
func Today() string { return nowFunc().UTC().Format(DateLayout) }

// Time scans a TEXT timestamp column (TimeLayout) into a time.Time.
type Time struct{ time.Time }

// Scan implements sql.Scanner.
func (t *Time) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
		return nil
	case string:
		s = v
	case []byte:
		s = string(v)
	case time.Time:
		t.Time = v.UTC()
		return nil
	default:
		return fmt.Errorf("database.Time: cannot scan %T", src)
	}
	parsed, err := time.Parse(TimeLayout, s)
	if err != nil {
		return fmt.Errorf("database.Time: %w", err)
	}
	t.Time = parsed
	return nil
}

// Value implements driver.Valuer.
func (t Time) Value() (driver.Value, error) {
	if t.IsZero() {
		return nil, nil
	}
	return t.UTC().Format(TimeLayout), nil
}

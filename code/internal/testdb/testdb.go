// Package testdb creates a fresh, fully migrated SQLite database in a
// temporary directory for each test.
package testdb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	kanban "kanban"
	"kanban/internal/database"
)

// New returns a migrated database that is closed and deleted when the test ends.
func New(t testing.TB) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db, kanban.Migrations, nil); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return db
}

package database_test

import (
	"context"
	"path/filepath"
	"testing"

	kanban "kanban"
	"kanban/internal/database"
)

func TestMigrateIsIdempotentAndEnablesForeignKeys(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for i := 0; i < 2; i++ {
		if err := database.Migrate(ctx, db, kanban.Migrations, nil); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}

	var fk int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, err = %v; want 1", fk, err)
	}

	// A task pointing at a non-existent board must be rejected.
	_, err = db.ExecContext(ctx, `INSERT INTO boards (name, owner_id, created_at, updated_at) VALUES ('x', 999, 'a', 'a')`)
	if err == nil {
		t.Fatal("expected foreign key violation inserting board with unknown owner")
	}
}

func TestTimeScanRoundTrip(t *testing.T) {
	var tm database.Time
	if err := tm.Scan("2026-11-04T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if tm.Hour() != 9 || tm.Year() != 2026 {
		t.Fatalf("unexpected time %v", tm.Time)
	}
	v, _ := tm.Value()
	if v != "2026-11-04T09:00:00Z" {
		t.Fatalf("Value() = %v", v)
	}
}

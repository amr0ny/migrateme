package database

import (
	"context"
	"strings"
	"testing"
)

func TestSQLiteMigrationLedgerLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := NewDB(ctx, ":memory:", DialectSQLite, "schema_migrations")
	if err != nil {
		t.Fatalf("NewDB sqlite failed: %v", err)
	}
	defer db.Close()

	if err := db.EnsureMigrationsTable(ctx); err != nil {
		t.Fatalf("EnsureMigrationsTable failed: %v", err)
	}
	if err := db.RecordMigration(ctx, "20260101000000__init"); err != nil {
		t.Fatalf("RecordMigration failed: %v", err)
	}

	applied, err := db.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) != 1 || applied[0] != "20260101000000__init" {
		t.Fatalf("unexpected applied migrations: %#v", applied)
	}

	if err := db.RemoveMigration(ctx, "20260101000000__init"); err != nil {
		t.Fatalf("RemoveMigration failed: %v", err)
	}
	applied, err = db.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected no applied migrations, got %#v", applied)
	}
}

func TestSQLiteApplyAndRevertMigrationTransactional(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := NewDB(ctx, ":memory:", DialectSQLite, "schema_migrations")
	if err != nil {
		t.Fatalf("NewDB sqlite failed: %v", err)
	}
	defer db.Close()

	if err := db.EnsureMigrationsTable(ctx); err != nil {
		t.Fatalf("EnsureMigrationsTable failed: %v", err)
	}

	upSQL := strings.Join([]string{
		`CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO users(id, name) VALUES (1, 'alex')`,
	}, ";\n")
	if err := db.ApplyMigration(ctx, "20260101010101__users", upSQL); err != nil {
		t.Fatalf("ApplyMigration failed: %v", err)
	}

	applied, err := db.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected one applied migration, got %v", applied)
	}

	if err := db.RevertMigration(ctx, "20260101010101__users", `DROP TABLE IF EXISTS users`); err != nil {
		t.Fatalf("RevertMigration failed: %v", err)
	}
	applied, err = db.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected empty applied migrations after revert, got %v", applied)
	}
}

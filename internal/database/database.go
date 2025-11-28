package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewDB(ctx context.Context, connString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	db.Pool.Close()
}

func (db *DB) EnsureMigrationsTable(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	return err
}

func (db *DB) GetAppliedMigrations(ctx context.Context) ([]string, error) {
	if err := db.EnsureMigrationsTable(ctx); err != nil {
		return nil, err
	}

	rows, err := db.Pool.Query(ctx, `SELECT name FROM schema_migrations ORDER BY applied_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrations []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		migrations = append(migrations, name)
	}

	return migrations, nil
}

func (db *DB) RecordMigration(ctx context.Context, name string) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, name)
	return err
}

func (db *DB) RemoveMigration(ctx context.Context, name string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM schema_migrations WHERE name = $1`, name)
	return err
}

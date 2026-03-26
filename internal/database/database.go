package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/mattn/go-sqlite3"
)

type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite"
)

type DB struct {
	Dialect         Dialect
	Pool            *pgxpool.Pool
	SQLDB           *sql.DB
	migrationsTable string
}

func ParseDialect(raw string) Dialect {
	switch Dialect(strings.ToLower(strings.TrimSpace(raw))) {
	case DialectSQLite:
		return DialectSQLite
	default:
		return DialectPostgres
	}
}

func NewDB(ctx context.Context, connString string, dialect Dialect, migrationsTable string) (*DB, error) {
	if migrationsTable == "" {
		migrationsTable = "schema_migrations"
	}

	switch dialect {
	case DialectSQLite:
		db, err := sql.Open("sqlite3", connString)
		if err != nil {
			return nil, fmt.Errorf("failed to open sqlite database: %w", err)
		}
		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
		}
		return &DB{
			Dialect:         DialectSQLite,
			SQLDB:           db,
			migrationsTable: migrationsTable,
		}, nil
	default:
		pool, err := pgxpool.New(ctx, connString)
		if err != nil {
			return nil, fmt.Errorf("failed to create connection pool: %w", err)
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}
		return &DB{
			Dialect:         DialectPostgres,
			Pool:            pool,
			migrationsTable: migrationsTable,
		}, nil
	}
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
	if db.SQLDB != nil {
		_ = db.SQLDB.Close()
	}
}

func (db *DB) exec(ctx context.Context, query string, args ...any) error {
	if db.Dialect == DialectSQLite {
		_, err := db.SQLDB.ExecContext(ctx, query, args...)
		return err
	}
	_, err := db.Pool.Exec(ctx, query, args...)
	return err
}

func (db *DB) ExecSQL(ctx context.Context, sqlText string) error {
	return db.exec(ctx, sqlText)
}

func (db *DB) EnsureMigrationsTable(ctx context.Context) error {
	stmt := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`, db.migrationsTable)
	if db.Dialect == DialectSQLite {
		stmt = fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			name TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`, db.migrationsTable)
	}
	return db.exec(ctx, stmt)
}

func (db *DB) GetAppliedMigrations(ctx context.Context) ([]string, error) {
	if err := db.EnsureMigrationsTable(ctx); err != nil {
		return nil, err
	}

	var migrations []string
	query := fmt.Sprintf(`SELECT name FROM %s ORDER BY applied_at ASC`, db.migrationsTable)
	if db.Dialect == DialectSQLite {
		rows, err := db.SQLDB.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			migrations = append(migrations, name)
		}
		return migrations, nil
	}

	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
	if db.Dialect == DialectSQLite {
		return db.exec(ctx, fmt.Sprintf(`INSERT INTO %s(name) VALUES (?)`, db.migrationsTable), name)
	}
	return db.exec(ctx, fmt.Sprintf(`INSERT INTO %s(name) VALUES ($1)`, db.migrationsTable), name)
}

func (db *DB) RemoveMigration(ctx context.Context, name string) error {
	if db.Dialect == DialectSQLite {
		return db.exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, db.migrationsTable), name)
	}
	return db.exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE name = $1`, db.migrationsTable), name)
}

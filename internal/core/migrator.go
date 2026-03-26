package core

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/internal/database"
	"github.com/amr0ny/migrateme/pkg/config"
	"github.com/amr0ny/migrateme/pkg/migrate"
	schema2 "github.com/amr0ny/migrateme/pkg/schema"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Migrator struct {
	config *config.Config
	db     *database.DB
}

func NewMigrator(cfg *config.Config, db *database.DB) *Migrator {
	return &Migrator{
		config: cfg,
		db:     db,
	}
}

type GenerateOptions struct {
	MigrationName string
	DryRun        bool
}

type GenerateResult struct {
	CreatedFiles []string
	Changes      []TableChange
	Warnings     []string
}

type TableChange struct {
	TableName string
	Type      ChangeType
	Details   string
}

type ChangeType string

const (
	CreateTable      ChangeType = "create_table"
	DropTable        ChangeType = "drop_table"
	AddColumns       ChangeType = "add_columns"
	DropColumns      ChangeType = "drop_columns"
	ModifyColumns    ChangeType = "modify_columns"
	AlterConstraints ChangeType = "alter_constraints"
)

func (m *Migrator) Generate(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	if hasUnapplied, err := m.hasUnappliedMigrations(ctx); err != nil {
		return nil, fmt.Errorf("failed to check for unapplied migrations: %w", err)
	} else if hasUnapplied {
		return nil, fmt.Errorf("there are unapplied migrations. Please run 'migrate run' before generating new migrations")
	}

	if err := os.MkdirAll(m.config.GetMigrationsDir(), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create migrations directory: %w", err)
	}

	schemaFetcher, err := m.newSchemaFetcher()
	if err != nil {
		return nil, err
	}
	newSchemas, oldSchemas, dependencyGraph, err := m.buildSchemaDependencies(ctx, schemaFetcher)
	if err != nil {
		return nil, err
	}

	sortedTables, err := topologicalSort(dependencyGraph, getTableNames(newSchemas))
	if err != nil {
		return nil, fmt.Errorf("failed to sort tables topologically: %w", err)
	}

	changes, upStatements, downStatements, warnings, err := m.generateMigrationSQL(sortedTables, newSchemas, oldSchemas)
	if err != nil {
		return nil, err
	}
	if len(upStatements) == 0 {
		return &GenerateResult{
			CreatedFiles: []string{},
			Changes:      changes,
			Warnings:     warnings,
		}, nil
	}

	if opts.DryRun {
		return &GenerateResult{
			CreatedFiles: []string{},
			Changes:      changes,
			Warnings:     warnings,
		}, nil
	}

	createdFiles, err := m.createMigrationFiles(opts.MigrationName, changes, upStatements, downStatements)
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		CreatedFiles: createdFiles,
		Changes:      changes,
		Warnings:     warnings,
	}, nil
}
func (m *Migrator) buildSchemaDependencies(ctx context.Context, fetcher schema2.TableFetcher) (
	map[string]migrate.TableSchema,
	map[string]migrate.TableSchema,
	map[string][]string,
	error,
) {
	newSchemas := make(map[string]migrate.TableSchema)
	oldSchemas := make(map[string]migrate.TableSchema)
	dependencyGraph := make(map[string][]string)

	for table, builder := range m.config.Registry {
		newSchemas[table] = builder(table)
		dependencyGraph[table] = []string{} // Инициализируем для всех таблиц
	}

	allTables := getTableNames(newSchemas)
	sort.Strings(allTables)

	for _, table := range allTables {
		oldSchema, err := fetcher.Fetch(ctx, table)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to fetch schema for table %s: %w", table, err)
		}
		oldSchemas[table] = oldSchema

		newSchema := newSchemas[table]
		for _, column := range newSchema.Columns {
			if column.Attrs.ForeignKey != nil {
				refTable := column.Attrs.ForeignKey.Table
				if _, exists := m.config.Registry[refTable]; exists {
					// Добавляем зависимость, даже если это self-reference
					dependencyGraph[refTable] = append(dependencyGraph[refTable], table)
				}
			}
		}
	}

	return newSchemas, oldSchemas, dependencyGraph, nil
}

func (m *Migrator) generateMigrationSQL(
	sortedTables []string,
	newSchemas map[string]migrate.TableSchema,
	oldSchemas map[string]migrate.TableSchema,
) ([]TableChange, []string, []string, []string, error) {
	var changes []TableChange
	var allUpStatements []string
	var allDownStatements []string
	var warnings []string

	var diffGenerator schema2.SchemaDiffer
	var sqlitePlanner *schema2.SQLitePlanner
	if m.db.Dialect == database.DialectSQLite {
		sqlitePlanner = schema2.NewSQLitePlanner()
		diffGenerator = sqlitePlanner
	} else {
		diffGenerator = schema2.NewDiffGenerator()
	}

	for _, table := range sortedTables {
		newSchema := migrate.NormalizeSchema(newSchemas[table])
		oldSchema := migrate.NormalizeSchema(oldSchemas[table])
		if m.db.Dialect == database.DialectSQLite {
			for _, d := range schema2.ValidateSQLiteCapabilities(newSchema) {
				warnings = append(warnings, fmt.Sprintf("%s: %s", d.Table, d.Message))
			}
		}

		diff := diffGenerator.DiffSchemas(oldSchema, newSchema)
		if sqlitePlanner != nil {
			for _, d := range sqlitePlanner.Diagnostics() {
				warnings = append(warnings, fmt.Sprintf("%s: %s", d.Table, d.Message))
			}
		}
		if diff.IsEmpty() {
			continue
		}

		changeType := m.analyzeTableChange(oldSchema, newSchema)
		changes = append(changes, TableChange{
			TableName: table,
			Type:      changeType,
			Details:   fmt.Sprintf("%d changes", len(diff.Up)),
		})

		allUpStatements = append(allUpStatements, fmt.Sprintf("-- Changes for table: %s", table))
		allUpStatements = append(allUpStatements, diff.Up...)
		allUpStatements = append(allUpStatements, "")

		tableDown := append([]string{fmt.Sprintf("-- Revert changes for table: %s", table)}, diff.Down...)
		tableDown = append(tableDown, "")
		allDownStatements = append(tableDown, allDownStatements...)
	}

	return changes, allUpStatements, allDownStatements, dedupeWarnings(warnings), nil
}

func dedupeWarnings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, w := range in {
		key := strings.TrimSpace(w)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (m *Migrator) newSchemaFetcher() (schema2.TableFetcher, error) {
	switch m.db.Dialect {
	case database.DialectSQLite:
		if m.db.SQLDB == nil {
			return nil, fmt.Errorf("sqlite database connection is not initialized")
		}
		return schema2.NewSQLiteFetcher(m.db.SQLDB), nil
	default:
		if m.db.Pool == nil {
			return nil, fmt.Errorf("postgres connection pool is not initialized")
		}
		return schema2.NewFetcher(m.db.Pool), nil
	}
}

func (m *Migrator) analyzeTableChange(old, new migrate.TableSchema) ChangeType {
	hasAdded := hasNewColumns(old, new)
	hasDropped := hasDroppedColumns(old, new)
	hasType := hasTypeChanges(old, new)
	hasColumnDef := hasColumnDefinitionChanges(old, new)
	hasConstraints := hasConstraintChanges(old, new)

	switch {
	case len(old.Columns) == 0 && len(new.Columns) > 0:
		return CreateTable
	case len(old.Columns) > 0 && len(new.Columns) == 0:
		return DropTable
	case hasAdded && !hasDropped && !hasType && !hasColumnDef && !hasConstraints:
		return AddColumns
	case hasDropped && !hasAdded && !hasType && !hasColumnDef && !hasConstraints:
		return DropColumns
	case hasType || hasColumnDef:
		return ModifyColumns
	case hasConstraints:
		return AlterConstraints
	case hasAdded:
		return AddColumns
	case hasDropped:
		return DropColumns
	default:
		return ModifyColumns
	}
}

func (m *Migrator) createMigrationFiles(
	migrationName string,
	changes []TableChange,
	upStatements, downStatements []string,
) ([]string, error) {
	timestamp := time.Now().UTC().Format("20060102150405")
	suffix := randomHex(4)

	baseName := m.generateMigrationName(timestamp, suffix, migrationName, changes)
	upPath := filepath.Join(m.config.GetMigrationsDir(), baseName+".up.sql")
	downPath := filepath.Join(m.config.GetMigrationsDir(), baseName+".down.sql")

	// Записываем файлы
	upContent := schema2.WrapTxForDialect(string(m.db.Dialect), upStatements)
	if err := os.WriteFile(upPath, []byte(upContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write up migration: %w", err)
	}

	downContent := schema2.WrapTxForDialect(string(m.db.Dialect), downStatements)
	if err := os.WriteFile(downPath, []byte(downContent), 0o644); err != nil {
		os.Remove(upPath) // Cleanup on error
		return nil, fmt.Errorf("failed to write down migration: %w", err)
	}

	return []string{baseName + ".up.sql", baseName + ".down.sql"}, nil
}

func (m *Migrator) generateMigrationName(timestamp, suffix, customName string, changes []TableChange) string {
	if customName != "" {
		return fmt.Sprintf("%s__%s__%s", timestamp, normalizeName(customName), suffix)
	}

	changedTables := make([]string, len(changes))
	for i, change := range changes {
		changedTables[i] = change.TableName
	}

	return fmt.Sprintf("%s__%s__%s", timestamp, generateAutoName(changedTables), suffix)
}

func generateAutoName(changedTables []string) string {
	if len(changedTables) == 0 {
		return "no_changes"
	}

	if len(changedTables) == 1 {
		return fmt.Sprintf("update_%s", normalizeName(changedTables[0]))
	}

	if len(changedTables) <= 3 {
		tableNames := make([]string, len(changedTables))
		for i, table := range changedTables {
			tableNames[i] = normalizeName(table)
		}
		return fmt.Sprintf("update_%s_tables", strings.Join(tableNames, "_"))
	}

	return fmt.Sprintf("update_%d_tables", len(changedTables))
}

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

	schemaFetcher := schema2.NewFetcher(m.db.Pool)
	newSchemas, dependencyGraph, err := m.buildSchemaDependencies(ctx, schemaFetcher)
	if err != nil {
		return nil, err
	}

	sortedTables, err := topologicalSort(dependencyGraph, getTableNames(newSchemas))
	if err != nil {
		return nil, fmt.Errorf("failed to sort tables topologically: %w", err)
	}

	changes, upStatements, downStatements := m.generateMigrationSQL(ctx, sortedTables, newSchemas, schemaFetcher)
	if len(upStatements) == 0 {
		return &GenerateResult{
			CreatedFiles: []string{},
			Changes:      changes,
		}, nil
	}

	if opts.DryRun {
		return &GenerateResult{
			CreatedFiles: []string{},
			Changes:      changes,
		}, nil
	}

	createdFiles, err := m.createMigrationFiles(opts.MigrationName, changes, upStatements, downStatements)
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		CreatedFiles: createdFiles,
		Changes:      changes,
	}, nil
}

func (m *Migrator) buildSchemaDependencies(ctx context.Context, fetcher *schema2.Fetcher) (
	map[string]migrate.TableSchema,
	map[string][]string,
	error,
) {
	newSchemas := make(map[string]migrate.TableSchema)
	dependencyGraph := make(map[string][]string)

	for table, builder := range m.config.Registry {
		newSchemas[table] = builder(table)
	}

	allTables := getTableNames(newSchemas)
	sort.Strings(allTables)

	oldSchemas := make(map[string]migrate.TableSchema)
	for _, table := range allTables {
		oldSchema, err := fetcher.Fetch(ctx, table)
		if err != nil {
			fmt.Fprintf(os.Stderr, "schema fetch for %s failed — treating as new table: %v\n", table, err)
			oldSchema = migrate.TableSchema{}
		}
		oldSchemas[table] = oldSchema

		newSchema := newSchemas[table]
		for _, column := range newSchema.Columns {
			if column.Attrs.ForeignKey != nil {
				refTable := column.Attrs.ForeignKey.Table
				if _, exists := m.config.Registry[refTable]; exists {
					dependencyGraph[refTable] = append(dependencyGraph[refTable], table)
				}
			}
		}
	}

	return newSchemas, dependencyGraph, nil
}

func (m *Migrator) generateMigrationSQL(
	ctx context.Context,
	sortedTables []string,
	newSchemas map[string]migrate.TableSchema,
	fetcher *schema2.Fetcher,
) ([]TableChange, []string, []string) {
	var changes []TableChange
	var allUpStatements []string
	var allDownStatements []string

	diffGenerator := schema2.NewDiffGenerator()

	for _, table := range sortedTables {
		newSchema := migrate.NormalizeSchema(newSchemas[table])
		oldSchema, _ := fetcher.Fetch(ctx, table)
		oldSchema = migrate.NormalizeSchema(oldSchema)

		diff := diffGenerator.DiffSchemas(oldSchema, newSchema)
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

	return changes, allUpStatements, allDownStatements
}

func (m *Migrator) analyzeTableChange(old, new migrate.TableSchema) ChangeType {
	switch {
	case len(old.Columns) == 0 && len(new.Columns) > 0:
		return CreateTable
	case len(old.Columns) > 0 && len(new.Columns) == 0:
		return DropTable
	case hasNewColumns(old, new):
		return AddColumns
	case hasDroppedColumns(old, new):
		return DropColumns
	case hasTypeChanges(old, new):
		return ModifyColumns
	case hasConstraintChanges(old, new):
		return AlterConstraints
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
	upContent := schema2.WrapTx(upStatements)
	if err := os.WriteFile(upPath, []byte(upContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write up migration: %w", err)
	}

	downContent := schema2.WrapTx(downStatements)
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

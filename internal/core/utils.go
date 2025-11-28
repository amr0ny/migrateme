package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/amr0ny/migrateme/internal/infrastructure/postgres/schema"
	"os"
	"sort"
	"strings"
)

// topologicalSort и другие utility функции
func topologicalSort(graph map[string][]string, allTables []string) ([]string, error) {
	inDegree := make(map[string]int)
	for _, table := range allTables {
		inDegree[table] = 0
	}

	for _, dependents := range graph {
		for _, dep := range dependents {
			inDegree[dep]++
		}
	}

	queue := make([]string, 0)
	for table, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, table)
		}
	}

	result := make([]string, 0, len(allTables))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(allTables) {
		// Находим таблицы, которые остались в цикле
		remainingTables := make([]string, 0)
		for _, table := range allTables {
			found := false
			for _, processedTable := range result {
				if table == processedTable {
					found = true
					break
				}
			}
			if !found {
				remainingTables = append(remainingTables, table)
			}
		}

		cycleInfo := "\nCyclic dependency detected in foreign keys. Problematic tables and their dependencies:\n"
		for _, table := range remainingTables {
			deps := graph[table]
			if len(deps) > 0 {
				cycleInfo += fmt.Sprintf("  - %s depends on: %v\n", table, deps)
			} else {
				cycleInfo += fmt.Sprintf("  - %s (isolated table with cyclic reference)\n", table)
			}
		}

		cycleInfo += "\nFull dependency graph:\n"
		for table, deps := range graph {
			if len(deps) > 0 {
				cycleInfo += fmt.Sprintf("  - %s -> %v\n", table, deps)
			}
		}

		return nil, fmt.Errorf(cycleInfo)
	}

	return result, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getTableNames(schemas map[string]schema.TableSchema) []string {
	tables := make([]string, 0, len(schemas))
	for table := range schemas {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

func normalizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")

	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}

	if len(name) > 50 {
		name = name[:50]
	}

	return strings.Trim(name, "_")
}

// Вспомогательные функции для анализа изменений
func hasNewColumns(old, new schema.TableSchema) bool {
	oldCols := make(map[string]bool)
	for _, col := range old.Columns {
		oldCols[col.ColumnName] = true
	}

	for _, col := range new.Columns {
		if !oldCols[col.ColumnName] {
			return true
		}
	}
	return false
}

func hasDroppedColumns(old, new schema.TableSchema) bool {
	newCols := make(map[string]bool)
	for _, col := range new.Columns {
		newCols[col.ColumnName] = true
	}

	for _, col := range old.Columns {
		if !newCols[col.ColumnName] {
			return true
		}
	}
	return false
}

func hasTypeChanges(old, new schema.TableSchema) bool {
	oldTypes := make(map[string]string)
	for _, col := range old.Columns {
		oldTypes[col.ColumnName] = col.Attrs.PgType
	}

	for _, col := range new.Columns {
		if oldType, exists := oldTypes[col.ColumnName]; exists && oldType != col.Attrs.PgType {
			return true
		}
	}
	return false
}

func hasConstraintChanges(old, new schema.TableSchema) bool {
	oldConstraints := countConstraints(old)
	newConstraints := countConstraints(new)
	return oldConstraints != newConstraints
}

func countConstraints(schema schema.TableSchema) int {
	count := 0
	for _, col := range schema.Columns {
		if col.Attrs.Unique || col.Attrs.IsPK || col.Attrs.ForeignKey != nil {
			count++
		}
	}
	return count
}

func (m *Migrator) hasUnappliedMigrations(ctx context.Context) (bool, error) {
	applied, err := m.db.GetAppliedMigrations(ctx)
	if err != nil {
		return false, err
	}

	files, err := m.getMigrationFiles()
	if err != nil {
		return false, err
	}

	appliedSet := make(map[string]bool)
	for _, a := range applied {
		appliedSet[a] = true
	}

	for _, file := range files {
		if strings.HasSuffix(file, ".up.sql") {
			base := strings.TrimSuffix(file, ".up.sql")
			if !appliedSet[base] {
				return true, nil
			}
		}
	}

	return false, nil
}

func (m *Migrator) getMigrationFiles() ([]string, error) {
	entries, err := os.ReadDir(m.config.GetMigrationsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)
	return files, nil
}
func filterUpFiles(files []string) []string {
	var upFiles []string
	for _, file := range files {
		if strings.HasSuffix(file, ".up.sql") {
			upFiles = append(upFiles, file)
		}
	}
	sort.Strings(upFiles)
	return upFiles
}

func extractMigrationBases(upFiles []string) []string {
	var bases []string
	for _, file := range upFiles {
		base := strings.TrimSuffix(file, ".up.sql")
		bases = append(bases, base)
	}
	return bases
}

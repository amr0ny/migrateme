package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"os"
	"sort"
	"strings"
)

func topologicalSort(graph map[string][]string, allTables []string) ([]string, error) {
	inDegree := make(map[string]int)
	for _, table := range allTables {
		inDegree[table] = 0
	}

	// Увеличиваем степень входа для зависимостей, исключая self-reference
	for from, dependents := range graph {
		for _, to := range dependents {
			if from != to { // Игнорируем self-reference
				inDegree[to]++
			}
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
			if current != neighbor { // Игнорируем self-reference
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					queue = append(queue, neighbor)
				}
			}
		}
	}

	if len(result) != len(allTables) {
		// Проверяем, связана ли проблема с self-reference
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

		// Отладочная информация о циклах
		cycleInfo := "\nDependency resolution failed. Problematic tables:\n"
		cycleInfo += "\nDetailed analysis:\n"
		for _, table := range remainingTables {
			deps := graph[table]
			selfRef := hasSelfReference(graph, table)
			nonSelfCount := countNonSelfReferences(graph, table)

			cycleInfo += fmt.Sprintf("  - %s: self-reference=%v, external-deps=%d, all-deps=%v\n",
				table, selfRef, nonSelfCount, deps)
		}

		// Пытаемся добавить оставшиеся таблицы (те, у которых только self-reference)
		for _, table := range remainingTables {
			deps := graph[table]
			onlySelfRef := true
			for _, dep := range deps {
				if dep != table {
					onlySelfRef = false
					break
				}
			}
			if onlySelfRef && len(deps) > 0 {
				result = append(result, table)
			}
		}

		// Если после этого все еще есть проблемы
		if len(result) != len(allTables) {
			return nil, fmt.Errorf("%s", cycleInfo)
		}
	}

	return result, nil
}
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getTableNames(schemas map[string]migrate.TableSchema) []string {
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

func hasNewColumns(old, new migrate.TableSchema) bool {
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

func hasDroppedColumns(old, new migrate.TableSchema) bool {
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

func hasTypeChanges(old, new migrate.TableSchema) bool {
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

func hasConstraintChanges(old, new migrate.TableSchema) bool {
	oldCols := make(map[string]migrate.ColumnMeta, len(old.Columns))
	for _, c := range old.Columns {
		oldCols[c.ColumnName] = c
	}
	newCols := make(map[string]migrate.ColumnMeta, len(new.Columns))
	for _, c := range new.Columns {
		newCols[c.ColumnName] = c
	}

	for name, oldCol := range oldCols {
		newCol, exists := newCols[name]
		if !exists {
			continue
		}
		if oldCol.Attrs.IsPK != newCol.Attrs.IsPK || oldCol.Attrs.Unique != newCol.Attrs.Unique {
			return true
		}
		if !foreignKeysEqualForCore(oldCol.Attrs.ForeignKey, newCol.Attrs.ForeignKey) {
			return true
		}
	}

	oldIndexes := make(map[string]struct{}, len(old.Indexes))
	for _, idx := range old.Indexes {
		oldIndexes[coreIndexKey(idx)] = struct{}{}
	}
	newIndexes := make(map[string]struct{}, len(new.Indexes))
	for _, idx := range new.Indexes {
		newIndexes[coreIndexKey(idx)] = struct{}{}
	}
	if len(oldIndexes) != len(newIndexes) {
		return true
	}
	for k := range oldIndexes {
		if _, ok := newIndexes[k]; !ok {
			return true
		}
	}

	oldChecks := make(map[string]struct{}, len(old.Checks))
	for _, chk := range old.Checks {
		oldChecks[coreCheckKey(chk)] = struct{}{}
	}
	newChecks := make(map[string]struct{}, len(new.Checks))
	for _, chk := range new.Checks {
		newChecks[coreCheckKey(chk)] = struct{}{}
	}
	if len(oldChecks) != len(newChecks) {
		return true
	}
	for k := range oldChecks {
		if _, ok := newChecks[k]; !ok {
			return true
		}
	}

	return false
}

func hasColumnDefinitionChanges(old, new migrate.TableSchema) bool {
	oldCols := make(map[string]migrate.ColumnMeta, len(old.Columns))
	for _, c := range old.Columns {
		oldCols[c.ColumnName] = c
	}
	for _, c := range new.Columns {
		oldCol, exists := oldCols[c.ColumnName]
		if !exists {
			continue
		}
		oldDef := ""
		if oldCol.Attrs.Default != nil {
			oldDef = *oldCol.Attrs.Default
		}
		newDef := ""
		if c.Attrs.Default != nil {
			newDef = *c.Attrs.Default
		}
		if oldCol.Attrs.NotNull != c.Attrs.NotNull || oldDef != newDef {
			return true
		}
	}
	return false
}

func coreIndexKey(idx migrate.IndexMeta) string {
	where := ""
	if idx.Where != nil {
		where = strings.TrimSpace(*idx.Where)
	}
	return fmt.Sprintf("unique=%t|cols=%s|where=%s", idx.Unique, strings.Join(idx.Columns, "\x1f"), where)
}

func coreCheckKey(chk migrate.CheckMeta) string {
	return strings.TrimSpace(chk.Expr)
}

func foreignKeysEqualForCore(a, b *migrate.ForeignKey) bool {
	if a == nil || b == nil {
		return a == b
	}
	return normalizeRefName(a.Table) == normalizeRefName(b.Table) &&
		normalizeRefName(a.Column) == normalizeRefName(b.Column)
}

func normalizeRefName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`+"`")
	parts := strings.Split(v, ".")
	return strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
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

// internal/core/utils.go
func hasSelfReference(graph map[string][]string, table string) bool {
	deps := graph[table]
	for _, dep := range deps {
		if dep == table {
			return true
		}
	}
	return false
}

func countNonSelfReferences(graph map[string][]string, table string) int {
	count := 0
	deps := graph[table]
	for _, dep := range deps {
		if dep != table {
			count++
		}
	}
	return count
}

package schema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amr0ny/migrateme/pkg/migrate"
)

type SQLiteDiffGenerator struct{}

func NewSQLiteDiffGenerator() *SQLiteDiffGenerator {
	return &SQLiteDiffGenerator{}
}

func (g *SQLiteDiffGenerator) DiffSchemas(old, new migrate.TableSchema) migrate.TableDiff {
	if len(old.Columns) == 0 && len(new.Columns) > 0 {
		return g.generateCreateTableDiff(new)
	}

	diff := migrate.TableDiff{Up: []string{}, Down: []string{}}
	oldCols := makeColumnMap(old.Columns)
	newCols := makeColumnMap(new.Columns)

	for _, name := range sortedColumnNames(newCols) {
		if _, exists := oldCols[name]; exists {
			continue
		}
		col := newCols[name]
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", quoteSQLiteIdent(new.TableName), g.sqliteColumnDefinition(col))
		diff.Up = append(diff.Up, stmt)
		diff.Down = append([]string{fmt.Sprintf("-- SQLite rollback for added column %s is not supported", quoteSQLiteIdent(name))}, diff.Down...)
	}

	g.handleSQLiteIndexChanges(&diff, old, new)
	return diff
}

func (g *SQLiteDiffGenerator) generateCreateTableDiff(new migrate.TableSchema) migrate.TableDiff {
	cols := make([]string, 0, len(new.Columns)+len(new.Checks))
	for _, c := range new.Columns {
		cols = append(cols, g.sqliteColumnDefinition(c))
	}
	for _, chk := range new.Checks {
		name := chk.Name
		if strings.TrimSpace(name) == "" {
			name = defaultCheckName(new.TableName, chk.Expr)
		}
		cols = append(cols, fmt.Sprintf("CONSTRAINT %s CHECK (%s)", quoteSQLiteIdent(name), strings.TrimSpace(chk.Expr)))
	}

	createStmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)",
		quoteSQLiteIdent(new.TableName), strings.Join(cols, ",\n  "))

	diff := migrate.TableDiff{
		Up:   []string{createStmt},
		Down: []string{fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteSQLiteIdent(new.TableName))},
	}

	indexes := append([]migrate.IndexMeta(nil), new.Indexes...)
	sort.Slice(indexes, func(i, j int) bool { return indexes[i].Name < indexes[j].Name })
	for _, idx := range indexes {
		name := idx.Name
		if strings.TrimSpace(name) == "" {
			name = defaultIndexName(new.TableName, idx.Columns)
		}
		diff.Up = append(diff.Up, g.sqliteCreateIndexStatement(new.TableName, name, idx.Columns, idx.Unique, idx.Where))
		diff.Down = append([]string{fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteSQLiteIdent(name))}, diff.Down...)
	}

	return diff
}

func (g *SQLiteDiffGenerator) sqliteColumnDefinition(col migrate.ColumnMeta) string {
	def := fmt.Sprintf("%s %s", quoteSQLiteIdent(col.ColumnName), normalizeSQLiteType(col.Attrs.PgType))
	if col.Attrs.NotNull {
		def += " NOT NULL"
	}
	if col.Attrs.Default != nil {
		def += " DEFAULT " + *col.Attrs.Default
	}
	if col.Attrs.IsPK {
		def += " PRIMARY KEY"
	}
	if col.Attrs.Unique {
		def += " UNIQUE"
	}
	if fk := col.Attrs.ForeignKey; fk != nil {
		def += fmt.Sprintf(" REFERENCES %s(%s)",
			quoteSQLiteIdent(fk.Table), quoteSQLiteIdent(fk.Column))
		if action := strings.TrimSpace(getForeignKeyAction(fk.OnDelete)); action != "" {
			def += " ON DELETE " + action
		}
		if action := strings.TrimSpace(getForeignKeyAction(fk.OnUpdate)); action != "" {
			def += " ON UPDATE " + action
		}
	}
	return def
}

func (g *SQLiteDiffGenerator) sqliteCreateIndexStatement(table, name string, cols []string, unique bool, where *string) string {
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		parts = append(parts, quoteSQLiteIdent(c))
	}
	uniq := ""
	if unique {
		uniq = "UNIQUE "
	}
	stmt := fmt.Sprintf(`CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)`,
		uniq, quoteSQLiteIdent(name), quoteSQLiteIdent(table), strings.Join(parts, ", "))
	if where != nil && strings.TrimSpace(*where) != "" {
		stmt += " WHERE " + strings.TrimSpace(*where)
	}
	return stmt
}

func (g *SQLiteDiffGenerator) handleSQLiteIndexChanges(diff *migrate.TableDiff, old, new migrate.TableSchema) {
	oldByKey := map[string]migrate.IndexMeta{}
	for _, idx := range old.Indexes {
		oldByKey[indexKey(idx)] = idx
	}
	newByKey := map[string]migrate.IndexMeta{}
	for _, idx := range new.Indexes {
		newByKey[indexKey(idx)] = idx
	}

	for _, key := range sortedIndexKeys(newByKey) {
		n := newByKey[key]
		if _, exists := oldByKey[key]; exists {
			continue
		}
		name := n.Name
		if strings.TrimSpace(name) == "" {
			name = defaultIndexName(new.TableName, n.Columns)
		}
		diff.Up = append(diff.Up, g.sqliteCreateIndexStatement(new.TableName, name, n.Columns, n.Unique, n.Where))
		diff.Down = append([]string{fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteSQLiteIdent(name))}, diff.Down...)
	}

	for _, key := range sortedIndexKeys(oldByKey) {
		o := oldByKey[key]
		if _, exists := newByKey[key]; exists {
			continue
		}
		name := o.Name
		if strings.TrimSpace(name) == "" {
			name = defaultIndexName(old.TableName, o.Columns)
		}
		diff.Up = append(diff.Up, fmt.Sprintf("DROP INDEX IF EXISTS %s", quoteSQLiteIdent(name)))
		diff.Down = append(diff.Down, g.sqliteCreateIndexStatement(old.TableName, name, o.Columns, o.Unique, o.Where))
	}
}

func normalizeSQLiteType(t string) string {
	t = strings.TrimSpace(strings.ToLower(t))
	if t == "" {
		return "text"
	}
	switch t {
	case "uuid", "jsonb", "timestamptz", "timestamp with time zone":
		return "text"
	case "boolean":
		return "integer"
	}
	return t
}

func ValidateSQLiteDiffSupport(old, new migrate.TableSchema) error {
	if len(old.Columns) == 0 {
		return nil
	}
	oldCols := makeColumnMap(old.Columns)
	newCols := makeColumnMap(new.Columns)

	for _, oldCol := range old.Columns {
		newCol, exists := newCols[oldCol.ColumnName]
		if !exists {
			return fmt.Errorf("sqlite dialect does not support dropping columns (%s.%s)", new.TableName, oldCol.ColumnName)
		}
		if oldCol.Attrs.PgType != newCol.Attrs.PgType {
			return fmt.Errorf("sqlite dialect does not support altering column type (%s.%s)", new.TableName, newCol.ColumnName)
		}
		oldDef, newDef := "", ""
		if oldCol.Attrs.Default != nil {
			oldDef = strings.TrimSpace(*oldCol.Attrs.Default)
		}
		if newCol.Attrs.Default != nil {
			newDef = strings.TrimSpace(*newCol.Attrs.Default)
		}
		if oldDef != newDef {
			return fmt.Errorf("sqlite dialect does not support altering column default (%s.%s)", new.TableName, newCol.ColumnName)
		}
		if oldCol.Attrs.NotNull != newCol.Attrs.NotNull {
			return fmt.Errorf("sqlite dialect does not support altering NOT NULL (%s.%s)", new.TableName, newCol.ColumnName)
		}
		if oldCol.Attrs.Unique != newCol.Attrs.Unique || oldCol.Attrs.IsPK != newCol.Attrs.IsPK {
			return fmt.Errorf("sqlite dialect does not support altering PK/UNIQUE constraints (%s.%s)", new.TableName, newCol.ColumnName)
		}
		if !foreignKeysEqual(oldCol.Attrs.ForeignKey, newCol.Attrs.ForeignKey) {
			return fmt.Errorf("sqlite dialect does not support altering foreign keys (%s.%s)", new.TableName, newCol.ColumnName)
		}
	}

	for _, newCol := range new.Columns {
		if _, exists := oldCols[newCol.ColumnName]; exists {
			continue
		}
		if newCol.Attrs.ForeignKey != nil {
			return fmt.Errorf("sqlite dialect does not support adding foreign key on existing table via ADD COLUMN (%s.%s)", new.TableName, newCol.ColumnName)
		}
	}

	oldChecks := map[string]struct{}{}
	for _, chk := range old.Checks {
		oldChecks[checkKey(chk)] = struct{}{}
	}
	newChecks := map[string]struct{}{}
	for _, chk := range new.Checks {
		newChecks[checkKey(chk)] = struct{}{}
	}
	if len(oldChecks) != len(newChecks) {
		return fmt.Errorf("sqlite dialect does not support altering CHECK constraints on existing tables (%s)", new.TableName)
	}
	for k := range oldChecks {
		if _, ok := newChecks[k]; !ok {
			return fmt.Errorf("sqlite dialect does not support altering CHECK constraints on existing tables (%s)", new.TableName)
		}
	}

	return nil
}

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

type SQLitePlanner struct {
	diagnostics []Diagnostic
}

func NewSQLitePlanner() *SQLitePlanner {
	return &SQLitePlanner{diagnostics: make([]Diagnostic, 0)}
}

func (p *SQLitePlanner) Diagnostics() []Diagnostic {
	out := make([]Diagnostic, len(p.diagnostics))
	copy(out, p.diagnostics)
	return out
}

func (p *SQLitePlanner) addWarning(table, msg string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Severity: SeverityWarning,
		Table:    table,
		Message:  msg,
	})
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

func (p *SQLitePlanner) DiffSchemas(old, new migrate.TableSchema) migrate.TableDiff {
	p.diagnostics = p.diagnostics[:0]
	if len(old.Columns) == 0 && len(new.Columns) > 0 {
		return NewSQLiteDiffGenerator().DiffSchemas(old, new)
	}

	needsRebuild, reasons := sqliteNeedsRebuild(old, new)
	for _, reason := range reasons {
		p.addWarning(new.TableName, reason)
	}
	if needsRebuild {
		p.addWarning(new.TableName, "[rebuild] automatic SQLite table rebuild will be used for schema changes")
		return p.rebuildTableDiff(old, new)
	}
	return NewSQLiteDiffGenerator().DiffSchemas(old, new)
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

func ValidateSQLiteCapabilities(new migrate.TableSchema) []Diagnostic {
	diags := make([]Diagnostic, 0)
	for _, col := range new.Columns {
		pt := strings.ToLower(strings.TrimSpace(col.Attrs.PgType))
		switch {
		case strings.Contains(pt, "jsonb"):
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Table:    new.TableName,
				Message:  fmt.Sprintf("[mapped] column %s: jsonb mapped to TEXT in sqlite", col.ColumnName),
			})
		case strings.Contains(pt, "uuid"):
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Table:    new.TableName,
				Message:  fmt.Sprintf("[mapped] column %s: uuid mapped to TEXT in sqlite", col.ColumnName),
			})
		case strings.Contains(pt, "timestamptz") || strings.Contains(pt, "timestamp with time zone"):
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Table:    new.TableName,
				Message:  fmt.Sprintf("[mapped] column %s: timestamptz mapped to TEXT in sqlite", col.ColumnName),
			})
		case pt == "boolean":
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Table:    new.TableName,
				Message:  fmt.Sprintf("[mapped] column %s: boolean mapped to INTEGER in sqlite", col.ColumnName),
			})
		}
	}
	for _, idx := range new.Indexes {
		if idx.Where != nil && strings.TrimSpace(*idx.Where) != "" {
			diags = append(diags, Diagnostic{
				Severity: SeverityInfo,
				Table:    new.TableName,
				Message:  fmt.Sprintf("[direct] partial index %s will be generated for sqlite as declared", idx.Name),
			})
		}
	}
	return diags
}

func sqliteNeedsRebuild(old, new migrate.TableSchema) (bool, []string) {
	reasons := make([]string, 0)
	if len(old.Columns) == 0 {
		return false, reasons
	}
	oldCols := makeColumnMap(old.Columns)
	newCols := makeColumnMap(new.Columns)

	for _, oldCol := range old.Columns {
		newCol, exists := newCols[oldCol.ColumnName]
		if !exists {
			reasons = append(reasons, fmt.Sprintf("column drop detected (%s.%s)", new.TableName, oldCol.ColumnName))
			continue
		}
		if oldCol.Attrs.PgType != newCol.Attrs.PgType {
			reasons = append(reasons, fmt.Sprintf("column type change detected (%s.%s)", new.TableName, newCol.ColumnName))
		}
		oldDef, newDef := "", ""
		if oldCol.Attrs.Default != nil {
			oldDef = strings.TrimSpace(*oldCol.Attrs.Default)
		}
		if newCol.Attrs.Default != nil {
			newDef = strings.TrimSpace(*newCol.Attrs.Default)
		}
		if oldDef != newDef {
			reasons = append(reasons, fmt.Sprintf("column default change detected (%s.%s)", new.TableName, newCol.ColumnName))
		}
		if oldCol.Attrs.NotNull != newCol.Attrs.NotNull {
			reasons = append(reasons, fmt.Sprintf("NOT NULL change detected (%s.%s)", new.TableName, newCol.ColumnName))
		}
		if oldCol.Attrs.Unique != newCol.Attrs.Unique || oldCol.Attrs.IsPK != newCol.Attrs.IsPK {
			reasons = append(reasons, fmt.Sprintf("PK/UNIQUE change detected (%s.%s)", new.TableName, newCol.ColumnName))
		}
		if !foreignKeysEqual(oldCol.Attrs.ForeignKey, newCol.Attrs.ForeignKey) {
			reasons = append(reasons, fmt.Sprintf("foreign key change detected (%s.%s)", new.TableName, newCol.ColumnName))
		}
	}

	for _, newCol := range new.Columns {
		if _, exists := oldCols[newCol.ColumnName]; exists {
			continue
		}
		if newCol.Attrs.ForeignKey != nil {
			reasons = append(reasons, fmt.Sprintf("new column with foreign key requires rebuild (%s.%s)", new.TableName, newCol.ColumnName))
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
		reasons = append(reasons, fmt.Sprintf("CHECK constraints changed (%s)", new.TableName))
	}
	for k := range oldChecks {
		if _, ok := newChecks[k]; !ok {
			reasons = append(reasons, fmt.Sprintf("CHECK constraints changed (%s)", new.TableName))
			break
		}
	}

	return len(reasons) > 0, reasons
}

func (p *SQLitePlanner) rebuildTableDiff(old, new migrate.TableSchema) migrate.TableDiff {
	tmpTable := "__migrateme_tmp_" + new.TableName
	tmpSchema := new
	tmpSchema.TableName = tmpTable
	createTmp := NewSQLiteDiffGenerator().generateCreateTableDiff(tmpSchema).Up

	oldCols := makeColumnMap(old.Columns)
	insertCols := make([]string, 0)
	selectExpr := make([]string, 0)
	for _, col := range new.Columns {
		insertCols = append(insertCols, quoteSQLiteIdent(col.ColumnName))
		if oldCol, exists := oldCols[col.ColumnName]; exists {
			if strings.TrimSpace(oldCol.Attrs.PgType) != strings.TrimSpace(col.Attrs.PgType) {
				selectExpr = append(selectExpr, fmt.Sprintf("CAST(%s AS %s)", quoteSQLiteIdent(col.ColumnName), normalizeSQLiteType(col.Attrs.PgType)))
			} else {
				selectExpr = append(selectExpr, quoteSQLiteIdent(col.ColumnName))
			}
			continue
		}
		if col.Attrs.Default != nil {
			selectExpr = append(selectExpr, *col.Attrs.Default)
		} else {
			selectExpr = append(selectExpr, "NULL")
		}
	}

	up := make([]string, 0, len(createTmp)+5)
	up = append(up, createTmp...)
	up = append(up, fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
		quoteSQLiteIdent(tmpTable),
		strings.Join(insertCols, ", "),
		strings.Join(selectExpr, ", "),
		quoteSQLiteIdent(old.TableName),
	))
	up = append(up, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteSQLiteIdent(old.TableName)))
	up = append(up, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteSQLiteIdent(tmpTable), quoteSQLiteIdent(new.TableName)))

	newIndexes := append([]migrate.IndexMeta(nil), new.Indexes...)
	sort.Slice(newIndexes, func(i, j int) bool { return newIndexes[i].Name < newIndexes[j].Name })
	g := NewSQLiteDiffGenerator()
	for _, idx := range newIndexes {
		name := idx.Name
		if strings.TrimSpace(name) == "" {
			name = defaultIndexName(new.TableName, idx.Columns)
		}
		up = append(up, g.sqliteCreateIndexStatement(new.TableName, name, idx.Columns, idx.Unique, idx.Where))
	}

	return migrate.TableDiff{
		Up: up,
		Down: []string{
			fmt.Sprintf("-- SQLite auto-rebuild rollback for table %s is not generated automatically", quoteSQLiteIdent(new.TableName)),
		},
	}
}

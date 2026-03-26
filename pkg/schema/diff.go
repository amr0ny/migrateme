package schema

import (
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"sort"
	"strings"
)

type DiffGenerator struct{}

func NewDiffGenerator() *DiffGenerator {
	return &DiffGenerator{}
}

func (g *DiffGenerator) DiffSchemas(old, new migrate.TableSchema) migrate.TableDiff {
	oldCols := makeColumnMap(old.Columns)
	newCols := makeColumnMap(new.Columns)

	mig := migrate.TableDiff{Up: []string{}, Down: []string{}}

	pushUp := func(s string) { mig.Up = append(mig.Up, s) }
	pushDownFront := func(s string) { mig.Down = append([]string{s}, mig.Down...) }
	pushDown := func(s string) { mig.Down = append(mig.Down, s) }

	if len(oldCols) == 0 && len(newCols) > 0 {
		return g.generateCreateTableDiff(new)
	}

	for _, name := range sortedColumnNames(newCols) {
		newCol := newCols[name]
		oldCol, exists := oldCols[name]
		if !exists {
			g.handleAddedColumn(&mig, new.TableName, newCol, pushUp, pushDownFront)
			continue
		}
		g.handleChangedColumn(&mig, new.TableName, oldCol, newCol, pushUp, pushDownFront)
	}

	for _, name := range sortedColumnNames(oldCols) {
		oldCol := oldCols[name]
		if _, exists := newCols[name]; !exists {
			g.handleRemovedColumn(&mig, old.TableName, oldCol, pushUp, pushDownFront)
		}
	}

	oldPKs := collectPKs(old)
	newPKs := collectPKs(new)
	if !stringSlicesEqual(oldPKs, newPKs) {
		g.handlePKChanges(&mig, new.TableName, oldPKs, newPKs, pushUp, pushDownFront)
	}

	g.handleIndexChanges(&mig, old, new, pushUp, pushDownFront, pushDown)
	g.handleCheckChanges(&mig, old, new, pushUp, pushDownFront, pushDown)

	return mig
}

func (g *DiffGenerator) generateCreateTableDiff(new migrate.TableSchema) migrate.TableDiff {
	mig := migrate.TableDiff{}

	columns := make([]string, 0, len(new.Columns))
	pkCols := make([]string, 0)
	constraints := []string{}

	for _, c := range new.Columns {
		colDef := g.buildColumnDefinition(c)
		columns = append(columns, colDef)

		if c.Attrs.IsPK {
			pkCols = append(pkCols, quoteIdent(c.ColumnName))
		}

		if c.Attrs.Unique {
			constrName := uniqueConstraintName(new.TableName, c.ColumnName)
			constraints = append(constraints, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)",
				quoteIdent(constrName), quoteIdent(c.ColumnName)))
		}
	}

	if len(pkCols) > 0 {
		columns = append(columns, fmt.Sprintf("CONSTRAINT %s PRIMARY KEY (%s)",
			quoteIdent(pkConstraintName(new.TableName)), strings.Join(pkCols, ", ")))
	}

	columns = append(columns, constraints...)

	createStmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)",
		quoteIdent(new.TableName), strings.Join(columns, ",\n  "))

	mig.Up = append(mig.Up, createStmt)
	mig.Down = append([]string{fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE",
		quoteIdent(new.TableName))}, mig.Down...)

	for _, c := range new.Columns {
		if c.Attrs.ForeignKey != nil {
			g.addForeignKey(&mig, new.TableName, c)
		}
	}

	// Add check constraints after table exists.
	for _, chk := range new.Checks {
		name := chk.Name
		if strings.TrimSpace(name) == "" {
			name = defaultCheckName(new.TableName, chk.Expr)
		}
		mig.Up = append(mig.Up, g.addCheckStatement(new.TableName, name, chk.Expr))
	}

	// Create indexes after table/constraints exist.
	for _, idx := range new.Indexes {
		name := idx.Name
		if strings.TrimSpace(name) == "" {
			name = defaultIndexName(new.TableName, idx.Columns)
		}
		mig.Up = append(mig.Up, g.createIndexStatement(new.TableName, name, idx.Columns, idx.Unique, idx.Where))
	}

	return mig
}

func (g *DiffGenerator) handleCheckChanges(
	mig *migrate.TableDiff,
	old, new migrate.TableSchema,
	pushUp func(string),
	pushDownFront func(string),
	pushDown func(string),
) {
	oldByKey := make(map[string]migrate.CheckMeta, len(old.Checks))
	for _, chk := range old.Checks {
		oldByKey[checkKey(chk)] = chk
	}

	newByKey := make(map[string]migrate.CheckMeta, len(new.Checks))
	for _, chk := range new.Checks {
		newByKey[checkKey(chk)] = chk
	}

	// Added checks.
	for _, key := range sortedCheckKeys(newByKey) {
		newChk := newByKey[key]
		if _, exists := oldByKey[key]; exists {
			continue
		}

		name := newChk.Name
		if strings.TrimSpace(name) == "" {
			name = defaultCheckName(new.TableName, newChk.Expr)
		}

		pushUp(g.addCheckStatement(new.TableName, name, newChk.Expr))
		pushDownFront(dropConstraintIfExists(new.TableName, name))
	}

	// Removed checks.
	for _, key := range sortedCheckKeys(oldByKey) {
		oldChk := oldByKey[key]
		if _, exists := newByKey[key]; exists {
			continue
		}

		name := oldChk.Name
		if strings.TrimSpace(name) == "" {
			// Shouldn't happen for fetched DB constraints, but keep it safe.
			name = defaultCheckName(old.TableName, oldChk.Expr)
		}

		pushUp(dropConstraintIfExists(old.TableName, name))
		pushDown(g.addCheckStatement(old.TableName, name, oldChk.Expr))
	}
}

func (g *DiffGenerator) handleIndexChanges(
	mig *migrate.TableDiff,
	old, new migrate.TableSchema,
	pushUp func(string),
	pushDownFront func(string),
	pushDown func(string),
) {
	oldByKey := make(map[string]migrate.IndexMeta, len(old.Indexes))
	for _, idx := range old.Indexes {
		oldByKey[indexKey(idx)] = idx
	}

	newByKey := make(map[string]migrate.IndexMeta, len(new.Indexes))
	for _, idx := range new.Indexes {
		newByKey[indexKey(idx)] = idx
	}

	// Added indexes.
	for _, key := range sortedIndexKeys(newByKey) {
		newIdx := newByKey[key]
		if _, exists := oldByKey[key]; exists {
			continue
		}

		name := newIdx.Name
		if strings.TrimSpace(name) == "" {
			name = defaultIndexName(new.TableName, newIdx.Columns)
		}

		pushUp(g.createIndexStatement(new.TableName, name, newIdx.Columns, newIdx.Unique, newIdx.Where))
		pushDownFront(fmt.Sprintf(`DROP INDEX IF EXISTS %s`, quoteIdent(name)))
	}

	// Removed indexes.
	for _, key := range sortedIndexKeys(oldByKey) {
		oldIdx := oldByKey[key]
		if _, exists := newByKey[key]; exists {
			continue
		}

		name := oldIdx.Name
		if strings.TrimSpace(name) == "" {
			// Shouldn't happen for fetched DB indexes, but keep it safe.
			name = defaultIndexName(old.TableName, oldIdx.Columns)
		}

		pushUp(fmt.Sprintf(`DROP INDEX IF EXISTS %s`, quoteIdent(name)))
		pushDown(g.createIndexStatement(old.TableName, name, oldIdx.Columns, oldIdx.Unique, oldIdx.Where))
	}
}

func indexKey(idx migrate.IndexMeta) string {
	where := ""
	if idx.Where != nil {
		where = strings.TrimSpace(*idx.Where)
	}
	return fmt.Sprintf("unique=%t|cols=%s|where=%s", idx.Unique, strings.Join(idx.Columns, "\x1f"), where)
}

func defaultIndexName(table string, cols []string) string {
	base := fmt.Sprintf("idx_%s_%s", table, strings.Join(cols, "_"))

	// Keep index names reasonably safe/deterministic.
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	base = strings.ReplaceAll(base, ".", "_")
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.Trim(base, "_")

	if len(base) > 60 {
		base = base[:60]
		base = strings.Trim(base, "_")
	}

	return base
}

func (g *DiffGenerator) createIndexStatement(table, name string, cols []string, unique bool, where *string) string {
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		parts = append(parts, quoteIdent(c))
	}

	uniq := ""
	if unique {
		uniq = "UNIQUE "
	}

	stmt := fmt.Sprintf(
		`CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)`,
		uniq,
		quoteIdent(name),
		quoteIdent(table),
		strings.Join(parts, ", "),
	)
	if where != nil && strings.TrimSpace(*where) != "" {
		stmt += " WHERE " + strings.TrimSpace(*where)
	}
	return stmt
}

func checkKey(chk migrate.CheckMeta) string {
	// Name is not part of identity; expr defines semantics.
	return fmt.Sprintf("expr=%s", strings.TrimSpace(chk.Expr))
}

func defaultCheckName(table, expr string) string {
	// Deterministic-ish name based on expression. Keep it simple and avoid new deps.
	e := strings.ToLower(strings.TrimSpace(expr))
	e = strings.ReplaceAll(e, " ", "_")
	e = strings.ReplaceAll(e, "\t", "_")
	e = strings.ReplaceAll(e, "\n", "_")
	e = strings.ReplaceAll(e, "\r", "_")
	e = strings.ReplaceAll(e, "(", "")
	e = strings.ReplaceAll(e, ")", "")
	e = strings.ReplaceAll(e, "'", "")
	e = strings.ReplaceAll(e, "\"", "")

	for strings.Contains(e, "__") {
		e = strings.ReplaceAll(e, "__", "_")
	}
	e = strings.Trim(e, "_")
	if e == "" {
		e = "check"
	}

	base := fmt.Sprintf("chk_%s_%s", table, e)
	if len(base) > 60 {
		base = base[:60]
		base = strings.Trim(base, "_")
	}
	return base
}

func (g *DiffGenerator) addCheckStatement(table, name, expr string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimSuffix(expr, ";")
	expr = strings.TrimSpace(expr)

	stmt := fmt.Sprintf(
		`ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)`,
		quoteIdent(table),
		quoteIdent(name),
		expr,
	)

	// Mirror the unique/fk approach: only add if missing.
	return addConstraintIfNotExists(stmt, name)
}

func (g *DiffGenerator) handleAddedColumn(mig *migrate.TableDiff, table string, col migrate.ColumnMeta, pushUp, pushDownFront func(string)) {
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		quoteIdent(table), quoteIdent(col.ColumnName), col.Attrs.PgType)

	if col.Attrs.Default != nil {
		stmt += " DEFAULT " + *col.Attrs.Default
	}

	if col.Attrs.NotNull {
		if col.Attrs.Default == nil {

			pushUp(stmt)

			guard := fmt.Sprintf(`DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM %s WHERE %s IS NULL) THEN
    ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;
  END IF;
END $$;`, quoteIdent(table), quoteIdent(col.ColumnName), quoteIdent(table), quoteIdent(col.ColumnName))
			pushUp(guard)
		} else {

			stmt += " NOT NULL"
			pushUp(stmt)
		}
	} else {
		pushUp(stmt)
	}

	pushDownFront(fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s",
		quoteIdent(table), quoteIdent(col.ColumnName)))

	if col.Attrs.Unique {
		g.addUniqueConstraint(mig, table, col, pushUp, pushDownFront)
	}

	if col.Attrs.ForeignKey != nil {
		g.addForeignKey(mig, table, col)
	}
}

func (g *DiffGenerator) handleChangedColumn(mig *migrate.TableDiff, table string, oldCol, newCol migrate.ColumnMeta, pushUp, pushDownFront func(string)) {

	if oldCol.Attrs.PgType != newCol.Attrs.PgType {
		up := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
			quoteIdent(table), quoteIdent(newCol.ColumnName), newCol.Attrs.PgType,
			quoteIdent(newCol.ColumnName), newCol.Attrs.PgType)
		down := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
			quoteIdent(table), quoteIdent(newCol.ColumnName), oldCol.Attrs.PgType,
			quoteIdent(newCol.ColumnName), oldCol.Attrs.PgType)
		pushUp(up)
		pushDownFront(down)
	}

	if oldCol.Attrs.NotNull != newCol.Attrs.NotNull {
		if newCol.Attrs.NotNull {

			guard := fmt.Sprintf(`DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM %s WHERE %s IS NULL) THEN
    ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;
  END IF;
END $$;`, quoteIdent(table), quoteIdent(newCol.ColumnName), quoteIdent(table), quoteIdent(newCol.ColumnName))
			pushUp(guard)
			pushDownFront(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
				quoteIdent(table), quoteIdent(newCol.ColumnName)))
		} else {
			pushUp(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
				quoteIdent(table), quoteIdent(newCol.ColumnName)))

			pushDownFront(fmt.Sprintf(`DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM %s WHERE %s IS NULL) THEN
    ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;
  END IF;
END $$;`, quoteIdent(table), quoteIdent(newCol.ColumnName), quoteIdent(table), quoteIdent(newCol.ColumnName)))
		}
	}

	oldDef := ""
	if oldCol.Attrs.Default != nil {
		oldDef = *oldCol.Attrs.Default
	}
	newDef := ""
	if newCol.Attrs.Default != nil {
		newDef = *newCol.Attrs.Default
	}
	if oldDef != newDef {
		if newCol.Attrs.Default != nil {
			pushUp(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
				quoteIdent(table), quoteIdent(newCol.ColumnName), newDef))
		} else {
			pushUp(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
				quoteIdent(table), quoteIdent(newCol.ColumnName)))
		}

		if oldCol.Attrs.Default != nil {
			pushDownFront(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
				quoteIdent(table), quoteIdent(newCol.ColumnName), oldDef))
		} else {
			pushDownFront(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
				quoteIdent(table), quoteIdent(newCol.ColumnName)))
		}
	}

	if oldCol.Attrs.Unique != newCol.Attrs.Unique {
		if newCol.Attrs.Unique {
			g.addUniqueConstraint(mig, table, newCol, pushUp, pushDownFront)
		} else {
			g.dropUniqueConstraint(mig, table, oldCol, pushUp, pushDownFront)
		}
	}

	g.handleForeignKeyChanges(mig, table, oldCol, newCol, pushUp, pushDownFront)
}

func (g *DiffGenerator) handleRemovedColumn(mig *migrate.TableDiff, table string, oldCol migrate.ColumnMeta, pushUp, pushDownFront func(string)) {

	pushUp(fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s",
		quoteIdent(table), quoteIdent(oldCol.ColumnName)))

	down := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		quoteIdent(table), quoteIdent(oldCol.ColumnName), oldCol.Attrs.PgType)

	if oldCol.Attrs.Default != nil {
		down += " DEFAULT " + *oldCol.Attrs.Default
	}
	if oldCol.Attrs.NotNull || oldCol.Attrs.IsPK {
		down += " NOT NULL"
	}

	if oldCol.Attrs.IsPK {
		down += fmt.Sprintf("; ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
			quoteIdent(table), quoteIdent(pkConstraintName(table)), quoteIdent(oldCol.ColumnName))
	}
	if oldCol.Attrs.Unique {
		constrName := g.getConstraintName(oldCol, uniqueConstraintName(table, oldCol.ColumnName))
		down += fmt.Sprintf("; %s", addConstraintIfNotExists(
			fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)",
				quoteIdent(table), quoteIdent(constrName), quoteIdent(oldCol.ColumnName)),
			constrName))
	}
	if oldCol.Attrs.ForeignKey != nil {
		fk := oldCol.Attrs.ForeignKey
		constrName := g.getConstraintName(oldCol, fkConstraintName(table, oldCol.ColumnName))
		down += fmt.Sprintf("; %s", addConstraintIfNotExists(
			fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s ON UPDATE %s",
				quoteIdent(table), quoteIdent(constrName), quoteIdent(oldCol.ColumnName),
				quoteIdent(fk.Table), quoteIdent(fk.Column),
				getForeignKeyAction(fk.OnDelete), getForeignKeyAction(fk.OnUpdate)),
			constrName))
	}

	pushDownFront(down)
}

func (g *DiffGenerator) addUniqueConstraint(mig *migrate.TableDiff, table string, col migrate.ColumnMeta, pushUp, pushDownFront func(string)) {
	constrName := uniqueConstraintName(table, col.ColumnName)
	addUnique := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)",
		quoteIdent(table), quoteIdent(constrName), quoteIdent(col.ColumnName))
	pushUp(addConstraintIfNotExists(addUnique, constrName))
	pushDownFront(dropConstraintIfExists(table, constrName))
}

func (g *DiffGenerator) dropUniqueConstraint(mig *migrate.TableDiff, table string, col migrate.ColumnMeta, pushUp, pushDownFront func(string)) {
	constrName := g.getConstraintName(col, uniqueConstraintName(table, col.ColumnName))
	pushUp(dropConstraintIfExists(table, constrName))
	pushDownFront(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)",
		quoteIdent(table), quoteIdent(constrName), quoteIdent(col.ColumnName)))
}

func (g *DiffGenerator) addForeignKey(mig *migrate.TableDiff, table string, col migrate.ColumnMeta) {
	fk := col.Attrs.ForeignKey
	constrName := fkConstraintName(table, col.ColumnName)
	addFK := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s ON UPDATE %s",
		quoteIdent(table), quoteIdent(constrName), quoteIdent(col.ColumnName),
		quoteIdent(fk.Table), quoteIdent(fk.Column),
		getForeignKeyAction(fk.OnDelete), getForeignKeyAction(fk.OnUpdate))
	mig.Up = append(mig.Up, addConstraintIfNotExists(addFK, constrName))
	mig.Down = append([]string{dropConstraintIfExists(table, constrName)}, mig.Down...)
}

func (g *DiffGenerator) handleForeignKeyChanges(mig *migrate.TableDiff, table string, oldCol, newCol migrate.ColumnMeta, pushUp, pushDownFront func(string)) {
	oldFK := oldCol.Attrs.ForeignKey
	newFK := newCol.Attrs.ForeignKey

	fkChanged := false
	if (oldFK == nil) != (newFK == nil) {
		fkChanged = true
	} else if oldFK != nil && newFK != nil {
		if !foreignKeysEqual(oldFK, newFK) {
			fkChanged = true
		}
	}

	if fkChanged {

		if oldFK != nil {
			constrName := g.getConstraintName(oldCol, fkConstraintName(table, oldCol.ColumnName))
			pushUp(dropConstraintIfExists(table, constrName))

			pushDownFront(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s ON UPDATE %s",
				quoteIdent(table), quoteIdent(constrName), quoteIdent(oldCol.ColumnName),
				quoteIdent(oldFK.Table), quoteIdent(oldFK.Column),
				getForeignKeyAction(oldFK.OnDelete), getForeignKeyAction(oldFK.OnUpdate)))
		}

		if newFK != nil {
			g.addForeignKey(mig, table, newCol)
		}
	}
}

func foreignKeysEqual(a, b *migrate.ForeignKey) bool {
	if a == nil || b == nil {
		return a == b
	}
	return normalizeRefIdent(a.Table) == normalizeRefIdent(b.Table) &&
		normalizeRefIdent(a.Column) == normalizeRefIdent(b.Column)
}

func normalizeRefIdent(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`+"`")
	if v == "" {
		return ""
	}
	parts := strings.Split(v, ".")
	last := strings.TrimSpace(parts[len(parts)-1])
	last = strings.Trim(last, `"'`+"`")
	return strings.ToLower(last)
}

func (g *DiffGenerator) handlePKChanges(mig *migrate.TableDiff, table string, oldPKs, newPKs []string, pushUp, pushDownFront func(string)) {

	if len(oldPKs) > 0 {
		pushUp(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s",
			quoteIdent(table), quoteIdent(pkConstraintName(table))))
		pushDownFront(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
			quoteIdent(table), quoteIdent(pkConstraintName(table)), strings.Join(oldPKs, ", ")))
	}

	if len(newPKs) > 0 {
		pushUp(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s)",
			quoteIdent(table), quoteIdent(pkConstraintName(table)), strings.Join(newPKs, ", ")))
		pushDownFront(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s",
			quoteIdent(table), quoteIdent(pkConstraintName(table))))
	}
}

func (g *DiffGenerator) buildColumnDefinition(col migrate.ColumnMeta) string {
	def := fmt.Sprintf("%s %s", quoteIdent(col.ColumnName), col.Attrs.PgType)

	if col.Attrs.NotNull {
		def += " NOT NULL"
	}

	if col.Attrs.Default != nil {
		def += " DEFAULT " + *col.Attrs.Default
	}

	return def
}

func (g *DiffGenerator) getConstraintName(col migrate.ColumnMeta, defaultName string) string {
	if col.Attrs.ConstraintName != nil {
		return *col.Attrs.ConstraintName
	}
	return defaultName
}

func makeColumnMap(columns []migrate.ColumnMeta) map[string]migrate.ColumnMeta {
	m := make(map[string]migrate.ColumnMeta)
	for _, col := range columns {
		m[col.ColumnName] = col
	}
	return m
}

func sortedColumnNames(columns map[string]migrate.ColumnMeta) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedIndexKeys(indexes map[string]migrate.IndexMeta) []string {
	keys := make([]string, 0, len(indexes))
	for key := range indexes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedCheckKeys(checks map[string]migrate.CheckMeta) []string {
	keys := make([]string, 0, len(checks))
	for key := range checks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func quoteIdent(name string) string {
	name = strings.ReplaceAll(name, `"`, `""`)
	return `"` + name + `"`
}

func pkConstraintName(table string) string {
	return fmt.Sprintf("%s_pkey", table)
}

func uniqueConstraintName(table, column string) string {
	return fmt.Sprintf("uc_%s_%s", table, column)
}

func fkConstraintName(table, column string) string {
	return fmt.Sprintf("fk_%s_%s", table, column)
}

func addConstraintIfNotExists(stmt string, constraintName string) string {
	constraintName = quoteLiteral(constraintName)
	return fmt.Sprintf(
		`DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = '%s') THEN
    %s;
  END IF;
END $$;`, constraintName, stmt)
}

func dropConstraintIfExists(table, constraintName string) string {
	return fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s`,
		quoteIdent(table), quoteIdent(constraintName))
}

func quoteLiteral(v string) string {
	return strings.ReplaceAll(v, `'`, `''`)
}

func getForeignKeyAction(action migrate.OnActionType) string {
	if action == "" {
		return "NO ACTION"
	}
	return string(action)
}

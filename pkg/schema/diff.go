package schema

import (
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
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

	if len(oldCols) == 0 && len(newCols) > 0 {
		return g.generateCreateTableDiff(new)
	}

	for name, newCol := range newCols {
		oldCol, exists := oldCols[name]
		if !exists {
			g.handleAddedColumn(&mig, new.TableName, newCol, pushUp, pushDownFront)
			continue
		}
		g.handleChangedColumn(&mig, new.TableName, oldCol, newCol, pushUp, pushDownFront)
	}

	for name, oldCol := range oldCols {
		if _, exists := newCols[name]; !exists {
			g.handleRemovedColumn(&mig, old.TableName, oldCol, pushUp, pushDownFront)
		}
	}

	oldPKs := collectPKs(old)
	newPKs := collectPKs(new)
	if !stringSlicesEqual(oldPKs, newPKs) {
		g.handlePKChanges(&mig, new.TableName, oldPKs, newPKs, pushUp, pushDownFront)
	}

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

	return mig
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
		if oldFK.Table != newFK.Table || oldFK.Column != newFK.Column ||
			oldFK.OnDelete != newFK.OnDelete || oldFK.OnUpdate != newFK.OnUpdate {
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

func getForeignKeyAction(action migrate.OnActionType) string {
	if action == "" {
		return "NO ACTION"
	}
	return string(action)
}

package schema

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
)

type PgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Fetcher struct {
	pool PgxQuerier
}

func NewFetcher(pool PgxQuerier) *Fetcher {
	return &Fetcher{pool: pool}
}
func (f *Fetcher) Fetch(ctx context.Context, table string) (migrate.TableSchema, error) {
	// First, detect relation existence explicitly so we can distinguish:
	// - table truly does not exist
	// - metadata query unexpectedly returned no columns for an existing table
	const existsQ = `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind = 'r'
			  AND c.relname = $1
			  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		);
	`
	var tableExists bool
	if err := f.pool.QueryRow(ctx, existsQ, table).Scan(&tableExists); err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query table existence: %w", err)
	}

	// ---------- Columns ----------
	const colsQ = `
		SELECT
			col.column_name,
			pg_catalog.format_type(a.atttypid, a.atttypmod) AS formatted_type,
			col.is_nullable,
			col.column_default
		FROM information_schema.columns col
		JOIN pg_catalog.pg_class c ON c.relname = col.table_name
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attname = col.column_name
		WHERE col.table_name = $1
		  AND col.table_schema = current_schema()
		  AND c.relnamespace = (SELECT oid FROM pg_catalog.pg_namespace WHERE nspname = current_schema())
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY col.ordinal_position;
	`
	rows, err := f.pool.Query(ctx, colsQ, table)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()

	colsMap := map[string]migrate.ColumnMeta{}
	colOrder := make([]string, 0)

	for rows.Next() {
		var name, pgType, isNullableStr string
		var colDefault *string

		if err := rows.Scan(&name, &pgType, &isNullableStr, &colDefault); err != nil {
			return migrate.TableSchema{}, err
		}

		attrs := migrate.ColumnAttributes{
			PgType:  pgType,
			NotNull: isNullableStr == "NO",
		}

		if colDefault != nil {
			d := *colDefault
			attrs.Default = &d
		}

		colsMap[name] = migrate.ColumnMeta{
			FieldName:  name,
			ColumnName: name,
			Attrs:      attrs,
		}
		colOrder = append(colOrder, name)
	}
	if err := rows.Err(); err != nil {
		return migrate.TableSchema{}, fmt.Errorf("iterate columns: %w", err)
	}

	if tableExists && len(colsMap) == 0 {
		return migrate.TableSchema{}, fmt.Errorf(
			"table %s exists but no columns were fetched (check search_path/permissions/table naming)",
			table,
		)
	}

	// ---------- PRIMARY KEY (+ real constraint name) ----------
	const pkQ = `
		SELECT
			a.attname,
			c.conname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_constraint c ON c.conindid = i.indexrelid
		WHERE t.relname = $1 AND i.indisprimary;
	`
	pkRows, err := f.pool.Query(ctx, pkQ, table)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query primary keys: %w", err)
	}
	for pkRows.Next() {
		var colName, conName string
		if err := pkRows.Scan(&colName, &conName); err != nil {
			pkRows.Close()
			return migrate.TableSchema{}, fmt.Errorf("scan primary key row: %w", err)
		}
		if cm, ok := colsMap[colName]; ok {
			cm.Attrs.IsPK = true
			cm.Attrs.NotNull = true
			cm.Attrs.ConstraintName = &conName
			colsMap[colName] = cm
		}
	}
	if err := pkRows.Err(); err != nil {
		pkRows.Close()
		return migrate.TableSchema{}, fmt.Errorf("iterate primary key rows: %w", err)
	}
	pkRows.Close()

	// ---------- UNIQUE (+ real constraint name) ----------
	const uniqQ = `
		SELECT
			a.attname,
			c.conname
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN unnest(c.conkey) WITH ORDINALITY AS cols(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = cols.attnum
		WHERE t.relname = $1 AND c.contype = 'u';
	`
	uqRows, err := f.pool.Query(ctx, uniqQ, table)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query unique constraints: %w", err)
	}
	for uqRows.Next() {
		var colName, conName string
		if err := uqRows.Scan(&colName, &conName); err != nil {
			uqRows.Close()
			return migrate.TableSchema{}, fmt.Errorf("scan unique row: %w", err)
		}
		if cm, ok := colsMap[colName]; ok {
			cm.Attrs.Unique = true
			cm.Attrs.ConstraintName = &conName
			colsMap[colName] = cm
		}
	}
	if err := uqRows.Err(); err != nil {
		uqRows.Close()
		return migrate.TableSchema{}, fmt.Errorf("iterate unique rows: %w", err)
	}
	uqRows.Close()

	// ---------- FOREIGN KEY (+ real constraint name) ----------
	const fkQ = `
		SELECT
			a_local.attname AS column_name,
			foreign_table.relname AS foreign_table_name,
			a_foreign.attname AS foreign_column_name,
			CASE con.confupdtype
				WHEN 'a' THEN 'NO ACTION'
				WHEN 'r' THEN 'RESTRICT'
				WHEN 'c' THEN 'CASCADE'
				WHEN 'n' THEN 'SET NULL'
				WHEN 'd' THEN 'SET DEFAULT'
			END AS update_rule,
			CASE con.confdeltype
				WHEN 'a' THEN 'NO ACTION'
				WHEN 'r' THEN 'RESTRICT'
				WHEN 'c' THEN 'CASCADE'
				WHEN 'n' THEN 'SET NULL'
				WHEN 'd' THEN 'SET DEFAULT'
			END AS delete_rule,
			con.conname AS constraint_name
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class local_table ON local_table.oid = con.conrelid
		JOIN pg_catalog.pg_namespace local_ns ON local_ns.oid = local_table.relnamespace
		JOIN pg_catalog.pg_class foreign_table ON foreign_table.oid = con.confrelid
		JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS lk(attnum, ord) ON true
		JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS fk(attnum, ord) ON fk.ord = lk.ord
		JOIN pg_catalog.pg_attribute a_local
			ON a_local.attrelid = con.conrelid AND a_local.attnum = lk.attnum
		JOIN pg_catalog.pg_attribute a_foreign
			ON a_foreign.attrelid = con.confrelid AND a_foreign.attnum = fk.attnum
		WHERE con.contype = 'f'
		  AND local_table.relname = $1
		  AND local_ns.nspname = current_schema();
	`
	fkRows, err := f.pool.Query(ctx, fkQ, table)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query foreign keys: %w", err)
	}
	for fkRows.Next() {
		var col, fTable, fCol, onUpdate, onDelete, conName string
		if err := fkRows.Scan(&col, &fTable, &fCol, &onUpdate, &onDelete, &conName); err != nil {
			fkRows.Close()
			return migrate.TableSchema{}, fmt.Errorf("scan foreign key row: %w", err)
		}
		if cm, ok := colsMap[col]; ok {
			cm.Attrs.ForeignKey = &migrate.ForeignKey{
				Table:    fTable,
				Column:   fCol,
				OnUpdate: migrate.OnActionType(strings.ToUpper(onUpdate)),
				OnDelete: migrate.OnActionType(strings.ToUpper(onDelete)),
			}
			cm.Attrs.ConstraintName = &conName
			colsMap[col] = cm
		}
	}
	if err := fkRows.Err(); err != nil {
		fkRows.Close()
		return migrate.TableSchema{}, fmt.Errorf("iterate foreign key rows: %w", err)
	}
	fkRows.Close()

	// ---------- Non-constraint indexes (incl. composite) ----------
	// Exclude indexes backing constraints by filtering out indexes referenced by pg_constraint.conindid.
	// This keeps us focused on regular indexes declared by `CREATE INDEX` (including UNIQUE indexes).
	const idxQ = `
		SELECT
			i.relname AS index_name,
			ix.indisunique AS is_unique,
			ARRAY_AGG(a.attname ORDER BY k.ord) AS cols,
			pg_get_expr(ix.indpred, ix.indrelid) AS pred
		FROM pg_index ix
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		LEFT JOIN pg_constraint c ON c.conindid = ix.indexrelid
		WHERE t.relname = $1
		  AND n.nspname = current_schema()
		  AND c.oid IS NULL
		  AND ix.indisprimary = false
		GROUP BY i.relname, ix.indisunique, ix.indpred, ix.indrelid;
	`
	idxRows, err := f.pool.Query(ctx, idxQ, table)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query indexes: %w", err)
	}

	indexes := make([]migrate.IndexMeta, 0)
	defer idxRows.Close()
	for idxRows.Next() {
		var indexName string
		var isUnique bool
		var cols []string
		var pred *string
		if err := idxRows.Scan(&indexName, &isUnique, &cols, &pred); err != nil {
			return migrate.TableSchema{}, fmt.Errorf("scan index row: %w", err)
		}
		if len(cols) == 0 {
			continue
		}
		indexes = append(indexes, migrate.IndexMeta{
			Name:    indexName,
			Columns: cols,
			Unique:  isUnique,
			Where:   pred,
		})
	}
	if err := idxRows.Err(); err != nil {
		return migrate.TableSchema{}, fmt.Errorf("iterate index rows: %w", err)
	}

	// ---------- CHECK constraints ----------
	const chkQ = `
		SELECT
			c.conname AS constraint_name,
			pg_get_constraintdef(c.oid) AS condef
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		WHERE t.relname = $1 AND c.contype = 'c';
	`
	chkRows, err := f.pool.Query(ctx, chkQ, table)
	checks := make([]migrate.CheckMeta, 0)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query check constraints: %w", err)
	}
	defer chkRows.Close()
	for chkRows.Next() {
		var name, def string
		if err := chkRows.Scan(&name, &def); err != nil {
			return migrate.TableSchema{}, fmt.Errorf("scan check row: %w", err)
		}

		// def is like: "CHECK ((price > 0))"
		def = strings.TrimSpace(def)
		def = strings.TrimSuffix(def, ";")
		def = strings.TrimSpace(def)
		def = strings.TrimPrefix(def, "CHECK")
		def = strings.TrimPrefix(def, "check")
		def = strings.TrimSpace(def)

		for strings.HasPrefix(def, "(") && strings.HasSuffix(def, ")") {
			def = strings.TrimSpace(def[1 : len(def)-1])
		}

		if def == "" {
			continue
		}

		checks = append(checks, migrate.CheckMeta{
			Name: name,
			Expr: def,
		})
	}
	if err := chkRows.Err(); err != nil {
		return migrate.TableSchema{}, fmt.Errorf("iterate check rows: %w", err)
	}

	cols := make([]migrate.ColumnMeta, 0, len(colOrder))
	for _, colName := range colOrder {
		if col, ok := colsMap[colName]; ok {
			cols = append(cols, col)
		}
	}

	sort.Slice(indexes, func(i, j int) bool { return indexes[i].Name < indexes[j].Name })
	sort.Slice(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })

	return migrate.TableSchema{
		TableName: table,
		Columns:   cols,
		Indexes:   indexes,
		Checks:    checks,
	}, nil
}

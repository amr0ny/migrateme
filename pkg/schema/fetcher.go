package schema

import (
	"context"
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"github.com/jackc/pgx/v5"
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
			col.udt_name,
			col.data_type,
			col.is_nullable,
			col.column_default
		FROM information_schema.columns col
		WHERE col.table_name = $1
		ORDER BY col.ordinal_position;
	`
	rows, err := f.pool.Query(ctx, colsQ, table)
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()

	colsMap := map[string]migrate.ColumnMeta{}

	for rows.Next() {
		var name, udtName, dataType, isNullableStr string
		var colDefault *string

		if err := rows.Scan(&name, &udtName, &dataType, &isNullableStr, &colDefault); err != nil {
			return migrate.TableSchema{}, err
		}

		pgType := udtName
		if dataType == "ARRAY" {
			pgType = dataType
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
	}
	if err := rows.Err(); err != nil {
		return migrate.TableSchema{}, err
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
	if err == nil {
		for pkRows.Next() {
			var colName, conName string
			if err := pkRows.Scan(&colName, &conName); err == nil {
				if cm, ok := colsMap[colName]; ok {
					cm.Attrs.IsPK = true
					cm.Attrs.NotNull = true
					cm.Attrs.ConstraintName = &conName
					colsMap[colName] = cm
				}
			}
		}
		pkRows.Close()
	}

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
	if err == nil {
		for uqRows.Next() {
			var colName, conName string
			if err := uqRows.Scan(&colName, &conName); err == nil {
				if cm, ok := colsMap[colName]; ok {
					cm.Attrs.Unique = true
					cm.Attrs.ConstraintName = &conName
					colsMap[colName] = cm
				}
			}
		}
		uqRows.Close()
	}

	// ---------- FOREIGN KEY (+ real constraint name) ----------
	const fkQ = `
		SELECT
			kcu.column_name,
			ccu.table_name AS foreign_table_name,
			ccu.column_name AS foreign_column_name,
			rc.update_rule,
			rc.delete_rule,
			tc.constraint_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
		   AND tc.table_name = kcu.table_name
		JOIN information_schema.constraint_column_usage ccu
			ON ccu.constraint_name = tc.constraint_name
		JOIN information_schema.referential_constraints rc
			ON rc.constraint_name = tc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_name = $1;
	`
	fkRows, err := f.pool.Query(ctx, fkQ, table)
	if err == nil {
		for fkRows.Next() {
			var col, fTable, fCol, onUpdate, onDelete, conName string

			if err := fkRows.Scan(&col, &fTable, &fCol, &onUpdate, &onDelete, &conName); err == nil {
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
		}
		fkRows.Close()
	}

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
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		LEFT JOIN pg_constraint c ON c.conindid = ix.indexrelid
		WHERE t.relname = $1
		  AND c.oid IS NULL
		  AND ix.indisprimary = false
		GROUP BY i.relname, ix.indisunique;
	`
	idxRows, err := f.pool.Query(ctx, idxQ, table)
	if err != nil {
		// indexes are optional for migrations generation; treat fetch failure as "no indexes"
		idxRows = nil
	}

	indexes := make([]migrate.IndexMeta, 0)
	if idxRows != nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var indexName string
			var isUnique bool
			var cols []string
			var pred *string
			if err := idxRows.Scan(&indexName, &isUnique, &cols, &pred); err != nil {
				return migrate.TableSchema{}, err
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
	if err == nil {
		defer chkRows.Close()
		for chkRows.Next() {
			var name, def string
			if err := chkRows.Scan(&name, &def); err != nil {
				return migrate.TableSchema{}, err
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
	}

	cols := make([]migrate.ColumnMeta, 0, len(colsMap))
	for _, col := range colsMap {
		cols = append(cols, col)
	}

	return migrate.TableSchema{
		TableName: table,
		Columns:   cols,
		Indexes:   indexes,
		Checks:    checks,
	}, nil
}

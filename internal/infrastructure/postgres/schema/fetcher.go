package schema

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
)

type PgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Fetcher struct {
	pool PgxQuerier
}

func NewFetcher(pool PgxQuerier) *Fetcher {
	return &Fetcher{pool: pool}
}
func (f *Fetcher) Fetch(ctx context.Context, table string) (TableSchema, error) {
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
		return TableSchema{}, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()

	colsMap := map[string]ColumnMeta{}

	for rows.Next() {
		var name, udtName, dataType, isNullableStr string
		var colDefault *string

		if err := rows.Scan(&name, &udtName, &dataType, &isNullableStr, &colDefault); err != nil {
			return TableSchema{}, err
		}

		pgType := udtName
		if dataType == "ARRAY" {
			pgType = dataType
		}

		attrs := ColumnAttributes{
			PgType:  pgType,
			NotNull: isNullableStr == "NO",
		}

		if colDefault != nil {
			d := *colDefault
			attrs.Default = &d
		}

		colsMap[name] = ColumnMeta{
			FieldName:  name,
			ColumnName: name,
			Attrs:      attrs,
		}
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
					cm.Attrs.ForeignKey = &ForeignKey{
						Table:    fTable,
						Column:   fCol,
						OnUpdate: OnActionType(strings.ToUpper(onUpdate)),
						OnDelete: OnActionType(strings.ToUpper(onDelete)),
					}
					cm.Attrs.ConstraintName = &conName
					colsMap[col] = cm
				}
			}
		}
		fkRows.Close()
	}

	cols := make([]ColumnMeta, 0, len(colsMap))
	for _, col := range colsMap {
		cols = append(cols, col)
	}

	return TableSchema{
		TableName: table,
		Columns:   cols,
	}, nil
}

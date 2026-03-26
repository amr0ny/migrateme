package schema

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/amr0ny/migrateme/pkg/migrate"
)

type SQLiteFetcher struct {
	db *sql.DB
}

func NewSQLiteFetcher(db *sql.DB) *SQLiteFetcher {
	return &SQLiteFetcher{db: db}
}

func (f *SQLiteFetcher) Fetch(ctx context.Context, table string) (migrate.TableSchema, error) {
	var createSQL string
	err := f.db.QueryRowContext(ctx,
		`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name = ?`,
		table,
	).Scan(&createSQL)
	if err == sql.ErrNoRows {
		return migrate.TableSchema{TableName: table}, nil
	}
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query sqlite table ddl: %w", err)
	}

	colsByName := map[string]migrate.ColumnMeta{}
	colOrder := make([]string, 0)

	pragmaTable := quoteSQLiteIdent(table)
	colRows, err := f.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, pragmaTable))
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query sqlite table_info: %w", err)
	}
	for colRows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			defaultV  sql.NullString
			pkOrdinal int
		)
		if err := colRows.Scan(&cid, &name, &ctype, &notnull, &defaultV, &pkOrdinal); err != nil {
			colRows.Close()
			return migrate.TableSchema{}, fmt.Errorf("scan sqlite table_info row: %w", err)
		}
		attrs := migrate.ColumnAttributes{
			PgType:  strings.ToLower(strings.TrimSpace(ctype)),
			NotNull: notnull == 1,
			IsPK:    pkOrdinal > 0,
		}
		if attrs.PgType == "" {
			attrs.PgType = "text"
		}
		if defaultV.Valid {
			v := defaultV.String
			attrs.Default = &v
		}
		col := migrate.ColumnMeta{
			FieldName:  name,
			ColumnName: name,
			Idx:        cid,
			Attrs:      attrs,
		}
		colsByName[name] = col
		colOrder = append(colOrder, name)
	}
	if err := colRows.Err(); err != nil {
		colRows.Close()
		return migrate.TableSchema{}, fmt.Errorf("iterate sqlite table_info rows: %w", err)
	}
	colRows.Close()

	fkRows, err := f.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, pragmaTable))
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query sqlite foreign_key_list: %w", err)
	}
	for fkRows.Next() {
		var (
			id       int
			seq      int
			refTable string
			fromCol  string
			toCol    string
			onUpdate string
			onDelete string
			match    string
		)
		if err := fkRows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
			fkRows.Close()
			return migrate.TableSchema{}, fmt.Errorf("scan sqlite foreign_key_list row: %w", err)
		}
		col, ok := colsByName[fromCol]
		if !ok {
			continue
		}
		col.Attrs.ForeignKey = &migrate.ForeignKey{
			Table:    refTable,
			Column:   toCol,
			OnUpdate: migrate.OnActionType(strings.ToUpper(strings.TrimSpace(onUpdate))),
			OnDelete: migrate.OnActionType(strings.ToUpper(strings.TrimSpace(onDelete))),
		}
		colsByName[fromCol] = col
	}
	if err := fkRows.Err(); err != nil {
		fkRows.Close()
		return migrate.TableSchema{}, fmt.Errorf("iterate sqlite foreign_key_list rows: %w", err)
	}
	fkRows.Close()

	indexes := make([]migrate.IndexMeta, 0)
	idxRows, err := f.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA index_list(%s)`, pragmaTable))
	if err != nil {
		return migrate.TableSchema{}, fmt.Errorf("query sqlite index_list: %w", err)
	}
	for idxRows.Next() {
		var (
			seq     int
			index   string
			unique  int
			origin  string
			partial int
		)
		if err := idxRows.Scan(&seq, &index, &unique, &origin, &partial); err != nil {
			idxRows.Close()
			return migrate.TableSchema{}, fmt.Errorf("scan sqlite index_list row: %w", err)
		}
		if origin == "pk" {
			continue
		}
		cols, err := f.sqliteIndexColumns(ctx, index)
		if err != nil {
			idxRows.Close()
			return migrate.TableSchema{}, err
		}
		if len(cols) == 0 {
			continue
		}
		if unique == 1 && len(cols) == 1 {
			if col, ok := colsByName[cols[0]]; ok {
				col.Attrs.Unique = true
				colsByName[cols[0]] = col
			}
		}
		var where *string
		if partial == 1 {
			w, err := f.sqliteIndexWhere(ctx, index)
			if err != nil {
				idxRows.Close()
				return migrate.TableSchema{}, err
			}
			where = w
		}
		indexes = append(indexes, migrate.IndexMeta{
			Name:    index,
			Columns: cols,
			Unique:  unique == 1,
			Where:   where,
		})
	}
	if err := idxRows.Err(); err != nil {
		idxRows.Close()
		return migrate.TableSchema{}, fmt.Errorf("iterate sqlite index_list rows: %w", err)
	}
	idxRows.Close()

	checks := extractSQLiteChecks(createSQL, table)

	cols := make([]migrate.ColumnMeta, 0, len(colOrder))
	for _, name := range colOrder {
		cols = append(cols, colsByName[name])
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

func (f *SQLiteFetcher) sqliteIndexColumns(ctx context.Context, indexName string) ([]string, error) {
	rows, err := f.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA index_info(%s)`, quoteSQLiteIdent(indexName)))
	if err != nil {
		return nil, fmt.Errorf("query sqlite index_info: %w", err)
	}
	defer rows.Close()

	cols := make([]string, 0)
	for rows.Next() {
		var seqno int
		var cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, fmt.Errorf("scan sqlite index_info row: %w", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite index_info rows: %w", err)
	}
	return cols, nil
}

func (f *SQLiteFetcher) sqliteIndexWhere(ctx context.Context, indexName string) (*string, error) {
	var sqlText string
	err := f.db.QueryRowContext(ctx,
		`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type='index' AND name=?`,
		indexName,
	).Scan(&sqlText)
	if err != nil {
		return nil, fmt.Errorf("query sqlite index sql: %w", err)
	}
	up := strings.ToUpper(sqlText)
	pos := strings.Index(up, " WHERE ")
	if pos == -1 {
		return nil, nil
	}
	where := strings.TrimSpace(sqlText[pos+len(" WHERE "):])
	where = strings.TrimSuffix(where, ";")
	if where == "" {
		return nil, nil
	}
	return &where, nil
}

var sqliteNamedCheckRe = regexp.MustCompile(`(?is)CONSTRAINT\s+([A-Za-z0-9_]+)\s+CHECK\s*\((.+?)\)`)
var sqliteCheckRe = regexp.MustCompile(`(?is)CHECK\s*\((.+?)\)`)

func extractSQLiteChecks(createSQL, table string) []migrate.CheckMeta {
	createSQL = strings.TrimSpace(createSQL)
	if createSQL == "" {
		return nil
	}
	checks := make([]migrate.CheckMeta, 0)
	matches := sqliteNamedCheckRe.FindAllStringSubmatch(createSQL, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		expr := strings.TrimSpace(m[2])
		if expr == "" {
			continue
		}
		checks = append(checks, migrate.CheckMeta{Name: m[1], Expr: expr})
	}
	if len(checks) > 0 {
		return checks
	}
	for _, m := range sqliteCheckRe.FindAllStringSubmatch(createSQL, -1) {
		if len(m) < 2 {
			continue
		}
		expr := strings.TrimSpace(m[1])
		if expr == "" {
			continue
		}
		checks = append(checks, migrate.CheckMeta{
			Name: defaultCheckName(table, expr),
			Expr: expr,
		})
	}
	return checks
}

func quoteSQLiteIdent(name string) string {
	name = strings.ReplaceAll(name, `"`, `""`)
	return `"` + name + `"`
}

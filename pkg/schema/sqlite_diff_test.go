package schema

import (
	"strings"
	"testing"

	"github.com/amr0ny/migrateme/pkg/migrate"
)

func TestSQLiteDiffCreateTable(t *testing.T) {
	t.Parallel()

	g := NewSQLiteDiffGenerator()
	newSchema := migrate.TableSchema{
		TableName: "events",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "integer", IsPK: true}},
			{ColumnName: "status", Attrs: migrate.ColumnAttributes{PgType: "text", Default: strPtrSQLite("'new'")}},
		},
		Checks: []migrate.CheckMeta{
			{Name: "chk_events_status", Expr: "status IN ('new','done')"},
		},
	}
	diff := g.DiffSchemas(migrate.TableSchema{}, newSchema)
	if len(diff.Up) == 0 {
		t.Fatalf("expected create table statements")
	}
	if !strings.Contains(diff.Up[0], `CREATE TABLE IF NOT EXISTS "events"`) {
		t.Fatalf("unexpected create statement: %s", diff.Up[0])
	}
}

func TestValidateSQLiteDiffSupportRejectsTypeChange(t *testing.T) {
	t.Parallel()

	old := migrate.TableSchema{
		TableName: "users",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "age", Attrs: migrate.ColumnAttributes{PgType: "integer"}},
		},
	}
	newSchema := migrate.TableSchema{
		TableName: "users",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "age", Attrs: migrate.ColumnAttributes{PgType: "text"}},
		},
	}
	if err := ValidateSQLiteDiffSupport(old, newSchema); err == nil {
		t.Fatalf("expected validation error for sqlite type change")
	}
}

func strPtrSQLite(v string) *string { return &v }

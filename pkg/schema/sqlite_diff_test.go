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
	planner := NewSQLitePlanner()
	diff := planner.DiffSchemas(old, newSchema)
	if len(diff.Up) == 0 {
		t.Fatalf("expected rebuild statements for sqlite type change")
	}
	if !strings.Contains(strings.Join(diff.Up, "\n"), `ALTER TABLE "__migrateme_tmp_users" RENAME TO "users"`) {
		t.Fatalf("expected temp-table rebuild plan, got: %v", diff.Up)
	}
	diags := planner.Diagnostics()
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics for rebuild plan")
	}
}

func TestValidateSQLiteCapabilitiesWarnsOnMappedTypes(t *testing.T) {
	t.Parallel()

	s := migrate.TableSchema{
		TableName: "events",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
			{ColumnName: "meta", Attrs: migrate.ColumnAttributes{PgType: "jsonb"}},
			{ColumnName: "active", Attrs: migrate.ColumnAttributes{PgType: "boolean"}},
		},
	}
	diags := ValidateSQLiteCapabilities(s)
	if len(diags) < 3 {
		t.Fatalf("expected mapped type diagnostics, got: %#v", diags)
	}
}

func strPtrSQLite(v string) *string { return &v }

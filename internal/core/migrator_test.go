package core

import (
	"testing"

	"github.com/amr0ny/migrateme/pkg/migrate"
)

func TestAnalyzeTableChange_PrefersAddColumnsForPureAdds(t *testing.T) {
	t.Parallel()

	m := &Migrator{}
	old := migrate.TableSchema{
		TableName: "users",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
		},
	}
	newSchema := migrate.TableSchema{
		TableName: "users",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
			{ColumnName: "email", Attrs: migrate.ColumnAttributes{PgType: "text"}},
		},
	}

	if got := m.analyzeTableChange(old, newSchema); got != AddColumns {
		t.Fatalf("analyzeTableChange = %q, want %q", got, AddColumns)
	}
}

func TestAnalyzeTableChange_RecognizesConstraintOnlyChanges(t *testing.T) {
	t.Parallel()

	m := &Migrator{}
	old := migrate.TableSchema{
		TableName: "users",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
			{ColumnName: "email", Attrs: migrate.ColumnAttributes{PgType: "text"}},
		},
	}
	newSchema := migrate.TableSchema{
		TableName: "users",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
			{ColumnName: "email", Attrs: migrate.ColumnAttributes{PgType: "text", Unique: true}},
		},
	}

	if got := m.analyzeTableChange(old, newSchema); got != AlterConstraints {
		t.Fatalf("analyzeTableChange = %q, want %q", got, AlterConstraints)
	}
}

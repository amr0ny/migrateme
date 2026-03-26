package schema

import (
	"strings"
	"testing"

	"github.com/amr0ny/migrateme/pkg/migrate"
)

func TestDiffSchemas_AddedColumnsAreDeterministic(t *testing.T) {
	t.Parallel()

	g := NewDiffGenerator()
	old := migrate.TableSchema{
		TableName: "demo",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
		},
	}
	newSchema := migrate.TableSchema{
		TableName: "demo",
		Columns: []migrate.ColumnMeta{
			{ColumnName: "id", Attrs: migrate.ColumnAttributes{PgType: "uuid"}},
			{ColumnName: "z_col", Attrs: migrate.ColumnAttributes{PgType: "text"}},
			{ColumnName: "a_col", Attrs: migrate.ColumnAttributes{PgType: "text"}},
		},
	}

	diff := g.DiffSchemas(old, newSchema)
	if len(diff.Up) < 2 {
		t.Fatalf("expected >=2 up statements, got %d", len(diff.Up))
	}

	joined := strings.Join(diff.Up, "\n")
	idxA := strings.Index(joined, `ADD COLUMN IF NOT EXISTS "a_col"`)
	idxZ := strings.Index(joined, `ADD COLUMN IF NOT EXISTS "z_col"`)
	if idxA == -1 || idxZ == -1 {
		t.Fatalf("expected add statements for a_col and z_col, got:\n%s", joined)
	}
	if idxA > idxZ {
		t.Fatalf("expected a_col statement before z_col for deterministic order, got:\n%s", joined)
	}
}

func TestDiffSchemas_FKActionOnlyChangeDoesNotProduceDiff(t *testing.T) {
	t.Parallel()

	g := NewDiffGenerator()
	old := migrate.TableSchema{
		TableName: "child",
		Columns: []migrate.ColumnMeta{
			{
				ColumnName: "parent_id",
				Attrs: migrate.ColumnAttributes{
					PgType: "uuid",
					ForeignKey: &migrate.ForeignKey{
						Table:    "parents",
						Column:   "id",
						OnDelete: migrate.NoAction,
						OnUpdate: migrate.NoAction,
					},
				},
			},
		},
	}
	newSchema := migrate.TableSchema{
		TableName: "child",
		Columns: []migrate.ColumnMeta{
			{
				ColumnName: "parent_id",
				Attrs: migrate.ColumnAttributes{
					PgType: "uuid",
					ForeignKey: &migrate.ForeignKey{
						Table:    "public.parents",
						Column:   `"id"`,
						OnDelete: migrate.Cascade,
						OnUpdate: migrate.Restrict,
					},
				},
			},
		},
	}

	diff := g.DiffSchemas(old, newSchema)
	if !diff.IsEmpty() {
		t.Fatalf("expected empty diff for FK action-only change, got up=%v down=%v", diff.Up, diff.Down)
	}
}

func TestAddConstraintIfNotExists_EscapesConstraintName(t *testing.T) {
	t.Parallel()

	stmt := addConstraintIfNotExists(`ALTER TABLE "t" ADD CONSTRAINT "x" CHECK (x > 0)`, `bad'name`)
	if !strings.Contains(stmt, `conname = 'bad''name'`) {
		t.Fatalf("expected escaped constraint name in SQL, got:\n%s", stmt)
	}
}

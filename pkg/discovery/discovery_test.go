package discovery

import (
	"go/ast"
	"testing"
)

func TestExtractIndexesComment_HandlesNestedFunctions(t *testing.T) {
	t.Parallel()

	doc := &ast.CommentGroup{
		List: []*ast.Comment{
			{Text: "// index: idx_solutions_change_window_at(GREATEST(updated_at, COALESCE(published_at, updated_at)))"},
		},
	}

	indexes := extractIndexesComment(doc)
	if len(indexes) != 1 {
		t.Fatalf("expected 1 index, got %d", len(indexes))
	}

	idx := indexes[0]
	if idx.Name != "idx_solutions_change_window_at" {
		t.Fatalf("unexpected name: %q", idx.Name)
	}
	if len(idx.Columns) != 1 {
		t.Fatalf("expected 1 column expression, got %d: %v", len(idx.Columns), idx.Columns)
	}
	want := "GREATEST(updated_at, COALESCE(published_at, updated_at))"
	if idx.Columns[0] != want {
		t.Fatalf("unexpected column: got %q, want %q", idx.Columns[0], want)
	}
}

func TestExtractIndexesComment_HandlesSimpleCompositeIndex(t *testing.T) {
	t.Parallel()

	doc := &ast.CommentGroup{
		List: []*ast.Comment{
			{Text: "// index: unique idx_name(col1, col2) where deleted_at IS NULL"},
		},
	}

	indexes := extractIndexesComment(doc)
	if len(indexes) != 1 {
		t.Fatalf("expected 1 index, got %d", len(indexes))
	}

	idx := indexes[0]
	if !idx.Unique {
		t.Fatal("expected unique index")
	}
	if idx.Name != "idx_name" {
		t.Fatalf("unexpected name: %q", idx.Name)
	}
	if len(idx.Columns) != 2 || idx.Columns[0] != "col1" || idx.Columns[1] != "col2" {
		t.Fatalf("unexpected columns: %v", idx.Columns)
	}
	if idx.Where == nil || *idx.Where != "deleted_at IS NULL" {
		t.Fatalf("unexpected where: %v", idx.Where)
	}
}

func TestExtractChecksComment_HandlesInnerParentheses(t *testing.T) {
	t.Parallel()

	doc := &ast.CommentGroup{
		List: []*ast.Comment{
			{Text: "// check: chk_resolution_state_check(resolution_state in ('registered', 'finalized'))"},
			{Text: "// check: chk_processing_state_check(processing_state in ('new','ready','processing','done','failed'))"},
		},
	}

	checks := extractChecksComment(doc)
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	if checks[0].Expr != "resolution_state in ('registered', 'finalized')" {
		t.Fatalf("unexpected first check expr: %q", checks[0].Expr)
	}
	if checks[1].Expr != "processing_state in ('new','ready','processing','done','failed')" {
		t.Fatalf("unexpected second check expr: %q", checks[1].Expr)
	}
}

package discovery

import (
	"go/ast"
	"testing"
)

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

package schema

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteFetcherFetchesCoreMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  org_id INTEGER NOT NULL,
  email TEXT NOT NULL UNIQUE,
  status TEXT DEFAULT 'new',
  CONSTRAINT chk_users_status CHECK (status IN ('new','active')),
  CONSTRAINT fk_users_org FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE ON UPDATE NO ACTION
)`); err != nil {
		t.Fatalf("seed schema failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX idx_users_status ON users(status)`); err != nil {
		t.Fatalf("seed schema failed: %v", err)
	}

	f := NewSQLiteFetcher(db)
	s, err := f.Fetch(ctx, "users")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(s.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(s.Columns))
	}
	foundFK := false
	for _, c := range s.Columns {
		if c.ColumnName == "org_id" && c.Attrs.ForeignKey != nil && c.Attrs.ForeignKey.Table == "orgs" {
			foundFK = true
		}
	}
	if !foundFK {
		t.Fatalf("expected org_id foreign key metadata")
	}
	if len(s.Checks) == 0 {
		t.Fatalf("expected at least one check constraint")
	}
	if !strings.Contains(strings.ToLower(s.Checks[0].Expr), "status") {
		t.Fatalf("unexpected check expression: %q", s.Checks[0].Expr)
	}
}

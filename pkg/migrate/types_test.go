package migrate

import "testing"

func TestNormalizePgTypeAliases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "INT4", want: "integer"},
		{in: "character varying(100)", want: "varchar(100)"},
		{in: "timestamp with time zone", want: "timestamptz"},
		{in: "float8", want: "double precision"},
		{in: "int4[]", want: "integer[]"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := normalizePgType(tc.in); got != tc.want {
				t.Fatalf("normalizePgType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeActionFormats(t *testing.T) {
	t.Parallel()

	if got := normalizeAction(OnActionType("no_action")); got != NoAction {
		t.Fatalf("normalizeAction(no_action) = %q, want %q", got, NoAction)
	}
	if got := normalizeAction(OnActionType("  set   null ")); got != SetNull {
		t.Fatalf("normalizeAction(set null) = %q, want %q", got, SetNull)
	}
}

func TestNormalizeSchemaNormalizesNestedMetadata(t *testing.T) {
	t.Parallel()

	where := " deleted_at IS NULL ; "
	s := TableSchema{
		TableName: "users",
		Columns: []ColumnMeta{
			{
				ColumnName: "company_id",
				Attrs: ColumnAttributes{
					PgType:  "INT4",
					Default: strPtr("'ABC'::varchar"),
					ForeignKey: &ForeignKey{
						Table:    "Public.Companies",
						Column:   "ID",
						OnDelete: OnActionType("no_action"),
						OnUpdate: OnActionType("restrict"),
					},
				},
			},
		},
		Indexes: []IndexMeta{
			{Name: "idx_users_company", Columns: []string{`"company_id"`}, Where: &where},
		},
		Checks: []CheckMeta{
			{Name: "chk_nonempty", Expr: " CHECK ((company_id > 0)); "},
		},
	}

	n := NormalizeSchema(s)
	col := n.Columns[0]

	if col.Attrs.PgType != "integer" {
		t.Fatalf("normalized type = %q, want integer", col.Attrs.PgType)
	}
	if col.Attrs.Default == nil || *col.Attrs.Default != "'abc'" {
		t.Fatalf("normalized default = %v, want 'abc'", col.Attrs.Default)
	}
	if col.Attrs.ForeignKey == nil {
		t.Fatalf("foreign key unexpectedly nil")
	}
	if col.Attrs.ForeignKey.Table != "public.companies" || col.Attrs.ForeignKey.Column != "id" {
		t.Fatalf("normalized fk ref = %s.%s, want public.companies.id", col.Attrs.ForeignKey.Table, col.Attrs.ForeignKey.Column)
	}
	if col.Attrs.ForeignKey.OnDelete != NoAction || col.Attrs.ForeignKey.OnUpdate != Restrict {
		t.Fatalf("normalized fk actions = (%s,%s), want (%s,%s)", col.Attrs.ForeignKey.OnDelete, col.Attrs.ForeignKey.OnUpdate, NoAction, Restrict)
	}
	if n.Indexes[0].Where == nil || *n.Indexes[0].Where != "deleted_at IS NULL" {
		t.Fatalf("normalized where = %v, want deleted_at IS NULL", n.Indexes[0].Where)
	}
	if n.Checks[0].Expr != "company_id > 0" {
		t.Fatalf("normalized check expr = %q, want company_id > 0", n.Checks[0].Expr)
	}
}

func strPtr(v string) *string { return &v }

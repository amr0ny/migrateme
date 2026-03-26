package migrate

import (
	"regexp"
	"strings"
)

type EntityInfo struct {
	StructName string
	TableName  string
	Package    string
	FilePath   string
	Fields     []FieldInfo
	Indexes    []IndexMeta
	Checks     []CheckMeta
}

type FieldInfo struct {
	FieldName  string
	ColumnName string
	Idx        int
	ForeignKey string
	RawTag     string
}

type TableSchema struct {
	TableName string
	Columns   []ColumnMeta
	Indexes   []IndexMeta
	Checks    []CheckMeta
}

type IndexMeta struct {
	// Name is optional in code comments; when missing, the migrator will generate
	// a deterministic name for CREATE statements.
	Name string

	// Columns must be in index order (composite indexes are ordered).
	Columns []string

	Unique bool
	Where  *string
}

type CheckMeta struct {
	// Name is optional; if omitted, migrator generates deterministic name.
	Name string
	Expr string
}

type ColumnMeta struct {
	FieldName  string
	ColumnName string
	Idx        int
	Attrs      ColumnAttributes
}

type OnActionType string

const (
	Cascade  OnActionType = "CASCADE"
	SetNull  OnActionType = "SET NULL"
	Restrict OnActionType = "RESTRICT"
	NoAction OnActionType = "NO ACTION"
)

type ForeignKey struct {
	Table    string
	Column   string
	OnDelete OnActionType
	OnUpdate OnActionType
}

type ColumnAttributes struct {
	PgType         string
	NotNull        bool
	Unique         bool
	IsPK           bool
	Default        *string
	ForeignKey     *ForeignKey
	ConstraintName *string
}

type TableDiff struct {
	Up   []string
	Down []string
}

func (d TableDiff) IsEmpty() bool {
	return len(d.Up) == 0 && len(d.Down) == 0
}

type SchemaRegistry map[string]func(string) TableSchema

func NormalizeSchema(s TableSchema) TableSchema {
	out := s

	for i, c := range out.Columns {
		c.Attrs.PgType = normalizePgType(c.Attrs.PgType)
		c.Attrs.Default = normalizeDefault(c.Attrs.Default)

		if fk := c.Attrs.ForeignKey; fk != nil {
			fk.Table = strings.ToLower(fk.Table)
			fk.Column = strings.ToLower(fk.Column)
			fk.OnDelete = normalizeAction(fk.OnDelete)
			fk.OnUpdate = normalizeAction(fk.OnUpdate)
		}

		out.Columns[i] = c
	}

	for i, idx := range out.Indexes {
		idx.Columns = normalizeIndexColumns(idx.Columns)
		idx.Where = normalizeWhere(idx.Where)
		out.Indexes[i] = idx
	}

	for i, chk := range out.Checks {
		chk.Expr = normalizeCheckExpr(chk.Expr)
		out.Checks[i] = chk
	}

	return out
}

func normalizeIndexColumns(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		c = strings.TrimSpace(c)
		c = strings.Trim(c, `"'`+"`")
		if c == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func normalizeWhere(where *string) *string {
	if where == nil {
		return nil
	}
	v := strings.TrimSpace(*where)
	v = strings.TrimSuffix(v, ";")
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func normalizeCheckExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimSuffix(expr, ";")
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, "CHECK")
	expr = strings.TrimPrefix(expr, "check")
	expr = strings.TrimSpace(expr)

	// Strip redundant outer parentheses, e.g. "((price > 0))" -> "price > 0".
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}

	return expr
}

func normalizePgType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	isArray := strings.HasSuffix(t, "[]")
	if isArray {
		t = strings.TrimSuffix(t, "[]")
	}
	t = strings.Join(strings.Fields(t), " ")
	switch t {
	case "int2", "smallint":
		t = "smallint"
	case "int4", "integer", "int":
		t = "integer"
	case "int8", "bigint":
		t = "bigint"
	case "bool", "boolean":
		t = "boolean"
	case "float4", "real":
		t = "real"
	case "float8", "double precision":
		t = "double precision"
	case "timestamp without time zone", "timestamp":
		t = "timestamp"
	case "timestamp with time zone", "timestamptz":
		t = "timestamptz"
	case "character varying", "varchar", "varchar()", "varchar(255)":
		t = "varchar"
	default:
		if m := varcharWithLenRe.FindStringSubmatch(t); len(m) == 2 {
			t = "varchar(" + m[1] + ")"
		} else if strings.HasPrefix(t, "character varying(") {
			t = strings.Replace(t, "character varying(", "varchar(", 1)
		} else if strings.HasPrefix(t, "timestamp with time zone") {
			t = strings.Replace(t, "timestamp with time zone", "timestamptz", 1)
		} else if strings.HasPrefix(t, "timestamp without time zone") {
			t = strings.Replace(t, "timestamp without time zone", "timestamp", 1)
		}
	}
	if isArray {
		return t + "[]"
	}
	return t
}

func normalizeAction(a OnActionType) OnActionType {
	s := strings.ToUpper(strings.TrimSpace(string(a)))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Join(strings.Fields(s), " ")
	a = OnActionType(s)
	if a == "" {
		return NoAction
	}

	return a
}

var varcharWithLenRe = regexp.MustCompile(`^character varying\((\d+)\)$`)

func normalizeDefault(d *string) *string {
	if d == nil {
		return nil
	}
	v := strings.TrimSpace(*d)
	v = strings.TrimSuffix(v, "::text")
	v = strings.TrimSuffix(v, "::varchar")
	v = strings.ToLower(v)
	return &v
}

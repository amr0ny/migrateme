package migrate

import (
	"strings"
)

type TableSchema struct {
	TableName string
	Columns   []ColumnMeta
}

// ColumnMeta метаданные колонки
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

// ForeignKey внешний ключ
type ForeignKey struct {
	Table    string
	Column   string
	OnDelete OnActionType
	OnUpdate OnActionType
}

// ColumnAttributes атрибуты колонки
type ColumnAttributes struct {
	PgType         string
	NotNull        bool
	Unique         bool
	IsPK           bool
	Default        *string
	ForeignKey     *ForeignKey
	ConstraintName *string
}

// TableDiff разница между схемами
type TableDiff struct {
	Up   []string
	Down []string
}

func (d TableDiff) IsEmpty() bool {
	return len(d.Up) == 0 && len(d.Down) == 0
}

// SchemaRegistry реестр схем таблиц
type SchemaRegistry map[string]func(string) TableSchema

// NormalizeSchema нормализует схему для сравнения
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

	return out
}

func normalizePgType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
	case "character varying", "varchar", "varchar()", "varchar(255)":
		return "varchar"
	}
	return t
}

func normalizeAction(a OnActionType) OnActionType {
	a = OnActionType(strings.ToUpper(strings.TrimSpace(string(a))))
	if a == "" {
		return NoAction
	}
	return a
}

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

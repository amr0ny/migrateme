package schema

import (
	"github.com/amr0ny/migrateme/pkg/migrate"
	"strings"
)

func collectPKs(s migrate.TableSchema) []string {
	var pk []string
	for _, c := range s.Columns {
		if c.Attrs.IsPK {
			pk = append(pk, c.ColumnName)
		}
	}
	return pk
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]struct{}{}
	for _, x := range a {
		am[x] = struct{}{}
	}
	for _, y := range b {
		if _, ok := am[y]; !ok {
			return false
		}
	}
	return true
}

func BuildSchema(e migrate.EntityInfo) migrate.TableSchema {
	schema := migrate.TableSchema{
		TableName: e.TableName,
		Columns:   make([]migrate.ColumnMeta, 0),
	}

	for _, f := range e.Fields {
		attrs := parseColumnTag(f.RawTag)

		schema.Columns = append(schema.Columns, migrate.ColumnMeta{
			FieldName:  f.FieldName,
			ColumnName: f.ColumnName,
			Idx:        f.Idx,
			Attrs:      attrs,
		})
	}

	return schema
}

func parseColumnTag(tag string) migrate.ColumnAttributes {
	attrs := migrate.ColumnAttributes{}

	raw := extractTag(tag, "db")
	if raw == "" || raw == "-" {
		return attrs
	}

	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return attrs
	}

	for _, p := range parts[1:] {
		switch {
		case p == "pk":
			attrs.IsPK = true
			attrs.NotNull = true
		case p == "notnull":
			attrs.NotNull = true
		case p == "unique":
			attrs.Unique = true

		case strings.HasPrefix(p, "type="):
			attrs.PgType = strings.TrimPrefix(p, "type=")

		case strings.HasPrefix(p, "default="):
			v := strings.TrimPrefix(p, "default=")
			attrs.Default = &v

		case strings.HasPrefix(p, "fk="):
			ref := strings.TrimPrefix(p, "fk=")
			parts := strings.Split(ref, ".")
			if len(parts) == 2 {
				attrs.ForeignKey = &migrate.ForeignKey{
					Table:  parts[0],
					Column: parts[1],
				}
			}

		case strings.HasPrefix(p, "delete="):
			if attrs.ForeignKey != nil {
				attrs.ForeignKey.OnDelete = migrate.OnActionType(strings.ToUpper(strings.TrimPrefix(p, "delete=")))
			}

		case strings.HasPrefix(p, "update="):
			if attrs.ForeignKey != nil {
				attrs.ForeignKey.OnUpdate = migrate.OnActionType(strings.ToUpper(strings.TrimPrefix(p, "update=")))
			}
		}
	}

	if attrs.PgType == "" {
		attrs.PgType = "text"
	}

	return attrs
}

func extractTag(tag, key string) string {
	needle := key + `:"`
	idx := strings.Index(tag, needle)
	if idx == -1 {
		return ""
	}

	rest := tag[idx+len(needle):]
	end := strings.Index(rest, `"`)
	if end == -1 {
		return ""
	}

	return rest[:end]
}

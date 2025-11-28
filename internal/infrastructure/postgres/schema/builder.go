package schema

import (
	"fmt"
	"hash/crc32"
	"io"
	"reflect"
	"strings"
	"sync"

	"github.com/amr0ny/migrateme/internal/domain"
)

var columnCache sync.Map

// BuildSchema строит схему таблицы на основе структуры
func BuildSchema[T domain.Migratable](table string) TableSchema {
	var t T
	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	schema := TableSchema{
		TableName: table,
		Columns:   []ColumnMeta{},
	}

	processFields(typ, "", &schema.Columns)
	return schema
}

// BuildSchemaFromRegistry строит схемы из реестра
func BuildSchemaFromRegistry(registry SchemaRegistry) map[string]TableSchema {
	schemas := make(map[string]TableSchema)
	for tableName, builder := range registry {
		schemas[tableName] = builder(tableName)
	}
	return schemas
}

// extractColumns извлекает колонки из структуры (для репозиториев)
func ExtractColumns[T any]() []ColumnMeta {
	typeName := reflect.TypeOf((*T)(nil)).Elem().String()
	checksum := checksumStruct[T]()

	cacheKey := fmt.Sprintf("%s:%d", typeName, checksum)

	if cached, ok := columnCache.Load(cacheKey); ok {
		return cached.([]ColumnMeta)
	}

	var t T
	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	cols := make([]ColumnMeta, 0)
	processFields(typ, "", &cols)

	columnCache.Store(cacheKey, cols)
	return cols
}

// processFields рекурсивно обрабатывает поля структуры
func processFields(t reflect.Type, prefix string, cols *[]ColumnMeta) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if field.Anonymous {
			processFields(field.Type, prefix, cols)
			continue
		}

		tag := field.Tag.Get("db")
		name, attrs := parseTag(tag, field.Type)
		if name == "" {
			continue
		}

		columnName := name
		if prefix != "" {
			columnName = prefix + "_" + name
		}

		*cols = append(*cols, ColumnMeta{
			FieldName:  field.Name,
			ColumnName: columnName,
			Idx:        i,
			Attrs:      attrs,
		})
	}
}

// checksumStruct создает контрольную сумму структуры для кеширования
func checksumStruct[T any]() uint32 {
	var t T
	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	h := crc32.NewIEEE()

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)

		io.WriteString(h, f.Name)
		io.WriteString(h, f.Type.String())
		io.WriteString(h, string(f.Tag))
	}

	return h.Sum32()
}

// inferPgType определяет PostgreSQL тип на основе Go типа
func inferPgType(fieldType reflect.Type) string {
	fullTypeName := fieldType.String()
	switch fullTypeName {
	case "time.Time":
		return "timestamptz"
	case "uuid.UUID":
		return "uuid"
	}

	switch fieldType.Kind() {
	case reflect.String:
		return "text"
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		return "integer"
	case reflect.Bool:
		return "boolean"
	case reflect.Float32, reflect.Float64:
		return "real"
	case reflect.Struct:
		return "jsonb"
	case reflect.Slice, reflect.Array:
		if fieldType.Elem().Kind() == reflect.Uint8 {
			return "bytea"
		}
		return "jsonb"
	case reflect.Ptr:
		return inferPgType(fieldType.Elem())
	}
	return "text"
}

// parseTag парсит тег db и возвращает имя колонки и атрибуты
func parseTag(tag string, fieldType reflect.Type) (string, ColumnAttributes) {
	attrs := ColumnAttributes{}

	parts := strings.Split(tag, ",")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "-" {
		return "", attrs
	}

	name := parts[0]

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
				attrs.ForeignKey = &ForeignKey{
					Table:  parts[0],
					Column: parts[1],
				}
			}
		case strings.HasPrefix(p, "delete="):
			if attrs.ForeignKey != nil {
				attrs.ForeignKey.OnDelete = OnActionType(strings.ToUpper(strings.TrimPrefix(p, "delete=")))
			}
		case strings.HasPrefix(p, "update="):
			if attrs.ForeignKey != nil {
				attrs.ForeignKey.OnUpdate = OnActionType(strings.ToUpper(strings.TrimPrefix(p, "update=")))
			}
		}
	}

	if attrs.PgType == "" {
		attrs.PgType = inferPgType(fieldType)
	}

	return name, attrs
}

// Вспомогательные функции для работы с PK
func collectPKs(s TableSchema) []string {
	pk := []string{}
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

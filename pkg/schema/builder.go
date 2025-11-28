package schema

import (
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"hash/crc32"
	"io"
	"reflect"
	"strings"
	"sync"
)

var columnCache sync.Map

// BuildSchema строит схему таблицы на основе структуры
func BuildSchema(table string, model interface{}) migrate.TableSchema {
	typ := reflect.TypeOf(model)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	schema := migrate.TableSchema{
		TableName: table,
		Columns:   []migrate.ColumnMeta{},
	}

	processFields(typ, "", &schema.Columns)
	return schema
}

// BuildSchemaFromRegistry строит схемы из реестра
func BuildSchemaFromRegistry(registry migrate.SchemaRegistry) map[string]migrate.TableSchema {
	schemas := make(map[string]migrate.TableSchema)
	for tableName, builder := range registry {
		schemas[tableName] = builder(tableName)
	}
	return schemas
}

// extractColumns извлекает колонки из структуры (для репозиториев)
func ExtractColumns(model interface{}) []migrate.ColumnMeta {
	typ := reflect.TypeOf(model)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	typeName := typ.String()
	checksum := checksumStruct(model)

	cacheKey := fmt.Sprintf("%s:%d", typeName, checksum)

	if cached, ok := columnCache.Load(cacheKey); ok {
		return cached.([]migrate.ColumnMeta)
	}

	cols := make([]migrate.ColumnMeta, 0)
	processFields(typ, "", &cols)

	columnCache.Store(cacheKey, cols)
	return cols
}

// processFields рекурсивно обрабатывает поля структуры
func processFields(t reflect.Type, prefix string, cols *[]migrate.ColumnMeta) {
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

		*cols = append(*cols, migrate.ColumnMeta{
			FieldName:  field.Name,
			ColumnName: columnName,
			Idx:        i,
			Attrs:      attrs,
		})
	}
}

// checksumStruct создает контрольную сумму структуры для кеширования
func checksumStruct(model interface{}) uint32 {
	typ := reflect.TypeOf(model)
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
func parseTag(tag string, fieldType reflect.Type) (string, migrate.ColumnAttributes) {
	attrs := migrate.ColumnAttributes{}

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
		attrs.PgType = inferPgType(fieldType)
	}

	return name, attrs
}

// Вспомогательные функции для работы с PK
func collectPKs(s migrate.TableSchema) []string {
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

// BuildSchemaFromEntity строит схему из структуры (упрощенная версия)
func BuildSchemaFromEntity(model interface{}, table string) migrate.TableSchema {
	if model == nil {
		return migrate.TableSchema{TableName: table}
	}

	typ := reflect.TypeOf(model)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	schema := migrate.TableSchema{
		TableName: table,
		Columns:   []migrate.ColumnMeta{},
	}

	processStructFields(typ, "", &schema.Columns)
	return schema
}

// processStructFields обрабатывает поля структуры
func processStructFields(t reflect.Type, prefix string, cols *[]migrate.ColumnMeta) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Обрабатываем анонимные поля (встраивание)
		if field.Anonymous {
			processStructFields(field.Type, prefix, cols)
			continue
		}

		tag := field.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}

		name, attrs := parseTag(tag, field.Type)
		if name == "" {
			continue
		}

		columnName := name
		if prefix != "" {
			columnName = prefix + "_" + name
		}

		*cols = append(*cols, migrate.ColumnMeta{
			FieldName:  field.Name,
			ColumnName: columnName,
			Attrs:      attrs,
		})
	}
}

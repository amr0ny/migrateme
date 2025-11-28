package discovery

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"
)

// EntityInfo информация о найденной сущности
type EntityInfo struct {
	StructName string
	TableName  string
	Package    string
	FilePath   string
	// TypeInfo содержит go/types представление типа (nil если не удалось загрузить)
	TypeInfo types.Type
	Fields   []FieldInfo
}

type FieldInfo struct {
	FieldName  string
	ColumnName string
	Idx        int
	// Foreign key in format table.column if present, empty otherwise
	ForeignKey string
	// RawTag original struct tag string
	RawTag string
}

// DiscoverEntities находит сущности в указанных путях (файлы или директории)
func DiscoverEntities(paths []string) ([]EntityInfo, error) {
	var all []EntityInfo
	seenTables := map[string]struct{}{}

	for _, p := range paths {
		e, err := discoverInPath(p)
		if err != nil {
			return nil, fmt.Errorf("discover in %s: %w", p, err)
		}
		for _, ent := range e {
			tnLower := strings.ToLower(ent.TableName)
			if tnLower == "" {
				continue
			}
			if _, ok := seenTables[tnLower]; ok {
				// warn + пропускаем (берём первый)
				fmt.Printf("Warning: duplicate table name detected '%s' in %s — skipping\n", ent.TableName, ent.FilePath)
				continue
			}
			seenTables[tnLower] = struct{}{}
			all = append(all, ent)
		}
	}

	// Попытка загрузить type-информацию пакетами по найденным сущностям
	// группируем по пакету (package name + dir)
	if len(all) > 0 {
		if err := resolveTypesForEntities(all); err != nil {
			// не критично — логируем, но возвращаем найденные сущности
			fmt.Printf("Warning: failed to resolve types for some entities: %v\n", err)
		}
	}

	return all, nil
}

func discoverInPath(path string) ([]EntityInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return discoverInDirectory(path)
	}
	return discoverInFile(path)
}

func discoverInDirectory(dir string) ([]EntityInfo, error) {
	var entities []EntityInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// пропускаем скрытые и vendor
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		ents, err := discoverInFile(path)
		if err != nil {
			fmt.Printf("Warning: failed to parse %s: %v\n", path, err)
			return nil
		}
		entities = append(entities, ents...)
		return nil
	})
	return entities, err
}

var tableNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)table\s*:\s*"([^"]+)"`), // table: "users"
	regexp.MustCompile(`(?i)table\s*:\s*'([^']+)'`),
	regexp.MustCompile(`(?i)table\s*:\s*([^\s]+)`),      // table: users
	regexp.MustCompile(`(?i)tableName\s*:\s*"([^"]+)"`), // tableName:"users"
	regexp.MustCompile(`(?i)tableName\s*:\s*'([^']+)'`),
	regexp.MustCompile(`(?i)\+table\s*:\s*([^\s]+)`),    // +table: users
	regexp.MustCompile(`(?i)migratable\s*:\s*([^\s]+)`), // migratable: users
}

// extractTableNameFromComment извлекает имя таблицы из комментария/DocGroup
func extractTableNameFromComment(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	for _, c := range doc.List {
		// убираем префиксы // /* */
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "/*"))
		text = strings.TrimSpace(strings.TrimSuffix(text, "*/"))
		for _, re := range tableNamePatterns {
			if m := re.FindStringSubmatch(text); len(m) == 2 {
				return strings.Trim(m[1], `"'`)
			}
		}
	}
	return ""
}

// discoverInFile парсит файл и извлекает структуры с table-метками
func discoverInFile(filePath string) ([]EntityInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var entities []EntityInfo
	pkgName := node.Name.Name

	ast.Inspect(node, func(n ast.Node) bool {
		gen, ok := n.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			return true
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// сначала пробуем получить имя таблицы из комментария типа (GenDecl.Doc)
			tableName := extractTableNameFromComment(gen.Doc)
			// если нет — пробуем комментарий прямо над TypeSpec (ts.Doc)
			if tableName == "" {
				tableName = extractTableNameFromComment(ts.Doc)
			}
			// Если нет — пробуем doc комментарии у самого типа (ast.CommentGroup у Struct? уже покрыто выше)
			// fallback: проверим комментарии внутри полей (редко, но)
			if tableName == "" {
				// также поддерживаем запись в виде: // tableName:"contacts" в той же строке, где struct name
				if ts.Comment != nil {
					tableName = extractTableNameFromComment(ts.Comment)
				}
			}

			if tableName == "" {
				// нет маркера — пропускаем
				continue
			}

			ent := EntityInfo{
				StructName: ts.Name.Name,
				TableName:  tableName,
				Package:    pkgName,
				FilePath:   filePath,
			}

			// собираем поля и парсим db-теги
			for i, field := range st.Fields.List {
				// поле может иметь несколько имён (a, b int)
				names := field.Names
				// если нет имён — embedded (мы не поддерживаем embedded как сущности, но поля могут быть интересны)
				var fieldName string
				if len(names) == 0 {
					// embedded: возьмём имя по типу (примерно)
					switch expr := field.Type.(type) {
					case *ast.Ident:
						fieldName = expr.Name
					case *ast.SelectorExpr:
						// pkg.Type
						if id, ok := expr.X.(*ast.Ident); ok {
							fieldName = id.Name + "." + expr.Sel.Name
						} else {
							fieldName = expr.Sel.Name
						}
					default:
						fieldName = "embedded"
					}
				} else {
					fieldName = names[0].Name
				}

				columnName := ""
				fk := ""
				rawTag := ""
				if field.Tag != nil {
					// field.Tag.Value содержит строку с обратными кавычкам, напр. "`db:\"phone\"`"
					tagValue := strings.Trim(field.Tag.Value, "`")
					rawTag = tagValue
					// парсим db:"..."
					// простая логика: ищем db:"...". Если есть, берём до запятой как имя колонки. Также парсим fk=...
					// Позволяем формат: db:"phone" или db:"phone,fk=partners.id" или db:"-,fk=..."
					// На случай нескольких тегов: используем reflect.StructTag semantics
					// но здесь простая парсинга:
					// Найдём db:"...".
					if m := regexp.MustCompile(`db\s*:\s*"([^"]*)"`).FindStringSubmatch(tagValue); len(m) == 2 {
						dbInner := m[1]
						parts := strings.Split(dbInner, ",")
						if len(parts) > 0 {
							// первый элемент может быть имя колонки или -
							if parts[0] != "-" && parts[0] != "" {
								columnName = parts[0]
							}
						}
						// ищем fk=...
						for _, p := range parts {
							p = strings.TrimSpace(p)
							if strings.HasPrefix(p, "fk=") {
								fk = strings.TrimPrefix(p, "fk=")
								// fk может быть partners.id или partners (в этом случае колонка — id)
								fk = strings.Trim(fk, `"`)
							}
						}
					} else {
						// также поддерживаем синтаксис db:"phone" без кавычек (маловероятно) — игнорируем
					}
				}

				// если имя колонки не указанo в тэге, можно использовать snake_case версии поля как fallback,
				// но по твоему комментарию fallback не нужен — оставим пустым (где нужно — SchemaBuilder будет решать)
				fi := FieldInfo{
					FieldName:  fieldName,
					ColumnName: columnName,
					Idx:        i,
					ForeignKey: fk,
					RawTag:     rawTag,
				}
				ent.Fields = append(ent.Fields, fi)
			}

			entities = append(entities, ent)
		}
		return true
	})

	return entities, nil
}

// resolveTypesForEntities пытается загрузить информацию о типах через go/packages
// и сопоставить найденные структуры с их types.Type
func resolveTypesForEntities(entities []EntityInfo) error {
	// собираем уникальные директории файлов
	dirSet := map[string]struct{}{}
	for _, e := range entities {
		dir := filepath.Dir(e.FilePath)
		dirSet[dir] = struct{}{}
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}

	if len(dirs) == 0 {
		return nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		// Use current module context (observed from environment)
	}

	pkgs, err := packages.Load(cfg, dirs...)
	if err != nil {
		return err
	}

	// build index: package Dir -> *packages.Package
	byDir := map[string]*packages.Package{}
	for _, pk := range pkgs {
		for _, f := range pk.GoFiles {
			dir := filepath.Dir(f)
			byDir[dir] = pk
		}
	}

	// сопоставляем по file path + struct name
	for i := range entities {
		dir := filepath.Dir(entities[i].FilePath)
		pk, ok := byDir[dir]
		if !ok {
			// пакет не загружен — оставляем nil
			continue
		}

		// в packages.Package.Types.Scope() ищем объект по имени типа
		obj := pk.Types.Scope().Lookup(entities[i].StructName)
		if obj == nil {
			// возможно тип определён в другом файле того же пакета (packages.Load покрывает все GoFiles), но Lookup должен находить
			continue
		}
		typ := obj.Type()
		entities[i].TypeInfo = typ
	}

	return nil
}

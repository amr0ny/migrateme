package discovery

import (
	"fmt"
	"github.com/amr0ny/migrateme/pkg/migrate"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func DiscoverEntities(paths []string) ([]migrate.EntityInfo, error) {
	var all []migrate.EntityInfo
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

				fmt.Printf("Warning: duplicate table name detected '%s' in %s — skipping\n", ent.TableName, ent.FilePath)
				continue
			}
			seenTables[tnLower] = struct{}{}
			all = append(all, ent)
		}
	}

	return all, nil
}

func discoverInPath(path string) ([]migrate.EntityInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return discoverInDirectory(path)
	}
	return discoverInFile(path)
}

func discoverInDirectory(dir string) ([]migrate.EntityInfo, error) {
	var entities []migrate.EntityInfo
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {

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
	regexp.MustCompile(`(?i)table\s*:\s*"([^"]+)"`),
	regexp.MustCompile(`(?i)table\s*:\s*'([^']+)'`),
	regexp.MustCompile(`(?i)table\s*:\s*([^\s]+)`),
	regexp.MustCompile(`(?i)tableName\s*:\s*"([^"]+)"`),
	regexp.MustCompile(`(?i)tableName\s*:\s*'([^']+)'`),
	regexp.MustCompile(`(?i)\+table\s*:\s*([^\s]+)`),
	regexp.MustCompile(`(?i)migratable\s*:\s*([^\s]+)`),
}

func extractTableNameFromComment(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	for _, c := range doc.List {

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

func discoverInFile(filePath string) ([]migrate.EntityInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var entities []migrate.EntityInfo
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

			tableName := extractTableNameFromComment(gen.Doc)

			if tableName == "" {
				tableName = extractTableNameFromComment(ts.Doc)
			}

			if tableName == "" {

				if ts.Comment != nil {
					tableName = extractTableNameFromComment(ts.Comment)
				}
			}

			if tableName == "" {
				continue
			}

			ent := migrate.EntityInfo{
				StructName: ts.Name.Name,
				TableName:  tableName,
				Package:    pkgName,
				FilePath:   filePath,
			}

			for i, field := range st.Fields.List {

				names := field.Names
				var fieldName string
				if len(names) == 0 {

					switch expr := field.Type.(type) {
					case *ast.Ident:
						fieldName = expr.Name
					case *ast.SelectorExpr:

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
					tagValue := strings.Trim(field.Tag.Value, "`")
					rawTag = tagValue
					if m := regexp.MustCompile(`db\s*:\s*"([^"]*)"`).FindStringSubmatch(tagValue); len(m) == 2 {
						dbInner := m[1]
						parts := strings.Split(dbInner, ",")
						if len(parts) > 0 {
							if parts[0] != "-" && parts[0] != "" {
								columnName = parts[0]
							}
						}
						for _, p := range parts {
							p = strings.TrimSpace(p)
							if strings.HasPrefix(p, "fk=") {
								fk = strings.TrimPrefix(p, "fk=")
								fk = strings.Trim(fk, `"`)
							}
						}
					} else {
					}
				}

				fi := migrate.FieldInfo{
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

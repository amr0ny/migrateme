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
				fmt.Printf("Warning: duplicate table name '%s' in %s — skipping\n", ent.TableName, ent.FilePath)
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

// ------------------------- RECURSIVE FIELD EXTRACTOR -------------------------

func expandStructFields(st *ast.StructType, file *ast.File, pkg string, filePath string, visited map[string]bool) []migrate.FieldInfo {
	var fields []migrate.FieldInfo

	for _, field := range st.Fields.List {
		// Field name
		var name string
		if len(field.Names) > 0 {
			name = field.Names[0].Name
		}

		// Try detect struct type and recurse
		switch t := field.Type.(type) {

		case *ast.StructType:
			// Inline struct
			if visited["inline"] {
				continue
			}
			visited["inline"] = true
			fields = append(fields, expandStructFields(t, file, pkg, filePath, visited)...)

		case *ast.Ident:
			// Struct defined in same file
			if obj := t.Obj; obj != nil {
				if st2, ok := obj.Decl.(*ast.TypeSpec); ok {
					if inner, ok2 := st2.Type.(*ast.StructType); ok2 {
						key := pkg + "." + st2.Name.Name
						if visited[key] {
							continue
						}
						visited[key] = true
						fields = append(fields, expandStructFields(inner, file, pkg, filePath, visited)...)
						continue
					}
				}
			}

		case *ast.SelectorExpr:
			// Imported type — skip or handle if needed
		}

		// Extract db tag if exists
		dbTag := ""
		fk := ""
		rawTag := ""
		if field.Tag != nil {
			raw := strings.Trim(field.Tag.Value, "`")
			rawTag = raw
			if m := regexp.MustCompile(`db\s*:\s*"([^"]*)"`).FindStringSubmatch(raw); len(m) == 2 {
				dbTag = strings.Split(m[1], ",")[0]
			}
			if m := regexp.MustCompile(`fk=([^,"]+)`).FindStringSubmatch(raw); len(m) == 2 {
				fk = m[1]
			}
		}

		// Skip fields without db-tag
		if dbTag == "" {
			continue
		}

		fields = append(fields, migrate.FieldInfo{
			FieldName:  name,
			ColumnName: dbTag,
			Idx:        len(fields),
			ForeignKey: fk,
			RawTag:     rawTag,
		})
	}

	return fields
}

// ------------------------- MAIN FILE PARSER -------------------------

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

			// Get table name
			tableName := extractTableNameFromComment(gen.Doc)
			if tableName == "" {
				tableName = extractTableNameFromComment(ts.Doc)
			}
			if tableName == "" && ts.Comment != nil {
				tableName = extractTableNameFromComment(ts.Comment)
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

			// NEW: recursive field expansion
			visited := map[string]bool{}
			ent.Fields = expandStructFields(st, node, pkgName, filePath, visited)

			entities = append(entities, ent)
		}
		return true
	})

	return entities, nil
}

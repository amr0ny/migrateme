// ==========================================
// discovery.go (исправленная версия)
// ==========================================

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

// DiscoverEntities finds all table-annotated structs
func DiscoverEntities(ctx *DiscoverContext, paths []string) ([]migrate.EntityInfo, error) {
	var out []migrate.EntityInfo
	seenTables := map[string]struct{}{}

	for _, p := range paths {
		abs, _ := filepath.Abs(p)
		ents, err := discoverInPath(ctx, abs)
		if err != nil {
			return nil, err
		}

		for _, e := range ents {
			t := strings.ToLower(e.TableName)
			if _, exists := seenTables[t]; exists {
				fmt.Printf("Warning: duplicate table '%s' in %s — skipping\n", e.TableName, e.FilePath)
				continue
			}
			seenTables[t] = struct{}{}
			out = append(out, e)
		}
	}

	return out, nil
}

func discoverInPath(ctx *DiscoverContext, path string) ([]migrate.EntityInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return discoverInDirectory(ctx, path)
	}

	return discoverInFile(ctx, path)
}

func discoverInDirectory(ctx *DiscoverContext, dir string) ([]migrate.EntityInfo, error) {
	var entities []migrate.EntityInfo

	filepath.Walk(dir, func(path string, info os.FileInfo, _ error) error {
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		ents, err := discoverInFile(ctx, path)
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			return nil
		}
		entities = append(entities, ents...)
		return nil
	})

	return entities, nil
}

// comment parsing
var tableNamePatterns = []string{
	`table\s*:\s*"([^"]+)"`,
	`tableName\s*:\s*"([^"]+)"`,
}

func extractTableComment(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	text := doc.Text()
	for _, p := range tableNamePatterns {
		re := regexp.MustCompile(`(?i)` + p)
		m := re.FindStringSubmatch(text)
		if len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// main file-level discovery - ИСПРАВЛЕННАЯ ВЕРСИЯ
func discoverInFile(ctx *DiscoverContext, filePath string) ([]migrate.EntityInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var results []migrate.EntityInfo
	pkgPath := filepath.Dir(filePath)

	// Проходим по всем декларациям в файле
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}

		// Обрабатываем все спецификации в одной декларации
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// Пытаемся извлечь имя таблицы из комментариев
			tn := extractTableComment(gen.Doc)
			if tn == "" && ts.Doc != nil {
				tn = extractTableComment(ts.Doc)
			}
			if tn == "" {
				continue // пропускаем структуры без аннотации таблицы
			}

			// Composite index directives are stored in struct-level comments.
			indexes := make([]migrate.IndexMeta, 0)
			indexes = append(indexes, extractIndexesComment(gen.Doc)...)
			indexes = append(indexes, extractIndexesComment(ts.Doc)...)

			checks := make([]migrate.CheckMeta, 0)
			checks = append(checks, extractChecksComment(gen.Doc)...)
			checks = append(checks, extractChecksComment(ts.Doc)...)

			// Создаем информацию о сущности
			ent := migrate.EntityInfo{
				StructName: ts.Name.Name,
				TableName:  tn,
				Package:    pkgPath,
				FilePath:   filePath,
				Indexes:    indexes,
				Checks:     checks,
			}

			// Расширяем поля (включая встроенные структуры)
			ent.Fields = ExpandFields(ctx, pkgPath, st, file, map[string]bool{})
			results = append(results, ent)
		}
	}

	return results, nil
}

// Supported syntax (struct-level comments):
//
//	index: idx_name(col1, col2)
//	index: unique idx_name(col1, col2)
//	index: idx_name(col1) where deleted_at IS NULL
//	index: (col1, col2)  // name optional; migrator will handle name later
var indexDirectiveRE = regexp.MustCompile(`(?mi)index\s*:\s*(unique\s+)?(?:([A-Za-z0-9_\-]+)\s*)?\(([^)]*)\)\s*(?:where\s+([^\n]+))?`)

func extractIndexesComment(doc *ast.CommentGroup) []migrate.IndexMeta {
	if doc == nil {
		return nil
	}

	text := doc.Text()
	if strings.TrimSpace(text) == "" {
		return nil
	}

	matches := indexDirectiveRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	out := make([]migrate.IndexMeta, 0, len(matches))
	for _, m := range matches {
		// m[1] = unique (optional)
		// m[2] = index name (optional)
		// m[3] = columns inside parentheses
		// m[4] = where predicate (optional)

		if len(m) < 5 {
			continue
		}

		unique := strings.TrimSpace(m[1]) != ""
		name := strings.TrimSpace(m[2])
		colsRaw := m[3]
		whereRaw := strings.TrimSpace(m[4])

		var cols []string
		for _, c := range strings.Split(colsRaw, ",") {
			c = strings.TrimSpace(c)
			c = strings.Trim(c, `"'`+"`")
			if c != "" {
				cols = append(cols, c)
			}
		}

		if len(cols) == 0 {
			continue
		}

		var where *string
		if whereRaw != "" {
			where = &whereRaw
		}

		out = append(out, migrate.IndexMeta{
			Name:    name,
			Columns: cols,
			Unique:  unique,
			Where:   where,
		})
	}

	return out
}

// Supported syntax (struct-level comments):
//
//	check: chk_name(expr)
//	check: (expr)    // name optional; migrator will handle name later
var checkDirectiveRE = regexp.MustCompile(`(?mi)check\s*:\s*(?:([A-Za-z0-9_\-]+)\s*)?\(([^)]*)\)`)

func extractChecksComment(doc *ast.CommentGroup) []migrate.CheckMeta {
	if doc == nil {
		return nil
	}

	text := doc.Text()
	if strings.TrimSpace(text) == "" {
		return nil
	}

	matches := checkDirectiveRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	out := make([]migrate.CheckMeta, 0, len(matches))
	for _, m := range matches {
		// m[1] = name (optional)
		// m[2] = expr
		if len(m) < 3 {
			continue
		}
		name := strings.TrimSpace(m[1])
		expr := strings.TrimSpace(m[2])
		expr = strings.TrimSuffix(expr, ";")
		expr = strings.TrimSpace(expr)
		if expr == "" {
			continue
		}
		out = append(out, migrate.CheckMeta{
			Name: name,
			Expr: expr,
		})
	}

	return out
}

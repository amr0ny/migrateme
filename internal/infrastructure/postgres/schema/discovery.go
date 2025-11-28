package schema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type DiscoveredEntity struct {
	StructName string
	TableName  string
	File       string
}

func DiscoverEntities(files []string) ([]DiscoveredEntity, error) {
	var out []DiscoveredEntity

	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}

			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				// 1. check embedding BaseMigratable
				if isEmbeddingBaseMigratable(st) {
					out = append(out, DiscoveredEntity{
						StructName: ts.Name.Name,
						TableName:  toSnake(ts.Name.Name),
						File:       file,
					})
					continue
				}

				// 2. check comment-based annotation
				if hasMigratableComment(gen.Doc) {
					out = append(out, DiscoveredEntity{
						StructName: ts.Name.Name,
						TableName:  parseCommentTable(gen.Doc, ts.Name.Name),
						File:       file,
					})
				}
			}
		}
	}
	return out, nil
}

func isEmbeddingBaseMigratable(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		sel, ok := field.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == "domain" && sel.Sel.Name == "BaseMigratable" {
			return true
		}
	}
	return false
}

func hasMigratableComment(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.Contains(c.Text, "@migratable") {
			return true
		}
	}
	return false
}

func parseCommentTable(doc *ast.CommentGroup, defaultName string) string {
	if doc == nil {
		return toSnake(defaultName)
	}
	for _, c := range doc.List {
		if strings.Contains(c.Text, "table=") {
			parts := strings.Split(c.Text, "table=")
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return toSnake(defaultName)
}

func toSnake(s string) string {
	if s == "" {
		return ""
	}

	length := len(s)
	buf := make([]byte, 0, length*2)

	for i := 0; i < length; i++ {
		c := s[i]

		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				buf = append(buf, '_')
			}
			buf = append(buf, c+32)
		} else {
			buf = append(buf, c)
		}
	}

	return string(buf)
}

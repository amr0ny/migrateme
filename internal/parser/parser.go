package parser

import (
	"github.com/amr0ny/migrateme/internal/domain"
	"go/ast"
	"go/parser"
	"go/token"
)

func DiscoverEntities(files []string) ([]domain.EntityMetaInfo, error) {
	var result []domain.EntityMetaInfo

	fset := token.NewFileSet()

	for _, path := range files {
		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		pkg := node.Name.Name

		for _, decl := range node.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				_, ok = ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				// нашли struct, но теперь нужно проверить реализует ли Migratable

				implementsMigratable := false

				// ищем метод TableName()
				for _, d2 := range node.Decls {
					fn, ok := d2.(*ast.FuncDecl)
					if !ok || fn.Recv == nil {
						continue
					}

					if len(fn.Recv.List) == 0 {
						continue
					}

					star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
					if !ok {
						continue
					}

					ident, ok := star.X.(*ast.Ident)
					if !ok || ident.Name != ts.Name.Name {
						continue
					}

					if fn.Name.Name == "TableName" {
						implementsMigratable = true
						break
					}
				}

				if implementsMigratable {
					result = append(result, domain.EntityMetaInfo{
						StructName: ts.Name.Name,
						Package:    pkg,
					})
				}
			}
		}
	}

	return result, nil
}

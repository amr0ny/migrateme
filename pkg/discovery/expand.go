package discovery

import (
	"github.com/amr0ny/migrateme/pkg/migrate"
	"go/ast"
	"regexp"
	"strings"
)

// ExpandFields recursively resolves embedded structs, including other packages
func ExpandFields(
	ctx *DiscoverContext,
	pkgPath string,
	st *ast.StructType,
	file *ast.File,
	visited map[string]bool,
) []migrate.FieldInfo {

	var out []migrate.FieldInfo

	for _, field := range st.Fields.List {
		tagText := ""
		if field.Tag != nil {
			tagText = strings.Trim(field.Tag.Value, "`")
		}

		column := parseDBTag(tagText)

		// embedded struct?
		switch t := field.Type.(type) {

		case *ast.Ident:
			if next := ctx.Packages[pkgPath].Structs[t.Name]; next != nil {
				key := pkgPath + "." + t.Name
				if visited[key] {
					continue
				}
				visited[key] = true
				out = append(out, ExpandFields(ctx, pkgPath, next, file, visited)...)
				continue
			}

		case *ast.SelectorExpr:
			pkgAlias := t.X.(*ast.Ident).Name
			importPath := resolveImportPath(file, pkgAlias)
			if importPath == "" {
				break
			}

			if pkg := ctx.Packages[importPath]; pkg != nil {
				if next := pkg.Structs[t.Sel.Name]; next != nil {
					key := importPath + "." + t.Sel.Name
					if visited[key] {
						continue
					}
					visited[key] = true
					out = append(out, ExpandFields(ctx, importPath, next, file, visited)...)
					continue
				}
			}
		}

		if column != "" && len(field.Names) > 0 {
			out = append(out, migrate.FieldInfo{
				FieldName:  field.Names[0].Name,
				ColumnName: column,
				Idx:        len(out),
				RawTag:     tagText,
			})
		}
	}

	return out
}

func resolveImportPath(file *ast.File, alias string) string {
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")

		// explicit alias
		if imp.Name != nil && imp.Name.Name == alias {
			return path
		}

		// default alias = last segment
		parts := strings.Split(path, "/")
		if parts[len(parts)-1] == alias {
			return path
		}
	}
	return ""
}

func parseDBTag(tag string) string {
	re := regexp.MustCompile(`db:"([^"]*)"`)
	m := re.FindStringSubmatch(tag)
	if len(m) != 2 {
		return ""
	}
	col := strings.Split(m[1], ",")[0]
	if col == "-" {
		return ""
	}
	return col
}

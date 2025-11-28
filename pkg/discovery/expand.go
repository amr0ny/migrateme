package discovery

import (
	"github.com/amr0ny/migrateme/pkg/migrate"
	"go/ast"
	"path/filepath"
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

		// If field has column tag — collect it later
		column := parseDBTag(tagText)

		// If field is anonymous (embedded)
		isEmbedded := len(field.Names) == 0

		switch t := field.Type.(type) {

		// ========== IDENT:   type MyStruct struct { BaseEntity } ==========
		case *ast.Ident:
			// anonymous embedded struct
			if isEmbedded {
				if next := ctx.Packages[pkgPath].Structs[t.Name]; next != nil {
					key := pkgPath + "." + t.Name
					if visited[key] {
						break
					}
					visited[key] = true
					out = append(out,
						ExpandFields(ctx, pkgPath, next, file, visited)...,
					)
					continue
				}
			}

		// ========== SELECTOR:   domain.BaseEntity ==========
		case *ast.SelectorExpr:
			if isEmbedded {
				alias := t.X.(*ast.Ident).Name
				importPath := resolveImportPath(file, alias, ctx.ModulePath, ctx.ModuleRoot)
				if importPath != "" {
					if pkg := ctx.Packages[importPath]; pkg != nil {
						if next := pkg.Structs[t.Sel.Name]; next != nil {
							key := importPath + "." + t.Sel.Name
							if visited[key] {
								break
							}
							visited[key] = true
							out = append(out,
								ExpandFields(ctx, importPath, next, file, visited)...,
							)
							continue
						}
					}
				}
			}

		// ========== ANONYMOUS STRUCT:   struct { X int; Y string } ==========
		case *ast.StructType:
			if isEmbedded {
				// treat inline anonymous struct as embedded
				out = append(out,
					ExpandFields(ctx, pkgPath, t, file, visited)...,
				)
				continue
			}
		}

		// ========== REGULAR FIELD WITH COLUMN ==========
		if !isEmbedded && column != "" && len(field.Names) > 0 {
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

func resolveImportPath(file *ast.File, alias string, modulePrefix, moduleRoot string) string {
	for _, imp := range file.Imports {
		modPath := strings.Trim(imp.Path.Value, "\"")

		if imp.Name != nil && imp.Name.Name == alias {
			return patch(modPath, modulePrefix, moduleRoot)
		}

		parts := strings.Split(modPath, "/")
		if parts[len(parts)-1] == alias {
			return patch(modPath, modulePrefix, moduleRoot)
		}
	}
	return ""
}

func patch(importPath, modulePrefix, moduleRoot string) string {
	if !strings.HasPrefix(importPath, modulePrefix) {
		return importPath
	}

	rel := strings.TrimPrefix(importPath, modulePrefix)
	rel = strings.TrimPrefix(rel, "/")

	return filepath.Join(moduleRoot, rel)
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

package discovery

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type PackageInfo struct {
	Path    string                     // package import path
	Structs map[string]*ast.StructType // all struct definitions in the package
	Files   []*ast.File
}

type DiscoverContext struct {
	Packages map[string]*PackageInfo // key = import path
}

func LoadPackages(root string) (*DiscoverContext, error) {
	ctx := &DiscoverContext{
		Packages: map[string]*PackageInfo{},
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, _ error) error {
		if info.IsDir() {
			// skip vendor, hidden, build dirs
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		pkgPath := filepath.Dir(path)
		pkg := ctx.Packages[pkgPath]
		if pkg == nil {
			pkg = &PackageInfo{
				Path:    pkgPath,
				Structs: map[string]*ast.StructType{},
			}
			ctx.Packages[pkgPath] = pkg
		}

		pkg.Files = append(pkg.Files, f)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// collect struct types
	for _, pkg := range ctx.Packages {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if st, ok := ts.Type.(*ast.StructType); ok {
						pkg.Structs[ts.Name.Name] = st
					}
				}
			}
		}
	}

	return ctx, nil
}

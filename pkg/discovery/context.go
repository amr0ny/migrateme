package discovery

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type PackageInfo struct {
	Path    string                     // absolute filesystem path
	Structs map[string]*ast.StructType // struct definitions
	Files   []*ast.File
}

type DiscoverContext struct {
	Packages   map[string]*PackageInfo
	ModuleRoot string // absolute path: /Users/.../migrateme
	ModulePath string // module path:  github.com/amr0ny/migrateme
}

//
// MAIN ENTRY
//

func LoadPackages() (*DiscoverContext, error) {
	root, err := findModuleRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to find module root: %w", err)
	}

	modulePath, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("failed to read module path: %w", err)
	}

	ctx := &DiscoverContext{
		Packages:   map[string]*PackageInfo{},
		ModuleRoot: root,
		ModulePath: modulePath,
	}

	//
	// Walk through all *.go files inside module root
	//
	err = filepath.Walk(root, func(path string, info os.FileInfo, _ error) error {
		if info.IsDir() {
			// skip vendor, hidden, build dirs
			if strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// skip test files & non-go files
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		absDir := filepath.Dir(path)

		// Create package if not exists
		pkg := ctx.Packages[absDir]
		if pkg == nil {
			pkg = &PackageInfo{
				Path:    absDir,
				Structs: map[string]*ast.StructType{},
			}
			ctx.Packages[absDir] = pkg

			// add SECOND key: module import path
			rel := strings.TrimPrefix(absDir, ctx.ModuleRoot)
			rel = strings.TrimPrefix(rel, string(filepath.Separator))

			modImportPath := filepath.ToSlash(filepath.Join(ctx.ModulePath, rel))

			ctx.Packages[modImportPath] = pkg
		}

		pkg.Files = append(pkg.Files, f)
		return nil
	})

	if err != nil {
		return nil, err
	}

	//
	// Collect struct definitions for each package
	//
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

//
// HELPERS
//

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func readModulePath(goModPath string) (string, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}

	return "", fmt.Errorf("module path not found in go.mod")
}

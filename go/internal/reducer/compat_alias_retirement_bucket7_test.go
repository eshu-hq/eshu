// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBucket7DomainRetired pins the #6626 batch 6 bucket 7
// retirement: the Domain alias (deleted from intent.go) whose callers
// lived across storage, replay, ifa, cmd and reducer trees (all repointed at
// reducercontract.Domain) must stay absent from the whole reducer package
// directory — a re-declared alias in any file would still compile. The
// canonical reducercontract.Domain spelling is the only one.
func TestBucket7DomainRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		"Domain",
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed, cannot locate package directory")
	}
	pkgDir := filepath.Dir(thisFile)

	fset := token.NewFileSet()
	// Walk every file in the package directory (parser.ParseFile per
	// file — parser.ParseDir is deprecated since Go 1.25) so a
	// re-declared alias in any sibling compat file is still seen.
	files, err := filepath.Glob(filepath.Join(pkgDir, "*.go"))
	if err != nil {
		t.Fatalf("filepath.Glob(%q) error = %v", pkgDir, err)
	}

	declared := map[string]string{}
	for _, fileName := range files {
		f, err := parser.ParseFile(fset, fileName, nil, 0)
		if err != nil {
			t.Fatalf("parser.ParseFile(%q) error = %v", fileName, err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if isRetiredBucket7(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredBucket7(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredBucket7(s.Name.Name, retired) {
							declared[s.Name.Name] = fileName
						}
					}
				}
			}
		}
	}

	for _, name := range retired {
		if fileName, found := declared[name]; found {
			t.Errorf("%s still declares %q, want it retired (callers name reducercontract.Domain directly)",
				filepath.Base(fileName), name)
		}
	}
}

func isRetiredBucket7(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}

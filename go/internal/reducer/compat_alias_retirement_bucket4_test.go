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

// TestBucket4KubernetesCorrelationAliasRetired pins the #6626 batch 6 bucket 4
// retirement: the single-name kubernetes_correlation_compat stanza whose last
// callers lived in internal/storage/postgres' reducer queue readiness SQL and
// its readiness tests plus the reducer readiness seam test (all repointed at
// kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass) must
// stay absent from the whole reducer package directory, not just
// compat_cloud.go — a re-declared alias in a sibling compat_*.go file would
// still compile. The canonical kubernetescorrelation spelling is the only one.
func TestBucket4KubernetesCorrelationAliasRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		"KubernetesCorrelationNodesNotReadyFailureClass",
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
				if isRetiredBucket4(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetiredBucket4(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetiredBucket4(s.Name.Name, retired) {
							declared[s.Name.Name] = fileName
						}
					}
				}
			}
		}
	}

	for _, name := range retired {
		if fileName, found := declared[name]; found {
			t.Errorf("%s still declares %q, want it retired (callers name kubernetescorrelation.%s directly)",
				filepath.Base(fileName), name, name)
		}
	}
}

func isRetiredBucket4(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}

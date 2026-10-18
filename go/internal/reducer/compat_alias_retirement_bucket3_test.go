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

// TestBucket3TestOnlyDecodeAliasesRetired pins the #6626 batch 6 bucket 3
// retirement: the six decode-seam aliases whose last callers lived in
// codedataflow_input_invalid_test.go (since repointed at schemadecode)
// must stay absent from the whole reducer package directory, not just
// compat_decode.go — a re-declared alias in a sibling compat_*.go file
// would still compile. The canonical schemadecode.Decode* functions are
// the only spelling.
func TestBucket3TestOnlyDecodeAliasesRetired(t *testing.T) {
	t.Parallel()

	retired := []string{
		"decodeCodeFunctionSource",
		"decodeCodeFunctionSummary",
		"decodeCodeInterprocEvidence",
		"decodeCodeTaintEvidence",
		"decodeCodeDataflowFunction",
		"decodeCodeDataflowScanned",
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
				if isRetired(d.Name.Name, retired) {
					declared[d.Name.Name] = fileName
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if isRetired(name.Name, retired) {
								declared[name.Name] = fileName
							}
						}
					case *ast.TypeSpec:
						if isRetired(s.Name.Name, retired) {
							declared[s.Name.Name] = fileName
						}
					}
				}
			}
		}
	}

	for _, name := range retired {
		if fileName, found := declared[name]; found {
			t.Errorf("%s still declares %q, want it retired (callers name schemadecode.Decode%s directly)",
				filepath.Base(fileName), name, name[len("decode"):])
		}
	}
}

func isRetired(name string, retired []string) bool {
	for _, r := range retired {
		if name == r {
			return true
		}
	}
	return false
}

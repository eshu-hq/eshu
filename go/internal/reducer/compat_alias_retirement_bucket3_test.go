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
// must stay absent from compat_decode.go. The canonical
// schemadecode.Decode* functions are the only spelling.
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
		t.Fatal("runtime.Caller(0) failed, cannot locate compat_decode.go")
	}
	compatPath := filepath.Join(filepath.Dir(thisFile), "compat_decode.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, compatPath, nil, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile(%q) error = %v", compatPath, err)
	}

	declared := map[string]bool{}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			declared[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range s.Names {
						declared[name.Name] = true
					}
				case *ast.TypeSpec:
					declared[s.Name.Name] = true
				}
			}
		}
	}

	for _, name := range retired {
		if declared[name] {
			t.Errorf("compat_decode.go still declares %q, want it retired (callers name schemadecode.Decode%s directly)",
				name, name[len("decode"):])
		}
	}
}

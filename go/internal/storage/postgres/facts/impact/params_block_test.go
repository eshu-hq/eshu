// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
	"testing"
)

// TestEveryParamConstantIsBelowSentinel keeps paramEnd the upper bound of
// every Param, so the loops in TestEveryParamIsClassified and paramSource
// reach every list. It type-checks this package's non-test source and
// requires each package-level constant whose type is Param, other than
// paramEnd itself, to be below paramEnd. A list appended after the sentinel,
// or a Param declared anywhere else by type, conversion, alias, or typed
// expression, fails here instead of escaping the classification check.
func TestEveryParamConstantIsBelowSentinel(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}
	config := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := config.Check("factimpact", fset, files, nil)
	if err != nil {
		t.Fatalf("type-check package: %v", err)
	}
	paramType := pkg.Scope().Lookup("Param").Type()
	end, ok := constant.Int64Val(pkg.Scope().Lookup("paramEnd").(*types.Const).Val())
	if !ok {
		t.Fatal("paramEnd is not an integer constant")
	}
	for _, name := range pkg.Scope().Names() {
		value, isConst := pkg.Scope().Lookup(name).(*types.Const)
		if !isConst || name == "paramEnd" || !types.Identical(value.Type(), paramType) {
			continue
		}
		if v, exact := constant.Int64Val(value.Val()); !exact || v >= end {
			t.Errorf("Param constant %s = %s is not below paramEnd (%d); declare it above the sentinel and classify it", name, value.Val(), end)
		}
	}
}

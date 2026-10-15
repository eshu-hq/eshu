// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure_test

import (
	"go/ast"
	"go/types"
	"os"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestEveryReportedAssertionIsListed ties the conditions in check code to
// Assertions(), so the wrong-sample registry covers every one of them. It
// type-checks this package's non-test files and requires:
//
//   - every constant-valued expression of type Assertion is a declared
//     Assertion constant, so a check cannot report Assertion("...") or an
//     untyped string literal that no sample covers;
//   - the declared Assertion constants and the Assertions() entries are the
//     same set, with no entry listed twice;
//   - every Assertions() entry is referenced outside Assertions() and its own
//     declaration, so a listed assertion is actually checked.
//
// It reads source from disk, so a go test -overlay mutant of check.go is
// invisible to it: mutate the file, run, then git restore.
func TestEveryReportedAssertionIsListed(t *testing.T) {
	t.Parallel()
	pkg := loadFailurePackage(t)
	assertionType := pkg.Types.Scope().Lookup("Assertion").Type()

	declared := map[types.Object]bool{}
	for _, name := range pkg.Types.Scope().Names() {
		if c, ok := pkg.Types.Scope().Lookup(name).(*types.Const); ok && types.Identical(c.Type(), assertionType) {
			declared[c] = true
		}
	}

	listedBody := assertionsBody(t, pkg)
	listed := map[types.Object]bool{}
	ast.Inspect(listedBody, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			if obj := pkg.TypesInfo.Uses[ident]; declared[obj] {
				listed[obj] = true
			}
		}
		return true
	})

	declSpecs := map[ast.Expr]bool{}
	references := map[types.Object]int{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range gen.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok {
						for _, expr := range value.Values {
							declSpecs[expr] = true
						}
					}
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == listedBody {
				return false
			}
			expr, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			if declSpecs[expr] {
				return false
			}
			if ident, ok := expr.(*ast.Ident); ok && declared[pkg.TypesInfo.Uses[ident]] {
				references[pkg.TypesInfo.Uses[ident]]++
				return false
			}
			if tv, ok := pkg.TypesInfo.Types[expr]; ok && tv.Value != nil && types.Identical(tv.Type, assertionType) {
				t.Errorf("%s: constant Assertion %s is not a declared Assertion constant; declare it, list it in Assertions(), and give it a wrong sample",
					pkg.Fset.Position(expr.Pos()), types.ExprString(expr))
				return false
			}
			return true
		})
	}

	for obj := range declared {
		if !listed[obj] {
			t.Errorf("Assertion constant %s is not listed in Assertions()", obj.Name())
		}
		if references[obj] == 0 {
			t.Errorf("Assertion constant %s is listed but no check reports it", obj.Name())
		}
	}
	seen := map[failure.Assertion]bool{}
	for _, assertion := range failure.Assertions() {
		if seen[assertion] {
			t.Errorf("Assertions() lists %q twice", assertion)
		}
		seen[assertion] = true
	}
	if len(seen) != len(declared) {
		t.Errorf("Assertions() has %d distinct entries, %d Assertion constants are declared", len(seen), len(declared))
	}
}

// loadFailurePackage type-checks this package's non-test files from disk.
func loadFailurePackage(t *testing.T) *packages.Package {
	t.Helper()
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
		Dir: ".",
		Env: append(os.Environ(), "GOWORK=off"),
	}, ".")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		t.Fatalf("load package: got %d packages, errors %v", len(loaded), packageErrors(loaded))
	}
	return loaded[0]
}

// assertionsBody returns the body of func Assertions.
func assertionsBody(t *testing.T, pkg *packages.Package) *ast.BlockStmt {
	t.Helper()
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Assertions" {
				return fn.Body
			}
		}
	}
	t.Fatal("func Assertions not found")
	return nil
}

// packageErrors flattens every load error for a failure message.
func packageErrors(loaded []*packages.Package) []string {
	var messages []string
	for _, pkg := range loaded {
		for _, err := range pkg.Errors {
			messages = append(messages, err.Error())
		}
	}
	return messages
}

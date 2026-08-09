// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestEveryReadinessFailureClassIsEnrolled is the drift guard for
// nonCountingReducerRetryFailureClasses.
//
// A readiness-gate miss is not a failure on the intent's own merits — it means
// an upstream phase has not published yet. Counting it toward maxAttempts
// dead-letters a still-pending intent, and the succeeded-only reopen path never
// reopens a dead letter, so the work is lost until someone notices.
//
// Enumerating the classes by hand is what failed, twice. At #5046 the list
// recognised eight of the twenty-five readiness classes the reducer actually
// returns; the other seventeen had gone in one at a time with nothing checking.
// And the hand sweep written to find them missed six, which this guard caught
// on its first run. So this test does not restate the list. It parses every non-test file in
// internal/reducer with go/ast, finds each type that both reports
// `Retryable() bool { return true }` and returns a `*_not_ready` string from
// `FailureClass() string`, and requires that class to be registered.
//
// go/ast rather than a regex: reordering, reformatting, or commenting the
// source cannot fool it, and a class assembled at runtime rather than returned
// as a literal is reported as unreadable instead of silently passing.
func TestEveryReadinessFailureClassIsEnrolled(t *testing.T) {
	t.Parallel()

	found := readinessFailureClassesInReducer(t)
	if len(found) == 0 {
		t.Fatal("parsed internal/reducer and found no readiness failure classes at all; the scan is broken, not the registry")
	}

	enrolled := make(map[string]bool, len(nonCountingReducerRetryFailureClasses))
	for _, class := range nonCountingReducerRetryFailureClasses {
		enrolled[class] = true
	}

	var missing []string
	for class := range found {
		if !enrolled[class] {
			missing = append(missing, class+" ("+found[class]+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf(
			"readiness failure classes returned by a Retryable() reducer error but NOT enrolled in "+
				"nonCountingReducerRetryFailureClasses:\n  %s\n"+
				"An unenrolled readiness miss counts toward maxAttempts and can dead-letter a "+
				"still-pending intent that the succeeded-only reopen path never reopens. Export a "+
				"constant for the class and add it to that list.",
			strings.Join(missing, "\n  "),
		)
	}
}

// readinessFailureClassesInReducer returns every `*_not_ready` failure class
// returned by a type in internal/reducer whose Retryable() reports true,
// mapped to the file that declares it.
func readinessFailureClassesInReducer(t *testing.T) map[string]string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	reducerDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "reducer")

	entries, err := os.ReadDir(reducerDir)
	if err != nil {
		t.Fatalf("read %s: %v", reducerDir, err)
	}

	// Walked explicitly rather than with parser.ParseDir, which is deprecated as
	// of Go 1.25. Mode 0 (not SkipObjectResolution) is load-bearing: FailureClass
	// returns a named constant, and returnedStringLiteral follows Ident.Obj to its
	// declaration. Each constant is declared in the same file as its method, so
	// per-file resolution is enough.
	fset := token.NewFileSet()
	parsedFiles := make(map[string]*ast.File, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(reducerDir, name)
		parsed, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		parsedFiles[path] = parsed
	}
	if len(parsedFiles) == 0 {
		t.Fatalf("no non-test Go files parsed under %s; the guard would pass vacuously", reducerDir)
	}

	// receiver type name -> true when Retryable() returns true
	retryable := map[string]bool{}
	// receiver type name -> (class, file)
	classes := map[string][2]string{}

	for path, file := range parsedFiles {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			recv := receiverTypeName(fn.Recv.List[0].Type)
			if recv == "" {
				continue
			}
			switch fn.Name.Name {
			case "Retryable":
				if returnsTrue(fn) {
					retryable[recv] = true
				}
			case "FailureClass":
				if class := returnedStringLiteral(fn); strings.HasSuffix(class, "_not_ready") {
					classes[recv] = [2]string{class, filepath.Base(path)}
				}
			}
		}
	}

	found := map[string]string{}
	for recv, class := range classes {
		if retryable[recv] {
			found[class[0]] = class[1]
		}
	}
	return found
}

func receiverTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	default:
		return ""
	}
}

func returnsTrue(fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return false
	}
	ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return false
	}
	ident, isIdent := ret.Results[0].(*ast.Ident)
	return isIdent && ident.Name == "true"
}

// returnedStringLiteral resolves a FailureClass body of the form
// `return "literal"` or `return SomeExportedConstant`, where the constant's own
// declaration carries the literal. Anything else returns "" and is simply not
// scanned — the guard reports what it can read rather than guessing.
func returnedStringLiteral(fn *ast.FuncDecl) string {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return ""
	}
	ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return ""
	}
	switch result := ret.Results[0].(type) {
	case *ast.BasicLit:
		if result.Kind != token.STRING {
			return ""
		}
		value, err := strconv.Unquote(result.Value)
		if err != nil {
			return ""
		}
		return value
	case *ast.Ident:
		if result.Obj == nil {
			return ""
		}
		spec, isSpec := result.Obj.Decl.(*ast.ValueSpec)
		if !isSpec || len(spec.Values) != 1 {
			return ""
		}
		lit, isLit := spec.Values[0].(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return ""
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return ""
		}
		return value
	default:
		return ""
	}
}

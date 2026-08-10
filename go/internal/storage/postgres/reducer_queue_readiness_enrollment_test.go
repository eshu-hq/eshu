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

	found, unreadable := readinessFailureClassesInReducer(t)
	if len(found) == 0 {
		t.Fatal("parsed internal/reducer and found no readiness failure classes at all; the scan is broken, not the registry")
	}

	// A FailureClass() the scan could not read is NOT a pass. Object resolution
	// is per-file, so a constant declared in a different file from its method
	// resolves to nothing — and without this the class would simply vanish from
	// `found` while the test stayed green on the other 25.
	for _, ref := range unreadable {
		t.Errorf(
			"FailureClass() in %s returns %s, which this scan cannot resolve to a string. "+
				"Object resolution is per-file, so the constant is probably declared in another "+
				"file. Move it beside its method, or teach returnedStringLiteral to resolve it — "+
				"leaving it unreadable would silently exempt the class from this guard.",
			ref.file, ref.ident,
		)
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

// unreadableFailureClassRef is a FailureClass() the scan could not reduce to a
// string: it returns a named constant whose declaration this pass never saw.
type unreadableFailureClassRef struct {
	ident string
	file  string
}

// readinessFailureClassesInReducer returns every `*_not_ready` failure class
// returned by a type in internal/reducer whose Retryable() reports true,
// mapped to the file that declares it, plus every FailureClass() on such a type
// that the scan could NOT read.
//
// The second return is what keeps the first honest. A class the scan cannot
// read is indistinguishable from a class that does not exist, so without it a
// constant declared away from its method would drop out of the results and the
// guard would pass while silently exempting that class.
func readinessFailureClassesInReducer(t *testing.T) (map[string]string, []unreadableFailureClassRef) {
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
	// receiver type name -> the identifier whose value could not be read
	unreadable := map[string]unreadableFailureClassRef{}

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
				class, unresolvedIdent := returnedStringLiteral(fn)
				switch {
				case strings.HasSuffix(class, "_not_ready"):
					classes[recv] = [2]string{class, filepath.Base(path)}
				case unresolvedIdent != "":
					unreadable[recv] = unreadableFailureClassRef{
						ident: unresolvedIdent,
						file:  filepath.Base(path),
					}
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

	// Only unreadable classes on RETRYABLE types matter; a terminal error's
	// class is not this guard's business.
	var unreadableRefs []unreadableFailureClassRef
	for recv, ref := range unreadable {
		if retryable[recv] {
			unreadableRefs = append(unreadableRefs, ref)
		}
	}
	sort.Slice(unreadableRefs, func(i, j int) bool {
		return unreadableRefs[i].ident < unreadableRefs[j].ident
	})
	return found, unreadableRefs
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
// declaration carries the literal.
//
// It returns (value, "") when it could read the class, and ("", identifier)
// when the body returns a NAMED constant it could not reduce to a string —
// object resolution is per-file, so a constant declared in another file lands
// here. That second return exists so the caller can fail instead of treating an
// unreadable class as an absent one; silently returning "" for both was the
// hole (#6014 review).
//
// A body that returns neither a string literal nor a bare identifier yields
// ("", "") and is simply not scanned.
func returnedStringLiteral(fn *ast.FuncDecl) (string, string) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", ""
	}
	ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return "", ""
	}
	switch result := ret.Results[0].(type) {
	case *ast.BasicLit:
		if result.Kind != token.STRING {
			return "", ""
		}
		value, err := strconv.Unquote(result.Value)
		if err != nil {
			return "", ""
		}
		return value, ""
	case *ast.Ident:
		if result.Obj == nil {
			return "", result.Name
		}
		spec, isSpec := result.Obj.Decl.(*ast.ValueSpec)
		if !isSpec || len(spec.Values) != 1 {
			return "", result.Name
		}
		lit, isLit := spec.Values[0].(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return "", result.Name
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", result.Name
		}
		return value, ""
	default:
		return "", ""
	}
}

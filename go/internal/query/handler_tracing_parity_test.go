// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// TestHandlerTracingCopiesStayBehaviorIdentical is the copy-drift guard for
// the four family-local startQueryHandlerSpan helpers (root's
// handler_tracing.go, supplychain/handler_tracing.go,
// codeowners/handler_tracing.go, and codequery/code_handler_tracing.go):
// a one-sided edit to any of the four must fail loudly here instead of
// silently forking emitted spans.
//
// The codequery copy joined this list when the code family became its own
// package (#6060 lane A) and took the canonical queryHandlerTracer and
// startQueryHandlerSpan names. It parses each file and compares the printed
// AST of the queryHandlerTracer var and the startQueryHandlerSpan func,
// ignoring the package clause, imports, and comments (each copy carries
// family-specific prose), and it requires the top-level declaration name
// sets to match exactly, so an added, removed, or renamed helper fails
// too. Anything behavioral -- a differently seeded tracer, a dropped
// attribute, a renamed span, a changed signature -- changes the printed
// declarations and fails this test.
func TestHandlerTracingCopiesStayBehaviorIdentical(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot locate handler_tracing.go copies")
	}
	root := filepath.Dir(thisFile)
	copies := map[string]string{
		"root":        filepath.Join(root, "handler_tracing.go"),
		"supplychain": filepath.Join(root, "supplychain", "handler_tracing.go"),
		"codeowners":  filepath.Join(root, "codeowners", "handler_tracing.go"),
		"codequery":   filepath.Join(root, "codequery", "code_handler_tracing.go"),
	}
	want := tracingBehaviorDecls(t, copies["root"])
	wantNames := tracingDeclNames(t, copies["root"])
	for name, path := range copies {
		if name == "root" {
			continue
		}
		gotNames := tracingDeclNames(t, path)
		if len(gotNames) != len(wantNames) {
			t.Fatalf("%s handler_tracing.go declares %d top-level names %v, want %d %v", name, len(gotNames), gotNames, len(wantNames), wantNames)
		}
		for i, wantName := range wantNames {
			if gotNames[i] != wantName {
				t.Fatalf("%s handler_tracing.go top-level names %v, want %v", name, gotNames, wantNames)
			}
		}
		got := tracingBehaviorDecls(t, path)
		if len(got) != len(want) {
			t.Fatalf("%s handler_tracing.go declares %d behavior decls, want %d", name, len(got), len(want))
		}
		for decl, wantBody := range want {
			gotBody, found := got[decl]
			if !found {
				t.Fatalf("%s handler_tracing.go is missing %q", name, decl)
			}
			if gotBody != wantBody {
				t.Fatalf("%s handler_tracing.go %q drifted from root:\nroot:\n%s\n%s:\n%s", name, decl, wantBody, name, gotBody)
			}
		}
	}
}

// tracingBehaviorDecls parses path and returns the printed AST of its two
// behavior declarations, keyed by name. Comments are not parsed, so
// family-specific prose cannot drift the comparison; import grouping cannot
// either, because imports are skipped.
func tracingBehaviorDecls(t *testing.T, path string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile(%s) error = %v", path, err)
	}
	decls := map[string]string{}
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for _, name := range value.Names {
					if name.Name == "queryHandlerTracer" {
						decls[name.Name] = printTracingDecl(fset, value)
					}
				}
			}
		case *ast.FuncDecl:
			if typed.Name.Name == "startQueryHandlerSpan" {
				decls[typed.Name.Name] = printTracingDecl(fset, typed)
			}
		}
	}
	for _, name := range []string{"queryHandlerTracer", "startQueryHandlerSpan"} {
		if _, found := decls[name]; !found {
			t.Fatalf("%s is missing behavior declaration %q", path, name)
		}
	}
	return decls
}

// tracingDeclNames returns the sorted names of every top-level function,
// type, const, and var declared in path, so a helper added to (or removed
// from) one copy fails the comparison even when the two pinned behavior
// declarations still match.
func tracingDeclNames(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parser.ParseFile(%s) error = %v", path, err)
	}
	var names []string
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						names = append(names, name.Name)
					}
				case *ast.TypeSpec:
					names = append(names, spec.Name.Name)
				}
			}
		case *ast.FuncDecl:
			names = append(names, typed.Name.Name)
		}
	}
	sort.Strings(names)
	return names
}

func printTracingDecl(fset *token.FileSet, node ast.Node) string {
	var rendered bytes.Buffer
	if err := printer.Fprint(&rendered, fset, node); err != nil {
		panic(err)
	}
	return rendered.String()
}

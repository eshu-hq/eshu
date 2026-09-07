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
	"testing"
)

// TestHandlerTracingCopiesStayBehaviorIdentical is the copy-drift guard for
// the three family-local startQueryHandlerSpan helpers (root's
// handler_tracing.go, supplychain/handler_tracing.go, and
// codeowners/handler_tracing.go): L3 adds no fourth copy, so a one-sided
// edit to any of the three must fail loudly here instead of silently
// forking emitted spans. It parses each file and compares the printed AST
// of the queryHandlerTracer var and the startQueryHandlerSpan func,
// ignoring the package clause, imports, and comments (each copy carries
// family-specific prose). Anything behavioral -- a differently seeded
// tracer, a dropped attribute, a renamed span, a changed signature --
// changes the printed declarations and fails this test.
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
	}
	want := tracingBehaviorDecls(t, copies["root"])
	for name, path := range copies {
		if name == "root" {
			continue
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

func printTracingDecl(fset *token.FileSet, node ast.Node) string {
	var rendered bytes.Buffer
	if err := printer.Fprint(&rendered, fset, node); err != nil {
		panic(err)
	}
	return rendered.String()
}

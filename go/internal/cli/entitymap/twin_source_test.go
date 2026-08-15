// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The value readers here are deliberate copies of the trace* helpers in
// go/cmd/eshu/trace.go, which cannot be imported because that is package main.
// go/cmd/eshu/entitymap_parity_test.go is the primary guard and does more than
// this one: it runs both implementations and compares behaviour.
//
// This test exists because that guard cannot run from here. Editing values.go
// and running the natural focused loop -- `go test ./internal/cli/entitymap/`
// -- executes no parity check at all, so a copy can be changed, tested, and
// committed with everything green locally. Measured before adding this: the
// focused loop ran 0 of the guard's tests while `go test ./cmd/eshu/` ran 39.
//
// Comparing SOURCE rather than behaviour is what makes it possible from this
// side. Reading and parsing a file needs no import, so the package-main barrier
// that forces the copies does not block this direction.
func TestValueReadersMatchTheirTraceOriginals(t *testing.T) {
	t.Parallel()

	const (
		traceFile  = "../../../cmd/eshu/trace.go"
		valuesFile = "values.go"
	)
	pairs := []struct {
		traceName string
		mapName   string
	}{
		{"traceMap", "mapField"},
		{"traceSlice", "sliceField"},
		{"traceString", "stringField"},
		{"traceInt", "intField"},
		{"traceStrings", "stringList"},
		{"traceFirstString", "firstNonEmpty"},
	}

	for _, pair := range pairs {
		t.Run(pair.mapName, func(t *testing.T) {
			t.Parallel()
			original := twinFuncBody(t, traceFile, pair.traceName)
			copied := twinFuncBody(t, valuesFile, pair.mapName)
			if original != copied {
				t.Fatalf(
					"%s drifted from %s in go/cmd/eshu/trace.go; change both or neither\noriginal:\n%s\ncopy:\n%s",
					pair.mapName, pair.traceName, original, copied,
				)
			}
		})
	}
}

// twinFuncBody renders one top-level function's body as formatted source.
// Comments attach to the file rather than the body node, so a comment-only edit
// does not fire; a token change does.
func twinFuncBody(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name {
			continue
		}
		var rendered strings.Builder
		if err := format.Node(&rendered, fset, fn.Body); err != nil {
			t.Fatalf("render %s from %s: %v", name, file, err)
		}
		return rendered.String()
	}
	t.Fatalf("function %s not found in %s; if it moved or was renamed, update this test with it", name, file)
	return ""
}

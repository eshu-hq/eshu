// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The value readers here were forked from the trace* helpers in
// go/cmd/eshu/trace.go, which could not be imported because that is package
// main. Those originals are gone -- the component (#6139), entitymap (#6127),
// and trace (#6059) extractions removed their last cmd/eshu callers -- so
// this test pins the copies against their internal/cli/trace siblings, the
// set TestEnvelopeReaderParity already holds to change, freshness, and
// component. go/cmd/eshu/entitymap_parity_test.go is the primary guard and
// does more than this one: it runs both implementations and compares
// behaviour.
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
		traceValueFile = "../trace/value.go"
		valuesFile     = "values.go"
	)
	pairs := []struct {
		traceFile string
		traceName string
		mapName   string
	}{
		{traceValueFile, "mapValue", "mapField"},
		{traceValueFile, "sliceValue", "sliceField"},
		{traceValueFile, "stringValue", "stringField"},
		{traceValueFile, "intValue", "intField"},
		{traceValueFile, "stringsValue", "stringList"},
		{traceValueFile, "firstString", "firstNonEmpty"},
	}

	for _, pair := range pairs {
		t.Run(pair.mapName, func(t *testing.T) {
			t.Parallel()
			original := twinFuncBody(t, pair.traceFile, pair.traceName)
			copied := twinFuncBody(t, valuesFile, pair.mapName)
			if original != copied {
				t.Fatalf(
					"%s drifted from %s in %s; change both or neither\noriginal:\n%s\ncopy:\n%s",
					pair.mapName, pair.traceName, pair.traceFile, original, copied,
				)
			}
		})
	}
}

// TestTransportClassifierMatchesItsTraceOriginal runs the same source check
// for transportErrorCode in entitymap.go, whose original is
// traceErrorCodeFromTransport in go/cmd/eshu/trace.go. The two bodies cannot
// compare token-identical: this copy spells its statuses http.StatusNotFound
// while the original spells them 404. Normalising every http.Status* selector
// on both sides to its numeric value -- looked up from net/http itself, so
// the table cannot disagree with the compiler -- makes the two spellings
// comparable, while any token change that alters behaviour still diffs.
//
// A second behavioural table was considered and rejected for this job:
// TestErrorCodeFromTransportPrecedence already pins every arm of the mapping
// from this package, and a table that lives beside the copy gets edited in
// the same commit as the copy, so it stays green exactly when the two copies
// drift apart. Comparing against the original's source is the only check
// that catches that from this side of the package-main boundary.
func TestTransportClassifierMatchesItsTraceOriginal(t *testing.T) {
	t.Parallel()

	original := normalizedStatusConstants(t, twinFuncBody(t, "../../../cmd/eshu/trace.go", "traceErrorCodeFromTransport"))
	copied := normalizedStatusConstants(t, twinFuncBody(t, "entitymap.go", "transportErrorCode"))
	if original != copied {
		t.Fatalf(
			"transportErrorCode drifted from traceErrorCodeFromTransport in go/cmd/eshu/trace.go; change both or neither\noriginal:\n%s\ncopy:\n%s",
			original, copied,
		)
	}
}

// httpStatusValues names the http.Status* constants the classifier bodies may
// spell out, with values taken from net/http so an entry cannot be wrong,
// only missing -- and a missing one fails the normaliser loudly below rather
// than slipping through unreplaced.
var httpStatusValues = map[string]int{
	"StatusBadRequest":         http.StatusBadRequest,
	"StatusNotFound":           http.StatusNotFound,
	"StatusNotImplemented":     http.StatusNotImplemented,
	"StatusServiceUnavailable": http.StatusServiceUnavailable,
}

// normalizedStatusConstants rewrites http.Status* selectors in rendered
// source to their numeric values, so a body written with named constants
// compares equal to a body written with the numbers. Longer names are
// replaced first so a name that prefixes another cannot corrupt it.
func normalizedStatusConstants(t *testing.T, src string) string {
	t.Helper()
	names := make([]string, 0, len(httpStatusValues))
	for name := range httpStatusValues {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, name := range names {
		src = strings.ReplaceAll(src, "http."+name, strconv.Itoa(httpStatusValues[name]))
	}
	if index := strings.Index(src, "http.Status"); index >= 0 {
		end := index + 40
		if end > len(src) {
			end = len(src)
		}
		t.Fatalf("classifier body uses an http.Status constant missing from httpStatusValues (near %q); add it there with its net/http value", src[index:end])
	}
	return src
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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// Parity tests tying internal/cli/entitymap's deliberate copies to their
// siblings.
//
// The #6059 extraction copied traceErrorCodeFromTransport and the trace*
// value readers into internal/cli/entitymap (transportErrorCode and
// values.go) because this package is `package main` and cannot be imported.
// The entitymap AGENTS.md states the invariant: a behavior change to one copy
// is a bug unless it is made to both. Nothing enforced that until this file.
// The trace* value reader originals have since left cmd/eshu entirely (#6139
// and #6059 removed their last callers here), so the source-level pins below
// compare entitymap's set against internal/cli/trace's surviving copy; the
// classifier original is still declared and called in trace.go and keeps its
// pin.
//
// The tests live here, not in entitymap, because the import only works in
// this direction: package main can import internal/cli/entitymap, while
// nothing can import package main.
//
// Two enforcement shapes, matched to what each copy exposes:
//
//   - The transport classifier is reachable through the exported
//     entitymap.ErrorCodeFromTransport, so a shared input table runs through
//     BOTH classifiers and asserts identical results. The two documented
//     deliberate divergences (409 and nil) are pinned explicitly so a change
//     to either side of them fails too.
//   - The values.go readers are unexported and only partially reachable
//     through entitymap's exported surface, so they are pinned at the source
//     level instead: each reader's function body must be token-identical to
//     its internal/cli/trace sibling. mapField/stringField additionally get a
//     behavioral table through the exported FreshnessState.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/entitymap"
	"github.com/eshu-hq/eshu/go/internal/cli/trace"
)

// entityMapParityStatusErr carries an HTTP status through the
// apierr.HTTPStatusError contract with a controllable message, so the table
// below can combine a status with (or without) the substrings the classifiers
// check first.
type entityMapParityStatusErr struct {
	status  int
	message string
}

func (e *entityMapParityStatusErr) Error() string { return e.message }

func (e *entityMapParityStatusErr) HTTPStatusCode() int { return e.status }

// TestEntityMapTransportClassifierMatchesTrace runs one input table through
// both traceErrorCodeFromTransport (the go/cmd/eshu original) and
// entitymap.ErrorCodeFromTransport (which delegates to the entitymap copy)
// and requires identical results. It covers every mapped status, unmapped
// statuses, the substring checks and their precedence over a carried status,
// a wrapped status error, and a status-free error.
func TestEntityMapTransportClassifierMatchesTrace(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"status 400", &entityMapParityStatusErr{status: http.StatusBadRequest, message: "API error 400: bad selector"}, "invalid_argument"},
		{"status 404", &entityMapParityStatusErr{status: http.StatusNotFound, message: "API error 404: no such entity"}, "not_found"},
		{"status 501", &entityMapParityStatusErr{status: http.StatusNotImplemented, message: "API error 501: not implemented"}, "unsupported_capability"},
		{"status 503", &entityMapParityStatusErr{status: http.StatusServiceUnavailable, message: "API error 503: overloaded"}, "backend_unavailable"},
		{"unmapped status 500", &entityMapParityStatusErr{status: http.StatusInternalServerError, message: "API error 500: boom"}, "api_error"},
		{"unmapped status 418", &entityMapParityStatusErr{status: http.StatusTeapot, message: "API error 418: teapot"}, "api_error"},
		{"wrapped status 404", fmt.Errorf("fetch entity map: %w", &entityMapParityStatusErr{status: http.StatusNotFound, message: "API error 404: gone"}), "not_found"},
		{"status-free error", errors.New("dial tcp: no route to host"), "api_error"},
		{"connection refused, no status", errors.New("dial tcp 127.0.0.1:9040: connect: connection refused"), "backend_unavailable"},
		{"request failed, no status", errors.New("request failed: EOF"), "backend_unavailable"},
		{"connection refused outranks status 400", &entityMapParityStatusErr{status: http.StatusBadRequest, message: "API error 400: connection refused upstream"}, "backend_unavailable"},
		{"wrapped connection refused", fmt.Errorf("fetch entity map: %w", errors.New("connect: connection refused")), "backend_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			traceGot := traceErrorCodeFromTransport(tc.err)
			mapGot := entitymap.ErrorCodeFromTransport(tc.err)
			if traceGot != mapGot {
				t.Fatalf("classifiers diverged: traceErrorCodeFromTransport = %q, entitymap.ErrorCodeFromTransport = %q", traceGot, mapGot)
			}
			if traceGot != tc.want {
				t.Fatalf("both classifiers returned %q, want %q", traceGot, tc.want)
			}
		})
	}
}

// TestEntityMapTransportClassifierPinnedDivergences pins the only two places
// the entity map's exported classifier is allowed to differ from the trace
// original: a 409 means the selector was ambiguous (only the entity map uses
// that status), and a nil error classifies to the empty string rather than
// api_error. Anything else diverging is a bug the table test above catches;
// either of THESE changing silently would also be a bug, so both sides are
// asserted exactly.
func TestEntityMapTransportClassifierPinnedDivergences(t *testing.T) {
	conflict := &entityMapParityStatusErr{status: http.StatusConflict, message: "API error 409: ambiguous selector"}
	if got := entitymap.ErrorCodeFromTransport(conflict); got != "ambiguous" {
		t.Fatalf("entitymap.ErrorCodeFromTransport(409) = %q, want ambiguous", got)
	}
	if got := traceErrorCodeFromTransport(conflict); got != "api_error" {
		t.Fatalf("traceErrorCodeFromTransport(409) = %q, want api_error", got)
	}
	if got := entitymap.ErrorCodeFromTransport(nil); got != "" {
		t.Fatalf("entitymap.ErrorCodeFromTransport(nil) = %q, want empty", got)
	}
	if got := traceErrorCodeFromTransport(nil); got != "api_error" {
		t.Fatalf("traceErrorCodeFromTransport(nil) = %q, want api_error", got)
	}
}

// TestEntityMapFreshnessStateMatchesTraceReaders runs one table of truth
// blocks through entitymap.FreshnessState (stringField over mapField) and
// trace.ServiceFreshnessState, which composes internal/cli/trace's stringValue
// over mapValue the same way, asserting identical results. This is the
// behavioral slice of the values.go parity: these two readers are the only
// ones reachable through each package's exported surface, and the table
// covers the degradation cases where a drifted copy would differ -- nil maps,
// missing members, wrong-typed members, and whitespace trimming.
func TestEntityMapFreshnessStateMatchesTraceReaders(t *testing.T) {
	cases := []struct {
		name  string
		truth map[string]any
		want  string
	}{
		{"nil truth", nil, ""},
		{"missing freshness", map[string]any{"resolution": map[string]any{}}, ""},
		{"freshness not an object", map[string]any{"freshness": "stale"}, ""},
		{"state missing", map[string]any{"freshness": map[string]any{"generation": float64(4)}}, ""},
		{"state not a string", map[string]any{"freshness": map[string]any{"state": float64(1)}}, ""},
		{"state padded with whitespace", map[string]any{"freshness": map[string]any{"state": "  stale\n"}}, "stale"},
		{"state present", map[string]any{"freshness": map[string]any{"state": "fresh"}}, "fresh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			traceGot := trace.ServiceFreshnessState(trace.ServiceEnvelope{Truth: tc.truth})
			mapGot := entitymap.FreshnessState(entitymap.Envelope{Truth: tc.truth})
			if traceGot != mapGot {
				t.Fatalf("readers diverged: trace.ServiceFreshnessState = %q, entitymap.FreshnessState = %q", traceGot, mapGot)
			}
			if traceGot != tc.want {
				t.Fatalf("both readers returned %q, want %q", traceGot, tc.want)
			}
		})
	}
}

// entityMapParityFuncBody parses file and returns name's function body
// rendered through go/format, so gofmt-equivalent bodies compare equal while
// any token change -- a dropped branch, a reordered check, a changed literal
// -- does not. Comments are attached to the file, not the body node, so
// comment-only edits do not fire.
func entityMapParityFuncBody(t *testing.T, file, name string) string {
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
	t.Fatalf("function %s not found in %s; if it was renamed or moved, update this parity test with it", name, file)
	return ""
}

// TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers pins the four
// values.go readers the exported entitymap surface cannot reach (plus the two
// it can) to their internal/cli/trace siblings at the source level: each
// copy's function body must be token-identical to its pair. The cmd/eshu
// originals these copies were forked from are gone -- the component (#6139),
// entitymap (#6127), and trace (#6059) extractions removed their last callers
// here -- so the surviving copies pin to each other, with trace's set as the
// anchor because TestEnvelopeReaderParity already holds that set to change,
// freshness, and component. The copies were created byte-identical on
// purpose, and a behavior change to one is a bug unless made to both; a
// legitimate change made to both keeps this green.
func TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers(t *testing.T) {
	const (
		traceValueFile = "../../internal/cli/trace/value.go"
		valuesFile     = "../../internal/cli/entitymap/values.go"
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
			original := entityMapParityFuncBody(t, pair.traceFile, pair.traceName)
			copied := entityMapParityFuncBody(t, valuesFile, pair.mapName)
			if original != copied {
				t.Fatalf(
					"%s (entitymap/values.go) drifted from %s (%s); change both or neither\noriginal:\n%s\ncopy:\n%s",
					pair.mapName, pair.traceName, pair.traceFile, original, copied,
				)
			}
		})
	}
}

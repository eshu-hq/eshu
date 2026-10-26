// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// graphCall is one statement the fake graph saw.
type graphCall struct {
	cypher string
	params map[string]any
}

// fakeGraph answers the three statements the list route issues, told apart by
// a marker only that statement carries: the excluded-count statement groups by
// spec_kind, the name statement reads Repository.name, and anything else is the
// page statement.
type fakeGraph struct {
	mu               sync.Mutex
	pageRows         []map[string]any
	excludedRows     []map[string]any
	nameRows         []map[string]any
	pageErr          error
	excludedErr      error
	nameErr          error
	sectionRows      []map[string]any
	sectionErr       error
	sectionTotalRows []map[string]any
	sectionTotalErr  error
	calls            []graphCall
	sawDeadline      bool
}

func (g *fakeGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, graphCall{cypher: cypher, params: params})
	if _, ok := ctx.Deadline(); ok {
		g.sawDeadline = true
	}
	switch {
	case strings.Contains(cypher, "AS excluded_count"):
		return g.excludedRows, g.excludedErr
	case strings.Contains(cypher, "AS repo_name"):
		return g.nameRows, g.nameErr
	case strings.Contains(cypher, "AS declaration_total"):
		return g.sectionTotalRows, g.sectionTotalErr
	case strings.Contains(cypher, "AS lowest_resolved_major"):
		return g.sectionRows, g.sectionErr
	default:
		return g.pageRows, g.pageErr
	}
}

func (*fakeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// callContaining returns the first recorded call whose Cypher holds marker.
func (g *fakeGraph) callContaining(t *testing.T, marker string) graphCall {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, call := range g.calls {
		if strings.Contains(call.cypher, marker) {
			return call
		}
	}
	t.Fatalf("no graph call contained %q; calls = %d", marker, len(g.calls))
	return graphCall{}
}

func (g *fakeGraph) callCount(marker string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, call := range g.calls {
		if strings.Contains(call.cypher, marker) {
			n++
		}
	}
	return n
}

// decl builds one page row. Version parts are nil for alias, unresolved and
// range rows, which the projection leaves unset.
func decl(repo, path string, line, order int, runtime, spec, specKind string, major *int) map[string]any {
	row := map[string]any{
		"repo_id": repo, "relative_path": path, "scope_dir": ".", "line_number": int64(line),
		"order_index": int64(order), "runtime": runtime, "source_kind": "nvmrc",
		"raw_spec": spec, "spec_kind": specKind,
		"tool_name": nil, "version_major": nil, "version_minor": nil, "version_patch": nil,
		"distribution": nil, "range_lower": nil, "range_upper": nil,
		"range_lower_major": nil, "range_upper_major": nil,
		"range_lower_inclusive": nil, "range_upper_inclusive": nil,
	}
	if major != nil {
		row["version_major"] = int64(*major)
	}
	return row
}

func intp(v int) *int { return &v }

func newMux(graph querycontract.GraphQuery) *http.ServeMux {
	mux := http.NewServeMux()
	(&Handler{Neo4j: graph}).Mount(mux)
	return mux
}

func post(mux http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/runtime-declarations/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func httpReq(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/runtime-declarations/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func serve(mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

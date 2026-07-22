// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCallGraphMetricsReturnsBoundedHubFunctions(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})") {
					t.Fatalf("cypher = %q, want one repo-indexed CALLS pass", cypher)
				}
				if strings.Contains(cypher, "OPTIONAL MATCH") || strings.Contains(cypher, "REPO_CONTAINS") {
					t.Fatalf("cypher = %q, want no repeated repository expansion", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				edges := make([]map[string]any, 0, 12)
				for index := range 7 {
					edges = append(edges, callGraphMetricEdgeRow(
						fmt.Sprintf("caller-%d", index), "go/internal/api/callers.go", "go", "caller", index+1,
						"fn-start", "go/internal/api/server.go", "go", "Start", 42,
					))
				}
				for index := range 5 {
					edges = append(edges, callGraphMetricEdgeRow(
						"fn-start", "go/internal/api/server.go", "go", "Start", 42,
						fmt.Sprintf("callee-%d", index), "go/internal/api/callees.go", "go", "callee", index+1,
					))
				}
				return edges, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-graph/metrics",
		bytes.NewBufferString(`{"metric_type":"hub_functions","repo_id":"repo-1","language":"go","limit":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	functions, ok := resp["functions"].([]any)
	if !ok {
		t.Fatalf("functions type = %T, want []any", resp["functions"])
	}
	if got, want := len(functions), 1; got != want {
		t.Fatalf("len(functions) = %d, want %d", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := resp["next_offset"], float64(1); got != want {
		t.Fatalf("next_offset = %#v, want %#v", got, want)
	}
	first := functions[0].(map[string]any)
	if first["source_handle"] == nil {
		t.Fatalf("function missing source_handle: %#v", first)
	}
	if got, want := first["incoming_calls"], float64(7); got != want {
		t.Fatalf("incoming_calls = %#v, want %#v", got, want)
	}
	if got, want := first["outgoing_calls"], float64(5); got != want {
		t.Fatalf("outgoing_calls = %#v, want %#v", got, want)
	}
	if _, ok := resp["results"]; ok {
		t.Fatalf("response includes ambiguous results alias: %#v", resp["results"])
	}
}

func TestHandleCallGraphMetricsReturnsRecursiveFunctions(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})") {
					t.Fatalf("cypher = %q, want one repo-indexed CALLS pass", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					callGraphMetricEdgeRow("fn-walk", "src/a-tree.ts", "typescript", "walk", 1, "fn-walk", "src/a-tree.ts", "typescript", "walk", 1),
					callGraphMetricEdgeRow("fn-a", "src/z-parser.ts", "typescript", "parseA", 10, "fn-b", "src/z-parser.ts", "typescript", "parseB", 20),
					callGraphMetricEdgeRow("fn-b", "src/z-parser.ts", "typescript", "parseB", 20, "fn-a", "src/z-parser.ts", "typescript", "parseA", 10),
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-graph/metrics",
		bytes.NewBufferString(`{"metric_type":"recursive_functions","repo_id":"repo-1","language":"typescript","limit":2}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	functions, ok := resp["functions"].([]any)
	if !ok || len(functions) != 2 {
		t.Fatalf("functions = %#v, want two rows", resp["functions"])
	}
	self := functions[0].(map[string]any)
	if got, want := self["recursion_kind"], "self_call"; got != want {
		t.Fatalf("recursion_kind = %#v, want %#v", got, want)
	}
	mutual := functions[1].(map[string]any)
	if got, want := mutual["recursion_kind"], "mutual_call"; got != want {
		t.Fatalf("recursion_kind = %#v, want %#v", got, want)
	}
	if mutual["recursion_evidence"] == nil {
		t.Fatalf("mutual recursion missing evidence: %#v", mutual)
	}
}

func TestHandleCallGraphMetricsRejectsUnscopedRequests(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-graph/metrics",
		bytes.NewBufferString(`{"metric_type":"hub_functions","limit":25}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleCallGraphMetricsFailsClosedWhenEdgeScanLimitExceeded(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $edge_scan_limit") {
					t.Fatalf("cypher = %q, want bounded edge sentinel", cypher)
				}
				if got, want := params["edge_scan_limit"], 50001; got != want {
					t.Fatalf("params[edge_scan_limit] = %#v, want %#v", got, want)
				}
				return make([]map[string]any, 50001), nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-graph/metrics",
		bytes.NewBufferString(`{"metric_type":"hub_functions","repo_id":"repo-1","limit":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "call graph metrics scope exceeds internal edge scan limit") {
		t.Fatalf("body = %q, want bounded-scope error", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "functions") {
		t.Fatalf("body = %q, must not return partial metric rows", w.Body.String())
	}
}

func TestHandleCallGraphMetricsRejectsNegativeLimit(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-graph/metrics",
		bytes.NewBufferString(`{"metric_type":"hub_functions","repo_id":"repo-1","limit":-1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleCallGraphMetricsRejectsZeroLimit(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-graph/metrics",
		bytes.NewBufferString(`{"metric_type":"hub_functions","repo_id":"repo-1","limit":0}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestCallGraphMetricsResponseUsesGlobalRankAndCapsNextOffset(t *testing.T) {
	t.Parallel()

	resp := callGraphMetricsResponse(callGraphMetricsRequest{
		MetricType: "hub_functions",
		RepoID:     "repo-1",
		Limit:      intPtr(1),
		Offset:     callGraphMetricsMaxOffset,
	}, []map[string]any{
		{"repo_id": "repo-1", "file_path": "a.go", "function_id": "fn-a"},
		{"repo_id": "repo-1", "file_path": "b.go", "function_id": "fn-b"},
	})

	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got := resp["next_offset"]; got != nil {
		t.Fatalf("next_offset = %#v, want nil beyond max offset", got)
	}
	functions := resp["functions"].([]map[string]any)
	if got, want := functions[0]["rank"], callGraphMetricsMaxOffset+1; got != want {
		t.Fatalf("rank = %#v, want %#v", got, want)
	}
}

func intPtr(value int) *int {
	return &value
}

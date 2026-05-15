package query

import (
	"bytes"
	"context"
	"encoding/json"
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
				if !strings.Contains(cypher, "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)-[:CONTAINS]->(fn:Function)") {
					t.Fatalf("cypher = %q, want repo-anchored function query", cypher)
				}
				if !strings.Contains(cypher, "caller:Function)-[:CALLS]->(fn)") ||
					!strings.Contains(cypher, "callee:Function)<-[:CALLS]-(fn)") {
					t.Fatalf("cypher = %q, want repo-scoped incoming and outgoing call counts", cypher)
				}
				if !strings.Contains(cypher, "OPTIONAL MATCH (repo)-[:REPO_CONTAINS]") {
					t.Fatalf("cypher = %q, want repo-scoped call degree expansion", cypher)
				}
				if !strings.Contains(cypher, "ORDER BY total_degree DESC, incoming_calls DESC, outgoing_calls DESC, source_file.relative_path, fn.start_line, fn.name") {
					t.Fatalf("cypher = %q, want deterministic hub ordering", cypher)
				}
				if !strings.Contains(cypher, "SKIP $offset") || !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded pagination", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["language"], "go"; got != want {
					t.Fatalf("params[language] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":        "repo-1",
						"file_path":      "go/internal/api/server.go",
						"language":       "go",
						"function_id":    "fn-start",
						"function_name":  "Start",
						"start_line":     42,
						"end_line":       76,
						"incoming_calls": 7,
						"outgoing_calls": 5,
						"total_degree":   12,
					},
					{
						"repo_id":        "repo-1",
						"file_path":      "go/internal/api/routes.go",
						"language":       "go",
						"function_id":    "fn-route",
						"function_name":  "Route",
						"incoming_calls": 2,
						"outgoing_calls": 1,
						"total_degree":   3,
					},
				}, nil
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
				if !strings.Contains(cypher, "MATCH (fn)-[:CALLS]->(partner:Function)") ||
					!strings.Contains(cypher, "MATCH (partner)-[:CALLS]->(fn)") {
					t.Fatalf("cypher = %q, want two-hop recursion evidence", cypher)
				}
				if !strings.Contains(cypher, "WHERE source_key <= partner_key") {
					t.Fatalf("cypher = %q, want duplicate mutual-recursion guard", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":        "repo-1",
						"file_path":      "src/tree.ts",
						"language":       "typescript",
						"function_id":    "fn-walk",
						"function_name":  "walk",
						"partner_id":     "fn-walk",
						"partner_name":   "walk",
						"partner_file":   "src/tree.ts",
						"incoming_calls": 1,
						"outgoing_calls": 1,
					},
					{
						"repo_id":        "repo-1",
						"file_path":      "src/parser.ts",
						"language":       "typescript",
						"function_id":    "fn-a",
						"function_name":  "parseA",
						"partner_id":     "fn-b",
						"partner_name":   "parseB",
						"partner_file":   "src/parser.ts",
						"incoming_calls": 2,
						"outgoing_calls": 2,
					},
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

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

func TestHandleImportDependencyInvestigationReturnsBoundedImportsByFile(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File {relative_path: $source_file})") {
					t.Fatalf("cypher = %q, want repo and source file anchored import query", cypher)
				}
				if !strings.Contains(cypher, "ORDER BY source_file.relative_path, target_module.name, rel.line_number") {
					t.Fatalf("cypher = %q, want deterministic import ordering", cypher)
				}
				if !strings.Contains(cypher, "SKIP $offset") || !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded pagination", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				if got, want := params["source_file"], "src/module_a.py"; got != want {
					t.Fatalf("params[source_file] = %#v, want %#v", got, want)
				}
				if got, want := params["limit"], 2; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":       "repo-1",
						"source_file":   "src/module_a.py",
						"source_name":   "module_a.py",
						"language":      "python",
						"target_module": "requests",
						"imported_name": "Session",
						"alias":         "http",
						"line_number":   3,
					},
					{
						"repo_id":       "repo-1",
						"source_file":   "src/module_a.py",
						"source_name":   "module_a.py",
						"language":      "python",
						"target_module": "urllib",
						"line_number":   5,
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"imports_by_file","repo_id":"repo-1","source_file":"src/module_a.py","limit":1}`),
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
	dependencies, ok := resp["dependencies"].([]any)
	if !ok {
		t.Fatalf("dependencies type = %T, want []any", resp["dependencies"])
	}
	if got, want := len(dependencies), 1; got != want {
		t.Fatalf("len(dependencies) = %d, want %d", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := resp["next_offset"], float64(1); got != want {
		t.Fatalf("next_offset = %#v, want %#v", got, want)
	}
	first := dependencies[0].(map[string]any)
	if first["source_handle"] == nil {
		t.Fatalf("dependency missing source_handle: %#v", first)
	}
	if _, ok := resp["results"]; ok {
		t.Fatalf("response includes ambiguous results alias: %#v", resp["results"])
	}
	if _, ok := resp["matches"]; ok {
		t.Fatalf("response includes ambiguous matches alias: %#v", resp["matches"])
	}
	if _, ok := resp["modules"]; ok {
		t.Fatalf("imports_by_file response includes non-canonical modules key: %#v", resp["modules"])
	}
}

func TestHandleImportDependencyInvestigationReturnsFileImportCycles(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "source_file.name = source_module.name + '.py'") {
					t.Fatalf("cypher = %q, want Python module-to-file cycle guard", cypher)
				}
				if !strings.Contains(cypher, "target_file.name = target_module.name + '.py'") {
					t.Fatalf("cypher = %q, want target module-to-file cycle guard", cypher)
				}
				if got, want := params["language"], "python"; got != want {
					t.Fatalf("params[language] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":               "repo-1",
						"source_file":           "src/module_a.py",
						"target_file":           "src/module_b.py",
						"source_module":         "module_a",
						"target_module":         "module_b",
						"source_line_number":    4,
						"back_edge_line_number": 7,
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"file_import_cycles","repo_id":"repo-1","language":"python","limit":25}`),
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
	cycles, ok := resp["cycles"].([]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want one cycle", resp["cycles"])
	}
	cycle := cycles[0].(map[string]any)
	if got, want := cycle["cycle_length"], float64(2); got != want {
		t.Fatalf("cycle_length = %#v, want %#v", got, want)
	}
}

func TestHandleImportDependencyInvestigationReturnsCrossModuleCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (source_file)-[:CONTAINS]->(caller:Function)") ||
					!strings.Contains(cypher, "MATCH (caller)-[rel:CALLS]->(callee:Function)") {
					t.Fatalf("cypher = %q, want bounded call relationship query", cypher)
				}
				if !strings.Contains(cypher, "MATCH (source_file)-[:CONTAINS]->(source_module:Module {name: $source_module})") {
					t.Fatalf("cypher = %q, want source module anchor", cypher)
				}
				if !strings.Contains(cypher, "MATCH (target_file)-[:CONTAINS]->(target_module:Module {name: $target_module})") {
					t.Fatalf("cypher = %q, want target module anchor", cypher)
				}
				if got, want := params["source_module"], "payments.api"; got != want {
					t.Fatalf("params[source_module] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":       "repo-1",
						"source_file":   "src/api.py",
						"target_file":   "src/service.py",
						"source_name":   "charge",
						"source_id":     "function-charge",
						"target_name":   "persist",
						"target_id":     "function-persist",
						"source_module": "payments.api",
						"target_module": "payments.service",
						"call_kind":     "direct",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"cross_module_calls","repo_id":"repo-1","source_module":"payments.api","target_module":"payments.service","limit":10}`),
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
	calls, ok := resp["cross_module_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("cross_module_calls = %#v, want one call", resp["cross_module_calls"])
	}
	if _, ok := resp["dependencies"]; ok {
		t.Fatalf("cross_module_calls response includes non-canonical dependencies key: %#v", resp["dependencies"])
	}
}

func TestHandleImportDependencyInvestigationRejectsUnscopedRequests(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"imports_by_file","limit":25}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleImportDependencyInvestigationRejectsNegativeLimit(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"imports_by_file","repo_id":"repo-1","limit":-1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
				if !strings.Contains(cypher, "ORDER BY repo.id, source_file.relative_path, target_module.name") {
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
				if !strings.Contains(cypher, "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)") {
					t.Fatalf("cypher = %q, want indexed repository anchor before import expansion", cypher)
				}
				if strings.Contains(cypher, "AND repo.id = $repo_id") {
					t.Fatalf("cypher = %q, repo filter must not remain after import expansion", cypher)
				}
				if !strings.Contains(cypher, "repo.name as repo_name") {
					t.Fatalf("cypher = %q, want repository display name projection", cypher)
				}
				if strings.Count(cypher, "MATCH ") != 1 || !strings.Contains(cypher, "LIMIT $scan_limit") {
					t.Fatalf("cypher = %q, want one bounded import-edge read", cypher)
				}
				if got, want := params["cycle_language"], "python"; got != want {
					t.Fatalf("params[cycle_language] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":       "repo-1",
						"repo_name":     "platform-api",
						"source_path":   "/proof/repo-1/src/module_a.py",
						"source_file":   "src/module_a.py",
						"source_name":   "module_a.py",
						"language":      "python",
						"target_module": "module_b",
						"line_number":   4,
					},
					{
						"repo_id":       "repo-1",
						"repo_name":     "platform-api",
						"source_path":   "/proof/repo-1/src/module_b.py",
						"source_file":   "src/module_b.py",
						"source_name":   "module_b.py",
						"language":      "python",
						"target_module": "module_a",
						"line_number":   7,
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
	if got, want := cycle["repo_name"], "platform-api"; got != want {
		t.Fatalf("repo_name = %#v, want %#v", got, want)
	}
	edges, ok := cycle["cycle_edges"].([]any)
	if !ok || len(edges) != 2 {
		t.Fatalf("cycle_edges = %#v, want two import proof edges", cycle["cycle_edges"])
	}
	first := edges[0].(map[string]any)
	if got, want := first["relationship_type"], "IMPORTS"; got != want {
		t.Fatalf("cycle_edges[0].relationship_type = %#v, want %#v", got, want)
	}
	if got, want := first["source_file"], "src/module_a.py"; got != want {
		t.Fatalf("cycle_edges[0].source_file = %#v, want %#v", got, want)
	}
	if got, want := first["line_number"], float64(4); got != want {
		t.Fatalf("cycle_edges[0].line_number = %#v, want %#v", got, want)
	}
}

func TestHandleImportDependencyInvestigationReturnsEmptyFileImportCycles(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"file_import_cycles","repo_id":"repo-1","limit":25}`),
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
	if !ok {
		t.Fatalf("cycles type = %T, want []any", resp["cycles"])
	}
	if len(cycles) != 0 {
		t.Fatalf("len(cycles) = %d, want 0", len(cycles))
	}
	if got, want := resp["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := resp["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestFileImportCycleEdgeRowsCypherPreservesUnscopedRepositoryShape(t *testing.T) {
	t.Parallel()

	cypher := fileImportCycleEdgeRowsCypher(importDependencyRequest{
		QueryType:  "file_import_cycles",
		SourceFile: "src/module_a.py",
		Limit:      6,
	})
	if !strings.Contains(cypher, "MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file:File)") {
		t.Fatalf("cypher = %q, want repository discovery when repo_id is absent", cypher)
	}
	if strings.Contains(cypher, "{id: $repo_id}") || strings.Contains(cypher, "repo.id = $repo_id") || strings.Contains(cypher, "$source_file") {
		t.Fatalf("cypher = %q, must not reference absent repo_id or remove reciprocal edges with a directional file filter", cypher)
	}
	if got := strings.Count(cypher, "MATCH "); got != 1 {
		t.Fatalf("MATCH count = %d, want one connected edge scan", got)
	}
}

func TestFileImportCycleEdgeRowsCypherAnchorsRepositoryBeforeExpansion(t *testing.T) {
	t.Parallel()

	cypher := fileImportCycleEdgeRowsCypher(importDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repository:proof",
		Limit:     6,
	})
	want := "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)"
	if !strings.Contains(cypher, want) {
		t.Fatalf("cypher = %q, want repository anchor %q", cypher, want)
	}
	if got := strings.Count(cypher, "MATCH "); got != 1 {
		t.Fatalf("MATCH count = %d, want one connected edge scan", got)
	}
}

func TestHandleImportDependencyInvestigationTruncatesFileImportCycles(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				if got, want := params["scan_limit"], importDependencyInternalScanLimit+1; got != want {
					t.Fatalf("params[scan_limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"repo_id": "repo-1", "repo_name": "proof", "source_path": "/proof/src/a.py", "source_file": "src/a.py", "source_name": "a.py", "language": "python", "target_module": "b", "line_number": 3},
					{"repo_id": "repo-1", "repo_name": "proof", "source_path": "/proof/src/b.py", "source_file": "src/b.py", "source_name": "b.py", "language": "python", "target_module": "a", "line_number": 5},
					{"repo_id": "repo-1", "repo_name": "proof", "source_path": "/proof/src/c.py", "source_file": "src/c.py", "source_name": "c.py", "language": "python", "target_module": "d", "line_number": 7},
					{"repo_id": "repo-1", "repo_name": "proof", "source_path": "/proof/src/d.py", "source_file": "src/d.py", "source_name": "d.py", "language": "python", "target_module": "c", "line_number": 11},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"file_import_cycles","repo_id":"repo-1","limit":1}`),
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
		t.Fatalf("cycles = %#v, want one truncated cycle", resp["cycles"])
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := resp["next_offset"], float64(1); got != want {
		t.Fatalf("next_offset = %#v, want %#v", got, want)
	}
}

func TestHandleImportDependencyInvestigationReportsUnavailableCycleBackend(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"file_import_cycles","repo_id":"repo-1","limit":25}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

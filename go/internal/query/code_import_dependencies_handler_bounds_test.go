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

func TestHandleImportDependencyInvestigationFailsClosedWhenCycleScanOverflows(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $scan_limit") {
					t.Fatalf("cypher = %q, want bounded internal edge scan", cypher)
				}
				if got, want := params["scan_limit"], importDependencyInternalScanLimit+1; got != want {
					t.Fatalf("params[scan_limit] = %#v, want %#v", got, want)
				}
				return make([]map[string]any, importDependencyInternalScanLimit+1), nil
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

	if got, want := w.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "narrow") {
		t.Fatalf("body = %s, want explicit instruction to narrow scope", w.Body.String())
	}
}

func TestHandleImportDependencyInvestigationUsesBoundedSourceModuleReads(t *testing.T) {
	t.Parallel()

	callCount := 0
	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				callCount++
				switch callCount {
				case 1:
					if !strings.Contains(cypher, "source_module:Module {name: $source_module}") {
						t.Fatalf("first cypher = %q, want source-module membership read", cypher)
					}
					return []map[string]any{{
						"repo_id":       "repo-1",
						"repo_name":     "platform",
						"source_path":   "/proof/repo-1/src/app.py",
						"source_file":   "src/app.py",
						"source_module": "payments.api",
					}}, nil
				case 2:
					if !strings.Contains(cypher, "source_file.path IN $source_paths") {
						t.Fatalf("second cypher = %q, want bounded path candidate read", cypher)
					}
					paths, ok := params["source_paths"].([]string)
					if !ok || len(paths) != 1 || paths[0] != "/proof/repo-1/src/app.py" {
						t.Fatalf("params[source_paths] = %#v, want exact candidate path", params["source_paths"])
					}
					return []map[string]any{{
						"repo_id":       "repo-1",
						"repo_name":     "platform",
						"source_path":   "/proof/repo-1/src/app.py",
						"source_file":   "src/app.py",
						"source_name":   "app.py",
						"language":      "python",
						"target_module": "requests",
						"line_number":   4,
					}}, nil
				default:
					t.Fatalf("unexpected graph call %d", callCount)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"imports_by_file","source_module":"payments.api","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := callCount, 2; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	dependencies, ok := resp["dependencies"].([]any)
	if !ok || len(dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want one dependency", resp["dependencies"])
	}
	dependency := dependencies[0].(map[string]any)
	if got, want := dependency["repo_id"], "repo-1"; got != want {
		t.Fatalf("repo_id = %#v, want %#v", got, want)
	}
	if got, want := dependency["source_module"], "payments.api"; got != want {
		t.Fatalf("source_module = %#v, want %#v", got, want)
	}
}

func TestHandleImportDependencyInvestigationPagesDistinctPackages(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "RETURN DISTINCT") {
					t.Fatalf("cypher = %q, want distinct logical modules before paging", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"repo_id": "repo-1", "target_module": "alpha", "language": "python"},
					{"repo_id": "repo-1", "target_module": "beta", "language": "python"},
					{"repo_id": "repo-1", "target_module": "gamma", "language": "python"},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"package_imports","repo_id":"repo-1","limit":2}`),
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
	modules, ok := resp["modules"].([]any)
	if !ok || len(modules) != 2 {
		t.Fatalf("modules = %#v, want two logical modules", resp["modules"])
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := resp["next_offset"], float64(2); got != want {
		t.Fatalf("next_offset = %#v, want %#v", got, want)
	}
}

func TestHandleImportDependencyInvestigationFiltersUnscopedCrossRepoCalls(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "source_repo.id = target_repo.id") {
					t.Fatalf("cypher = %q, NornicDB equality predicate corrupts the unscoped path", cypher)
				}
				if got, want := params["scan_limit"], importDependencyInternalScanLimit+1; got != want {
					t.Fatalf("params[scan_limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"source_repo_id": "repo-1", "target_repo_id": "repo-2", "source_file": "src/app.py", "target_file": "src/cross.py", "source_id": "cross-source", "target_id": "cross-target"},
					{"source_repo_id": "repo-1", "target_repo_id": "repo-1", "source_file": "src/app.py", "target_file": "src/local.py", "source_id": "local-source", "target_id": "local-target"},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(`{"query_type":"cross_module_calls","source_file":"src/app.py","limit":10}`),
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
		t.Fatalf("cross_module_calls = %#v, want one same-repository call", resp["cross_module_calls"])
	}
}

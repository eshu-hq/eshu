// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportDependencyUniqueModulesPreservesRepositoryAndLanguageIdentity(t *testing.T) {
	t.Parallel()

	modules := importDependencyUniqueModules([]map[string]any{
		{"repo_id": "repo-1", "target_module": "requests", "language": "python"},
		{"repo_id": "repo-1", "target_module": "requests", "language": "python"},
		{"repo_id": "repo-2", "target_module": "requests", "language": "python"},
		{"repo_id": "repo-1", "target_module": "requests", "language": "go"},
	})
	if got, want := len(modules), 3; got != want {
		t.Fatalf("logical modules = %d, want %d: %#v", got, want, modules)
	}
}

func TestUniqueImportDependencyScopesPreservesRepositoryPathIdentity(t *testing.T) {
	t.Parallel()

	scopes := uniqueImportDependencyScopes([]map[string]any{
		{"repo_id": "repo-2", "source_path": "/shared/src/app.py"},
		{"repo_id": "repo-1", "source_path": "/shared/src/app.py"},
		{"repo_id": "repo-1", "source_path": "/shared/src/app.py"},
	}, "source_path")
	if got, want := len(scopes), 2; got != want {
		t.Fatalf("scope count = %d, want %d: %#v", got, want, scopes)
	}
	if got, want := StringVal(scopes[0], "repo_id"), "repo-1"; got != want {
		t.Fatalf("first repo_id = %q, want %q", got, want)
	}
	if got, want := StringVal(scopes[1], "repo_id"), "repo-2"; got != want {
		t.Fatalf("second repo_id = %q, want %q", got, want)
	}
}

func TestBuildFileImportCycleRowsUsesExactDottedModuleNames(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/payments.api.py", "payments.api.py", "payments.client", 4),
		importDependencyCycleProofEdge("src/payments.client.py", "payments.client.py", "payments.api", 8),
		// Prefix-like names must not be mistaken for the reciprocal edge above.
		importDependencyCycleProofEdge("src/payments.py", "payments.py", "payments.client_extra", 12),
		importDependencyCycleProofEdge("src/payments.client_extra.py", "payments.client_extra.py", "payment", 16),
	}

	rows, err := buildFileImportCycleRows(importDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repo-1",
		Limit:     10,
	}, edges)
	if err != nil {
		t.Fatalf("buildFileImportCycleRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("cycle rows = %d, want %d: %#v", got, want, rows)
	}
	if got, want := StringVal(rows[0], "source_module"), "payments.api"; got != want {
		t.Fatalf("source_module = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "target_module"), "payments.client"; got != want {
		t.Fatalf("target_module = %q, want %q", got, want)
	}
}

func importDependencyCycleProofEdge(sourceFile, sourceName, targetModule string, line int) map[string]any {
	return map[string]any{
		"repo_id":       "repo-1",
		"repo_name":     "proof",
		"source_path":   "/proof/" + sourceFile,
		"source_file":   sourceFile,
		"source_name":   sourceName,
		"language":      "python",
		"target_module": targetModule,
		"line_number":   line,
	}
}

func TestImportDependencyCypherSupportsCanonicalAndLegacyFileLanguageProperties(t *testing.T) {
	t.Parallel()

	for name, cypher := range map[string]string{
		"direct imports": directImportRowsCypher(importDependencyRequest{RepoID: "repo-1", Language: "python"}),
		"cycle edges":    fileImportCycleEdgeRowsCypher(importDependencyRequest{RepoID: "repo-1"}),
		"cross calls": crossModuleCallRowsCypher(
			importDependencyRequest{RepoID: "repo-1", Language: "python"},
			nil,
			nil,
		),
	} {
		if !strings.Contains(cypher, ".language") || !strings.Contains(cypher, ".lang") {
			t.Errorf("%s cypher = %q, want canonical lang and legacy language support", name, cypher)
		}
	}
}

func TestHandleImportDependencyInvestigationSkipsImportReadForEmptySourceModule(t *testing.T) {
	t.Parallel()

	callCount := 0
	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		cypher string,
		_ map[string]any,
	) ([]map[string]any, error) {
		callCount++
		if !strings.Contains(cypher, "source_module:Module {name: $source_module}") {
			t.Fatalf("cypher = %q, want source-module membership read", cypher)
		}
		return nil, nil
	}}}

	response := serveImportDependencyRequest(t, handler, `{"query_type":"imports_by_file","source_module":"missing.module","limit":10}`)
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
	if got, want := callCount, 1; got != want {
		t.Fatalf("graph calls = %d, want membership-only %d", got, want)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

func TestHandleImportDependencyInvestigationFiltersRepositoryPathCollisions(t *testing.T) {
	t.Parallel()

	callCount := 0
	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		_ string,
		_ map[string]any,
	) ([]map[string]any, error) {
		callCount++
		if callCount == 1 {
			return []map[string]any{{
				"repo_id": "repo-1", "source_path": "/shared/src/app.py",
			}}, nil
		}
		return []map[string]any{
			{
				"repo_id": "repo-2", "source_path": "/shared/src/app.py",
				"source_file": "src/app.py", "target_module": "wrong-repository",
			},
			{
				"repo_id": "repo-1", "source_path": "/shared/src/app.py",
				"source_file": "src/app.py", "target_module": "correct-repository",
			},
		}, nil
	}}}

	response := serveImportDependencyRequest(
		t,
		handler,
		`{"query_type":"module_dependencies","source_module":"payments.api","limit":10}`,
	)
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	dependencies, ok := body["dependencies"].([]any)
	if !ok || len(dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want one exact repository-path result", body["dependencies"])
	}
	row, ok := dependencies[0].(map[string]any)
	if !ok || row["repo_id"] != "repo-1" || row["target_module"] != "correct-repository" {
		t.Fatalf("dependency = %#v, want repo-1 result", dependencies[0])
	}
	if _, leaked := row["source_path"]; leaked {
		t.Fatalf("dependency leaked internal absolute source_path: %#v", row)
	}
}

func TestHandleImportDependencyInvestigationFailsClosedWhenModuleMembershipOverflows(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		_ string,
		_ map[string]any,
	) ([]map[string]any, error) {
		return make([]map[string]any, importDependencyInternalScanLimit+1), nil
	}}}

	response := serveImportDependencyRequest(t, handler, `{"query_type":"imports_by_file","source_module":"wide.module","limit":10}`)
	if got, want := response.Code, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
	if !strings.Contains(strings.ToLower(response.Body.String()), "narrow") {
		t.Fatalf("body = %s, want instruction to narrow scope", response.Body.String())
	}
}

func TestHandleImportDependencyInvestigationReportsGraphReadErrors(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(
		_ context.Context,
		_ string,
		_ map[string]any,
	) ([]map[string]any, error) {
		return nil, errors.New("proof graph read failed")
	}}}

	response := serveImportDependencyRequest(t, handler, `{"query_type":"imports_by_file","repo_id":"repo-1","limit":10}`)
	if got, want := response.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, response.Body.String())
	}
}

func serveImportDependencyRequest(
	t *testing.T,
	handler *CodeHandler,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/imports/investigate",
		bytes.NewBufferString(body),
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

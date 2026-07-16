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

type argoCDCategoryGraph struct {
	calls []recordedInfraCall
}

func (g *argoCDCategoryGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.calls = append(g.calls, recordedInfraCall{Cypher: cypher, Params: params})
	if strings.Contains(cypher, "MATCH (n:ArgoCDApplicationSet)") {
		return []map[string]any{
			{"id": "set-z", "name": "zeta", "labels": []any{"ArgoCDApplicationSet"}},
			{"id": "set-a", "name": "aardvark", "labels": []any{"ArgoCDApplicationSet"}},
		}, nil
	}
	return []map[string]any{
		{"id": "app-b", "name": "beta", "labels": []any{"ArgoCDApplication"}},
		{"id": "app-a", "name": "alpha", "labels": []any{"ArgoCDApplication"}},
	}, nil
}

func (*argoCDCategoryGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func TestSearchInfraResourcesUsesBoundedLabelReadsForArgoCDCategory(t *testing.T) {
	t.Parallel()

	graph := &argoCDCategoryGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"category":"argocd","limit":2}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(graph.calls), 2; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}
	for _, call := range graph.calls {
		if strings.Contains(call.Cypher, "n:ArgoCDApplication OR") {
			t.Fatalf("Cypher retained broad OR-label scan:\n%s", call.Cypher)
		}
		if got, want := call.Params["limit"], 3; got != want {
			t.Fatalf("params[limit] = %#v, want %#v", got, want)
		}
	}
	if !strings.Contains(graph.calls[0].Cypher, "MATCH (n:ArgoCDApplication)") {
		t.Fatalf("first Cypher is not label anchored:\n%s", graph.calls[0].Cypher)
	}
	if !strings.Contains(graph.calls[1].Cypher, "MATCH (n:ArgoCDApplicationSet)") ||
		!strings.Contains(graph.calls[1].Cypher, "NOT n:ArgoCDApplication") {
		t.Fatalf("second Cypher does not exclude dual-labeled nodes:\n%s", graph.calls[1].Cypher)
	}

	var payload struct {
		Count     int              `json:"count"`
		Results   []map[string]any `json:"results"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := payload.Count, 2; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if !payload.Truncated {
		t.Fatal("truncated = false, want true")
	}
	gotNames := []string{
		payload.Results[0]["name"].(string),
		payload.Results[1]["name"].(string),
	}
	if strings.Join(gotNames, ",") != "aardvark,alpha" {
		t.Fatalf("result names = %v, want globally ordered bounded merge", gotNames)
	}
}

func TestSearchInfraResourcesArgoCDLabelReadsPreserveRepositoryScope(t *testing.T) {
	t.Parallel()

	graph := &argoCDCategoryGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"category":"argocd","limit":2}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
		AllowedScopeIDs:      []string{"git-repository-scope:team-a"},
	}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(graph.calls), 2; got != want {
		t.Fatalf("graph calls = %d, want %d", got, want)
	}
	for _, call := range graph.calls {
		if !strings.Contains(call.Cypher, "n.repo_id IN $allowed_repository_ids") {
			t.Fatalf("scoped Argo CD Cypher missing repository predicate:\n%s", call.Cypher)
		}
		if _, ok := call.Params["allowed_repository_ids"]; !ok {
			t.Fatalf("scoped params missing allowed_repository_ids: %v", call.Params)
		}
		if _, ok := call.Params["allowed_scope_ids"]; !ok {
			t.Fatalf("scoped params missing allowed_scope_ids: %v", call.Params)
		}
	}
}

func TestSearchInfraResourcesArgoCDWithAdditionalFilterKeepsGenericQuery(t *testing.T) {
	t.Parallel()

	graph := &argoCDCategoryGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"category":"argocd","query":"payments","limit":2}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(graph.calls), 1; got != want {
		t.Fatalf("graph calls = %d, want generic single read", got)
	}
	// The generic (non-argocd-only) path unions one MATCH(n:Label) branch
	// per label instead of a single MATCH(n) filtered by an (n:A OR n:B)
	// predicate — the same whole-graph-scan defect fixed generally for
	// every category, not only the bare argocd-with-no-other-filter shape
	// isArgoCDCategoryOnly special-cases. See infra.go's searchResources
	// comment for the two documented reasons (whole-graph scan cost, and
	// NornicDB's label-disjunction MATCH bug).
	cypher := graph.calls[0].Cypher
	if strings.Contains(cypher, "n:ArgoCDApplication OR n:ArgoCDApplicationSet") {
		t.Fatalf("generic filtered Cypher retained broad OR-label scan:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (n:ArgoCDApplication)") || !strings.Contains(cypher, "MATCH (n:ArgoCDApplicationSet)") {
		t.Fatalf("generic filtered Cypher is not label anchored per branch:\n%s", cypher)
	}
	if !strings.Contains(cypher, "\nUNION") {
		t.Fatalf("generic filtered Cypher does not union per-label branches:\n%s", cypher)
	}
}

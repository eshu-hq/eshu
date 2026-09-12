// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// iacResourceRepoNode is this package's own copy of root's
// resources_scope_auth_test.go's identically named fixture (#6642 Part A):
// iacResourceNode plus a durable repo_id, the field the scoped predicate
// binds against. Both copies exist because root's copy stays there (it needs
// the genuine production AuthMiddlewareWithScopedTokens, package query,
// which this leaf cannot import) while these current-inventory tests moved
// here with resources.go's listResources.
func iacResourceRepoNode(id, name, resourceType, provider, repoID string) map[string]any {
	node := iacResourceNode(id, name, resourceType, provider)
	node["repo_id"] = repoID
	node["generation_id"] = "generation-active"
	return node
}

type stubIaCInventoryStore struct {
	candidates   []InventoryCandidate
	summary      InventorySummary
	lastSearch   InventorySearch
	lastAccess   querycontract.RepositoryAccessFilter
	searchCalls  int
	summaryCalls int
}

func newIaCResourceTestHandler(graph querycontract.GraphQuery) *Handler {
	inventory := &stubIaCInventoryStore{}
	if stub, ok := graph.(*stubIaCResourceGraph); ok {
		for _, row := range stub.rows {
			if querycontract.StringVal(row, "generation_id") == "" {
				row["generation_id"] = "generation-active"
			}
			inventory.candidates = append(inventory.candidates, InventoryCandidate{
				ID:           querycontract.StringVal(row, "id"),
				Name:         querycontract.StringVal(row, "name"),
				GenerationID: querycontract.StringVal(row, "generation_id"),
			})
		}
	}
	return &Handler{
		Graph:     graph,
		Inventory: inventory,
	}
}

func (s *stubIaCInventoryStore) SearchActive(
	_ context.Context,
	search InventorySearch,
	access querycontract.RepositoryAccessFilter,
) ([]InventoryCandidate, error) {
	s.searchCalls++
	s.lastSearch = search
	s.lastAccess = access
	return append([]InventoryCandidate(nil), s.candidates...), nil
}

func (s *stubIaCInventoryStore) Summary(
	_ context.Context,
	access querycontract.RepositoryAccessFilter,
	limit int,
) (InventorySummary, error) {
	s.summaryCalls++
	s.lastAccess = access
	s.summary.FacetLimit = limit
	return s.summary, nil
}

func TestIaCResourceQueryHydratesOnlyIndexedCurrentCandidates(t *testing.T) {
	t.Parallel()

	filter := resourceFilter{
		Kind:         resourceKindResource,
		CandidateIDs: []string{"content-entity:e_1"},
		Limit:        11,
	}
	cypher, params := buildIaCResourceQuery(filter)

	if !strings.Contains(cypher, "WHERE n.uid IN $candidate_ids") {
		t.Fatalf("cypher missing indexed candidate anchor: %s", cypher)
	}
	for _, forbidden := range []string{"active_generation_ids", "n.id IN", "n.repo_id =", "n.resource_type ="} {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("cypher contains unproven post-anchor predicate %q: %s", forbidden, cypher)
		}
	}
	if got := params["candidate_ids"]; !reflect.DeepEqual(got, []string{"content-entity:e_1"}) {
		t.Fatalf("candidate_ids = %#v, want [content-entity:e_1]", got)
	}
}

func TestIaCResourcesSearchUsesCurrentInventoryCandidatesAndGraphHydration(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		candidates: []InventoryCandidate{
			{ID: "content-entity:e_1", Name: "aws_s3_bucket.logs", GenerationID: "generation-active"},
		},
	}
	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode(
			"content-entity:e_1",
			"aws_s3_bucket.logs",
			"aws_s3_bucket",
			"aws",
			"repository:r_active",
		),
	}}
	handler := &Handler{Graph: graph, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/iac/resources?kind=resource&q=logs&repository=repository%3Ar_active&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if inventory.searchCalls != 1 {
		t.Fatalf("SearchActive calls = %d, want 1", inventory.searchCalls)
	}
	if got, want := inventory.lastSearch.Query, "logs"; got != want {
		t.Fatalf("search query = %q, want %q", got, want)
	}
	if got, want := inventory.lastSearch.Repository, "repository:r_active"; got != want {
		t.Fatalf("search repository = %q, want %q", got, want)
	}
	if got, want := inventory.lastSearch.Kind, resourceKindResource; got != want {
		t.Fatalf("search kind = %q, want %q", got, want)
	}
	if !strings.Contains(graph.lastCypher, "n.uid IN $candidate_ids") {
		t.Fatalf("graph hydration must bind current candidate ids: %s", graph.lastCypher)
	}
	if strings.Contains(graph.lastCypher, "n.generation_id IN") {
		t.Fatalf("graph hydration contains an unmeasured post-anchor predicate: %s", graph.lastCypher)
	}
}

func TestIaCResourcesFailsWhenCurrentSearchCandidateIsMissingFromGraph(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		candidates: []InventoryCandidate{
			{ID: "content-entity:e_missing", Name: "aws_s3_bucket.missing", GenerationID: "generation-active"},
		},
	}
	handler := &Handler{Graph: &stubIaCResourceGraph{}, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?q=missing&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "current inventory and graph projection disagree") {
		t.Fatalf("body missing exactness failure: %s", w.Body.String())
	}
}

func TestIaCResourcesFailsWhenCurrentSearchCandidateNameDiffersFromGraph(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		candidates: []InventoryCandidate{
			{ID: "content-entity:e_1", Name: "aws_s3_bucket.current", GenerationID: "generation-active"},
		},
	}
	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode(
			"content-entity:e_1",
			"aws_s3_bucket.stale",
			"aws_s3_bucket",
			"aws",
			"repository:r_active",
		),
	}}
	handler := &Handler{Graph: graph, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?q=bucket&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "current inventory and graph projection disagree") {
		t.Fatalf("body missing exactness failure: %s", w.Body.String())
	}
}

func TestIaCResourcesFailsWhenCurrentSearchCandidateGenerationDiffersFromGraph(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		candidates: []InventoryCandidate{
			{ID: "content-entity:e_1", Name: "aws_s3_bucket.current", GenerationID: "generation-active"},
		},
	}
	row := iacResourceRepoNode(
		"content-entity:e_1",
		"aws_s3_bucket.current",
		"aws_s3_bucket",
		"aws",
		"repository:r_active",
	)
	row["generation_id"] = "generation-stale"
	handler := &Handler{Graph: &stubIaCResourceGraph{rows: []map[string]any{row}}, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?q=bucket&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "current inventory and graph projection disagree") {
		t.Fatalf("body missing exactness failure: %s", w.Body.String())
	}
}

func TestIaCResourcesSkipsGraphWhenNoActiveGenerationExists(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		summary: InventorySummary{
			ByKind: make(map[resourceKind]int),
		},
	}
	graph := &stubIaCResourceGraph{rows: []map[string]any{
		iacResourceRepoNode(
			"content-entity:e_historical",
			"aws_s3_bucket.historical",
			"aws_s3_bucket",
			"aws",
			"repository:r_old",
		),
	}}
	handler := &Handler{Graph: graph, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/iac/resources?include_facets=true&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.calls != 0 {
		t.Fatalf("graph calls = %d, want 0 without an active generation", graph.calls)
	}
	if !strings.Contains(w.Body.String(), `"resources":[]`) ||
		!strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatalf("response missing authoritative empty current inventory: %s", w.Body.String())
	}
}

func TestIaCResourcesEmptySearchStillReturnsInventorySummary(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		summary: InventorySummary{Total: 7, ByKind: map[resourceKind]int{
			resourceKindResource: 7,
		}},
	}
	graph := &stubIaCResourceGraph{}
	handler := &Handler{Graph: graph, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/iac/resources?q=does-not-exist&include_facets=true&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.calls != 0 {
		t.Fatalf("graph calls = %d, want 0 for an empty current candidate set", graph.calls)
	}
	if inventory.summaryCalls != 1 || !strings.Contains(w.Body.String(), `"total":7`) {
		t.Fatalf("response missing authoritative empty-search summary: %s", w.Body.String())
	}
}

func TestIaCResourcesReturnsAuthoritativeBoundedFacets(t *testing.T) {
	t.Parallel()

	inventory := &stubIaCInventoryStore{
		summary: InventorySummary{
			Total: 24610,
			ByKind: map[resourceKind]int{
				resourceKindResource:   17117,
				resourceKindModule:     612,
				resourceKindDataSource: 6881,
			},
			Types:        []InventoryFacet{{Kind: resourceKindResource, Value: "aws_s3_bucket", Count: 500}},
			Providers:    []InventoryFacet{{Kind: resourceKindResource, Value: "aws", Count: 1000}},
			Modules:      []InventoryFacet{{Kind: resourceKindModule, Value: "vpc", Count: 25}},
			Repositories: []InventoryFacet{{Value: "repository:r_active", Count: 100}},
			Truncated:    map[string]bool{"types": true},
		},
	}
	graph := &stubIaCResourceGraph{}
	handler := &Handler{Graph: graph, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=10&include_facets=true", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if inventory.summaryCalls != 1 {
		t.Fatalf("Summary calls = %d, want 1", inventory.summaryCalls)
	}
	if !strings.Contains(w.Body.String(), `"total":24610`) ||
		!strings.Contains(w.Body.String(), `"repository:r_active"`) ||
		!strings.Contains(w.Body.String(), `"truncated":{"types":true}`) ||
		!strings.Contains(w.Body.String(), `"basis":"hybrid"`) {
		t.Fatalf("response missing authoritative summary/facets: %s", w.Body.String())
	}
}

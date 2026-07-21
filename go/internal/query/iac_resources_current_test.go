// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type stubIaCInventoryStore struct {
	candidates   []iacInventoryCandidate
	summary      iacInventorySummary
	lastSearch   iacInventorySearch
	lastAccess   repositoryAccessFilter
	searchCalls  int
	summaryCalls int
}

func newIaCResourceTestHandler(graph GraphQuery) *IaCHandler {
	inventory := &stubIaCInventoryStore{}
	if stub, ok := graph.(*stubIaCResourceGraph); ok {
		for _, row := range stub.rows {
			if StringVal(row, "generation_id") == "" {
				row["generation_id"] = "generation-active"
			}
			inventory.candidates = append(inventory.candidates, iacInventoryCandidate{
				ID:           StringVal(row, "id"),
				Name:         StringVal(row, "name"),
				GenerationID: StringVal(row, "generation_id"),
			})
		}
	}
	return &IaCHandler{
		Graph:     graph,
		Inventory: inventory,
	}
}

func (s *stubIaCInventoryStore) SearchActive(
	_ context.Context,
	search iacInventorySearch,
	access repositoryAccessFilter,
) ([]iacInventoryCandidate, error) {
	s.searchCalls++
	s.lastSearch = search
	s.lastAccess = access
	return append([]iacInventoryCandidate(nil), s.candidates...), nil
}

func (s *stubIaCInventoryStore) Summary(
	_ context.Context,
	access repositoryAccessFilter,
	limit int,
) (iacInventorySummary, error) {
	s.summaryCalls++
	s.lastAccess = access
	s.summary.FacetLimit = limit
	return s.summary, nil
}

func TestIaCResourceQueryHydratesOnlyIndexedCurrentCandidates(t *testing.T) {
	t.Parallel()

	filter := iacResourceFilter{
		Kind:         iacResourceKindResource,
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
		candidates: []iacInventoryCandidate{
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
	handler := &IaCHandler{Graph: graph, Inventory: inventory}
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
	if got, want := inventory.lastSearch.Kind, iacResourceKindResource; got != want {
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
		candidates: []iacInventoryCandidate{
			{ID: "content-entity:e_missing", Name: "aws_s3_bucket.missing", GenerationID: "generation-active"},
		},
	}
	handler := &IaCHandler{Graph: &stubIaCResourceGraph{}, Inventory: inventory}
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
		candidates: []iacInventoryCandidate{
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
	handler := &IaCHandler{Graph: graph, Inventory: inventory}
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
		candidates: []iacInventoryCandidate{
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
	handler := &IaCHandler{Graph: &stubIaCResourceGraph{rows: []map[string]any{row}}, Inventory: inventory}
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
		summary: iacInventorySummary{
			ByKind: make(map[iacResourceKind]int),
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
	handler := &IaCHandler{Graph: graph, Inventory: inventory}
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
		summary: iacInventorySummary{Total: 7, ByKind: map[iacResourceKind]int{
			iacResourceKindResource: 7,
		}},
	}
	graph := &stubIaCResourceGraph{}
	handler := &IaCHandler{Graph: graph, Inventory: inventory}
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
		summary: iacInventorySummary{
			Total: 24610,
			ByKind: map[iacResourceKind]int{
				iacResourceKindResource:   17117,
				iacResourceKindModule:     612,
				iacResourceKindDataSource: 6881,
			},
			Types:        []iacInventoryFacet{{Kind: iacResourceKindResource, Value: "aws_s3_bucket", Count: 500}},
			Providers:    []iacInventoryFacet{{Kind: iacResourceKindResource, Value: "aws", Count: 1000}},
			Modules:      []iacInventoryFacet{{Kind: iacResourceKindModule, Value: "vpc", Count: 25}},
			Repositories: []iacInventoryFacet{{Value: "repository:r_active", Count: 100}},
			Truncated:    map[string]bool{"types": true},
		},
	}
	graph := &stubIaCResourceGraph{}
	handler := &IaCHandler{Graph: graph, Inventory: inventory}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/iac/resources?limit=10&include_facets=true", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
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

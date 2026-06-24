// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// semanticSearchDocWithHandles builds a derived search document carrying the
// given graph handles for reranking tests.
func semanticSearchDocWithHandles(
	id, repoID, title, contextText string,
	handles ...searchdocs.GraphHandle,
) searchdocs.Document {
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindRuntimeSummary,
		Title:       title,
		Path:        "docs/runbook.md",
		ContextText: contextText,
		UpdatedAt:   time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC),
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisReadModel,
		},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		AccessScope:  searchdocs.AccessScope{RepoID: repoID},
		GraphHandles: handles,
		Labels:       []string{"runtime"},
		Provenance: searchdocs.Provenance{
			SourceTable: "service_runtime_summaries",
			SourceIDs:   []string{id},
		},
	}
}

func TestSemanticSearchHandlerRerankPromotesServiceAnchoredResult(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 2,
			Candidates: []searchretrieval.Candidate{
				{
					// Higher lexical score, but anchored to a different service.
					Document: semanticSearchDocWithHandles(
						"searchdoc:unrelated", "repo-payments", "Unrelated runbook",
						"payment runbook ownership",
						searchdocs.GraphHandle{Kind: "repository", ID: "repo-payments"},
						searchdocs.GraphHandle{Kind: "service", ID: "svc-other"},
					),
					Score:    3.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
				{
					// Lower lexical score, but anchored to the requested service.
					Document: semanticSearchDocWithHandles(
						"searchdoc:payments", "repo-payments", "Payments service runbook",
						"payment runbook ownership",
						searchdocs.GraphHandle{Kind: "repository", ID: "repo-payments"},
						searchdocs.GraphHandle{Kind: "service", ID: "svc-payments"},
						searchdocs.GraphHandle{Kind: "incident", ID: "inc-42"},
					),
					Score:    1.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
			},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
		"service_id": "svc-payments",
		"rerank":     true,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := envelope.Data.(map[string]any)

	rerank, ok := data["rerank"].(map[string]any)
	if !ok {
		t.Fatalf("response missing rerank block: %#v", data["rerank"])
	}
	if got, want := rerank["state"], "applied"; got != want {
		t.Fatalf("rerank.state = %#v, want %#v", got, want)
	}
	if got, want := rerank["applied"], true; got != want {
		t.Fatalf("rerank.applied = %#v, want %#v", got, want)
	}

	results := data["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	top := results[0].(map[string]any)
	if got, want := top["document"].(map[string]any)["id"], "searchdoc:payments"; got != want {
		t.Fatalf("top result id = %#v, want %#v (service-anchored promotion)", got, want)
	}
	if got, want := top["rank"], float64(1); got != want {
		t.Fatalf("top rank = %#v, want 1", got)
	}

	basis, ok := top["ranking_basis"].(map[string]any)
	if !ok {
		t.Fatalf("top result missing ranking_basis: %#v", top["ranking_basis"])
	}
	if got, want := basis["baseline_rank"], float64(2); got != want {
		t.Fatalf("baseline_rank = %#v, want 2", got)
	}
	if got, want := basis["final_rank"], float64(1); got != want {
		t.Fatalf("final_rank = %#v, want 1", got)
	}
	if got, want := basis["baseline_score"], float64(1.0); got != want {
		t.Fatalf("baseline_score = %#v, want 1.0 (preserved)", got)
	}
	contributions := basis["contributions"].([]any)
	foundService := false
	for _, c := range contributions {
		cm := c.(map[string]any)
		if cm["kind"] == "service_anchor" {
			foundService = true
			if got, want := cm["handle"], "service:svc-payments"; got != want {
				t.Fatalf("service contribution handle = %#v, want %#v", got, want)
			}
		}
	}
	if !foundService {
		t.Fatalf("expected service_anchor contribution, got %#v", contributions)
	}

	nextCalls, ok := data["recommended_next_calls"].([]any)
	if !ok || len(nextCalls) == 0 {
		t.Fatalf("expected recommended_next_calls, got %#v", data["recommended_next_calls"])
	}
	tools := map[string]map[string]any{}
	for _, call := range nextCalls {
		c := call.(map[string]any)
		args, _ := c["arguments"].(map[string]any)
		tools[c["tool"].(string)] = args
	}
	if _, ok := tools["get_service_story"]; !ok {
		t.Fatalf("expected get_service_story next call, got %#v", tools)
	}
	if _, ok := tools["get_incident_context"]; !ok {
		t.Fatalf("expected get_incident_context next call from incident handle, got %#v", tools)
	}
	// Next-call arguments must use the tools' real dispatch keys so they are
	// executable as advertised (get_service_story reads workload_id, not service).
	if got := tools["get_service_story"]["workload_id"]; got != "svc-payments" {
		t.Fatalf("get_service_story must use executable workload_id key, got %#v", tools["get_service_story"])
	}
	if got := tools["get_incident_context"]["incident_id"]; got != "inc-42" {
		t.Fatalf("get_incident_context must use executable incident_id key, got %#v", tools["get_incident_context"])
	}
}

func TestSemanticSearchHandlerRerankOmittedByDefault(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 1,
			Candidates: []searchretrieval.Candidate{
				{
					Document: semanticSearchDocWithHandles(
						"searchdoc:payments", "repo-payments", "Payments runbook",
						"payment runbook",
						searchdocs.GraphHandle{Kind: "service", ID: "svc-payments"},
					),
					Score:    2.0,
					Metadata: map[string]string{"search_method": "bm25"},
				},
			},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
		"service_id": "svc-payments",
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := envelope.Data.(map[string]any)
	if _, ok := data["rerank"]; ok {
		t.Fatalf("rerank block must be absent when rerank is not requested: %#v", data["rerank"])
	}
	if _, ok := data["recommended_next_calls"]; ok {
		t.Fatalf("recommended_next_calls must be absent without rerank: %#v", data["recommended_next_calls"])
	}
	result := data["results"].([]any)[0].(map[string]any)
	if _, ok := result["ranking_basis"]; ok {
		t.Fatalf("ranking_basis must be absent without rerank: %#v", result["ranking_basis"])
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

func TestSemanticSearchHandlerConfiguredSemanticModeUsesLocalVectorRetrieval(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{
			{Document: semanticSearchDocumentFixture("searchdoc:billing", "repo-payments", "Billing", "invoice ledger")},
			{Document: semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "checkout payment refund")},
		},
	}
	handler := &SemanticSearchHandler{
		Index:       index,
		LocalHybrid: NewLocalSemanticSearchHybrid(documents),
		Profile:     ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "refund",
		"mode":       "semantic",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := index.calls, 0; got != want {
		t.Fatalf("persisted index calls = %d, want %d", got, want)
	}
	if got, want := documents.calls, 1; got != want {
		t.Fatalf("document store calls = %d, want %d", got, want)
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["retrieval_state"], "semantic_active"; got != want {
		t.Fatalf("retrieval_state = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if len(results) == 0 {
		t.Fatal("results empty, want vector result")
	}
	result := results[0].(map[string]any)
	if got, want := result["search_method"], "vector"; got != want {
		t.Fatalf("search_method = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerConfiguredHybridReportsHybridParticipation(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{}
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{
			{Document: semanticSearchDocumentFixture("searchdoc:billing", "repo-payments", "Billing", "invoice ledger")},
			{Document: semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "payment refund payment")},
		},
	}
	handler := &SemanticSearchHandler{
		Index:       index,
		LocalHybrid: NewLocalSemanticSearchHybrid(documents),
		Profile:     ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment refund",
		"mode":       "hybrid",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["retrieval_state"], "hybrid_active"; got != want {
		t.Fatalf("retrieval_state = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	if len(results) == 0 {
		t.Fatal("results empty, want hybrid result")
	}
	result := results[0].(map[string]any)
	if got, want := result["search_method"], "rrf_hybrid"; got != want {
		t.Fatalf("search_method = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerHybridWithoutLocalEmbedderReportsDegradedKeywordState(t *testing.T) {
	t.Parallel()

	index := &fakeSemanticSearchIndexStore{
		result: semanticSearchIndexResult{
			IndexedDocumentCount: 1,
			Candidates: []searchretrieval.Candidate{{
				Document: semanticSearchDocumentFixture("searchdoc:payments", "repo-payments", "Payments", "payment refund"),
				Score:    2,
				Metadata: map[string]string{"search_method": "bm25"},
			}},
		},
	}
	handler := &SemanticSearchHandler{Index: index, Profile: ProfileProduction}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment refund",
		"mode":       "hybrid",
		"limit":      5,
		"timeout_ms": 250,
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	data := semanticSearchEnvelopeData(t, rec)
	if got, want := data["retrieval_state"], "hybrid_degraded"; got != want {
		t.Fatalf("retrieval_state = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchHandlerConfiguredHybridPreservesRepoScopeAndSourceKinds(t *testing.T) {
	t.Parallel()

	documents := &fakeSemanticSearchDocumentStore{}
	handler := &SemanticSearchHandler{
		Index:       &fakeSemanticSearchIndexStore{},
		LocalHybrid: NewLocalSemanticSearchHybrid(documents),
		Profile:     ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":      "repo-payments",
		"service_id":   "svc-payments",
		"query":        "payment",
		"mode":         "semantic",
		"limit":        3,
		"timeout_ms":   250,
		"source_kinds": []string{"runtime_summary"},
	})
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	if got, want := documents.query.ScopeID, "repo-payments"; got != want {
		t.Fatalf("document ScopeID = %q, want %q", got, want)
	}
	if got, want := documents.query.RepoID, "repo-payments"; got != want {
		t.Fatalf("document RepoID = %q, want %q", got, want)
	}
	if got, want := documents.query.SourceKinds[0], searchdocs.SourceKindRuntimeSummary; got != want {
		t.Fatalf("source kind = %q, want %q", got, want)
	}
}

type fakeSemanticSearchDocumentStore struct {
	rows  []semanticSearchDocumentRow
	query semanticSearchDocumentQuery
	calls int
	err   error
}

func (s *fakeSemanticSearchDocumentStore) ListActiveDocuments(
	_ context.Context,
	query semanticSearchDocumentQuery,
) ([]semanticSearchDocumentRow, error) {
	s.calls++
	s.query = query
	if s.err != nil {
		return nil, s.err
	}
	rows := append([]semanticSearchDocumentRow(nil), s.rows...)
	return rows, nil
}

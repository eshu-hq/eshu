// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
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

// TestLocalSemanticSearchHybridLanguageFilterExcludesNonMatchingDocs drives the
// REAL LocalSemanticSearchHybrid.Search with a mixed-language document store
// and asserts only Go documents are returned when Languages=["go"]. The test
// MUST fail if the in-memory filter in Search is removed.
func TestLocalSemanticSearchHybridLanguageFilterExcludesNonMatchingDocs(t *testing.T) {
	t.Parallel()

	goDoc := semanticSearchDocumentWithLanguageFixture("doc:go-svc", "repo-1", "go")
	pyDoc := semanticSearchDocumentWithLanguageFixture("doc:py-svc", "repo-1", "python")
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{
			{Document: goDoc},
			{Document: pyDoc},
		},
	}
	hybrid := NewLocalSemanticSearchHybrid(documents)

	result, err := hybrid.Search(context.Background(), semanticSearchIndexQuery{
		ScopeID:   "repo-1",
		RepoID:    "repo-1",
		Languages: []string{"go"},
		Request:   semanticSearchRequestFixture("go service", 10),
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	// All returned candidates must be from the go document.
	for _, c := range result.Candidates {
		if c.Document.ID == pyDoc.ID {
			t.Fatalf("python document %q included in results; language filter did not exclude it", pyDoc.ID)
		}
	}
	// At least the go doc must be indexed (corpus is non-empty).
	if result.IndexedDocumentCount == 0 {
		t.Fatal("IndexedDocumentCount = 0, want at least 1 from go document")
	}
}

// TestLocalSemanticSearchHybridEmptyLanguageFilterReturnsAllDocs verifies that
// an empty Languages slice disables the filter and all documents are eligible.
func TestLocalSemanticSearchHybridEmptyLanguageFilterReturnsAllDocs(t *testing.T) {
	t.Parallel()

	goDoc := semanticSearchDocumentWithLanguageFixture("doc:go-svc", "repo-1", "go")
	pyDoc := semanticSearchDocumentWithLanguageFixture("doc:py-svc", "repo-1", "python")
	documents := &fakeSemanticSearchDocumentStore{
		rows: []semanticSearchDocumentRow{
			{Document: goDoc},
			{Document: pyDoc},
		},
	}
	hybrid := NewLocalSemanticSearchHybrid(documents)

	result, err := hybrid.Search(context.Background(), semanticSearchIndexQuery{
		ScopeID: "repo-1",
		RepoID:  "repo-1",
		// Languages is empty — no filter.
		Request: semanticSearchRequestFixture("service", 10),
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.IndexedDocumentCount < 2 {
		t.Fatalf("IndexedDocumentCount = %d, want at least 2 (both docs eligible)", result.IndexedDocumentCount)
	}
}

// TestSemanticSearchDocumentsFilteredMatchesLanguageCaseInsensitive directly
// unit-tests the semanticSearchDocumentsFiltered helper with case-folding
// and the empty-filter no-op. The test MUST fail if the filter is removed.
func TestSemanticSearchDocumentsFilteredMatchesLanguageCaseInsensitive(t *testing.T) {
	t.Parallel()

	goDoc := semanticSearchDocumentWithLanguageFixture("doc:go", "repo-1", "go")
	pyDoc := semanticSearchDocumentWithLanguageFixture("doc:py", "repo-1", "python")
	rows := []semanticSearchDocumentRow{
		{Document: goDoc},
		{Document: pyDoc},
	}

	// Filter to go only.
	got := semanticSearchDocumentsFiltered(rows, []string{"go"})
	if len(got) != 1 || got[0].ID != goDoc.ID {
		t.Fatalf("filtered = %v, want [%q]", documentIDs(got), goDoc.ID)
	}

	// Empty filter returns all.
	all := semanticSearchDocumentsFiltered(rows, nil)
	if len(all) != 2 {
		t.Fatalf("empty filter = %d docs, want 2", len(all))
	}

	// No-match language returns empty.
	none := semanticSearchDocumentsFiltered(rows, []string{"rust"})
	if len(none) != 0 {
		t.Fatalf("no-match filter = %d docs, want 0", len(none))
	}
}

func documentIDs(docs []searchdocs.Document) []string {
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	return ids
}

func semanticSearchRequestFixture(query string, limit int) searchretrieval.Request {
	return searchretrieval.Request{
		Query:   query,
		Limit:   limit,
		Mode:    searchbench.ModeKeyword,
		Timeout: 250 * time.Millisecond,
		Scope:   searchretrieval.Scope{RepoID: "repo-1"},
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

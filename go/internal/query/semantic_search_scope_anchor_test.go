// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// semanticSearchAnchorDocuments returns a small corpus whose documents carry the
// CANONICAL repository id, which is what the projector stores in
// searchdocs.Document.RepoID and what the persisted index stores in
// eshu_search_index_documents.repo_id.
func semanticSearchAnchorDocuments(repoID string) []semanticSearchDocumentRow {
	return []semanticSearchDocumentRow{
		{Document: searchdocs.Document{
			ID:           "doc-refund-1",
			RepoID:       repoID,
			SourceKind:   searchdocs.SourceKindCodeEntity,
			Title:        "RefundHandler",
			Path:         "internal/payments/refund.go",
			ContextText:  "func RefundHandler processes a refund request",
			GraphHandles: []searchdocs.GraphHandle{{Kind: "repository", ID: repoID}},
			TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
		}},
		{Document: searchdocs.Document{
			ID:           "doc-refund-2",
			RepoID:       repoID,
			SourceKind:   searchdocs.SourceKindRepositoryFile,
			Title:        "refund README",
			Path:         "docs/refund.md",
			ContextText:  "refund policy documentation",
			GraphHandles: []searchdocs.GraphHandle{{Kind: "repository", ID: repoID}},
			TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
		}},
	}
}

func semanticSearchResultCount(t *testing.T, rec *httptest.ResponseRecorder) (int, int) {
	t.Helper()

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Results              []map[string]any `json:"results"`
			IndexedDocumentCount int              `json:"indexed_document_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return len(envelope.Data.Results), envelope.Data.IndexedDocumentCount
}

// TestSemanticSearchKeywordReturnsResultsForDirectScopeGrant is the #5052
// regression: a caller that addresses the repository by its INGESTION SCOPE id
// resolves to the right scope and the right canonical repo id, but the
// retrieval anchor kept the raw request id. Every document is then rejected by
// the anchor filter, so the response carries a healthy indexed_document_count
// and an empty results array.
func TestSemanticSearchKeywordReturnsResultsForDirectScopeGrant(t *testing.T) {
	t.Parallel()

	const canonicalRepoID = "repository:r_payments"
	const scopeID = "git-repository-scope:repository:r_payments"

	documents := &fakeSemanticSearchDocumentStore{rows: semanticSearchAnchorDocuments(canonicalRepoID)}
	resolver := &fakeSemanticSearchScopeResolver{directRepoID: canonicalRepoID}
	handler := &SemanticSearchHandler{
		Index:         NewLocalSemanticSearchHybrid(documents),
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    scopeID,
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 2000,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		AllowedScopeIDs: []string{scopeID},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	results, indexed := semanticSearchResultCount(t, rec)
	if indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2; body=%s", indexed, rec.Body.String())
	}
	if results == 0 {
		t.Fatalf("keyword search returned 0 results for a term present in %d indexed documents; body=%s",
			indexed, rec.Body.String())
	}
}

// TestSemanticSearchKeywordReturnsResultsForAllScopesScopeID covers the same
// defect on the unscoped (shared/admin/local) path, where a
// "git-repository-scope:"-prefixed request id is resolved to its canonical
// repository id but the anchor is left holding the scope id.
func TestSemanticSearchKeywordReturnsResultsForAllScopesScopeID(t *testing.T) {
	t.Parallel()

	const canonicalRepoID = "repository:r_payments"
	const scopeID = "git-repository-scope:repository:r_payments"

	documents := &fakeSemanticSearchDocumentStore{rows: semanticSearchAnchorDocuments(canonicalRepoID)}
	resolver := &fakeSemanticSearchScopeResolver{directRepoID: canonicalRepoID}
	handler := &SemanticSearchHandler{
		Index:         NewLocalSemanticSearchHybrid(documents),
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    scopeID,
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 2000,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeShared}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	results, indexed := semanticSearchResultCount(t, rec)
	if indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2; body=%s", indexed, rec.Body.String())
	}
	if results == 0 {
		t.Fatalf("keyword search returned 0 results for a term present in %d indexed documents; body=%s",
			indexed, rec.Body.String())
	}
}

// TestSemanticSearchKeywordCanonicalRepoIDStillMatches pins the healthy case:
// when the caller addresses the repository by its canonical id the anchor and
// the stored document repo id already agree, and results come back. This is the
// control for the two regressions above.
func TestSemanticSearchKeywordCanonicalRepoIDStillMatches(t *testing.T) {
	t.Parallel()

	const canonicalRepoID = "repository:r_payments"

	documents := &fakeSemanticSearchDocumentStore{rows: semanticSearchAnchorDocuments(canonicalRepoID)}
	resolver := &fakeSemanticSearchScopeResolver{scopeID: "git-repository-scope:repository:r_payments"}
	handler := &SemanticSearchHandler{
		Index:         NewLocalSemanticSearchHybrid(documents),
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    canonicalRepoID,
		"query":      "refund",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 2000,
	})
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{canonicalRepoID},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	results, indexed := semanticSearchResultCount(t, rec)
	if indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2", indexed)
	}
	if results != 2 {
		t.Fatalf("results = %d, want 2; body=%s", results, rec.Body.String())
	}
}

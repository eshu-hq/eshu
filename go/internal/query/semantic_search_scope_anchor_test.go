// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
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

// semanticSearchAnchorResponse carries the response fields the anchor rebinding
// decides: the repository identity the read was bounded by, the anchor
// retrieval ranked against, and how many documents the index held.
type semanticSearchAnchorResponse struct {
	results    int
	indexed    int
	repoID     string
	anchorKind string
	anchorID   string
}

func semanticSearchDecodeAnchorResponse(
	t *testing.T,
	rec *httptest.ResponseRecorder,
) semanticSearchAnchorResponse {
	t.Helper()

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Results              []map[string]any `json:"results"`
			IndexedDocumentCount int              `json:"indexed_document_count"`
			RepoID               string           `json:"repo_id"`
			Anchor               struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"anchor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return semanticSearchAnchorResponse{
		results:    len(envelope.Data.Results),
		indexed:    envelope.Data.IndexedDocumentCount,
		repoID:     envelope.Data.RepoID,
		anchorKind: envelope.Data.Anchor.Kind,
		anchorID:   envelope.Data.Anchor.ID,
	}
}

// semanticSearchAssertCanonicalRepositoryAnchor is the assertion the whole #5052
// change exists for. Result counts alone would let a regression through whenever
// the corpus still happens to match, so every scope-addressed case checks the
// two ids the caller reads back. One helper, so the healthy control and the
// regressions assert against the same rule.
func semanticSearchAssertCanonicalRepositoryAnchor(
	t *testing.T,
	got semanticSearchAnchorResponse,
	canonicalRepoID string,
	rec *httptest.ResponseRecorder,
) {
	t.Helper()

	if got.repoID != canonicalRepoID {
		t.Fatalf("response repo_id = %q, want canonical %q; body=%s",
			got.repoID, canonicalRepoID, rec.Body.String())
	}
	if want := string(searchretrieval.ScopeKindRepo); got.anchorKind != want {
		t.Fatalf("response anchor.kind = %q, want %q; body=%s",
			got.anchorKind, want, rec.Body.String())
	}
	if got.anchorID != canonicalRepoID {
		t.Fatalf("response anchor.id = %q, want canonical %q; body=%s",
			got.anchorID, canonicalRepoID, rec.Body.String())
	}
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

	got := semanticSearchDecodeAnchorResponse(t, rec)
	if got.indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2; body=%s", got.indexed, rec.Body.String())
	}
	if got.results == 0 {
		t.Fatalf("keyword search returned 0 results for a term present in %d indexed documents; body=%s",
			got.indexed, rec.Body.String())
	}
	semanticSearchAssertCanonicalRepositoryAnchor(t, got, canonicalRepoID, rec)
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

	got := semanticSearchDecodeAnchorResponse(t, rec)
	if got.indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2; body=%s", got.indexed, rec.Body.String())
	}
	if got.results == 0 {
		t.Fatalf("keyword search returned 0 results for a term present in %d indexed documents; body=%s",
			got.indexed, rec.Body.String())
	}
	semanticSearchAssertCanonicalRepositoryAnchor(t, got, canonicalRepoID, rec)
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

	got := semanticSearchDecodeAnchorResponse(t, rec)
	if got.indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2", got.indexed)
	}
	if got.results != 2 {
		t.Fatalf("results = %d, want 2; body=%s", got.results, rec.Body.String())
	}
	semanticSearchAssertCanonicalRepositoryAnchor(t, got, canonicalRepoID, rec)
}

// TestSemanticSearchKeywordScopeIDWithBothGrantsReturnsCanonicalIDs covers a
// caller that holds a direct grant on the ingestion scope AND a canonical grant
// on the repository, addressing the repository by its scope id. The two grants
// are read separately -- allowsDirectScopeID looks in AllowedScopeIDs and
// allowsCanonicalRepositoryID looks in AllowedRepositoryIDs, both against the id
// the caller sent -- so holding the extra canonical grant must not change which
// resolveScope branch runs or which ids come back.
func TestSemanticSearchKeywordScopeIDWithBothGrantsReturnsCanonicalIDs(t *testing.T) {
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
		Mode:                 AuthModeScoped,
		AllowedScopeIDs:      []string{scopeID},
		AllowedRepositoryIDs: []string{canonicalRepoID},
	}))
	rec := httptest.NewRecorder()

	handler.search(rec, req)

	got := semanticSearchDecodeAnchorResponse(t, rec)
	if got.indexed != 2 {
		t.Fatalf("indexed_document_count = %d, want 2; body=%s", got.indexed, rec.Body.String())
	}
	if got.results == 0 {
		t.Fatalf("keyword search returned 0 results for a term present in %d indexed documents; body=%s",
			got.indexed, rec.Body.String())
	}
	semanticSearchAssertCanonicalRepositoryAnchor(t, got, canonicalRepoID, rec)
}

// TestSemanticSearchScopeIDWithServiceKeepsServiceAnchor pins what a
// scope-addressed request does when it also names a smaller scope. Scope.Anchor
// picks service before workload before repository, so anchor.id reports the
// service while repo_id still reports the canonical repository the read was
// bounded by. The documented behaviour of the rebinding is therefore about the
// repository boundary, not about anchor.id in every request shape.
func TestSemanticSearchScopeIDWithServiceKeepsServiceAnchor(t *testing.T) {
	t.Parallel()

	const canonicalRepoID = "repository:r_payments"
	const scopeID = "git-repository-scope:repository:r_payments"
	const serviceID = "service:svc_refunds"

	rows := semanticSearchAnchorDocuments(canonicalRepoID)
	for i := range rows {
		rows[i].Document.GraphHandles = append(rows[i].Document.GraphHandles,
			searchdocs.GraphHandle{Kind: "service", ID: serviceID})
	}
	documents := &fakeSemanticSearchDocumentStore{rows: rows}
	resolver := &fakeSemanticSearchScopeResolver{directRepoID: canonicalRepoID}
	handler := &SemanticSearchHandler{
		Index:         NewLocalSemanticSearchHybrid(documents),
		ScopeResolver: resolver,
		Profile:       ProfileProduction,
	}
	req := semanticSearchHTTPRequest(t, map[string]any{
		"repo_id":    scopeID,
		"service_id": serviceID,
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

	got := semanticSearchDecodeAnchorResponse(t, rec)
	if got.results == 0 {
		t.Fatalf("keyword search returned 0 results for a term present in %d indexed documents; body=%s",
			got.indexed, rec.Body.String())
	}
	if got.repoID != canonicalRepoID {
		t.Fatalf("response repo_id = %q, want canonical %q; body=%s",
			got.repoID, canonicalRepoID, rec.Body.String())
	}
	if want := string(searchretrieval.ScopeKindService); got.anchorKind != want {
		t.Fatalf("response anchor.kind = %q, want %q; body=%s",
			got.anchorKind, want, rec.Body.String())
	}
	if got.anchorID != serviceID {
		t.Fatalf("response anchor.id = %q, want the service id %q; body=%s",
			got.anchorID, serviceID, rec.Body.String())
	}
}

// TestSemanticSearchResolveScopeNeverReturnsScopeIDAsCanonicalRepository pins
// resolveScope's invariant directly: repositoryID is a canonical repository id
// or the resolution is empty. Both subtests reach the branch that would
// otherwise hand the requested id straight back as repositoryID, which for a
// scope-addressed request is the #5052 anchor mismatch.
func TestSemanticSearchResolveScopeNeverReturnsScopeIDAsCanonicalRepository(t *testing.T) {
	t.Parallel()

	const scopeID = "git-repository-scope:repository:r_payments"

	tests := []struct {
		name string
		auth AuthContext
	}{
		{
			// No active repository scope owns the id, so the direct lookup
			// comes back empty and nothing above claims the request.
			name: "all scopes caller, scope id has no active generation",
			auth: AuthContext{Mode: AuthModeShared},
		},
		{
			// The grant names the same scope id in both lists, so the caller
			// holds a direct scope grant AND a canonical repository grant on
			// it, and the scope-addressed branch is skipped.
			name: "grant names the scope id as both a scope and a repository",
			auth: AuthContext{
				Mode:                 AuthModeScoped,
				AllowedScopeIDs:      []string{scopeID},
				AllowedRepositoryIDs: []string{scopeID},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// directRepoID is empty: no active scope row maps this id to a
			// canonical repository. scopeID is non-empty so the canonical
			// lookup, if it ran, would produce a usable-looking resolution.
			resolver := &fakeSemanticSearchScopeResolver{scopeID: "git-repository-scope:repo-payments"}
			handler := &SemanticSearchHandler{ScopeResolver: resolver, Profile: ProfileProduction}
			ctx := ContextWithAuthContext(context.Background(), tc.auth)
			access := repositoryAccessFilterFromContext(ctx)

			resolution, err := handler.resolveScope(
				ctx,
				scopeID,
				access,
				access.allowsDirectScopeID(scopeID),
				access.allowsCanonicalRepositoryID(scopeID),
			)
			if err != nil {
				t.Fatalf("resolveScope() error = %v, want nil", err)
			}
			if strings.HasPrefix(resolution.repositoryID, semanticSearchIngestionScopePrefix) {
				t.Fatalf("resolveScope() repositoryID = %q, want a canonical repository id or empty; "+
					"a scope id here rebinds the anchor to an identity no document carries", resolution.repositoryID)
			}
			if resolution.repositoryID != "" {
				t.Fatalf("resolveScope() repositoryID = %q, want empty", resolution.repositoryID)
			}
			if resolution.scopeID != "" {
				t.Fatalf("resolveScope() scopeID = %q, want empty so the handler writes an empty bounded response",
					resolution.scopeID)
			}
		})
	}
}

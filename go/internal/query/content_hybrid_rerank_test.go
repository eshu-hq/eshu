// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchembed"
)

// TestSearchEntityContentResultsAreHybridReranked proves search_entity_content
// reorders lexical results by fused BM25+vector relevance. Both entities match
// the lexical pattern equally, so the Postgres content store returns them in
// insertion order; the row whose source body is dense with the query terms must
// be lifted above the weak match and carry the search_backend=hybrid marker.
func TestSearchEntityContentResultsAreHybridReranked(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		byEntityRepo: map[string][]EntityContent{
			"repo-team-a": {
				{
					RepoID:       "repo-team-a",
					EntityID:     "entity-weak",
					EntityName:   "processOrderTotals",
					EntityType:   "function",
					RelativePath: "billing/totals.go",
					Language:     "go",
					SourceCache:  "func processOrderTotals() { return sum(prices) }",
				},
				{
					RepoID:       "repo-team-a",
					EntityID:     "entity-strong",
					EntityName:   "processPaymentRefund",
					EntityType:   "function",
					RelativePath: "payments/refund.go",
					Language:     "go",
					SourceCache:  "process payment refund: validate payment, process refund, emit payment refund event",
				},
			},
		},
	}
	handler := &ContentHandler{
		Content:      store,
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: NewContentHybridRanker(true),
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"query":"payment refund","repo_id":"repo-team-a","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchEntities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	matches := decodeContentMatches(t, rec)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want 2 results", matches)
	}
	first := matches[0]
	if got, want := first["entity_id"], "entity-strong"; got != want {
		t.Fatalf("top entity_id = %v, want %v (hybrid rerank did not run)", got, want)
	}
	if first["search_backend"] != "hybrid" {
		t.Fatalf("top search_backend = %v, want hybrid", first["search_backend"])
	}
}

// TestSearchFileContentResultsAreHybridReranked proves search_file_content
// reorders lexical results by fused BM25+vector relevance, mirroring the entity
// path. The file whose body is dense with the query terms must be lifted above
// the weak match and carry the search_backend=hybrid marker.
func TestSearchFileContentResultsAreHybridReranked(t *testing.T) {
	t.Parallel()

	store := &recordingContentAuthzStore{
		byFileRepo: map[string][]FileContent{
			"repo-team-a": {
				{
					RepoID:       "repo-team-a",
					RelativePath: "billing/totals.go",
					Language:     "go",
					Content:      "package billing\nfunc processOrderTotals() { return sum(prices) }",
				},
				{
					RepoID:       "repo-team-a",
					RelativePath: "payments/refund.go",
					Language:     "go",
					Content:      "process payment refund: validate payment, process refund, emit payment refund event",
				},
			},
		},
	}
	handler := &ContentHandler{
		Content:      store,
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: NewContentHybridRanker(true),
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"query":"payment refund","repo_id":"repo-team-a","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	matches := decodeContentMatches(t, rec)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want 2 results", matches)
	}
	first := matches[0]
	if got, want := first["relative_path"], "payments/refund.go"; got != want {
		t.Fatalf("top relative_path = %v, want %v (hybrid rerank did not run)", got, want)
	}
	if first["search_backend"] != "hybrid" {
		t.Fatalf("top search_backend = %v, want hybrid", first["search_backend"])
	}
}

// TestContentHybridRerankNeverInvokesProviderEmbedder proves the content
// re-rank path embeds source text only through the process-local hash embedder.
// The ranker owns a deterministic local embedder that is not injectable, so a
// governed provider embedder (which POSTs text to an external endpoint) cannot
// reach this path and no result row's source body can egress.
func TestContentHybridRerankNeverInvokesProviderEmbedder(t *testing.T) {
	t.Parallel()

	ranker := NewContentHybridRanker(true)
	if _, ok := ranker.localEmbedder.(*searchembed.HashEmbedder); !ok {
		t.Fatalf("ranker.localEmbedder = %T, want *searchembed.HashEmbedder (no provider egress)", ranker.localEmbedder)
	}

	entities := []EntityContent{
		{EntityID: "entity-a", EntityName: "Alpha", RepoID: "repo-team-a", SourceCache: "alpha refund payment body"},
		{EntityID: "entity-b", EntityName: "Beta", RepoID: "repo-team-a", SourceCache: "beta refund payment body"},
	}
	reranked, applied := ranker.RerankEntities(context.Background(), "repo-team-a", "refund payment", entities)
	if !applied {
		t.Fatal("RerankEntities applied = false, want true (local re-rank should run)")
	}
	if len(reranked) != 2 {
		t.Fatalf("RerankEntities dropped rows: %#v", reranked)
	}
}

// TestContentHybridRerankFallsBackToLexicalOrder proves the re-rank pass is a
// deterministic no-op at its bounded edges: when it is disabled and when there
// is fewer than two rows to reorder. In both cases the lexical input order and
// length are preserved exactly and no search_backend marker is attached, so the
// content_index truth basis stays authoritative.
func TestContentHybridRerankFallsBackToLexicalOrder(t *testing.T) {
	t.Parallel()

	entities := []EntityContent{
		{EntityID: "entity-a", EntityName: "Alpha", RepoID: "repo-team-a", SourceCache: "alpha"},
		{EntityID: "entity-b", EntityName: "Beta", RepoID: "repo-team-a", SourceCache: "beta"},
	}
	files := []FileContent{
		{RepoID: "repo-team-a", RelativePath: "a.go", Content: "alpha"},
		{RepoID: "repo-team-a", RelativePath: "b.go", Content: "beta"},
	}

	disabled := NewContentHybridRanker(false)
	rerankedEntities, appliedEntities := disabled.RerankEntities(context.Background(), "repo-team-a", "alpha", entities)
	if appliedEntities {
		t.Fatal("RerankEntities applied = true when disabled, want false")
	}
	if len(rerankedEntities) != 2 ||
		rerankedEntities[0].EntityID != "entity-a" || rerankedEntities[1].EntityID != "entity-b" {
		t.Fatalf("RerankEntities reordered or dropped rows when disabled: %#v", rerankedEntities)
	}
	for _, row := range rerankedEntities {
		if row.SearchBackend != "" {
			t.Fatalf("disabled RerankEntities set search_backend = %q, want empty", row.SearchBackend)
		}
	}

	rerankedFiles, appliedFiles := disabled.RerankFiles(context.Background(), "repo-team-a", "alpha", files)
	if appliedFiles {
		t.Fatal("RerankFiles applied = true when disabled, want false")
	}
	if len(rerankedFiles) != 2 ||
		rerankedFiles[0].RelativePath != "a.go" || rerankedFiles[1].RelativePath != "b.go" {
		t.Fatalf("RerankFiles reordered or dropped rows when disabled: %#v", rerankedFiles)
	}
	for _, row := range rerankedFiles {
		if row.SearchBackend != "" {
			t.Fatalf("disabled RerankFiles set search_backend = %q, want empty", row.SearchBackend)
		}
	}

	// Single result on the enabled ranker: nothing to reorder, pass skipped.
	single := []EntityContent{{EntityID: "entity-a", RepoID: "repo-team-a", SourceCache: "alpha"}}
	_, applied := NewContentHybridRanker(true).RerankEntities(context.Background(), "repo-team-a", "alpha", single)
	if applied {
		t.Fatal("RerankEntities applied = true for single result, want false")
	}
}

// TestSearchFileContentWithEmptyBodiesKeepsLexicalOrder is the regression test
// for #3654: when the SQL search path returns FileContent rows with empty Content
// fields (the production behaviour for all file-search SQL paths), the hybrid
// rerank MUST NOT run — it would index only path/title/labels and mislabel
// results search_backend=hybrid based on path-hash noise. The lexical
// content_index order must be preserved and no search_backend marker set.
func TestSearchFileContentWithEmptyBodiesKeepsLexicalOrder(t *testing.T) {
	t.Parallel()

	// Simulate the production SQL paths: Content is always '' because the
	// SELECT hardcodes an empty string literal instead of the content column.
	store := &recordingContentAuthzStore{
		byFileRepo: map[string][]FileContent{
			"repo-team-a": {
				{
					RepoID:       "repo-team-a",
					RelativePath: "billing/totals.go",
					Language:     "go",
					Content:      "", // production SQL path returns empty body
				},
				{
					RepoID:       "repo-team-a",
					RelativePath: "payments/refund.go",
					Language:     "go",
					Content:      "", // production SQL path returns empty body
				},
			},
		},
	}
	handler := &ContentHandler{
		Content:      store,
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: NewContentHybridRanker(true),
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"query":"payment refund","repo_id":"repo-team-a","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.searchFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	matches := decodeContentMatches(t, rec)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want 2 results", matches)
	}
	// Lexical order (insertion order from the store) must be preserved.
	if got, want := matches[0]["relative_path"], "billing/totals.go"; got != want {
		t.Fatalf("top relative_path = %v, want %v (lexical order not preserved)", got, want)
	}
	if got, want := matches[1]["relative_path"], "payments/refund.go"; got != want {
		t.Fatalf("second relative_path = %v, want %v (lexical order not preserved)", got, want)
	}
	// No hybrid label must be set when bodies are absent: search_backend must be
	// empty (omitted from JSON or explicitly ""), never "hybrid".
	for i, match := range matches {
		if backend, _ := match["search_backend"].(string); backend == "hybrid" {
			t.Fatalf("matches[%d] search_backend = %q, want empty (mislabeled hybrid on empty-body file)", i, backend)
		}
	}
}

// TestRerankFilesSkipsWhenAllBodiesEmpty proves ContentHybridRanker.RerankFiles
// returns applied=false and the original lexical order when every FileContent row
// has an empty Content field. This is the direct unit contract for the fix.
func TestRerankFilesSkipsWhenAllBodiesEmpty(t *testing.T) {
	t.Parallel()

	ranker := NewContentHybridRanker(true)
	rows := []FileContent{
		{RepoID: "repo-team-a", RelativePath: "billing/totals.go", Content: ""},
		{RepoID: "repo-team-a", RelativePath: "payments/refund.go", Content: ""},
	}

	reranked, applied := ranker.RerankFiles(context.Background(), "repo-team-a", "payment refund", rows)
	if applied {
		t.Fatal("RerankFiles applied = true when all bodies empty, want false (must not mislabel hybrid)")
	}
	if len(reranked) != 2 ||
		reranked[0].RelativePath != "billing/totals.go" ||
		reranked[1].RelativePath != "payments/refund.go" {
		t.Fatalf("RerankFiles reordered or dropped rows when bodies absent: %#v", reranked)
	}
	for _, row := range reranked {
		if row.SearchBackend != "" {
			t.Fatalf("RerankFiles set search_backend = %q on empty-body row, want empty", row.SearchBackend)
		}
	}
}

// TestRerankEntitiesUnaffectedByFileFix proves entity-content rerank still runs
// when entities have populated SourceCache bodies. The fix for #3654 must not
// regress the entity path.
func TestRerankEntitiesUnaffectedByFileFix(t *testing.T) {
	t.Parallel()

	ranker := NewContentHybridRanker(true)
	rows := []EntityContent{
		{
			RepoID:      "repo-team-a",
			EntityID:    "entity-weak",
			EntityName:  "processOrderTotals",
			EntityType:  "function",
			SourceCache: "func processOrderTotals() { return sum(prices) }",
		},
		{
			RepoID:      "repo-team-a",
			EntityID:    "entity-strong",
			EntityName:  "processPaymentRefund",
			EntityType:  "function",
			SourceCache: "process payment refund: validate payment, process refund, emit payment refund event",
		},
	}

	reranked, applied := ranker.RerankEntities(context.Background(), "repo-team-a", "payment refund", rows)
	if !applied {
		t.Fatal("RerankEntities applied = false, want true (entity rerank must remain active)")
	}
	if len(reranked) != 2 {
		t.Fatalf("RerankEntities dropped rows: %#v", reranked)
	}
	if reranked[0].EntityID != "entity-strong" {
		t.Fatalf("top entity_id = %v, want entity-strong (entity rerank regressed)", reranked[0].EntityID)
	}
}

func decodeContentMatches(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	rawMatches, ok := body["matches"].([]any)
	if !ok {
		t.Fatalf("matches = %#v, want array", body["matches"])
	}
	matches := make([]map[string]any, 0, len(rawMatches))
	for _, raw := range rawMatches {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("match row = %#v, want object", raw)
		}
		matches = append(matches, row)
	}
	return matches
}

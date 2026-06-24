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

// TestCodeSearchContentResultsAreHybridReranked proves the find_code content
// path reorders lexical results by vector/hybrid relevance. The result whose
// source text is semantically closest to the query must rank first even though
// both entities match the lexical pattern equally.
func TestCodeSearchContentResultsAreHybridReranked(t *testing.T) {
	t.Parallel()

	// Both entities contain the lexical token "process" so the Postgres content
	// store returns them in insertion order. The first row is a weak match whose
	// body is unrelated; the second row's body is dense with the query terms, so
	// hybrid BM25+vector ranking must lift it above the first.
	content := &recordingCodeSearchContentStore{
		byRepo: map[string][]EntityContent{
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

	// A graph reader that returns no name matches forces the content fallback,
	// which is the path find_code uses for full-text relevance ranking.
	emptyGraph := fakeGraphReader{
		run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, nil
		},
	}
	handler := &CodeHandler{
		Neo4j:        emptyGraph,
		Content:      content,
		Profile:      ProfileLocalAuthoritative,
		HybridRanker: NewCodeHybridRanker(true),
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"payment refund","repo_id":"repo-team-a","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	matches, ok := body["matches"].([]any)
	if !ok || len(matches) != 2 {
		t.Fatalf("matches = %#v, want 2 results", body["matches"])
	}
	first, _ := matches[0].(map[string]any)
	if got, want := first["entity_id"], "entity-strong"; got != want {
		t.Fatalf("top result entity_id = %v, want %v (hybrid rerank did not run)", got, want)
	}
	if first["search_backend"] != "hybrid" {
		t.Fatalf("top result search_backend = %v, want hybrid", first["search_backend"])
	}
}

// TestCodeHybridRerankNeverInvokesProviderEmbedder proves the re-rank path never
// embeds source text through a provider embedder. The ranker owns a
// deterministic local hash embedder that is not injectable, so a governed
// provider embedder (which POSTs text to an external endpoint) cannot reach this
// path and no result row's source body can egress. The test asserts the embedder
// the ranker uses is the local hash embedder, then runs a real re-rank with
// source bodies present to prove embedding stays in-process: it completes with
// no HTTP server, which a provider embedder could not.
func TestCodeHybridRerankNeverInvokesProviderEmbedder(t *testing.T) {
	t.Parallel()

	ranker := NewCodeHybridRanker(true)
	if _, ok := ranker.localEmbedder.(*searchembed.HashEmbedder); !ok {
		t.Fatalf("ranker.localEmbedder = %T, want *searchembed.HashEmbedder (no provider egress)", ranker.localEmbedder)
	}

	results := []map[string]any{
		{"entity_id": "entity-a", "entity_name": "Alpha", "repo_id": "repo-team-a", "source_cache": "alpha refund payment body"},
		{"entity_id": "entity-b", "entity_name": "Beta", "repo_id": "repo-team-a", "source_cache": "beta refund payment body"},
	}
	reranked, applied := ranker.Rerank(context.Background(), "repo-team-a", "refund payment", results)
	if !applied {
		t.Fatal("Rerank applied = false, want true (local re-rank should run)")
	}
	if len(reranked) != 2 {
		t.Fatalf("Rerank dropped rows: %#v", reranked)
	}
}

// TestCodeSearchHybridRerankFallsBackToLexicalOrder proves the re-rank pass is a
// deterministic no-op at its bounded edges: when it is disabled and when there
// is nothing to reorder. In both cases the lexical input order and length are
// preserved exactly.
func TestCodeSearchHybridRerankFallsBackToLexicalOrder(t *testing.T) {
	t.Parallel()

	results := []map[string]any{
		{"entity_id": "entity-a", "entity_name": "Alpha", "repo_id": "repo-team-a", "source_cache": "alpha"},
		{"entity_id": "entity-b", "entity_name": "Beta", "repo_id": "repo-team-a", "source_cache": "beta"},
	}

	// Disabled ranker: the caller keeps the lexical order and basis unchanged.
	disabled := NewCodeHybridRanker(false)
	reranked, applied := disabled.Rerank(context.Background(), "repo-team-a", "alpha", results)
	if applied {
		t.Fatal("Rerank applied = true when disabled, want false")
	}
	if len(reranked) != 2 || reranked[0]["entity_id"] != "entity-a" || reranked[1]["entity_id"] != "entity-b" {
		t.Fatalf("Rerank reordered or dropped rows when disabled: %#v", reranked)
	}

	// Single result: nothing to reorder, so the pass is skipped.
	single := []map[string]any{{"entity_id": "entity-a", "repo_id": "repo-team-a", "source_cache": "alpha"}}
	_, applied = NewCodeHybridRanker(true).Rerank(context.Background(), "repo-team-a", "alpha", single)
	if applied {
		t.Fatal("Rerank applied = true for single result, want false")
	}
}

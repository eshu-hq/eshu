// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// rankAllDocs returns count in-scope documents plus one out-of-scope document.
// Every body shares a word with the query, so every document has a non-zero
// vector and a BM25 match.
func rankAllDocs(count int) []searchdocs.Document {
	docs := make([]searchdocs.Document, 0, count+1)
	for i := 0; i < count; i++ {
		body := fmt.Sprintf("payment handler %d ledger entry", i)
		if i%7 == 0 {
			body += " refund refund"
		}
		docs = append(docs, doc(fmt.Sprintf("doc-%03d", i), "repo-1", "handler", body))
	}
	return append(docs, doc("other", "repo-2", "handler", "payment refund"))
}

// TestRankAllRanksEveryInScopeDocumentPastTheRetrievalLimit pins that RankAll
// ranks the whole in-scope index, not a top-K bounded by the public retrieval
// limit (100), so a request-local re-rank of a 150-row page orders every row.
// Its order matches Search wherever Search's fusion pool is not cut (#7746).
func TestRankAllRanksEveryInScopeDocumentPastTheRetrievalLimit(t *testing.T) {
	t.Parallel()

	const count = 150
	index := mustIndex(t, rankAllDocs(count), Options{Embedder: &bagOfWordsEmbedder{dims: 32}})
	backend := Backend{Index: index}
	scope := searchretrieval.Scope{RepoID: "repo-1"}

	all, err := backend.RankAll(context.Background(), "payment refund", scope)
	if err != nil {
		t.Fatalf("RankAll error = %v", err)
	}
	if len(all) != count {
		t.Fatalf("RankAll returned %d candidates, want %d (every in-scope document)", len(all), count)
	}
	seen := make(map[string]struct{}, len(all))
	for _, candidate := range all {
		if candidate.Document.RepoID != "repo-1" {
			t.Fatalf("RankAll returned out-of-scope document %q", candidate.Document.ID)
		}
		if candidate.Metadata["search_method"] != "rrf_hybrid" {
			t.Fatalf("search_method = %q, want rrf_hybrid", candidate.Metadata["search_method"])
		}
		seen[candidate.Document.ID] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("RankAll returned %d distinct documents, want %d", len(seen), count)
	}

	top, err := backend.Search(context.Background(), request("payment refund", "repo-1", searchbench.ModeHybrid, 100))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	for i, candidate := range top {
		if candidate.Document.ID != all[i].Document.ID ||
			math.Float64bits(candidate.Score) != math.Float64bits(all[i].Score) {
			t.Fatalf("position %d: Search %s/%v != RankAll %s/%v",
				i, candidate.Document.ID, candidate.Score, all[i].Document.ID, all[i].Score)
		}
	}
}

// TestRankAllKeepsIndexOverflowSignal pins that RankAll is bounded by the index
// document cap with the same drop-by-id rule, and marks every candidate with
// the overflow signal Search uses.
func TestRankAllKeepsIndexOverflowSignal(t *testing.T) {
	t.Parallel()

	index := mustIndex(t, rankAllDocs(150), Options{MaxDocuments: 120, Embedder: &bagOfWordsEmbedder{dims: 32}})
	all, err := (Backend{Index: index}).RankAll(context.Background(), "payment refund", searchretrieval.Scope{RepoID: "repo-1"})
	if err != nil {
		t.Fatalf("RankAll error = %v", err)
	}
	// The cap keeps the 120 lowest ids: doc-000..doc-119 (the out-of-scope
	// "other" sorts after every doc-* id and is dropped).
	if len(all) != 120 {
		t.Fatalf("RankAll returned %d candidates, want 120", len(all))
	}
	for _, candidate := range all {
		if candidate.Document.ID > "doc-119" {
			t.Fatalf("RankAll returned %q, past the capped id range", candidate.Document.ID)
		}
		if candidate.Metadata["index_overflow"] != "true" {
			t.Fatalf("index_overflow = %q, want true", candidate.Metadata["index_overflow"])
		}
		if len(candidate.Failures) != 1 || candidate.Failures[0] != searchbench.FailureClassTruncation {
			t.Fatalf("failures = %v, want [truncation]", candidate.Failures)
		}
	}
}

// TestRankAllRejectsInvalidInput pins the error paths that make re-rank callers
// fall back to lexical order: blank query, missing scope, missing index, and a
// done context.
func TestRankAllRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	index := mustIndex(t, rankAllDocs(3), Options{Embedder: &bagOfWordsEmbedder{dims: 32}})
	scope := searchretrieval.Scope{RepoID: "repo-1"}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name    string
		backend Backend
		ctx     context.Context
		query   string
		scope   searchretrieval.Scope
	}{
		{name: "blank query", backend: Backend{Index: index}, ctx: context.Background(), query: "  ", scope: scope},
		{name: "missing scope", backend: Backend{Index: index}, ctx: context.Background(), query: "payment"},
		{name: "missing index", backend: Backend{}, ctx: context.Background(), query: "payment", scope: scope},
		{name: "done context", backend: Backend{Index: index}, ctx: cancelled, query: "payment", scope: scope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := tc.backend.RankAll(tc.ctx, tc.query, tc.scope); err == nil {
				t.Fatalf("RankAll returned %d candidates and no error, want an error", len(got))
			}
		})
	}
}

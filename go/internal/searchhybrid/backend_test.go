// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"hash/fnv"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// bagOfWordsEmbedder is a deterministic, hosted-dependency-free embedder for
// tests: it hashes each token into a fixed-dimension bag-of-words vector, so
// documents that share words are nearer in cosine space.
type bagOfWordsEmbedder struct {
	dims  int
	calls int
}

func (e *bagOfWordsEmbedder) Dimensions() int { return e.dims }

func (e *bagOfWordsEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	e.calls++
	vec := make([]float64, e.dims)
	for _, token := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(token))
		vec[int(h.Sum32())%e.dims]++
	}
	return vec, nil
}

func doc(id, repo, title, body string) searchdocs.Document {
	return searchdocs.Document{
		ID:           id,
		RepoID:       repo,
		SourceKind:   searchdocs.SourceKindCodeEntity,
		Title:        title,
		ContextText:  body,
		GraphHandles: []searchdocs.GraphHandle{{Kind: "repository", ID: repo}, {Kind: "content_entity", ID: id}},
		TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		UpdatedAt:    time.Unix(0, 0),
	}
}

func corpus() []searchdocs.Document {
	return []searchdocs.Document{
		doc("d-1", "repo-1", "Payment processor", "charge credit card and process payment refund"),
		doc("d-2", "repo-1", "Auth handler", "validate session token and login user"),
		doc("d-3", "repo-1", "Invoice builder", "build invoice line items and totals"),
		doc("d-other", "repo-2", "Payment gateway", "process payment with external gateway"),
	}
}

func mustIndex(t *testing.T, docs []searchdocs.Document, opts Options) *Index {
	t.Helper()
	index, err := NewIndex(docs, opts)
	if err != nil {
		t.Fatalf("NewIndex error = %v", err)
	}
	return index
}

func request(query, repo string, mode searchbench.Mode, limit int) searchretrieval.Request {
	return searchretrieval.Request{
		Query:   query,
		Scope:   searchretrieval.Scope{RepoID: repo},
		Mode:    mode,
		Limit:   limit,
		Timeout: time.Second,
	}
}

func TestBackendKeywordRanksByBM25AndScopes(t *testing.T) {
	t.Parallel()

	backend := Backend{Index: mustIndex(t, corpus(), Options{})}
	candidates, err := backend.Search(context.Background(), request("payment refund", "repo-1", searchbench.ModeKeyword, 10))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
	// The payment document must rank first within repo-1.
	if candidates[0].Document.ID != "d-1" {
		t.Errorf("top document = %q, want d-1", candidates[0].Document.ID)
	}
	// The out-of-scope repo-2 payment document must never appear.
	for _, candidate := range candidates {
		if candidate.Document.RepoID != "repo-1" {
			t.Errorf("out-of-scope document leaked: %q (%s)", candidate.Document.ID, candidate.Document.RepoID)
		}
		if candidate.Metadata["search_method"] != "bm25" {
			t.Errorf("search_method = %q, want bm25", candidate.Metadata["search_method"])
		}
		if candidate.Document.TruthScope.Level != searchdocs.TruthLevelDerived {
			t.Errorf("non-derived truth level leaked: %q", candidate.Document.TruthScope.Level)
		}
	}
	// Documents with no lexical overlap must be excluded.
	for _, candidate := range candidates {
		if candidate.Document.ID == "d-3" {
			t.Errorf("non-matching document d-3 should be excluded")
		}
	}
}

func TestBackendSemanticRequiresEmbedder(t *testing.T) {
	t.Parallel()

	backend := Backend{Index: mustIndex(t, corpus(), Options{})}
	if _, err := backend.Search(context.Background(), request("payment", "repo-1", searchbench.ModeSemantic, 5)); err == nil {
		t.Fatal("expected error for semantic mode without embedder")
	}
}

func TestBackendSemanticRanksByVector(t *testing.T) {
	t.Parallel()

	backend := Backend{Index: mustIndex(t, corpus(), Options{Embedder: &bagOfWordsEmbedder{dims: 32}})}
	candidates, err := backend.Search(context.Background(), request("process payment refund charge", "repo-1", searchbench.ModeSemantic, 5))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) == 0 || candidates[0].Document.ID != "d-1" {
		t.Fatalf("semantic top = %v, want d-1 first", candidateIDs(candidates))
	}
	for _, candidate := range candidates {
		if candidate.Metadata["search_method"] != "vector" {
			t.Errorf("search_method = %q, want vector", candidate.Metadata["search_method"])
		}
	}
}

func TestBackendHybridFusesWhenEmbedderPresent(t *testing.T) {
	t.Parallel()

	withEmbed := Backend{Index: mustIndex(t, corpus(), Options{Embedder: &bagOfWordsEmbedder{dims: 32}})}
	candidates, err := withEmbed.Search(context.Background(), request("payment refund", "repo-1", searchbench.ModeHybrid, 5))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) == 0 || candidates[0].Metadata["search_method"] != "rrf_hybrid" {
		t.Fatalf("hybrid method = %v, want rrf_hybrid", candidates)
	}

	// Without an embedder, hybrid degenerates to BM25 and says so.
	noEmbed := Backend{Index: mustIndex(t, corpus(), Options{})}
	bm25Only, err := noEmbed.Search(context.Background(), request("payment refund", "repo-1", searchbench.ModeHybrid, 5))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(bm25Only) == 0 || bm25Only[0].Metadata["search_method"] != "bm25" {
		t.Fatalf("hybrid-without-embedder method = %v, want bm25", bm25Only)
	}
}

func TestBackendDeterministicTopK(t *testing.T) {
	t.Parallel()

	backend := Backend{Index: mustIndex(t, corpus(), Options{Embedder: &bagOfWordsEmbedder{dims: 32}})}
	first, err := backend.Search(context.Background(), request("payment process refund", "repo-1", searchbench.ModeHybrid, 3))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	second, err := backend.Search(context.Background(), request("payment process refund", "repo-1", searchbench.ModeHybrid, 3))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if strings.Join(candidateIDs(first), ",") != strings.Join(candidateIDs(second), ",") {
		t.Fatalf("non-deterministic ordering: %v vs %v", candidateIDs(first), candidateIDs(second))
	}
}

func TestBackendReturnsLimitPlusOneForTruncation(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("a", "repo-1", "alpha", "shared term shared term"),
		doc("b", "repo-1", "beta", "shared term"),
		doc("c", "repo-1", "gamma", "shared term"),
	}
	backend := Backend{Index: mustIndex(t, docs, Options{})}
	candidates, err := backend.Search(context.Background(), request("shared", "repo-1", searchbench.ModeKeyword, 1))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2 (limit+1 for truncation detection)", len(candidates))
	}
}

func TestBackendRejectsNilIndexAndBadRequest(t *testing.T) {
	t.Parallel()

	if _, err := (Backend{}).Search(context.Background(), request("q", "repo-1", searchbench.ModeKeyword, 5)); err == nil {
		t.Fatal("expected error for nil index")
	}
	backend := Backend{Index: mustIndex(t, corpus(), Options{})}
	if _, err := backend.Search(context.Background(), searchretrieval.Request{Query: "q"}); err == nil {
		t.Fatal("expected validation error for unscoped request")
	}
}

func candidateIDs(candidates []searchretrieval.Candidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.Document.ID)
	}
	return ids
}

func BenchmarkBackendSearchHybrid(b *testing.B) {
	docs := make([]searchdocs.Document, 0, 2000)
	for i := 0; i < 2000; i++ {
		id := "d-" + strconv.Itoa(i)
		docs = append(docs, doc(id, "repo-1", "service "+id, "process payment refund token invoice "+id))
	}
	backend := Backend{Index: mustIndexB(b, docs, Options{Embedder: &bagOfWordsEmbedder{dims: 64}})}
	req := request("payment refund token", "repo-1", searchbench.ModeHybrid, 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := backend.Search(context.Background(), req); err != nil {
			b.Fatalf("Search error = %v", err)
		}
	}
}

func mustIndexB(b *testing.B, docs []searchdocs.Document, opts Options) *Index {
	b.Helper()
	index, err := NewIndex(docs, opts)
	if err != nil {
		b.Fatalf("NewIndex error = %v", err)
	}
	return index
}

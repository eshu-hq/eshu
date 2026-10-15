// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

func TestEshuSearchIndexStoreSearchesActiveGenerationBM25(t *testing.T) {
	t.Parallel()

	doc := searchIndexDocumentFixture("searchdoc:runtime:payments", "repo-1", "Payments runbook")
	payload, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{{
				int64(2500),
				false,
			}}},
			{Data: [][]any{{
				payload,
				1.75,
				int64(2500),
				false,
			}}},
		},
	}
	store := NewEshuSearchIndexStore(db)

	result, err := store.Search(context.Background(), EshuSearchIndexSearch{
		ScopeID: "repo-1",
		RepoID:  "repo-1",
		Query:   "payment runbook",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		Limit:   20,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if got, want := result.IndexedDocumentCount, 2500; got != want {
		t.Fatalf("IndexedDocumentCount = %d, want %d", got, want)
	}
	if result.CorpusMayBeTruncated {
		t.Fatal("CorpusMayBeTruncated = true, want false")
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	candidate := result.Candidates[0]
	if got, want := candidate.Document.ID, doc.ID; got != want {
		t.Fatalf("candidate document id = %q, want %q", got, want)
	}
	if got, want := candidate.Score, 1.75; got != want {
		t.Fatalf("candidate score = %v, want %v", got, want)
	}
	if got, want := candidate.Metadata["search_method"], "bm25"; got != want {
		t.Fatalf("search_method = %q, want %q", got, want)
	}

	if len(db.Queries) != 2 {
		t.Fatalf("queries = %d, want 2", len(db.Queries))
	}
	if q := db.Queries[0].Query; !strings.Contains(q, "FROM eshu_search_index_stats") {
		t.Fatalf("stats query missing index stats table:\n%s", q)
	}
	q := db.Queries[1].Query
	for _, fragment := range []string{
		"FROM eshu_search_index_terms",
		"JOIN eshu_search_index_documents",
		"FROM eshu_search_index_stats",
		"search_stats AS MATERIALIZED",
		"CROSS JOIN search_stats stats",
		"q.term_key = t.term_key AND q.term = t.term",
		"df.term_key = t.term_key AND df.term = t.term",
		"active_generation_id",
		"d.repo_id = $4",
		"ORDER BY score DESC, document_id ASC",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	if got, ok := db.Queries[1].Args[1].([]string); !ok || len(got) != 2 {
		t.Fatalf("query term arg = %#v, want two token strings", db.Queries[1].Args[1])
	}
	if got, ok := db.Queries[1].Args[2].([]string); !ok || len(got) != 2 {
		t.Fatalf("query term key arg = %#v, want two token keys", db.Queries[1].Args[2])
	}
}

func TestEshuSearchIndexStoreReportsIndexedCountWithoutMatches(t *testing.T) {
	t.Parallel()

	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{{
				int64(3800),
				false,
			}}},
			{Data: [][]any{}},
		},
	}
	store := NewEshuSearchIndexStore(db)

	result, err := store.Search(context.Background(), EshuSearchIndexSearch{
		ScopeID: "repo-1",
		RepoID:  "repo-1",
		Query:   "notfound",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if got, want := result.IndexedDocumentCount, 3800; got != want {
		t.Fatalf("IndexedDocumentCount = %d, want %d", got, want)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(result.Candidates))
	}
}

func TestEshuSearchIndexStoreRequiresBoundedSearch(t *testing.T) {
	t.Parallel()

	store := NewEshuSearchIndexStore(&fake.ExecQueryer{})
	if _, err := store.Search(context.Background(), EshuSearchIndexSearch{}); err == nil {
		t.Fatal("expected error when search lacks scope, query, anchor, and limit")
	}
}

func TestEshuSearchIndexStoreLanguageFilterAppendsLabelPredicate(t *testing.T) {
	t.Parallel()

	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{{int64(100), false}}},
			{Data: [][]any{}},
		},
	}
	store := NewEshuSearchIndexStore(db)

	_, err := store.Search(context.Background(), EshuSearchIndexSearch{
		ScopeID:   "repo-1",
		RepoID:    "repo-1",
		Query:     "payment",
		Anchor:    searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		Languages: []string{"go", "python"},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}

	if len(db.Queries) < 2 {
		t.Fatalf("queries = %d, want at least 2", len(db.Queries))
	}
	q := db.Queries[1].Query
	if !strings.Contains(q, "jsonb_array_elements_text") {
		t.Errorf("query missing jsonb_array_elements_text for language filter:\n%s", q)
	}
	// The label values must arrive as parameterised args, not interpolated into SQL.
	if strings.Contains(q, "language:go") {
		t.Errorf("language value was interpolated into SQL instead of parameterised:\n%s", q)
	}
	found := false
	for _, arg := range db.Queries[1].Args {
		if langs, ok := arg.([]string); ok {
			for _, l := range langs {
				if strings.HasPrefix(l, "language:") {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("expected a []string arg containing language: prefixed values; args = %#v", db.Queries[1].Args)
	}
}

func TestEshuSearchIndexStoreNoLanguageFilterOmitsLabelPredicate(t *testing.T) {
	t.Parallel()

	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{{int64(100), false}}},
			{Data: [][]any{}},
		},
	}
	store := NewEshuSearchIndexStore(db)

	_, err := store.Search(context.Background(), EshuSearchIndexSearch{
		ScopeID: "repo-1",
		RepoID:  "repo-1",
		Query:   "payment",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		// No Languages field.
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}

	if len(db.Queries) < 2 {
		t.Fatalf("queries = %d, want at least 2", len(db.Queries))
	}
	q := db.Queries[1].Query
	if strings.Contains(q, "jsonb_array_elements_text") {
		t.Errorf("query unexpectedly contains language filter when no languages requested:\n%s", q)
	}
}

func TestBuildEshuSearchIndexQueryBoundsDocumentLookupsBeforeRanking(t *testing.T) {
	t.Parallel()

	query, _ := BuildEshuSearchIndexQuery(EshuSearchIndexSearch{
		ScopeID: "scope-1",
		RepoID:  "repo-1",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		Limit:   11,
	}, []string{"common"}, []string{"term-key"})
	for _, fragment := range []string{
		"matched_terms AS MATERIALIZED",
		"document_frequency AS MATERIALIZED",
		"JOIN LATERAL (",
		"LIMIT 1",
		"GROUP BY d.scope_id, d.generation_id, d.document_id, stats.document_count",
		"top_hits AS (",
		"ORDER BY score DESC, document_id ASC",
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("BM25 query missing bounded ranking step %q", fragment)
		}
	}
	if strings.Contains(query, "GROUP BY d.document") {
		t.Fatal("BM25 query groups full document payload before ranking")
	}
}

func TestBuildEshuSearchIndexQueryNarrowsPostingLookupBeforeRanking(t *testing.T) {
	t.Parallel()

	query, _ := BuildEshuSearchIndexQuery(EshuSearchIndexSearch{
		ScopeID: "scope-1",
		RepoID:  "repo-1",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		Limit:   31,
	}, []string{"common"}, []string{"term-key"})
	// The per-posting lateral must carry only the scoring columns so the
	// covering documents index serves it without a heap fetch (#7727).
	for _, fragment := range []string{
		"SELECT d.scope_id, d.generation_id, d.document_id, d.repo_id, d.document_length",
		"search_stats AS MATERIALIZED",
		"CROSS JOIN search_stats stats",
		"top_hits AS (",
		"JOIN eshu_search_index_documents d",
		"d.document, hits.score",
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("BM25 query missing narrowed posting-lookup step %q", fragment)
		}
	}
	for _, fragment := range []string{
		"SELECT d.*",
		"JOIN eshu_search_index_stats stats",
		"jsonb_array_elements",
	} {
		if strings.Contains(query, fragment) {
			t.Errorf("BM25 query still fetches documents before the page limit via %q", fragment)
		}
	}
}

func TestBuildEshuSearchIndexQueryKeepsFilterColumnsInPostingLookup(t *testing.T) {
	t.Parallel()

	query, _ := BuildEshuSearchIndexQuery(EshuSearchIndexSearch{
		ScopeID:     "scope-1",
		RepoID:      "repo-1",
		Anchor:      searchretrieval.Anchor{Kind: searchretrieval.ScopeKindService, ID: "svc-1"},
		SourceKinds: []searchdocs.SourceKind{searchdocs.SourceKindRuntimeSummary},
		Languages:   []string{"go"},
		Limit:       31,
	}, []string{"common"}, []string{"term-key"})
	for _, fragment := range []string{
		"d.source_kind",
		"d.document",
		"jsonb_array_elements",
		"jsonb_array_elements_text",
		"search_stats AS MATERIALIZED",
		"CROSS JOIN search_stats stats",
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("BM25 filtered query missing posting-lookup step %q", fragment)
		}
	}
	if strings.Contains(query, "SELECT d.*") {
		t.Error("BM25 filtered query still selects the full document row before ranking")
	}
	if strings.Contains(query, "JOIN eshu_search_index_stats stats") {
		t.Error("BM25 filtered query still joins the stats row per posting")
	}
}

func TestBuildEshuSearchIndexQueryKeepsHandlePredicateForForeignRepoAnchor(t *testing.T) {
	t.Parallel()

	query, _ := BuildEshuSearchIndexQuery(EshuSearchIndexSearch{
		ScopeID: "scope-1",
		RepoID:  "repo-1",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-other"},
		Limit:   31,
	}, []string{"common"}, []string{"term-key"})
	if !strings.Contains(query, "jsonb_array_elements") {
		t.Error("BM25 query dropped the handle predicate for a repo anchor outside the repo scope")
	}
	if !strings.Contains(query, "d.document") {
		t.Error("BM25 query dropped the document column a foreign repo anchor still filters on")
	}
}

// TestBuildEshuSearchIndexQueryOrdersScoreSummation pins the deterministic
// summation order inside the BM25 score aggregate (#7737). Float addition is
// not associative, so the plan-dependent row order reaching a bare SUM changes
// score bits and tie order for three-or-more-term queries. The ordered
// aggregate fixes the summation order on the stable per-term key.
func TestBuildEshuSearchIndexQueryOrdersScoreSummation(t *testing.T) {
	t.Parallel()

	query, _ := BuildEshuSearchIndexQuery(EshuSearchIndexSearch{
		ScopeID: "scope-1",
		RepoID:  "repo-1",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"},
		Limit:   31,
	}, []string{"alpha", "beta", "gamma"}, []string{"key-alpha", "key-beta", "key-gamma"})
	const want = "ORDER BY t.term_key, t.term"
	if !strings.Contains(query, want) {
		t.Fatalf("BM25 score aggregate missing deterministic summation order %q:\n%s", want, query)
	}
}

func TestBuildEshuSearchIndexQueryKeepsHandlePredicateForUntrimmedRepoAnchor(t *testing.T) {
	t.Parallel()

	query, _ := BuildEshuSearchIndexQuery(EshuSearchIndexSearch{
		ScopeID: "scope-1",
		RepoID:  "repo-1",
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1 "},
		Limit:   31,
	}, []string{"common"}, []string{"term-key"})
	if !strings.Contains(query, "jsonb_array_elements") {
		t.Error("BM25 query dropped the handle predicate for a repo anchor whose raw ID differs from the repo scope")
	}
}

func searchIndexDocumentFixture(id string, repoID string, title string) searchdocs.Document {
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindRuntimeSummary,
		Title:       title,
		Path:        "docs/runbook.md",
		ContextText: "payment runbook escalation",
		UpdatedAt:   time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisReadModel,
		},
		Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		AccessScope: searchdocs.AccessScope{RepoID: repoID},
		GraphHandles: []searchdocs.GraphHandle{
			{Kind: "repository", ID: repoID},
			{Kind: "service", ID: "svc-payments"},
		},
	}
}

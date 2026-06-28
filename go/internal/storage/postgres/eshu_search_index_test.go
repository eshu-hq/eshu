// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				int64(2500),
				false,
			}}},
			{rows: [][]any{{
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

	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want 2", len(db.queries))
	}
	if q := db.queries[0].query; !strings.Contains(q, "FROM eshu_search_index_stats") {
		t.Fatalf("stats query missing index stats table:\n%s", q)
	}
	q := db.queries[1].query
	for _, fragment := range []string{
		"FROM eshu_search_index_terms",
		"JOIN eshu_search_index_documents",
		"JOIN eshu_search_index_stats",
		"q.term_key = t.term_key AND q.term = t.term",
		"df.term_key = t.term_key AND df.term = t.term",
		"active_generation_id",
		"jsonb_array_elements",
		"ORDER BY score DESC, document_id ASC",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	if got, ok := db.queries[1].args[1].([]string); !ok || len(got) != 2 {
		t.Fatalf("query term arg = %#v, want two token strings", db.queries[1].args[1])
	}
	if got, ok := db.queries[1].args[2].([]string); !ok || len(got) != 2 {
		t.Fatalf("query term key arg = %#v, want two token keys", db.queries[1].args[2])
	}
}

func TestEshuSearchIndexStoreReportsIndexedCountWithoutMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				int64(3800),
				false,
			}}},
			{rows: [][]any{}},
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

	store := NewEshuSearchIndexStore(&fakeExecQueryer{})
	if _, err := store.Search(context.Background(), EshuSearchIndexSearch{}); err == nil {
		t.Fatal("expected error when search lacks scope, query, anchor, and limit")
	}
}

func TestEshuSearchIndexStoreLanguageFilterAppendsLabelPredicate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(100), false}}},
			{rows: [][]any{}},
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

	if len(db.queries) < 2 {
		t.Fatalf("queries = %d, want at least 2", len(db.queries))
	}
	q := db.queries[1].query
	if !strings.Contains(q, "jsonb_array_elements_text") {
		t.Errorf("query missing jsonb_array_elements_text for language filter:\n%s", q)
	}
	// The label values must arrive as parameterised args, not interpolated into SQL.
	if strings.Contains(q, "language:go") {
		t.Errorf("language value was interpolated into SQL instead of parameterised:\n%s", q)
	}
	found := false
	for _, arg := range db.queries[1].args {
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
		t.Errorf("expected a []string arg containing language: prefixed values; args = %#v", db.queries[1].args)
	}
}

func TestEshuSearchIndexStoreNoLanguageFilterOmitsLabelPredicate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(100), false}}},
			{rows: [][]any{}},
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

	if len(db.queries) < 2 {
		t.Fatalf("queries = %d, want at least 2", len(db.queries))
	}
	q := db.queries[1].query
	if strings.Contains(q, "jsonb_array_elements_text") {
		t.Errorf("query unexpectedly contains language filter when no languages requested:\n%s", q)
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

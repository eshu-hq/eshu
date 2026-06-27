// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestEshuSearchDocumentStoreListsActiveDocuments(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"reducer_eshu_search_document:abc",
				"scope-1",
				"gen-1",
				"content_entities",
				observedAt,
				[]byte(`{
					"reducer_domain":"eshu_search_document",
					"scope_id":"scope-1",
					"generation_id":"gen-1",
					"repo_id":"repo-1",
					"source_kind":"code_entity",
					"document":{
						"ID":"searchdoc:content_entity:e-1",
						"RepoID":"repo-1",
						"SourceKind":"code_entity",
						"Title":"Function Handle",
						"GraphHandles":[{"Kind":"content_entity","ID":"e-1"}],
						"TruthScope":{"Level":"derived","Basis":"content_index"},
						"Freshness":{"State":"fresh"}
					}
				}`),
			}}},
		},
	}
	store := NewEshuSearchDocumentStore(db)

	rows, err := store.ListActiveDocuments(context.Background(), EshuSearchDocumentFilter{
		ScopeID:     "scope-1",
		RepoID:      "repo-1",
		SourceKinds: []searchdocs.SourceKind{searchdocs.SourceKindCodeEntity},
		Limit:       25,
	})
	if err != nil {
		t.Fatalf("ListActiveDocuments error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Document.ID != "searchdoc:content_entity:e-1" {
		t.Errorf("document id = %q", row.Document.ID)
	}
	if row.Document.TruthScope.Level != searchdocs.TruthLevelDerived {
		t.Errorf("truth level = %q, want derived", row.Document.TruthScope.Level)
	}
	if row.GenerationID != "gen-1" {
		t.Errorf("generation = %q, want gen-1", row.GenerationID)
	}

	// Verify the query bounds to the active generation and applies filters.
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, fragment := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = false",
		"scope.active_generation_id = fact.generation_id",
		"fact.payload->>'repo_id'",
		"fact.payload->>'source_kind' IN",
		"LIMIT",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	if got, want := db.queries[0].args[0], EshuSearchDocumentFactKind; got != want {
		t.Errorf("fact kind arg = %v, want %v", got, want)
	}
}

func TestEshuSearchDocumentStoreAnchorsExplicitGeneration(t *testing.T) {
	t.Parallel()

	query, args := buildEshuSearchDocumentQuery(EshuSearchDocumentFilter{
		ScopeID:      "scope-1",
		GenerationID: "gen-anchor",
		Limit:        25,
	})
	if strings.Contains(query, "scope.active_generation_id = fact.generation_id") {
		t.Fatalf("query should not require current active generation when anchored:\n%s", query)
	}
	if !strings.Contains(query, "fact.generation_id = $3") {
		t.Fatalf("query missing explicit generation predicate:\n%s", query)
	}
	if got, want := args[2], "gen-anchor"; got != want {
		t.Fatalf("generation arg = %v, want %v", got, want)
	}
}

func TestEshuSearchDocumentStoreRequiresScope(t *testing.T) {
	t.Parallel()

	store := NewEshuSearchDocumentStore(&fakeExecQueryer{})
	if _, err := store.ListActiveDocuments(context.Background(), EshuSearchDocumentFilter{}); err == nil {
		t.Fatal("expected error when scope id is missing")
	}
}

func TestEshuSearchDocumentStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := (EshuSearchDocumentStore{}).ListActiveDocuments(context.Background(), EshuSearchDocumentFilter{ScopeID: "scope-1"}); err == nil {
		t.Fatal("expected error when database is nil")
	}
}

func TestEshuSearchDocumentStoreLanguageFilterAppendsLabelPredicate(t *testing.T) {
	t.Parallel()

	query, args := buildEshuSearchDocumentQuery(EshuSearchDocumentFilter{
		ScopeID:   "scope-1",
		Languages: []string{"go", "python"},
		Limit:     25,
	})
	if !strings.Contains(query, "jsonb_array_elements_text") {
		t.Errorf("query missing jsonb_array_elements_text for language filter:\n%s", query)
	}
	// Language values must arrive as parameterised args, not interpolated into SQL.
	if strings.Contains(query, "language:go") {
		t.Errorf("language value was interpolated into SQL instead of parameterised:\n%s", query)
	}
	found := false
	for _, arg := range args {
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
		t.Errorf("expected a []string arg containing language: prefixed values; args = %#v", args)
	}
}

func TestEshuSearchDocumentStoreNoLanguageFilterOmitsLabelPredicate(t *testing.T) {
	t.Parallel()

	query, _ := buildEshuSearchDocumentQuery(EshuSearchDocumentFilter{
		ScopeID: "scope-1",
		Limit:   25,
	})
	if strings.Contains(query, "jsonb_array_elements_text") {
		t.Errorf("query unexpectedly contains language filter when no languages requested:\n%s", query)
	}
}

func TestEshuSearchDocumentFilterCapsLimit(t *testing.T) {
	t.Parallel()

	got := normalizeEshuSearchDocumentFilter(EshuSearchDocumentFilter{ScopeID: "s", Limit: 10000})
	if got.Limit != eshuSearchDocumentMaxLimit {
		t.Errorf("limit = %d, want %d", got.Limit, eshuSearchDocumentMaxLimit)
	}
	def := normalizeEshuSearchDocumentFilter(EshuSearchDocumentFilter{ScopeID: "s"})
	if def.Limit != eshuSearchDocumentDefaultLimit {
		t.Errorf("default limit = %d, want %d", def.Limit, eshuSearchDocumentDefaultLimit)
	}
}

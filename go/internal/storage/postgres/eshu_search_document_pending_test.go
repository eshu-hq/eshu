// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestEshuSearchDocumentPendingStoreListsScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"git-repository-scope:repository:r_a", "gen-a", "github"},
				{"git-repository-scope:repository:r_b", "gen-b", "github"},
			}},
		},
	}
	store := NewEshuSearchDocumentPendingStore(db)

	scopes, err := store.ListPendingSearchDocumentScopes(context.Background(), 50)
	if err != nil {
		t.Fatalf("ListPendingSearchDocumentScopes error = %v", err)
	}
	if len(scopes) != 2 {
		t.Fatalf("scopes = %d, want 2", len(scopes))
	}
	if scopes[0].ScopeID != "git-repository-scope:repository:r_a" || scopes[0].GenerationID != "gen-a" || scopes[0].SourceSystem != "github" {
		t.Errorf("scope[0] = %+v", scopes[0])
	}

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, fragment := range []string{
		"FROM ingestion_scopes",
		"scope_kind = 'repository'",
		"active_generation_id IS NOT NULL",
		"payload->>'repo_id'",
		"FROM content_entities",
		"NOT EXISTS",
		"eshu_search_document_projection_state",
		"projection.state = 'ready'",
		"projection.document_count = idx.document_count",
		"eshu_search_index_stats",
		"LIMIT $1",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	if strings.Contains(q, "f.fact_kind") {
		t.Errorf("query uses search-document fact presence as completion; zero-document ready projections have no fact:\n%s", q)
	}
	if got := db.queries[0].args[0]; got != 50 {
		t.Errorf("limit arg = %v, want 50", got)
	}
}

func TestEshuSearchDocumentPendingStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	if _, err := (EshuSearchDocumentPendingStore{}).ListPendingSearchDocumentScopes(context.Background(), 10); err == nil {
		t.Fatal("expected error when database is nil")
	}
}

func TestEshuSearchDocumentPendingStoreCapsLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	store := NewEshuSearchDocumentPendingStore(db)
	if _, err := store.ListPendingSearchDocumentScopes(context.Background(), 100000); err != nil {
		t.Fatalf("error = %v", err)
	}
	if got := db.queries[0].args[0]; got != eshuSearchDocumentPendingMaxLimit {
		t.Errorf("capped limit = %v, want %d", got, eshuSearchDocumentPendingMaxLimit)
	}
}

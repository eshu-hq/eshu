// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestEshuSearchVectorPendingStoreListsScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"git-repository-scope:repository:r_a", "gen-a", "repo-a"},
				{"git-repository-scope:repository:r_b", "gen-b", "repo-b"},
			}},
		},
	}
	store := NewEshuSearchVectorPendingStore(db)

	scopes, err := store.ListPendingSearchVectorScopes(context.Background(), EshuSearchVectorPendingRequest{
		ProviderProfileID:  "semantic-search-default",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              50,
	})
	if err != nil {
		t.Fatalf("ListPendingSearchVectorScopes error = %v", err)
	}
	if len(scopes) != 2 {
		t.Fatalf("scopes = %d, want 2", len(scopes))
	}
	if scopes[0].ScopeID != "git-repository-scope:repository:r_a" || scopes[0].GenerationID != "gen-a" || scopes[0].RepoID != "repo-a" {
		t.Errorf("scope[0] = %+v", scopes[0])
	}

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, fragment := range []string{
		"WITH active_docs AS",
		"scope.scope_kind = 'repository'",
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"fact.payload->'document'->>'id' AS document_id",
		"fact.payload->>'content_hash' AS content_hash",
		"eshu_search_vector_metadata",
		"eshu_search_vector_values",
		"meta.provider_profile_id = $2",
		"meta.source_class = $3",
		"LEFT JOIN eshu_search_vector_values",
		"value.provider_profile_id = meta.provider_profile_id",
		"value.source_class = meta.source_class",
		"ready.document_id = docs.document_id",
		"ready.embedding_content_hash = docs.content_hash",
		"ready.provider_profile_id = $2",
		"ready.source_class = $3",
		"meta.build_state = 'ready'",
		"meta.build_state = 'disabled'",
		"value.document_id IS NOT NULL",
		"WHERE ready.document_id IS NULL",
		"LIMIT $6",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	if got, want := db.queries[0].args[0], EshuSearchDocumentFactKind; got != want {
		t.Errorf("fact kind arg = %v, want %v", got, want)
	}
	if got := db.queries[0].args[1]; got != "semantic-search-default" {
		t.Errorf("provider profile arg = %v, want semantic-search-default", got)
	}
	if got := db.queries[0].args[2]; got != "search_documents" {
		t.Errorf("source class arg = %v, want search_documents", got)
	}
	if got := db.queries[0].args[3]; got != "local-hash-v1" {
		t.Errorf("model arg = %v, want local-hash-v1", got)
	}
	if got := db.queries[0].args[4]; got != "vector-v1" {
		t.Errorf("version arg = %v, want vector-v1", got)
	}
	if got := db.queries[0].args[5]; got != 50 {
		t.Errorf("limit arg = %v, want 50", got)
	}
}

func TestEshuSearchVectorPendingStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := (EshuSearchVectorPendingStore{}).ListPendingSearchVectorScopes(
		context.Background(),
		EshuSearchVectorPendingRequest{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	)

	if err == nil {
		t.Fatal("expected error when database is nil")
	}
}

func TestEshuSearchVectorPendingStoreCapsLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	store := NewEshuSearchVectorPendingStore(db)
	_, err := store.ListPendingSearchVectorScopes(context.Background(), EshuSearchVectorPendingRequest{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              100000,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got := db.queries[0].args[5]; got != eshuSearchVectorPendingMaxLimit {
		t.Errorf("capped limit = %v, want %d", got, eshuSearchVectorPendingMaxLimit)
	}
}

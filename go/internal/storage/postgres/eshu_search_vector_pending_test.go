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
		// active_docs CTE (unchanged from original)
		"WITH active_docs AS",
		"scope.scope_kind = 'repository'",
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"fact.payload->>'document_id' AS document_id",
		"fact.payload->>'content_hash' AS content_hash",
		// NOT EXISTS correlated subquery shape (#4233 rewrite)
		"WHERE NOT EXISTS",
		"eshu_search_vector_metadata",
		"eshu_search_vector_values",
		"LEFT JOIN eshu_search_vector_values",
		"meta.provider_profile_id = $2",
		"meta.source_class = $3",
		"meta.embedding_model_id = $4",
		"meta.vector_index_version = $5",
		"meta.scope_id = docs.scope_id",
		"meta.generation_id = docs.generation_id",
		"meta.document_id = docs.document_id",
		"meta.embedding_content_hash = docs.content_hash",
		"meta.build_state = 'ready'",
		"meta.build_state = 'disabled'",
		"value.document_id IS NOT NULL",
		"LIMIT $6",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	// #4885 regression: the pending lister MUST read the top-level
	// payload->>'document_id' key the writer emits, NOT the nested
	// payload->'document'->>'id'. searchdocs.Document has no JSON tags, so its
	// ID field marshals as the capitalized key "ID"; reading lowercase 'id'
	// from the nested document object yields NULL, which makes the terminal
	// metadata NOT EXISTS join never match and returns every active scope as
	// pending on every sweep forever.
	if strings.Contains(q, "payload->'document'->>'id'") {
		t.Errorf("query reads the NULL-yielding nested document.id key (#4885); it must read payload->>'document_id':\n%s", q)
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

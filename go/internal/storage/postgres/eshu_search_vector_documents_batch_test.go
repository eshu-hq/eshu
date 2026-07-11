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

func TestEshuSearchDocumentStoreListsPendingVectorDocumentsForScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 3, 13, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"reducer_eshu_search_document:pending",
				"scope-a",
				"gen-a",
				"content_entities",
				observedAt,
				"projected-hash",
				[]byte(`{"document":{"ID":"doc-a","RepoID":"repo-a","SourceKind":"code_entity","Title":"Handler","TruthScope":{"Level":"derived"},"Freshness":{"State":"fresh"}}}`),
			}}},
		},
	}
	store := NewEshuSearchDocumentStore(db)

	rows, err := store.ListPendingVectorDocumentsForScopes(context.Background(), EshuSearchVectorDocumentBatchFilter{
		Scopes: []EshuSearchVectorDocumentScope{
			{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a"},
			{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b"},
		},
		SourceKinds:        []searchdocs.SourceKind{searchdocs.SourceKindCodeEntity},
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              500,
	})
	if err != nil {
		t.Fatalf("ListPendingVectorDocumentsForScopes error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if got, want := rows[0].Document.ID, "doc-a"; got != want {
		t.Fatalf("document id = %q, want %q", got, want)
	}
	if got, want := rows[0].ContentHash, "projected-hash"; got != want {
		t.Fatalf("content hash = %q, want %q", got, want)
	}

	q := db.queries[0].query
	for _, fragment := range []string{
		"WITH selected(scope_id, generation_id, repo_id) AS (VALUES",
		"JOIN LATERAL",
		"FROM eshu_search_index_documents AS doc",
		"doc.content_hash",
		"scope.active_generation_id = doc.generation_id",
		"doc.scope_id = selected.scope_id",
		"doc.generation_id = selected.generation_id",
		"doc.repo_id = selected.repo_id",
		"scope.scope_kind = 'repository'",
		"doc.source_kind IN",
		"meta.embedding_content_hash = doc.content_hash",
		"LEFT JOIN eshu_search_vector_values value",
		"meta.build_state = 'disabled'",
		"meta.build_state = 'ready'",
		"value.document_id IS NOT NULL",
		"LIMIT",
	} {
		if !strings.Contains(q, fragment) {
			t.Errorf("query missing %q:\n%s", fragment, q)
		}
	}
	for _, forbidden := range []string{"ORDER BY", "OFFSET", "fact_records", "fact.payload"} {
		if strings.Contains(q, forbidden) {
			t.Fatalf("batched pending vector document query contains forbidden fragment %q:\n%s", forbidden, q)
		}
	}
}

func TestEshuSearchDocumentStoreBatchFilterDoesNotMutateCallerScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewEshuSearchDocumentStore(db)
	filter := EshuSearchVectorDocumentBatchFilter{
		Scopes: []EshuSearchVectorDocumentScope{
			{ScopeID: " scope-a ", GenerationID: " gen-a ", RepoID: " repo-a "},
		},
		ProviderProfileID:  " local ",
		SourceClass:        " search_documents ",
		EmbeddingModelID:   " local-hash-v1 ",
		VectorIndexVersion: " vector-v1 ",
		Limit:              500,
	}

	if _, err := store.ListPendingVectorDocumentsForScopes(context.Background(), filter); err != nil {
		t.Fatalf("ListPendingVectorDocumentsForScopes error = %v", err)
	}

	if got, want := filter.Scopes[0].ScopeID, " scope-a "; got != want {
		t.Fatalf("caller scope id mutated to %q, want %q", got, want)
	}
	if got, want := filter.Scopes[0].GenerationID, " gen-a "; got != want {
		t.Fatalf("caller generation id mutated to %q, want %q", got, want)
	}
	if got, want := filter.Scopes[0].RepoID, " repo-a "; got != want {
		t.Fatalf("caller repo id mutated to %q, want %q", got, want)
	}
}

func TestEshuSearchDocumentStoreBatchFilterAllowsBoundedTailLimit(t *testing.T) {
	t.Parallel()

	filter := normalizeEshuSearchVectorDocumentBatchFilter(EshuSearchVectorDocumentBatchFilter{
		Limit: eshuSearchVectorDocumentBatchMaxLimit + 1,
	})
	if got, want := filter.Limit, eshuSearchVectorDocumentBatchMaxLimit; got != want {
		t.Fatalf("limit = %d, want bounded tail limit %d", got, want)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ListPendingSearchVectorScopes: query shape guards
// ---------------------------------------------------------------------------

func TestListPendingSearchVectorScopesQueryShape(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{}}},
	}
	store := NewEshuSearchVectorScopeStateStore(db)

	scopes, err := store.ListPendingSearchVectorScopes(context.Background(), EshuSearchVectorPendingRequest{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
		Limit:              50,
	})
	if err != nil {
		t.Fatalf("ListPendingSearchVectorScopes error = %v", err)
	}
	if len(scopes) != 0 {
		t.Fatalf("scopes = %d, want 0", len(scopes))
	}

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{
		"eshu_search_document_projection_state",
		"JOIN ingestion_scopes",
		"active_generation_id",
		"LEFT JOIN eshu_search_vector_scope_state",
		"state='ready'",
		"document_count",
		"vs.state IS NULL",
		"ORDER BY ps.scope_id",
		"LIMIT $5",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing %q:\n%s", want, q)
		}
	}
	// MUST NOT contain the old WITH active_docs CTE.
	if strings.Contains(q, "WITH active_docs") {
		t.Fatalf("query still contains old WITH active_docs CTE (not the versioned scoped query):\n%s", q)
	}
}

func TestListPendingSearchVectorScopesNoFactRecordsScan(t *testing.T) {
	t.Parallel()

	q := listPendingSearchVectorScopesScopedSQL
	// The new query must NOT reference fact_records — it uses
	// eshu_search_document_projection_state and eshu_search_vector_scope_state.
	if strings.Contains(q, "fact_records") {
		t.Fatalf("new query references fact_records (unbounded scan):\n%s", q)
	}
}

// ---------------------------------------------------------------------------
// ScopeVectorComplete: query shape guards
// ---------------------------------------------------------------------------

func TestScopeVectorCompleteQueryShape(t *testing.T) {
	t.Parallel()

	q := scopeVectorCompleteSQL

	// Count-gate guard (new — #4233 amortization).
	for _, want := range []string{
		"completion_gate",
		"terminal_count < document_count",
		"eshu_search_document_projection_state",
		"ps.state = 'ready'",
		"meta.build_state IN ('ready', 'disabled')",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing count-gate %q:\n%s", want, q)
		}
	}

	// The exact anti-join must use the persisted search-document projection,
	// which is the same one-row-per-document source consumed by the builder.
	for _, want := range []string{
		"FROM eshu_search_index_documents doc",
		"doc.scope_id = $1",
		"doc.generation_id = $2",
		"eshu_search_vector_metadata",
		"eshu_search_vector_values",
		"meta.provider_profile_id = $3",
		"meta.source_class = $4",
		"meta.embedding_model_id = $5",
		"meta.vector_index_version = $6",
		"meta.embedding_content_hash = doc.content_hash",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing anti-join %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "fact_records") || strings.Contains(q, "fact.payload") {
		t.Fatalf("exact completeness query must not rescan fact_records JSON:\n%s", q)
	}
}

// ---------------------------------------------------------------------------
// ListPendingSearchVectorScopes: validation and limit clamping
// ---------------------------------------------------------------------------

func TestListPendingSearchVectorScopesRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := (EshuSearchVectorScopeStateStore{}).ListPendingSearchVectorScopes(
		context.Background(),
		EshuSearchVectorPendingRequest{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "m1",
			VectorIndexVersion: "v1",
		},
	)
	if err == nil {
		t.Fatal("expected error when database is nil")
	}
}

func TestListPendingSearchVectorScopesCapsLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	store := NewEshuSearchVectorScopeStateStore(db)
	_, err := store.ListPendingSearchVectorScopes(context.Background(), EshuSearchVectorPendingRequest{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
		Limit:              100000,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got := db.queries[0].args[4]; got != eshuSearchVectorPendingMaxLimit {
		t.Fatalf("capped limit = %v, want %d", got, eshuSearchVectorPendingMaxLimit)
	}
}

// ---------------------------------------------------------------------------
// ScopeVectorComplete: single scope bound
// ---------------------------------------------------------------------------

func TestScopeVectorCompleteRequiresDatabase(t *testing.T) {
	t.Parallel()

	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}
	_, err := (EshuSearchVectorScopeStateStore{}).ScopeVectorComplete(
		context.Background(), "scope-1", "gen-1", identity,
	)
	if err == nil {
		t.Fatal("expected error when database is nil")
	}
}

func TestScopeVectorCompleteReturnsTrue(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true}}},
		},
	}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}

	complete, err := store.ScopeVectorComplete(context.Background(), "scope-1", "gen-1", identity)
	if err != nil {
		t.Fatalf("ScopeVectorComplete error = %v", err)
	}
	if !complete {
		t.Fatal("ScopeVectorComplete = false, want true")
	}
}

func TestScopeVectorCompleteReturnsFalse(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{false}}},
		},
	}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}

	complete, err := store.ScopeVectorComplete(context.Background(), "scope-1", "gen-1", identity)
	if err != nil {
		t.Fatalf("ScopeVectorComplete error = %v", err)
	}
	if complete {
		t.Fatal("ScopeVectorComplete = true, want false")
	}
}

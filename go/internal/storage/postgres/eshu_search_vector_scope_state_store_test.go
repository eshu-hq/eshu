// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Vector scope state store: BeginBuilding
// ---------------------------------------------------------------------------

func TestEshuSearchVectorScopeStateBeginBuilding(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(1)}}},
		},
	}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}

	fence, err := store.BeginBuilding(context.Background(), "scope-1", "gen-1", identity, 2)
	if err != nil {
		t.Fatalf("BeginBuilding error = %v", err)
	}
	if fence != 1 {
		t.Fatalf("fence = %d, want 1", fence)
	}

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{
		"INSERT INTO eshu_search_vector_scope_state",
		"FROM eshu_search_document_projection_state projection",
		"projection.state = 'ready'",
		"projection.projection_revision = $7",
		"scope.active_generation_id = projection.generation_id",
		"ON CONFLICT (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE",
		"build_fence = COALESCE(eshu_search_vector_scope_state.build_fence, 0) + 1",
		"EXCLUDED.projection_revision > eshu_search_vector_scope_state.projection_revision",
		"eshu_search_vector_scope_state.state <> 'ready'",
		"state = 'building'",
		"document_cursor = CASE",
		"EXCLUDED.projection_revision > eshu_search_vector_scope_state.projection_revision",
		"RETURNING build_fence",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing %q:\n%s", want, q)
		}
	}
	// args: $1=scope, $2=gen, $3=provider, $4=source, $5=model, $6=version, $7=revision, $8=now
	if got, want := db.queries[0].args[0], "scope-1"; got != want {
		t.Fatalf("$1 = %v", got)
	}
	if got, want := db.queries[0].args[3], "search_documents"; got != want {
		t.Fatalf("$4 (source) = %v", got)
	}
	if got, want := db.queries[0].args[6], int64(2); got != want {
		t.Fatalf("$7 (revision) = %v", got)
	}
}

// ---------------------------------------------------------------------------
// Vector scope state store: FinalizeReady CAS
// ---------------------------------------------------------------------------

func TestEshuSearchVectorScopeStateFinalizeReadyCAS(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}

	ok, err := store.FinalizeReady(context.Background(), "scope-1", "gen-1", identity, 1, 1)
	if err != nil {
		t.Fatalf("FinalizeReady error = %v", err)
	}
	if !ok {
		t.Fatal("FinalizeReady = false, want true")
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	q := db.execs[0].query
	for _, want := range []string{
		"UPDATE eshu_search_vector_scope_state",
		"SET state = 'ready'",
		"generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)",
		"projection_revision = $7",
		"build_fence = $8",
		"projection.state = 'ready'",
		"projection.projection_revision = $7",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing %q:\n%s", want, q)
		}
	}
	// 9 args: scope, gen, provider, source, model, version, revision, fence, now
	if got, want := len(db.execs[0].args), 9; got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[0], "scope-1"; got != want {
		t.Fatalf("$1 = %v", got)
	}
	if got, want := db.execs[0].args[6], int64(1); got != want {
		t.Fatalf("$7 (revision) = %v", got)
	}
	if got, want := db.execs[0].args[7], int64(1); got != want {
		t.Fatalf("$8 (fence) = %v", got)
	}
}

func TestEshuSearchVectorScopeStateFinalizeReadyStaleReturnsFalse(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{zeroRowsResult{}},
	}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}

	ok, err := store.FinalizeReady(context.Background(), "scope-1", "gen-1", identity, 1, 1)
	if err != nil {
		t.Fatalf("FinalizeReady error = %v", err)
	}
	if ok {
		t.Fatal("FinalizeReady = true, want false (rows affected = 0)")
	}
}

func TestEshuSearchVectorScopeStateAdvancesCursorWithFenceCAS(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "m1", VectorIndexVersion: "v1",
	}
	ok, err := store.AdvanceDocumentCursor(context.Background(), "scope-1", "gen-1", identity, 2, 3, "doc-009")
	if err != nil {
		t.Fatalf("AdvanceDocumentCursor error = %v", err)
	}
	if !ok {
		t.Fatal("AdvanceDocumentCursor = false, want true")
	}
	q := db.execs[0].query
	for _, want := range []string{
		"UPDATE eshu_search_vector_scope_state",
		"document_cursor = GREATEST(document_cursor, $9)",
		"projection_revision = $7",
		"build_fence = $8",
		"state = 'building'",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("advance query missing %q:\n%s", want, q)
		}
	}
}

func TestEshuSearchVectorScopeStateResetsCursorWithFenceCAS(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{zeroRowsResult{}}}
	store := NewEshuSearchVectorScopeStateStore(db)
	identity := EshuSearchVectorIdentity{
		ProviderProfileID: "local", SourceClass: "search_documents",
		EmbeddingModelID: "m1", VectorIndexVersion: "v1",
	}
	ok, err := store.ResetDocumentCursor(context.Background(), "scope-1", "gen-1", identity, 2, 2)
	if err != nil {
		t.Fatalf("ResetDocumentCursor error = %v", err)
	}
	if ok {
		t.Fatal("ResetDocumentCursor = true, want stale-fence rejection")
	}
	q := db.execs[0].query
	for _, want := range []string{
		"document_cursor = ''",
		"projection_revision = $7",
		"build_fence = $8",
		"state = 'building'",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("reset query missing %q:\n%s", want, q)
		}
	}
}

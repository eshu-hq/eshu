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
		"ON CONFLICT (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version) DO UPDATE",
		"build_fence = COALESCE(eshu_search_vector_scope_state.build_fence, 0) + 1",
		"state = 'building'",
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
		"build_fence <= $8",
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

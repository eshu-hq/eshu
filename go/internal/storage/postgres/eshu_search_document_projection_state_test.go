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
// Projection state store: BeginBuilding
// ---------------------------------------------------------------------------

func TestEshuSearchDocumentProjectionStateBeginBuilding(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(1), int64(1)}}},
		},
	}
	store := NewEshuSearchDocumentProjectionStateStore(db)

	revision, fence, err := store.BeginBuilding(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("BeginBuilding error = %v", err)
	}
	if revision != 1 || fence != 1 {
		t.Fatalf("BeginBuilding = (%d, %d), want (1, 1)", revision, fence)
	}

	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{
		"INSERT INTO eshu_search_document_projection_state",
		"ON CONFLICT (scope_id, generation_id) DO UPDATE",
		"projection_revision = eshu_search_document_projection_state.projection_revision + 1",
		"build_fence = eshu_search_document_projection_state.build_fence + 1",
		"state = 'building'",
		"RETURNING projection_revision, build_fence",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing %q:\n%s", want, q)
		}
	}
}

func TestEshuSearchDocumentProjectionStateBeginBuildingRejectsEmptyScope(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchDocumentProjectionStateStore(db)

	_, _, err := store.BeginBuilding(context.Background(), "", "gen-1")
	if err == nil {
		t.Fatal("BeginBuilding error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "scope id") {
		t.Fatalf("error missing scope id: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Projection state store: FinalizeReady CAS
// ---------------------------------------------------------------------------

func TestEshuSearchDocumentProjectionStateFinalizeReadyCAS(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchDocumentProjectionStateStore(db)

	ok, err := store.FinalizeReady(context.Background(), "scope-1", "gen-1", 1, 1, 42)
	if err != nil {
		t.Fatalf("FinalizeReady error = %v", err)
	}
	if !ok {
		t.Fatal("FinalizeReady = false, want true (rows affected = 1)")
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	q := db.execs[0].query
	for _, want := range []string{
		"UPDATE eshu_search_document_projection_state",
		"SET state = 'ready'",
		"document_count = $5",
		"generation_id = $2",
		"generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)",
		"projection_revision = $3",
		"build_fence = $4",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing %q:\n%s", want, q)
		}
	}
	// args: $1=scopeID, $2=generationID, $3=revision, $4=fence,
	// $5=documentCount, $6=now
	if got, want := db.execs[0].args[0], "scope-1"; got != want {
		t.Fatalf("$1 = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "gen-1"; got != want {
		t.Fatalf("$2 (generation) = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[2], int64(1); got != want {
		t.Fatalf("$3 (revision) = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], int64(1); got != want {
		t.Fatalf("$4 (fence) = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[4], int64(42); got != want {
		t.Fatalf("$5 (docCount) = %v, want %v", got, want)
	}
	if len(db.execs[0].args) != 6 {
		t.Fatalf("arg count = %d, want 6", len(db.execs[0].args))
	}
}

func TestEshuSearchDocumentProjectionStateFinalizeReadyStaleReturnsFalse(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{zeroRowsResult{}},
	}
	store := NewEshuSearchDocumentProjectionStateStore(db)

	ok, err := store.FinalizeReady(context.Background(), "scope-1", "gen-1", 1, 1, 42)
	if err != nil {
		t.Fatalf("FinalizeReady error = %v", err)
	}
	if ok {
		t.Fatal("FinalizeReady = true, want false (rows affected = 0)")
	}
}

// ---------------------------------------------------------------------------
// Projection state store: MarkFailed CAS
// ---------------------------------------------------------------------------

func TestEshuSearchDocumentProjectionStateMarkFailedCAS(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchDocumentProjectionStateStore(db)

	ok, err := store.MarkFailed(context.Background(), "scope-1", "gen-1", 1, 1)
	if err != nil {
		t.Fatalf("MarkFailed error = %v", err)
	}
	if !ok {
		t.Fatal("MarkFailed = false, want true")
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	q := db.execs[0].query
	for _, want := range []string{
		"UPDATE eshu_search_document_projection_state",
		"SET state = 'failed'",
		"generation_id = $2",
		"generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)",
		"projection_revision = $3",
		"build_fence = $4",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("query missing %q:\n%s", want, q)
		}
	}
	if got, want := db.execs[0].args[1], "gen-1"; got != want {
		t.Fatalf("$2 (generation) = %v, want %v", got, want)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
)

// TestEshuSearchVectorBuildReadyStorePublishesWatermark proves
// PublishSearchVectorReady issues the singleton upsert against
// search_vector_build_materialization exactly once.
func TestEshuSearchVectorBuildReadyStorePublishesWatermark(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorBuildReadyStore(db)

	if err := store.PublishSearchVectorReady(context.Background()); err != nil {
		t.Fatalf("PublishSearchVectorReady error = %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	if db.execs[0].query != upsertSearchVectorBuildReadyQuery {
		t.Fatalf("issued the wrong upsert query: %q", db.execs[0].query)
	}
}

// TestEshuSearchVectorBuildReadyStorePropagatesExecError proves a failed
// upsert is reported, not swallowed.
func TestEshuSearchVectorBuildReadyStorePropagatesExecError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execErrors: []error{errors.New("write failed")}}
	store := NewEshuSearchVectorBuildReadyStore(db)

	if err := store.PublishSearchVectorReady(context.Background()); err == nil {
		t.Fatal("expected the exec error to propagate")
	}
}

// TestEshuSearchVectorBuildReadyStoreRequiresDB proves a nil database is a
// reported error rather than a panic.
func TestEshuSearchVectorBuildReadyStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewEshuSearchVectorBuildReadyStore(nil)
	if err := store.PublishSearchVectorReady(context.Background()); err == nil {
		t.Fatal("expected an error for a nil database")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
)

// TestRelationshipStoreIsGenerationActive proves the active-status lookup that
// backs the repo-dependency graph-projection authority gate: a generation reads
// active only after it has been activated, and a superseded generation reads
// inactive.
func TestRelationshipStoreIsGenerationActive(t *testing.T) {
	t.Parallel()

	db := newRelationshipTestDB()
	store := NewRelationshipStore(db)
	ctx := context.Background()

	if active, err := store.IsGenerationActive(ctx, "gen-1"); err != nil || active {
		t.Fatalf("IsGenerationActive(gen-1) before activation = (%v, %v), want (false, nil)", active, err)
	}

	if err := store.ActivateResolutionGeneration(ctx, "gen-1", "repo_deps"); err != nil {
		t.Fatalf("ActivateResolutionGeneration(gen-1): %v", err)
	}
	if active, err := store.IsGenerationActive(ctx, "gen-1"); err != nil || !active {
		t.Fatalf("IsGenerationActive(gen-1) after activation = (%v, %v), want (true, nil)", active, err)
	}

	// Activating a newer generation supersedes gen-1, which must then read
	// inactive so the gate stops granting authority to stale generations.
	if err := store.ActivateResolutionGeneration(ctx, "gen-2", "repo_deps"); err != nil {
		t.Fatalf("ActivateResolutionGeneration(gen-2): %v", err)
	}
	if active, err := store.IsGenerationActive(ctx, "gen-1"); err != nil || active {
		t.Fatalf("IsGenerationActive(gen-1) after supersede = (%v, %v), want (false, nil)", active, err)
	}
	if active, err := store.IsGenerationActive(ctx, "gen-2"); err != nil || !active {
		t.Fatalf("IsGenerationActive(gen-2) = (%v, %v), want (true, nil)", active, err)
	}
}

// TestNewRelationshipGenerationActiveLookupAdaptsStore proves the postgres
// adapter wires RelationshipStore.IsGenerationActive into the reducer authority
// gate and treats a blank generation id as inactive without a query.
func TestNewRelationshipGenerationActiveLookupAdaptsStore(t *testing.T) {
	t.Parallel()

	db := newRelationshipTestDB()
	store := NewRelationshipStore(db)
	lookup := NewRelationshipGenerationActiveLookup(store)

	if active, err := lookup("   "); err != nil || active {
		t.Fatalf("lookup(blank) = (%v, %v), want (false, nil)", active, err)
	}

	if err := store.ActivateResolutionGeneration(context.Background(), "gen-9", "repo_deps"); err != nil {
		t.Fatalf("ActivateResolutionGeneration(gen-9): %v", err)
	}
	if active, err := lookup("gen-9"); err != nil || !active {
		t.Fatalf("lookup(gen-9) = (%v, %v), want (true, nil)", active, err)
	}
}

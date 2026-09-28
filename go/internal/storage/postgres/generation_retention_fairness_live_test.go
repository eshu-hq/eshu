// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestGenerationRetentionFairnessAcrossScopesLive is P3: 30 eligible scopes,
// scope_id order opposed to age (the alphabetically-first scope owns the
// newest generation, the alphabetically-last owns the oldest), so a batch
// bounded to the first ten by scope_id would be the ten NEWEST, the opposite
// of what retention should reclaim first. The shipped store's batch must
// equal the oracle's oldest ten regardless of scope_id order.
func TestGenerationRetentionFairnessAcrossScopesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeCount = 30
	const batchLimit = 10

	type generation struct {
		id           string
		supersededAt time.Time
	}
	var generations []generation
	for i := 0; i < scopeCount; i++ {
		scopeID := retentionSelectionScopeID("fair", i)
		seedRetentionSelectionScope(t, ctx, database, scopeID)
		// Scope i=0 (sorts first) is newest; i=scopeCount-1 (sorts last) is
		// oldest, so scope_id order directly opposes age order.
		supersededAt := now.Add(-time.Duration(scopeCount-i) * time.Hour * 24)
		generationID := scopeID + "-g0"
		seedRetentionSelectionSupersededGeneration(t, ctx, database, scopeID, generationID, supersededAt)
		generations = append(generations, generation{id: generationID, supersededAt: supersededAt})
	}

	// Oracle: the batchLimit oldest generations by (supersededAt, generationID).
	oldest := append([]generation{}, generations...)
	for i := 0; i < len(oldest); i++ {
		for j := i + 1; j < len(oldest); j++ {
			if oldest[j].supersededAt.Before(oldest[i].supersededAt) ||
				(oldest[j].supersededAt.Equal(oldest[i].supersededAt) && oldest[j].id < oldest[i].id) {
				oldest[i], oldest[j] = oldest[j], oldest[i]
			}
		}
	}
	oracle := map[string]bool{}
	for _, g := range oldest[:batchLimit] {
		oracle[g.id] = true
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, retentionSelectionPolicy(0, 7*24*time.Hour, batchLimit))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != batchLimit {
		t.Fatalf("GenerationsPruned = %d, want %d", result.GenerationsPruned, batchLimit)
	}

	var ids []string
	for _, g := range generations {
		ids = append(ids, g.id)
	}
	remaining := remainingScopeGenerations(t, ctx, database, ids)
	for _, g := range generations {
		wantPruned := oracle[g.id]
		gone := !remaining[g.id]
		if wantPruned != gone {
			t.Errorf("generation %s (superseded %s): pruned=%v, want pruned=%v (oracle oldest %d)", g.id, g.supersededAt, gone, wantPruned, batchLimit)
		}
	}
}

// TestGenerationRetentionSkipsHeldScopeAndReplacesLive is P4: a second
// session holds the globally-oldest eligible scope's row FOR UPDATE
// (uncommitted). The store's pass must not wait for it: it is bounded by a
// short context deadline that only a genuine wait (not SKIP LOCKED's
// instant skip) could exceed, and the next-oldest candidate is pruned in its
// place.
func TestGenerationRetentionSkipsHeldScopeAndReplacesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()

	oldestScope, nextScope := "skip-000", "skip-001"
	seedRetentionSelectionScope(t, ctx, database, oldestScope)
	seedRetentionSelectionScope(t, ctx, database, nextScope)
	oldestGeneration := oldestScope + "-g0"
	nextGeneration := nextScope + "-g0"
	seedRetentionSelectionSupersededGeneration(t, ctx, database, oldestScope, oldestGeneration, now.Add(-10*24*time.Hour))
	seedRetentionSelectionSupersededGeneration(t, ctx, database, nextScope, nextGeneration, now.Add(-9*24*time.Hour))

	holderTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer func() { _ = holderTx.Rollback() }()
	if _, err := holderTx.ExecContext(ctx, `SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR UPDATE`, oldestScope); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	boundedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(boundedCtx, retentionSelectionPolicy(0, 7*24*time.Hour, 1))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v (a genuine wait on the held scope would exceed the bounded context)", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}

	remaining := remainingScopeGenerations(t, ctx, database, []string{oldestGeneration, nextGeneration})
	if !remaining[oldestGeneration] {
		t.Errorf("held-scope generation %s was pruned, want retained (its scope row was locked by another session)", oldestGeneration)
	}
	if remaining[nextGeneration] {
		t.Errorf("replacement generation %s was retained, want pruned", nextGeneration)
	}
}

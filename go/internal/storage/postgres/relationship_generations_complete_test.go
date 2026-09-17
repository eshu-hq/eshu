// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
)

// TestRelationshipStoreActiveScopeGenerationsComplete proves the corpus-wide
// fence backing the workload and deployable-unit correlation input gates: the
// load may proceed only when every active scope's current relationship
// generation is active, so the by-repos resolved read cannot serve a partial
// foreign set that varies run to run (#6184).
func TestRelationshipStoreActiveScopeGenerationsComplete(t *testing.T) {
	t.Parallel()

	seed := func(scopes map[string]scopeRecord, generations map[string]generationRecord) *RelationshipStore {
		db := newRelationshipTestDB()
		for id, scope := range scopes {
			db.scopes[id] = scope
		}
		for id, gen := range generations {
			db.generations[id] = gen
		}
		return NewRelationshipStore(db)
	}
	ctx := context.Background()

	t.Run("no scopes is vacuously complete", func(t *testing.T) {
		t.Parallel()
		if complete, err := seed(nil, nil).AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})

	t.Run("active scope with active current generation is complete", func(t *testing.T) {
		t.Parallel()
		store := seed(
			map[string]scopeRecord{"s1": {status: "active", activeGenerationID: "g1"}},
			map[string]generationRecord{"g1": {scope: "s1", status: "active"}},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})

	t.Run("retired generation pending re-resolution is incomplete", func(t *testing.T) {
		t.Parallel()
		store := seed(
			map[string]scopeRecord{"s1": {status: "active", activeGenerationID: "g1"}},
			map[string]generationRecord{"g1": {scope: "s1", status: "superseded"}},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || complete {
			t.Fatalf("complete() = (%v, %v), want (false, nil)", complete, err)
		}
	})

	t.Run("scope resolving for the first time is incomplete", func(t *testing.T) {
		t.Parallel()
		store := seed(
			map[string]scopeRecord{
				"s1": {status: "active", activeGenerationID: "g1"},
				"s2": {status: "active", activeGenerationID: "g2"},
			},
			map[string]generationRecord{"g1": {scope: "s1", status: "active"}},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || complete {
			t.Fatalf("complete() = (%v, %v), want (false, nil)", complete, err)
		}
	})

	t.Run("retired scope never holds the gate", func(t *testing.T) {
		t.Parallel()
		store := seed(
			map[string]scopeRecord{
				"s1": {status: "active", activeGenerationID: "g1"},
				"s9": {status: "retired", activeGenerationID: "g9"},
			},
			map[string]generationRecord{
				"g1": {scope: "s1", status: "active"},
				"g9": {scope: "s9", status: "superseded"},
			},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})

	t.Run("superseded older generation never holds the gate", func(t *testing.T) {
		t.Parallel()
		store := seed(
			map[string]scopeRecord{"s1": {status: "active", activeGenerationID: "g2"}},
			map[string]generationRecord{
				"g1": {scope: "s1", status: "superseded"},
				"g2": {scope: "s1", status: "active"},
			},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})
}

// TestNewRelationshipGenerationsCompleteLookupAdaptsStore proves the postgres
// adapter wires RelationshipStore into the corpus-wide input gate.
func TestNewRelationshipGenerationsCompleteLookupAdaptsStore(t *testing.T) {
	t.Parallel()

	db := newRelationshipTestDB()
	db.scopes["s1"] = scopeRecord{status: "active", activeGenerationID: "g1"}
	store := NewRelationshipStore(db)
	lookup := NewRelationshipGenerationsCompleteLookup(store)

	if complete, err := lookup(); err != nil || complete {
		t.Fatalf("lookup() before activation = (%v, %v), want (false, nil)", complete, err)
	}

	db.generations["g1"] = generationRecord{scope: "s1", status: "active"}
	if complete, err := lookup(); err != nil || !complete {
		t.Fatalf("lookup() after activation = (%v, %v), want (true, nil)", complete, err)
	}
}

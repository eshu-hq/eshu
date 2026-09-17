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

	seed := func(scopes map[string]scopeRecord, generations map[string]generationRecord, items []workItemRecord) *RelationshipStore {
		db := newRelationshipTestDB()
		for id, scope := range scopes {
			db.scopes[id] = scope
		}
		for id, gen := range generations {
			db.generations[id] = gen
		}
		db.workItems = append(db.workItems, items...)
		return NewRelationshipStore(db)
	}
	ctx := context.Background()

	t.Run("no scopes is vacuously complete", func(t *testing.T) {
		t.Parallel()
		if complete, err := seed(nil, nil, nil).AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})

	t.Run("active scope with active current generation is complete", func(t *testing.T) {
		t.Parallel()
		store := seed(
			map[string]scopeRecord{"s1": {status: "active", activeGenerationID: "g1"}},
			map[string]generationRecord{"g1": {scope: "s1", status: "active"}},
			nil,
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
			nil,
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
			[]workItemRecord{{scopeID: "s2", stage: "reducer", domain: "deployment_mapping", status: "retrying"}},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || complete {
			t.Fatalf("complete() = (%v, %v), want (false, nil)", complete, err)
		}
	})

	t.Run("scope with no generation row and no resolution work is complete", func(t *testing.T) {
		t.Parallel()
		// Live deadlock (#6730): an active scope whose generation row was
		// never created because cross-repo resolution never ran for it (no
		// deployment_mapping item in any status, e.g. an orphan repo with
		// nothing to resolve) contributes no rows to the by-repos read, so
		// it must not hold the gate waiting on a row that can never appear.
		store := seed(
			map[string]scopeRecord{
				"s1": {status: "active", activeGenerationID: "g1"},
				"s2": {status: "active", activeGenerationID: "g2"},
			},
			map[string]generationRecord{"g1": {scope: "s1", status: "active"}},
			nil,
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})

	t.Run("terminal resolution work never holds the gate", func(t *testing.T) {
		t.Parallel()
		// A succeeded or dead-lettered deployment_mapping item can no longer
		// create the scope's generation row: the former already activated it
		// (the row would exist), the latter is owned by dead-letter
		// handling, so neither keeps a row-less scope waiting.
		store := seed(
			map[string]scopeRecord{
				"s1": {status: "active", activeGenerationID: "g1"},
				"s2": {status: "active", activeGenerationID: "g2"},
			},
			map[string]generationRecord{"g1": {scope: "s1", status: "active"}},
			[]workItemRecord{
				{scopeID: "s2", stage: "reducer", domain: "deployment_mapping", status: "succeeded"},
				{scopeID: "s2", stage: "reducer", domain: "deployment_mapping", status: "dead_letter"},
			},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
		}
	})

	t.Run("resolution work in another domain never holds the gate", func(t *testing.T) {
		t.Parallel()
		// Only deployment_mapping runs cross-repo resolution (the sole
		// generation-row writer), so a live item in any other domain says
		// nothing about whether the scope's generation row is forthcoming.
		store := seed(
			map[string]scopeRecord{
				"s1": {status: "active", activeGenerationID: "g1"},
				"s2": {status: "active", activeGenerationID: "g2"},
			},
			map[string]generationRecord{"g1": {scope: "s1", status: "active"}},
			[]workItemRecord{{scopeID: "s2", stage: "reducer", domain: "workload_materialization", status: "retrying"}},
		)
		if complete, err := store.AreActiveScopeRelationshipGenerationsComplete(ctx); err != nil || !complete {
			t.Fatalf("complete() = (%v, %v), want (true, nil)", complete, err)
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
			nil,
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
			nil,
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
	db.workItems = append(db.workItems, workItemRecord{scopeID: "s1", stage: "reducer", domain: "deployment_mapping", status: "claimed"})
	store := NewRelationshipStore(db)
	lookup := NewRelationshipGenerationsCompleteLookup(store)

	if complete, err := lookup(); err != nil || complete {
		t.Fatalf("lookup() before activation = (%v, %v), want (false, nil)", complete, err)
	}

	db.generations["g1"] = generationRecord{scope: "s1", status: "active"}
	db.workItems = nil
	if complete, err := lookup(); err != nil || !complete {
		t.Fatalf("lookup() after activation = (%v, %v), want (true, nil)", complete, err)
	}
}

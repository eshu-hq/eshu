// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

	if complete, err := lookup(context.Background()); err != nil || complete {
		t.Fatalf("lookup() before activation = (%v, %v), want (false, nil)", complete, err)
	}

	db.generations["g1"] = generationRecord{scope: "s1", status: "active"}
	db.workItems = nil
	if complete, err := lookup(context.Background()); err != nil || !complete {
		t.Fatalf("lookup() after activation = (%v, %v), want (true, nil)", complete, err)
	}
}

// emptyCompletenessRowsStub is an ExecQueryer stub whose query surface
// returns a healthy empty row set: no rows, no iteration error. It drives
// the defensive no-rows branch of
// AreActiveScopeRelationshipGenerationsComplete, which the table fake
// cannot reach because the NOT EXISTS aggregate always yields one row.
type emptyCompletenessRowsStub struct{}

func (emptyCompletenessRowsStub) Next() bool { return false }

func (emptyCompletenessRowsStub) Scan(...any) error {
	return errors.New("scan called with no current row")
}

func (emptyCompletenessRowsStub) Err() error { return nil }

func (emptyCompletenessRowsStub) Close() error { return nil }

type emptyCompletenessDBStub struct{}

func (emptyCompletenessDBStub) QueryContext(context.Context, string, ...any) (Rows, error) {
	return emptyCompletenessRowsStub{}, nil
}

func (emptyCompletenessDBStub) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("exec not supported")
}

// TestAreActiveScopeRelationshipGenerationsCompleteNoRowsIsConcreteError
// pins the Copilot #6730 finding: a healthy empty row set must produce a
// concrete "no rows" error, never a fmt %w wrap over a nil rows.Err()
// (which formats as %!w(<nil>) and unwraps to nil, turning "no rows" into
// an error value that claims no cause).
func TestAreActiveScopeRelationshipGenerationsCompleteNoRowsIsConcreteError(t *testing.T) {
	t.Parallel()

	store := NewRelationshipStore(emptyCompletenessDBStub{})
	complete, err := store.AreActiveScopeRelationshipGenerationsComplete(context.Background())
	if err == nil {
		t.Fatal("complete() error = nil, want a concrete no-rows error")
	}
	if complete {
		t.Fatal("complete() = true with no rows, want false")
	}
	if strings.Contains(err.Error(), "%!") {
		t.Fatalf("complete() error = %q, want a clean message without a nil-%%w format defect", err.Error())
	}
	if errors.Unwrap(err) != nil {
		t.Fatalf("complete() error unwraps to %v, want a concrete no-cause error", errors.Unwrap(err))
	}
}

// TestIncompleteActiveScopeRelationshipGenerationsListsHolders pins the #6730
// owner finding: the holder-listing query must name exactly the scopes whose
// generations keep the boolean fence incomplete, sharing its predicate.
func TestIncompleteActiveScopeRelationshipGenerationsListsHolders(t *testing.T) {
	t.Parallel()

	db := newRelationshipTestDB()
	db.scopes["s-holding"] = scopeRecord{status: "active", activeGenerationID: "g1"}
	db.generations["g1"] = generationRecord{scope: "s-holding", status: "superseded"}
	db.scopes["s-fine"] = scopeRecord{status: "active", activeGenerationID: "g2"}
	db.generations["g2"] = generationRecord{scope: "s-fine", status: "active"}
	db.scopes["s-live-work"] = scopeRecord{status: "active", activeGenerationID: "g3"}
	db.workItems = append(db.workItems, workItemRecord{scopeID: "s-live-work", stage: "reducer", domain: "deployment_mapping", status: "claimed"})
	store := NewRelationshipStore(db)

	ids, err := store.IncompleteActiveScopeRelationshipGenerations(context.Background())
	if err != nil {
		t.Fatalf("IncompleteActiveScopeRelationshipGenerations() error = %v", err)
	}
	want := []string{"s-holding", "s-live-work"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("IncompleteActiveScopeRelationshipGenerations() = %v, want %v", ids, want)
	}
}

// TestIncompleteActiveScopeRelationshipGenerationsEmptyWhenComplete pins the
// agreement direction: a complete fence lists no holders.
func TestIncompleteActiveScopeRelationshipGenerationsEmptyWhenComplete(t *testing.T) {
	t.Parallel()

	db := newRelationshipTestDB()
	db.scopes["s1"] = scopeRecord{status: "active", activeGenerationID: "g1"}
	db.generations["g1"] = generationRecord{scope: "s1", status: "active"}
	store := NewRelationshipStore(db)

	ids, err := store.IncompleteActiveScopeRelationshipGenerations(context.Background())
	if err != nil {
		t.Fatalf("IncompleteActiveScopeRelationshipGenerations() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("IncompleteActiveScopeRelationshipGenerations() = %v, want empty", ids)
	}
}

// ctxCapturingCompletenessChecker records the context it receives so the
// adapter test below can prove caller-context propagation.
type ctxCapturingCompletenessChecker struct {
	got context.Context
}

func (c *ctxCapturingCompletenessChecker) AreActiveScopeRelationshipGenerationsComplete(ctx context.Context) (bool, error) {
	c.got = ctx
	return true, nil
}

// TestNewRelationshipGenerationsCompleteLookupPropagatesContext pins the
// #6730 owner finding: the fence lookup must run under the caller's context,
// not context.Background(), so cancellation reaches the Postgres read.
func TestNewRelationshipGenerationsCompleteLookupPropagatesContext(t *testing.T) {
	t.Parallel()

	checker := &ctxCapturingCompletenessChecker{}
	lookup := NewRelationshipGenerationsCompleteLookup(checker)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "fence")
	if _, err := lookup(ctx); err != nil {
		t.Fatalf("lookup() error = %v", err)
	}
	if checker.got == nil {
		t.Fatal("checker received nil context, want the caller context")
	}
	if v, _ := checker.got.Value(ctxKey{}).(string); v != "fence" {
		t.Fatal("checker did not receive the caller context value, want propagation")
	}
}

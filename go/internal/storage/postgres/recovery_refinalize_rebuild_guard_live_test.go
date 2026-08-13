// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Mutation proofs for the two dedup guards the #4594 rebuild reset deliberately
// leaves alone. The rebuild clears dedup state from the recovery path only; if a
// future change instead weakens ON CONFLICT DO NOTHING or the shared-intent
// COALESCE, every ordinary re-projection would re-drive its whole reducer
// catalog and re-drain every already-projected intent. These two tests are what
// go red when that happens.
// TestOrdinaryReducerEnqueueStillRefusesToResetSucceededWork is the mutation
// proof for the first guard. The rebuild reset must live in the recovery path
// only. If someone "fixes" #4594 by turning the enqueue's
// ON CONFLICT (work_item_id) DO NOTHING into a DO UPDATE, every ordinary
// re-projection would re-drive its whole reducer catalog and this test goes red.
func TestOrdinaryReducerEnqueueStillRefusesToResetSucceededWork(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	workItemID := seedRefinalizeResetReducerWork(t, ctx, db, scopeID, activeGeneration, "guard-entity", "succeeded")

	queue := NewReducerQueue(SQLDB{DB: db}, "refinalize-reset-test", time.Minute)
	result, err := queue.Enqueue(ctx, []projector.ReducerIntent{{
		ScopeID:      scopeID,
		GenerationID: activeGeneration,
		Domain:       reducer.DomainCodeCallMaterialization,
		EntityKey:    "guard-entity",
		Reason:       "refinalize rebuild reset proof",
		FactID:       "fact-guard-entity",
		SourceSystem: "git",
	}})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}

	if got, want := refinalizeResetWorkItemStatus(t, ctx, db, workItemID), "succeeded"; got != want {
		t.Fatalf("ordinary enqueue moved a succeeded work item to %q, want it left at %q: "+
			"re-opening completed work belongs to the recovery path, not to every projection", got, want)
	}
	if got, want := result.Count, 0; got != want {
		t.Fatalf("Enqueue() admitted %d rows, want %d: the duplicate must be dropped", got, want)
	}
}

// TestOrdinarySharedIntentUpsertStillRefusesToClearCompletedAt is the mutation
// proof for the second guard. Clearing completed_at is the recovery path's job.
// If someone drops the COALESCE in shared_intents_upsert.go, every ordinary
// re-emission of an already-projected intent would re-drain it and this test
// goes red.
func TestOrdinarySharedIntentUpsertStillRefusesToClearCompletedAt(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, db, suffix)

	completed := time.Now().UTC()
	intentID := "refinalize-reset-guard-intent-" + suffix
	row := reducer.SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: reducer.DomainCodeCalls,
		PartitionKey:     "caller->callee",
		ScopeID:          scopeID,
		AcceptanceUnitID: "unit-" + suffix,
		RepositoryID:     "repo-" + suffix,
		SourceRunID:      activeGeneration,
		GenerationID:     activeGeneration,
		Payload:          map[string]any{"action": "write"},
		CreatedAt:        completed,
		CompletedAt:      &completed,
	}

	store := NewSharedIntentStore(SQLDB{DB: db})
	if err := store.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{row}); err != nil {
		t.Fatalf("seed shared intent: %v", err)
	}

	// A handler re-emitting the same intent always builds it with CompletedAt nil,
	// which is exactly the shape that would clear the column without the COALESCE.
	row.CompletedAt = nil
	if err := store.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{row}); err != nil {
		t.Fatalf("re-upsert shared intent: %v", err)
	}

	if got := refinalizeResetIntentCompletedAt(t, ctx, db, intentID); got == nil {
		t.Fatal("an ordinary re-upsert cleared completed_at; that would re-drain every already-projected " +
			"intent on any routine handler re-run, not only during a disaster-recovery rebuild")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerQueueReprojectionReEnqueueAdmitsNothingLive pins the queue
// contract #7285's fix relies on (arbiter ruling, RED shape item 4), on real
// Postgres: once a generation's reducer rows succeeded, a projector
// re-projection of the same generation (a retry, or the liveness re-drive)
// re-enqueues the same intents and the queue admits none of them. Enqueue
// reports Count == 0, and every row stays succeeded with its attempt count
// and update time unchanged.
//
// That is why the projector must never destroy reducer-owned graph state on a
// re-projection: nothing re-runs the reducers to rebuild it. If this contract
// changes (for example the enqueue starts reopening succeeded rows), the
// #7285 design needs review, and this test is where that shows up.
//
// It reuses the ACK-reclaim proof's disposable schema, so it runs under
// ESHU_REDUCER_ACK_RECLAIM_PROOF_DSN and skips without it.
func TestReducerQueueReprojectionReEnqueueAdmitsNothingLive(t *testing.T) {
	db := openReducerAckReclaimProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "reducer-7285-live",
		LeaseDuration: time.Minute,
	}
	const scopeID = "repository:7285-reenqueue"
	const generationID = "generation:7285-reenqueue"
	seedReducerAckReclaimScope(t, ctx, db, scopeID)
	seedReducerAckReclaimGeneration(t, ctx, db, scopeID, generationID)

	intents := make([]runtime.ReducerIntent, 0, 2)
	for _, domain := range []reducer.Domain{reducer.DomainWorkloadMaterialization, reducer.DomainDeployableUnitCorrelation} {
		intents = append(intents, runtime.ReducerIntent{
			ScopeID:      scopeID,
			GenerationID: generationID,
			Domain:       domain,
			EntityKey:    "repository:r_7285",
			Reason:       "facts projected",
			FactID:       "fact-7285-repository",
			SourceSystem: "git",
		})
	}

	first, err := queue.Enqueue(ctx, intents)
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	if first.Count != len(intents) {
		t.Fatalf("first Enqueue().Count = %d, want %d", first.Count, len(intents))
	}

	claimed, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(claimed) != len(intents) {
		t.Fatalf("claimed %d reducer rows, want %d", len(claimed), len(intents))
	}
	results := make([]reducer.Result, len(claimed))
	for i, intent := range claimed {
		results[i] = reducer.Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: reducer.ResultStatusSucceeded}
	}
	if err := queue.AckBatch(ctx, claimed, results); err != nil {
		t.Fatalf("AckBatch() error = %v", err)
	}
	before := readReEnqueueRows(ctx, t, queue, intents)

	// The re-projection: the same generation's projector runs again and
	// re-enqueues the same intents.
	second, err := queue.Enqueue(ctx, intents)
	if err != nil {
		t.Fatalf("re-projection Enqueue() error = %v", err)
	}
	if second.Count != 0 {
		t.Fatalf("re-projection Enqueue().Count = %d, want 0: succeeded reducer rows must not be re-admitted", second.Count)
	}
	after := readReEnqueueRows(ctx, t, queue, intents)
	for id, row := range after {
		if row != before[id] || row.status != "succeeded" || row.attempts != 1 {
			t.Fatalf("row %s after re-enqueue = %+v, want unchanged succeeded row %+v", id, row, before[id])
		}
	}
}

// reEnqueueRow is the part of a fact_work_items row a re-enqueue must not touch.
type reEnqueueRow struct {
	status    string
	attempts  int
	updatedAt time.Time
}

func readReEnqueueRows(
	ctx context.Context,
	t *testing.T,
	queue ReducerQueue,
	intents []runtime.ReducerIntent,
) map[string]reEnqueueRow {
	t.Helper()
	rows := make(map[string]reEnqueueRow, len(intents))
	for _, intent := range intents {
		id := reducerWorkItemID(intent)
		var row reEnqueueRow
		if err := queue.database.(SQLDB).DB.QueryRowContext(ctx, `
SELECT status, attempt_count, updated_at FROM fact_work_items WHERE work_item_id = $1
`, id).Scan(&row.status, &row.attempts, &row.updatedAt); err != nil {
			t.Fatalf("read reducer row %s: %v", id, err)
		}
		rows[id] = row
	}
	return rows
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Shard-drain barrier RE-ENTRY coverage, split out of
// deferred_maintenance_barrier_test.go to keep that file under the 500-line
// cap. It covers the second pass over an already-completed epoch: the leader
// opens a fresh epoch and runs maintenance again rather than short-circuiting
// on the previous completion.

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestIngestionStoreShardDrainBarrierLeaderReentryRerunsMaintenance proves
// leader liveness after the split barrier-arrival and completion transactions.
// It simulates a re-run where the epoch is already at a full arrival count but
// not yet completed (the previous leader crashed before marking completion). A
// re-arriving shard still observes a full count, re-runs the idempotent
// maintenance, and marks completion in its own transaction, so waiting shards
// cannot block forever.
func TestIngestionStoreShardDrainBarrierLeaderReentryRerunsMaintenance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	// Re-run arrival: existing open epoch (not completed), arrival upsert keeps a
	// full count of 2 so this shard re-enters the leader path.
	barrierTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(9), 2, sql.NullTime{}}}},
			{rows: [][]any{{2}}},
		},
	}
	batchTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	reopenTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}
	fanInTx := deferredFanInFakeTx("gen-infra")
	completionTx := &fakeTx{}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{barrierTx, batchTx, fanInTx, reopenTx, completionTx},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`), catalogFakeObservedAt}}},
			{rows: [][]any{}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0, HasCommitted: true},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil", err)
	}
	if !barrierTx.committed || !batchTx.committed || !fanInTx.committed || !completionTx.committed {
		t.Fatalf("re-entry transactions not all committed: barrier=%v batch=%v fan-in=%v completion=%v",
			barrierTx.committed, batchTx.committed, fanInTx.committed, completionTx.committed)
	}
	// Re-run still performs maintenance writes and marks completion. Readiness
	// is published by the fan-in transaction, not the evidence batch.
	assertExecContains(t, fanInTx.execs, "INSERT INTO graph_projection_phase_state")
	assertExecContains(t, completionTx.execs, "completed_at = $4")
}

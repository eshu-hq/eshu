// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestIngestionStoreCommitScopeGenerationTakesSharedMaintenanceBarrier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if len(db.tx.execs) == 0 {
		t.Fatal("transaction execs = 0, want shared maintenance barrier lock before writes")
	}
	first := db.tx.execs[0]
	if !strings.Contains(first.query, "pg_advisory_xact_lock_shared") {
		t.Fatalf("first transaction exec = %q, want shared advisory maintenance barrier", first.query)
	}
	if !strings.Contains(first.query, "hashtext") {
		t.Fatalf("first transaction exec = %q, want repo-partitioned shared barrier", first.query)
	}
	if got, want := first.args[0], deferredMaintenanceLockNamespace; got != want {
		t.Fatalf("shared barrier namespace = %v, want %v", got, want)
	}
	if got, want := first.args[1], deferredMaintenanceRepoLockKey(scopeValue); got != want {
		t.Fatalf("shared barrier repo key = %v, want %v", got, want)
	}
}

func TestIngestionStoreRunDeferredRelationshipMaintenanceTakesPerRepoExclusiveBarrier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	batchTx := &fakeTx{
		queryResponses: []queueFakeRows{
			// Batch transaction re-loads active generations under the batch lock.
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	reopenTx := &fakeTx{
		queryResponses: []queueFakeRows{
			// ReopenDeploymentMappingWorkItems: one succeeded work item.
			{rows: [][]any{{"work-item-1"}}},
		},
	}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{batchTx, reopenTx},
		queryResponses: []queueFakeRows{
			// Snapshot reads: catalog, latest facts, active generations.
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)}}},
			{rows: [][]any{}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.RunDeferredRelationshipMaintenance(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v, want nil", err)
	}
	if got, want := db.beginCalls, 2; got != want {
		t.Fatalf("begin call count = %d, want %d (one batch + one reopen transaction)", got, want)
	}
	tx := batchTx
	if len(tx.execs) == 0 {
		t.Fatal("transaction execs = 0, want per-repo exclusive maintenance barrier lock")
	}
	first := tx.execs[0]
	if !strings.Contains(first.query, "pg_advisory_xact_lock(") || strings.Contains(first.query, "shared") {
		t.Fatalf("first transaction exec = %q, want exclusive advisory maintenance barrier", first.query)
	}
	if !strings.Contains(first.query, "hashtext") {
		t.Fatalf("first transaction exec = %q, want repo-partitioned exclusive barrier", first.query)
	}
	if got, want := first.args[0], deferredMaintenanceLockNamespace; got != want {
		t.Fatalf("exclusive barrier namespace = %v, want %v", got, want)
	}
	if got, want := first.args[1], deferredMaintenanceRepoLockKeyFromID("repo-infra"); got != want {
		t.Fatalf("exclusive barrier repo key = %v, want %v", got, want)
	}
	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
}

func TestIngestionStoreShardDrainBarrierNonLeaderWaitsForCompletion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{},
			{rows: [][]any{{1}}},
		},
	}
	db := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{
			{rows: [][]any{{sql.NullTime{Time: now.Add(time.Second), Valid: true}}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil", err)
	}
	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	for _, exec := range tx.execs {
		if strings.Contains(exec.query, "INSERT INTO graph_projection_phase_state") ||
			strings.Contains(exec.query, "UPDATE fact_work_items") ||
			strings.Contains(exec.query, "completed_at = $4") {
			t.Fatalf("arrival before full shard drain ran maintenance query:\n%s", exec.query)
		}
	}
	if got, want := len(tx.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got := tx.execs[0].query; !strings.Contains(got, "pg_advisory_xact_lock") {
		t.Fatalf("first exec = %q, want barrier state advisory lock", got)
	}
	if got := tx.execs[1].query; !strings.Contains(got, "INSERT INTO deferred_maintenance_barriers") {
		t.Fatalf("second exec = %q, want barrier epoch insert", got)
	}
	if got := tx.execs[2].query; !strings.Contains(got, "INSERT INTO deferred_maintenance_barrier_arrivals") {
		t.Fatalf("third exec = %q, want shard arrival insert", got)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("completion wait queries = %d, want 1", got)
	}
	if got := db.queries[0].query; !strings.Contains(got, "SELECT completed_at") {
		t.Fatalf("completion wait query = %q, want completed_at lookup", got)
	}
}

func TestIngestionStoreShardDrainBarrierLeaderRunsMaintenanceAfterAllShardsArrive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	// Barrier arrival transaction: epoch lookup then arrival count.
	barrierTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(7), 2, sql.NullTime{}}}},
			{rows: [][]any{{2}}},
		},
	}
	// Backfill batch transaction: re-load active generations under the lock.
	batchTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	// Reopen transaction: the reopen partition-memo fingerprint gate re-reads the
	// repository catalog first (computeCurrentReopenCatalogFingerprint, issue
	// #4770), then lists succeeded deployment_mapping work items (one, with its
	// scope/generation partition columns), then looks up its partition in the
	// reopen memo gate (no memo row staged, so it is a memo miss and reopens),
	// then the code_import_repo_edge listing (none, via fakeTx's default
	// fallback) and its own memo lookup default.
	reopenTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)}}},
			{rows: [][]any{{"work-item-1", "scope-infra", "gen-infra"}}},
		},
	}
	// Completion transaction marks the barrier complete after maintenance.
	completionTx := &fakeTx{}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{barrierTx, batchTx, reopenTx, completionTx},
		queryResponses: []queueFakeRows{
			// Backfill snapshot reads on the store db: catalog, facts, active gens.
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)}}},
			{rows: [][]any{}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 1},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil", err)
	}
	if !barrierTx.committed || !batchTx.committed || !reopenTx.committed || !completionTx.committed {
		t.Fatalf("not all transactions committed: barrier=%v batch=%v reopen=%v completion=%v",
			barrierTx.committed, batchTx.committed, reopenTx.committed, completionTx.committed)
	}
	if got, want := db.beginCalls, 4; got != want {
		t.Fatalf("begin call count = %d, want %d (arrival + batch + reopen + completion)", got, want)
	}
	// Barrier state lock (global, brief, released on arrival commit) is taken in
	// the arrival transaction, not held across maintenance.
	assertExecContains(t, barrierTx.execs, "pg_advisory_xact_lock($1)")
	assertExecContains(t, barrierTx.execs, "INSERT INTO deferred_maintenance_barrier_arrivals")
	// The arrival transaction must not run a fleet-wide exclusive maintenance lock
	// nor any maintenance writes; those happen in independent batch transactions.
	for _, exec := range barrierTx.execs {
		if strings.Contains(exec.query, "hashtext") {
			t.Fatalf("arrival transaction took a maintenance repo lock: %q", exec.query)
		}
		if strings.Contains(exec.query, "INSERT INTO graph_projection_phase_state") {
			t.Fatalf("arrival transaction ran maintenance writes: %q", exec.query)
		}
	}
	// Per-repo maintenance lock and readiness write live in the batch transaction.
	assertExecContains(t, batchTx.execs, "pg_advisory_xact_lock(hashtext($1), hashtext($2))")
	assertExecContains(t, batchTx.execs, "INSERT INTO graph_projection_phase_state")
	// Reopen runs in its own transaction.
	assertExecContains(t, reopenTx.execs, "UPDATE fact_work_items")
	// Completion is marked in its own transaction after maintenance.
	assertExecContains(t, completionTx.execs, "completed_at = $4")
}

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
	completionTx := &fakeTx{}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{barrierTx, batchTx, reopenTx, completionTx},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)}}},
			{rows: [][]any{}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 0},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil", err)
	}
	if !barrierTx.committed || !batchTx.committed || !completionTx.committed {
		t.Fatalf("re-entry transactions not all committed: barrier=%v batch=%v completion=%v",
			barrierTx.committed, batchTx.committed, completionTx.committed)
	}
	// Re-run still performs maintenance writes and marks completion.
	assertExecContains(t, batchTx.execs, "INSERT INTO graph_projection_phase_state")
	assertExecContains(t, completionTx.execs, "completed_at = $4")
}

func TestEnsureDeferredMaintenanceBarrierEpochClosesLatestRowsBeforeInsert(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	rows := &closeTrackingRows{}
	tx := &openRowsRejectingTx{latestRows: rows}

	epoch, err := ensureDeferredMaintenanceBarrierEpoch(context.Background(), tx, 2, now)
	if err != nil {
		t.Fatalf("ensureDeferredMaintenanceBarrierEpoch() error = %v, want nil", err)
	}
	if epoch != 1 {
		t.Fatalf("epoch = %d, want 1", epoch)
	}
	if !rows.closed {
		t.Fatal("latest barrier rows were not closed")
	}
	if got, want := tx.execCount, 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
}

func TestEnsureDeferredMaintenanceBarrierEpochClosesLatestRowsOnScanError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	rows := &closeTrackingRows{
		next:    true,
		scanErr: errors.New("decode latest epoch"),
	}
	tx := &openRowsRejectingTx{latestRows: rows}

	_, err := ensureDeferredMaintenanceBarrierEpoch(context.Background(), tx, 2, now)
	if err == nil {
		t.Fatal("ensureDeferredMaintenanceBarrierEpoch() error = nil, want scan error")
	}
	if !strings.Contains(err.Error(), "scan deferred maintenance barrier") {
		t.Fatalf("ensureDeferredMaintenanceBarrierEpoch() error = %v, want scan context", err)
	}
	if !rows.closed {
		t.Fatal("latest barrier rows were not closed after scan error")
	}
	if got, want := tx.execCount, 0; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
}

func TestIngestionStoreShardDrainBarrierRejectsShardCountChangeDuringOpenEpoch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(7), 3, sql.NullTime{}}}},
		},
	}
	db := &fakeTransactionalDB{tx: tx}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	err := store.RunDeferredRelationshipMaintenanceAfterShardDrain(
		context.Background(),
		DeferredMaintenanceBarrierConfig{ShardCount: 2, ShardIndex: 1},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("RunDeferredRelationshipMaintenanceAfterShardDrain() error = nil, want shard count mismatch")
	}
	if !strings.Contains(err.Error(), "open with shard count 3") {
		t.Fatalf("error = %q, want open shard count context", err)
	}
	if !tx.rolledBack {
		t.Fatal("transaction rolledBack = false, want true")
	}
}

func TestBootstrapDefinitionsIncludeDeferredMaintenanceBarrier(t *testing.T) {
	t.Parallel()

	for _, def := range BootstrapDefinitions() {
		if def.Name != "deferred_maintenance_barriers" {
			continue
		}
		if !strings.Contains(def.SQL, "CREATE TABLE IF NOT EXISTS deferred_maintenance_barriers") {
			t.Fatal("deferred maintenance barrier definition missing barrier table")
		}
		if !strings.Contains(def.SQL, "CREATE TABLE IF NOT EXISTS deferred_maintenance_barrier_arrivals") {
			t.Fatal("deferred maintenance barrier definition missing arrival table")
		}
		return
	}
	t.Fatal("BootstrapDefinitions() missing deferred_maintenance_barriers")
}

func assertExecContains(t *testing.T, execs []fakeExecCall, substring string) {
	t.Helper()
	for _, exec := range execs {
		if strings.Contains(exec.query, substring) {
			return
		}
	}
	t.Fatalf("execs missing query containing %q", substring)
}

type closeTrackingRows struct {
	next    bool
	closed  bool
	scanErr error
}

func (r *closeTrackingRows) Next() bool {
	if !r.next {
		return false
	}
	r.next = false
	return true
}

func (r *closeTrackingRows) Scan(...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return errors.New("scan called unexpectedly")
}

func (r *closeTrackingRows) Err() error { return nil }

func (r *closeTrackingRows) Close() error {
	r.closed = true
	return nil
}

type openRowsRejectingTx struct {
	latestRows *closeTrackingRows
	execCount  int
}

func (tx *openRowsRejectingTx) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM deferred_maintenance_barriers") {
		return nil, errors.New("unexpected query")
	}
	return tx.latestRows, nil
}

func (tx *openRowsRejectingTx) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	if !tx.latestRows.closed {
		return nil, errors.New("exec called before latest barrier rows were closed")
	}
	tx.execCount++
	return fakeResult{}, nil
}

func (*openRowsRejectingTx) Commit() error { return nil }

func (*openRowsRejectingTx) Rollback() error { return nil }

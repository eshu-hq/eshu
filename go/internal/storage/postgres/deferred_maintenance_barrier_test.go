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
	store.SkipRelationshipBackfill = true

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
	if got, want := first.args[0], deferredMaintenanceBarrierLockKey; got != want {
		t.Fatalf("shared barrier lock key = %v, want %v", got, want)
	}
}

func TestIngestionStoreRunDeferredRelationshipMaintenanceTakesExclusiveBarrier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)}}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
			{rows: [][]any{}},
			{rows: [][]any{{"work-item-1"}}},
		},
	}
	db := &fakeTransactionalDB{tx: tx}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.RunDeferredRelationshipMaintenance(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v, want nil", err)
	}
	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if len(tx.execs) == 0 {
		t.Fatal("transaction execs = 0, want exclusive maintenance barrier lock")
	}
	first := tx.execs[0]
	if !strings.Contains(first.query, "pg_advisory_xact_lock") || strings.Contains(first.query, "shared") {
		t.Fatalf("first transaction exec = %q, want exclusive advisory maintenance barrier", first.query)
	}
	if got, want := first.args[0], deferredMaintenanceBarrierLockKey; got != want {
		t.Fatalf("exclusive barrier lock key = %v, want %v", got, want)
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
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(7), 2, sql.NullTime{}}}},
			{rows: [][]any{{2}}},
			{rows: [][]any{{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)}}},
			{rows: [][]any{{"repo-infra", "scope-infra", "gen-infra"}}},
			{rows: [][]any{}},
			{rows: [][]any{{"work-item-1"}}},
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
	if err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenanceAfterShardDrain() error = %v, want nil", err)
	}
	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	assertExecContains(t, tx.execs, "pg_advisory_xact_lock($1)")
	assertExecContains(t, tx.execs, "INSERT INTO deferred_maintenance_barrier_arrivals")
	assertExecContains(t, tx.execs, "INSERT INTO graph_projection_phase_state")
	assertExecContains(t, tx.execs, "UPDATE fact_work_items")
	assertExecContains(t, tx.execs, "completed_at = $4")
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

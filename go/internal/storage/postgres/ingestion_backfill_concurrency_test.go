// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// concurrencyProbeDB is a Beginner whose Begin hands every batch transaction its
// OWN independent fakeExecQueryer (so concurrent batches do not serialize on one
// shared mutex) and records the peak number of simultaneously-open transactions.
// It lets the deferred-backfill concurrency test assert both that every batch
// committed and that fan-out stayed within the bounded worker pool.
type concurrencyProbeDB struct {
	activeGenRows [][]any

	mu          sync.Mutex
	open        int
	peakOpen    int
	beginCount  int
	committed   int
	allEvidence []fakeExecCall
}

func (db *concurrencyProbeDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	// The pre-batch active-generation load runs on the base handle.
	return &queueFakeRows{rows: db.activeGenRows}, nil
}

func (db *concurrencyProbeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *concurrencyProbeDB) Begin(context.Context) (Transaction, error) {
	db.mu.Lock()
	db.open++
	db.beginCount++
	if db.open > db.peakOpen {
		db.peakOpen = db.open
	}
	db.mu.Unlock()
	return &concurrencyProbeTx{db: db, gens: db.activeGenRows}, nil
}

type concurrencyProbeTx struct {
	db   *concurrencyProbeDB
	gens [][]any
}

func (tx *concurrencyProbeTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	if strings.HasPrefix(query, listArgoCDBearingPartitionsQuery) {
		// The memo-write path's ArgoCD-bearing-partition probe (only reached
		// when a caller supplies a non-blank catalogFingerprint) expects a
		// 2-column (scope_id, generation_id) row shape, not the 3-column
		// active-generation shape below. No caller of this fake seeds an
		// ArgoCD-bearing fixture, so every candidate partition is treated as
		// non-bearing (empty result set) here.
		return &queueFakeRows{}, nil
	}
	if strings.HasPrefix(query, activeScopeGenerationQuery) {
		// The fan-in's under-lock generation re-read
		// (loadActiveGenerationForScope) is a single-column lookup for ONE
		// scope. Answer it from the same seeded rows the batch re-read uses so
		// the fake never reports a generation the batch did not observe, which
		// would make every partition look advanced and silently publish
		// nothing.
		return &queueFakeRows{rows: tx.activeGenerationRowsForScope(args)}, nil
	}
	// Each batch re-reads active generations under its lock.
	return &queueFakeRows{rows: tx.gens}, nil
}

// activeGenerationRowsForScope projects the seeded (repo, scope, generation)
// rows down to the single generation column loadActiveGenerationForScope scans,
// filtered to the scope bound as $1. An unknown scope yields no rows, matching
// Postgres for a scope with no generation.
func (tx *concurrencyProbeTx) activeGenerationRowsForScope(args []any) [][]any {
	if len(args) == 0 {
		return nil
	}
	scopeID, ok := args[0].(string)
	if !ok {
		return nil
	}
	for _, row := range tx.gens {
		if len(row) < 3 {
			continue
		}
		if rowScope, ok := row[1].(string); ok && rowScope == scopeID {
			return [][]any{{row[2]}}
		}
	}
	return nil
}

func (tx *concurrencyProbeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	// Simulate per-statement work so overlapping batches actually overlap in time.
	time.Sleep(time.Millisecond)
	tx.db.mu.Lock()
	tx.db.allEvidence = append(tx.db.allEvidence, fakeExecCall{query: query, args: args})
	tx.db.mu.Unlock()
	return fakeResult{}, nil
}

func (tx *concurrencyProbeTx) Commit() error {
	tx.db.mu.Lock()
	tx.db.open--
	tx.db.committed++
	tx.db.mu.Unlock()
	return nil
}

func (tx *concurrencyProbeTx) Rollback() error {
	tx.db.mu.Lock()
	tx.db.open--
	tx.db.mu.Unlock()
	return nil
}

// TestWriteDeferredBackfillInBatchesRunsConcurrently is the #3704 concurrency
// gate. With several repository batches and a worker count above one, the
// per-repository batch transactions must run concurrently (peak open
// transactions > 1) yet stay bounded by the worker count, and every batch must
// still commit so no readiness row is lost.
func TestWriteDeferredBackfillInBatchesRunsConcurrently(t *testing.T) {
	t.Parallel()

	const repoCount = 64
	activeGen := make([][]any, 0, repoCount)
	for i := 0; i < repoCount; i++ {
		id := "repo-" + itoa(i)
		activeGen = append(activeGen, []any{id, "scope-" + itoa(i), "gen-" + itoa(i)})
	}
	probe := &concurrencyProbeDB{activeGenRows: activeGen}

	store := NewIngestionStore(probe)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.maintenanceBatchSize = 4 // 64 repos / 4 = 16 batches
	store.maintenanceWorkers = 6

	readiness, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if readiness != repoCount {
		t.Fatalf("published %d readiness rows, want %d", readiness, repoCount)
	}

	// Every repository owns its own scope, so the pass opens one transaction per
	// evidence batch plus one fan-in publication transaction per partition.
	wantBatches := repoCount / 4
	wantTransactions := wantBatches + repoCount
	if probe.beginCount != wantTransactions {
		t.Fatalf("opened %d transactions, want %d (%d evidence batches + %d fan-in publications)",
			probe.beginCount, wantTransactions, wantBatches, repoCount)
	}
	if probe.committed != wantTransactions {
		t.Fatalf("committed %d transactions, want %d", probe.committed, wantTransactions)
	}
	if probe.peakOpen <= 1 {
		t.Fatalf("peak concurrent batch transactions = %d, want > 1 (batches must run concurrently)", probe.peakOpen)
	}
	if probe.peakOpen > store.maintenanceWorkers {
		t.Fatalf("peak concurrent batch transactions = %d, exceeds worker bound %d", probe.peakOpen, store.maintenanceWorkers)
	}
}

// TestWriteDeferredBackfillInBatchesSerialWhenWorkerCountOne pins that a worker
// count of one keeps the pass single-flight (peak open == 1). This is the path a
// deployment pinned to ESHU_POSTGRES_MAX_OPEN_CONNS=1 takes (the operator sets
// ESHU_DEFERRED_BACKFILL_CONCURRENCY=1), and it must never run two batch
// transactions at once.
func TestWriteDeferredBackfillInBatchesSerialWhenWorkerCountOne(t *testing.T) {
	t.Parallel()

	const repoCount = 16
	activeGen := make([][]any, 0, repoCount)
	for i := 0; i < repoCount; i++ {
		activeGen = append(activeGen, []any{"repo-" + itoa(i), "scope-" + itoa(i), "gen-" + itoa(i)})
	}
	probe := &concurrencyProbeDB{activeGenRows: activeGen}

	store := NewIngestionStore(probe)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.maintenanceBatchSize = 4
	store.maintenanceWorkers = 1

	if _, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"",
		nil,
	); err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if probe.peakOpen != 1 {
		t.Fatalf("peak concurrent batch transactions = %d, want 1 for worker count 1", probe.peakOpen)
	}
}

// TestDeferredBackfillWorkerCountDefaultIsPoolSafe pins that the constructor's
// default worker count is a sane bounded value: at least one and never above the
// hard cap, so an unset ESHU_DEFERRED_BACKFILL_CONCURRENCY stays within the
// documented connection-pool budget.
func TestDeferredBackfillWorkerCountDefaultIsPoolSafe(t *testing.T) {
	t.Setenv("ESHU_DEFERRED_BACKFILL_CONCURRENCY", "")
	got := deferredBackfillWorkerCount()
	if got < 1 {
		t.Fatalf("default worker count = %d, want >= 1", got)
	}
	if got > deferredBackfillMaxWorkers {
		t.Fatalf("default worker count = %d, want <= hard cap %d", got, deferredBackfillMaxWorkers)
	}
}

func TestDeferredBackfillDefaultWorkerCountUsesHardCap(t *testing.T) {
	tests := []struct {
		name string
		cpus int
		want int
	}{
		{name: "defensive zero", cpus: 0, want: 1},
		{name: "single cpu", cpus: 1, want: 1},
		{name: "modest cpu count", cpus: 4, want: 4},
		{name: "roomy cpu count reaches hard cap", cpus: 16, want: deferredBackfillMaxWorkers},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deferredBackfillDefaultWorkerCount(tt.cpus); got != tt.want {
				t.Fatalf("default worker count for %d CPUs = %d, want %d", tt.cpus, got, tt.want)
			}
		})
	}
}

// TestDeferredBackfillWorkerCountEnvOverrideClampsToMax pins the env override and
// its hard ceiling.
func TestDeferredBackfillWorkerCountEnvOverrideClampsToMax(t *testing.T) {
	t.Setenv("ESHU_DEFERRED_BACKFILL_CONCURRENCY", "100")
	if got := deferredBackfillWorkerCount(); got != deferredBackfillMaxWorkers {
		t.Fatalf("worker count with override 100 = %d, want hard cap %d", got, deferredBackfillMaxWorkers)
	}
	t.Setenv("ESHU_DEFERRED_BACKFILL_CONCURRENCY", "2")
	if got := deferredBackfillWorkerCount(); got != 2 {
		t.Fatalf("worker count with override 2 = %d, want 2", got)
	}
}

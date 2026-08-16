// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"
)

// advisoryLockManager simulates Postgres transaction-level advisory lock
// semantics for the deferred-maintenance partition keys: many holders may share
// one key, an exclusive request blocks until no shared or exclusive holder
// remains on that key, and disjoint keys never contend. It lets the concurrency
// proofs run deterministically without a live database.
type advisoryLockManager struct {
	mu        sync.Mutex
	cond      *sync.Cond
	exclusive map[string]bool
	shared    map[string]int
}

func newAdvisoryLockManager() *advisoryLockManager {
	m := &advisoryLockManager{
		exclusive: make(map[string]bool),
		shared:    make(map[string]int),
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *advisoryLockManager) acquireExclusive(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.exclusive[key] || m.shared[key] > 0 {
		m.cond.Wait()
	}
	m.exclusive[key] = true
}

func (m *advisoryLockManager) acquireShared(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.exclusive[key] {
		m.cond.Wait()
	}
	m.shared[key]++
}

func (m *advisoryLockManager) release(exclusiveKeys, sharedKeys []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range exclusiveKeys {
		delete(m.exclusive, key)
	}
	for _, key := range sharedKeys {
		if m.shared[key] > 0 {
			m.shared[key]--
		}
	}
	m.cond.Broadcast()
}

// advisoryLockTx is a fake transaction that routes the partitioned advisory lock
// SQL into the simulated lock manager and records the keys it holds so they can
// be released on commit/rollback.
type advisoryLockTx struct {
	mgr           *advisoryLockManager
	exclusiveHeld []string
	sharedHeld    []string
}

func (tx *advisoryLockTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch query {
	case deferredMaintenancePartitionedExclusiveLockSQL:
		key := args[1].(string)
		tx.mgr.acquireExclusive(key)
		tx.exclusiveHeld = append(tx.exclusiveHeld, key)
	case deferredMaintenancePartitionedSharedLockSQL:
		key := args[1].(string)
		tx.mgr.acquireShared(key)
		tx.sharedHeld = append(tx.sharedHeld, key)
	}
	return fakeResult{}, nil
}

func (tx *advisoryLockTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return &queueFakeRows{}, nil
}

func (tx *advisoryLockTx) Commit() error {
	tx.mgr.release(tx.exclusiveHeld, tx.sharedHeld)
	return nil
}

func (tx *advisoryLockTx) Rollback() error {
	tx.mgr.release(tx.exclusiveHeld, tx.sharedHeld)
	return nil
}

// lockAwareMaintenanceDB drives the whole-corpus maintenance entrypoint with
// realistic advisory-lock semantics. Snapshot reads (catalog, latest facts,
// active generations) are answered from canned responses; every Begin returns a
// transaction wired to a shared advisoryLockManager so exclusive lock
// acquisition and release are observable. It tracks the peak number of repo
// exclusive locks held simultaneously so a test can prove the whole-corpus pass
// never holds a fleet-wide lock set.
//
// Note for anyone adding or reordering a query on this path: QueryContext here
// is FIFO over snapshotRows and ignores the query string, so its correctness
// depends on the ORDER of reads on the outer handle, not on their shape -- and
// once the staged list is exhausted it returns an empty result set rather than
// an error. A change that inserts a snapshot read, or reorders two, makes this
// fake mis-answer silently instead of failing. That is a different failure mode
// from the column-arity mismatch a shape change causes in the per-query stubs
// (see latencyBackfillDB in ingestion_backfill_bench_test.go), and it is not
// caught by running the benchmarks. Stage a matching row set and assert the
// call order when you touch the sequence.
type lockAwareMaintenanceDB struct {
	mgr              *advisoryLockManager
	snapshotRows     []*queueFakeRows
	snapshotIdx      int
	batchActiveGens  [][]any
	succeededWorkIDs [][]any
	mu               sync.Mutex
	beginCount       int
	heldExclusive    int
	peakHeld         int
	// concurrencyBarrier, when non-nil, makes the peak-held-locks assertion
	// deterministic across schedulers: every batch transaction calls it AFTER it
	// has acquired its locks (so the locks are counted into peakHeld) and blocks
	// until the expected concurrent cohort has all arrived, then proceeds to
	// commit/release. Without it, a fast scheduler (e.g. the macOS CI runner)
	// could run one batch to completion before the next acquires, so the observed
	// peak would be batchSize rather than the real workers*batchSize bound. nil
	// disables the barrier for tests that do not measure concurrent peak.
	concurrencyBarrier func()
}

func (db *lockAwareMaintenanceDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, stubErr("unexpected exec on outer db")
}

func (db *lockAwareMaintenanceDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.snapshotIdx >= len(db.snapshotRows) {
		return &queueFakeRows{}, nil
	}
	rows := db.snapshotRows[db.snapshotIdx]
	db.snapshotIdx++
	return rows, nil
}

func (db *lockAwareMaintenanceDB) Begin(context.Context) (Transaction, error) {
	db.mu.Lock()
	db.beginCount++
	db.mu.Unlock()
	return &lockAwareMaintenanceTx{db: db}, nil
}

func (db *lockAwareMaintenanceDB) acquired(n int) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.heldExclusive += n
	if db.heldExclusive > db.peakHeld {
		db.peakHeld = db.heldExclusive
	}
}

func (db *lockAwareMaintenanceDB) released(n int) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.heldExclusive -= n
}

type stubErr string

func (e stubErr) Error() string { return string(e) }

// lockAwareMaintenanceTx simulates one batch (or reopen) transaction. It routes
// advisory-lock SQL through the shared manager, answers the per-batch active
// generations reload and the succeeded-work-item list, and releases its locks on
// commit/rollback while updating the peak-held counter.
type lockAwareMaintenanceTx struct {
	db            *lockAwareMaintenanceDB
	exclusiveHeld []string
	gensServed    bool
	workServed    bool
}

func (tx *lockAwareMaintenanceTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if query == deferredMaintenancePartitionedExclusiveLockSQL {
		key := args[1].(string)
		tx.db.mgr.acquireExclusive(key)
		tx.exclusiveHeld = append(tx.exclusiveHeld, key)
		tx.db.acquired(1)
	}
	return fakeResult{}, nil
}

func (tx *lockAwareMaintenanceTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.HasPrefix(query, activeScopeGenerationQuery):
		// The fan-in publication transaction's under-lock re-read of ONE scope's
		// active generation. Answering it from the same batchActiveGens the batch
		// reload uses keeps the partition live, so the fan-in publishes and its
		// advisory locks are exercised on the publish path rather than on an
		// early skip. It deliberately does NOT trip the cohort barrier: the
		// barrier exists to force the concurrent BATCH peak, and the fan-in runs
		// after every batch has committed.
		return &queueFakeRows{rows: tx.db.activeGenerationRowFor(args)}, nil
	case strings.Contains(query, "fact_kind = 'repository'") && !tx.gensServed:
		tx.gensServed = true
		// This per-batch active-generations reload runs AFTER the batch has
		// acquired all its exclusive locks (acquireDeferredMaintenanceRepoExclusiveLocks
		// precedes it in writeDeferredBackfillBatch), so every concurrent batch's
		// locks are counted into peakHeld before any batch is allowed past the
		// barrier to commit and release. This forces the deterministic concurrent
		// peak regardless of the OS scheduler.
		if tx.db.concurrencyBarrier != nil {
			tx.db.concurrencyBarrier()
		}
		return &queueFakeRows{rows: tx.db.batchActiveGens}, nil
	case strings.Contains(query, "deployment_mapping") && !tx.workServed:
		tx.workServed = true
		return &queueFakeRows{rows: tx.db.succeededWorkIDs}, nil
	}
	return &queueFakeRows{}, nil
}

// activeGenerationRowFor projects batchActiveGens down to the single generation
// column loadActiveGenerationForScope scans, for the scope bound as $1. An
// unknown scope yields no rows, matching Postgres for a scope with no
// generation.
func (db *lockAwareMaintenanceDB) activeGenerationRowFor(args []any) [][]any {
	if len(args) == 0 {
		return nil
	}
	scopeID, ok := args[0].(string)
	if !ok {
		return nil
	}
	for _, row := range db.batchActiveGens {
		if len(row) < 3 {
			continue
		}
		if rowScope, ok := row[1].(string); ok && rowScope == scopeID {
			return [][]any{{row[2]}}
		}
	}
	return nil
}

func (tx *lockAwareMaintenanceTx) Commit() error {
	tx.db.released(len(tx.exclusiveHeld))
	tx.db.mgr.release(tx.exclusiveHeld, nil)
	return nil
}

func (tx *lockAwareMaintenanceTx) Rollback() error {
	tx.db.released(len(tx.exclusiveHeld))
	tx.db.mgr.release(tx.exclusiveHeld, nil)
	return nil
}

// newCohortBarrier returns a function that blocks each caller until `size`
// callers have arrived, then releases all of them, for the next cohort it resets
// (cyclic). A per-call deadline bounds the wait so a scheduling pathology fails
// the test loudly instead of hanging CI. It is used to force a deterministic
// observed peak in the concurrency proofs.
func newCohortBarrier(size int, deadline time.Duration) func() {
	if size < 1 {
		size = 1
	}
	var mu sync.Mutex
	count := 0
	gate := make(chan struct{})
	return func() {
		mu.Lock()
		count++
		if count == size {
			count = 0
			close(gate)
			gate = make(chan struct{})
			mu.Unlock()
			return
		}
		waitOn := gate
		mu.Unlock()
		select {
		case <-waitOn:
		case <-time.After(deadline):
		}
	}
}

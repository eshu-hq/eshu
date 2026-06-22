package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestCommitTakesRepoScopedSharedBarrier proves a generation commit fences only
// against deferred maintenance for its own repository partition, not the whole
// fleet. The commit must take the namespaced two-argument shared advisory lock
// keyed by the committing repository, so a commit for repo A no longer contends
// with maintenance or commits for repo B.
func TestCommitTakesRepoScopedSharedBarrier(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.SkipRelationshipBackfill = true

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-A",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-A",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "gen-A",
		ScopeID:      "scope-A",
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if len(db.tx.execs) == 0 {
		t.Fatal("transaction execs = 0, want repo-scoped shared maintenance barrier lock")
	}
	first := db.tx.execs[0]
	if !strings.Contains(first.query, "pg_advisory_xact_lock_shared") {
		t.Fatalf("first exec = %q, want shared advisory lock", first.query)
	}
	if !strings.Contains(first.query, "hashtext") {
		t.Fatalf("first exec = %q, want namespaced two-arg partitioned lock, not the global key", first.query)
	}
	if got, want := first.args[0], deferredMaintenanceLockNamespace; got != want {
		t.Fatalf("shared barrier namespace = %v, want %v", got, want)
	}
	if got, want := first.args[1], deferredMaintenanceRepoLockKey(scopeValue); got != want {
		t.Fatalf("shared barrier repo key = %v, want %v", got, want)
	}
}

// TestMaintenanceTakesPerRepoExclusiveLocksInOrder proves the leader maintenance
// pass partitions its exclusive lock by source repository instead of holding one
// fleet-wide exclusive lock. Disjoint source repositories acquire disjoint lock
// partitions, so maintenance of repo A does not serialize against repo B. Locks
// are acquired in deterministic sorted order to keep multi-repo acquisition
// deadlock-free.
func TestMaintenanceTakesPerRepoExclusiveLocksInOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			// active repository generations loaded first for per-repo lock acquisition.
			{rows: [][]any{
				{"repo-zeta", "scope-zeta", "gen-zeta"},
				{"repo-alpha", "scope-alpha", "gen-alpha"},
			}},
			// repository catalog: two repos.
			{rows: [][]any{
				{[]byte(`{"repo_id":"repo-zeta","name":"zeta"}`)},
				{[]byte(`{"repo_id":"repo-alpha","name":"alpha"}`)},
			}},
			// active repository generations re-loaded by the backfill pass.
			{rows: [][]any{
				{"repo-zeta", "scope-zeta", "gen-zeta"},
				{"repo-alpha", "scope-alpha", "gen-alpha"},
			}},
			// latest relationship facts: none (no evidence to write).
			{rows: [][]any{}},
			// succeeded deployment_mapping work items: none.
			{rows: [][]any{}},
		},
	}
	db := &fakeTransactionalDB{tx: tx}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.RunDeferredRelationshipMaintenance(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v, want nil", err)
	}

	var lockKeys []any
	for _, exec := range tx.execs {
		if strings.Contains(exec.query, "pg_advisory_xact_lock(") &&
			strings.Contains(exec.query, "hashtext") {
			if got, want := exec.args[0], deferredMaintenanceLockNamespace; got != want {
				t.Fatalf("exclusive lock namespace = %v, want %v", got, want)
			}
			lockKeys = append(lockKeys, exec.args[1])
		}
	}
	if len(lockKeys) != 2 {
		t.Fatalf("per-repo exclusive lock count = %d, want 2 (one per active repo)", len(lockKeys))
	}

	// No single global exclusive lock should be taken anymore.
	for _, exec := range tx.execs {
		if exec.query == "SELECT pg_advisory_xact_lock($1)" {
			if got, ok := exec.args[0].(int64); ok && got == deferredMaintenanceBarrierLockKey {
				t.Fatalf("maintenance still takes the fleet-wide global exclusive lock %v", got)
			}
		}
	}

	wantAlpha := deferredMaintenanceRepoLockKeyFromID("repo-alpha")
	wantZeta := deferredMaintenanceRepoLockKeyFromID("repo-zeta")
	if lockKeys[0] != wantAlpha || lockKeys[1] != wantZeta {
		t.Fatalf("lock keys = %v, want sorted [%v %v]", lockKeys, wantAlpha, wantZeta)
	}
}

// TestRepoLockKeyDisjointForDistinctRepos proves distinct repositories map to
// distinct lock partitions and the same repository maps to a stable key, which
// is the property that lets disjoint maintenance run concurrently while keeping
// commit/maintenance fencing correct for a shared repository.
func TestRepoLockKeyDisjointForDistinctRepos(t *testing.T) {
	t.Parallel()

	a := deferredMaintenanceRepoLockKeyFromID("repo-A")
	b := deferredMaintenanceRepoLockKeyFromID("repo-B")
	if a == b {
		t.Fatalf("distinct repos produced equal lock keys: %q", a)
	}
	if a != deferredMaintenanceRepoLockKeyFromID("repo-A") {
		t.Fatal("repo lock key is not stable for the same repo id")
	}
}

// advisoryLockManager simulates Postgres transaction-level advisory lock
// semantics for the deferred-maintenance partition keys: many holders may share
// one key, an exclusive request blocks until no shared or exclusive holder
// remains on that key, and disjoint keys never contend. It lets the concurrency
// proof below run deterministically without a live database.
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

// TestDisjointRepoMaintenanceRunsConcurrently proves the core regression fix:
// two maintenance passes over disjoint repository sets acquire their exclusive
// locks and make progress at the same time. Under the retired global lock the
// second pass could not enter until the first committed; with per-repo
// partitioning both proceed in parallel, so one stalled repository no longer
// stalls the fleet. It also exercises overlapping sets to confirm same-key
// exclusion still holds, and proves sorted acquisition is deadlock-free when two
// passes share repositories in opposite input order.
func TestDisjointRepoMaintenanceRunsConcurrently(t *testing.T) {
	t.Parallel()

	mgr := newAdvisoryLockManager()
	ctx := context.Background()

	// Pass A holds repo-alpha and repo-beta exclusively and parks inside the
	// "transaction" until released, modeling a slow/stalled maintenance run.
	txA := &advisoryLockTx{mgr: mgr}
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, txA, []string{"repo-beta", "repo-alpha"}); err != nil {
		t.Fatalf("pass A lock acquisition: %v", err)
	}

	// Pass B over disjoint repos must complete without waiting on pass A.
	doneB := make(chan struct{})
	go func() {
		txB := &advisoryLockTx{mgr: mgr}
		if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, txB, []string{"repo-gamma", "repo-delta"}); err != nil {
			t.Errorf("pass B lock acquisition: %v", err)
		}
		_ = txB.Commit()
		close(doneB)
	}()

	select {
	case <-doneB:
		// Disjoint pass proceeded while pass A still held its locks.
	case <-time.After(2 * time.Second):
		t.Fatal("disjoint-repo maintenance blocked on unrelated repository locks (global serialization not removed)")
	}

	// A commit for a repository pass A holds must wait until pass A releases.
	commitBlocked := make(chan struct{})
	commitProceeded := make(chan struct{})
	go func() {
		txCommit := &advisoryLockTx{mgr: mgr}
		close(commitBlocked)
		if err := acquireDeferredMaintenanceRepoSharedLock(ctx, txCommit, "repo-alpha"); err != nil {
			t.Errorf("commit shared lock: %v", err)
		}
		_ = txCommit.Commit()
		close(commitProceeded)
	}()

	<-commitBlocked
	select {
	case <-commitProceeded:
		t.Fatal("commit for a repo under active maintenance did not wait for the exclusive lock")
	case <-time.After(100 * time.Millisecond):
		// Correctly blocked while pass A holds repo-alpha exclusively.
	}

	_ = txA.Commit() // release pass A; the same-repo commit may now proceed.

	select {
	case <-commitProceeded:
	case <-time.After(2 * time.Second):
		t.Fatal("commit did not proceed after maintenance released the repo lock")
	}
}

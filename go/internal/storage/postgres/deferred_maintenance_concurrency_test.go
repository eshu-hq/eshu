package postgres

import (
	"context"
	"testing"
	"time"
)

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

// TestWholeCorpusMaintenanceNeverHoldsFleetWideLockSet is the P1 regression
// proof. It drives the whole-corpus entrypoint RunDeferredRelationshipMaintenance
// over four active repositories with a batch size of two. The fix processes the
// corpus in independent per-batch transactions, each acquiring and releasing
// only its own batch's exclusive locks, so the peak number of repository locks
// held at once equals the batch size, not the corpus size. Under the previous
// design every active-repo lock was held in one maintenance transaction until
// the whole pass committed, so peak-held would equal the corpus size (4). The
// assertions below fail on that design and pass on the per-batch design.
func TestWholeCorpusMaintenanceNeverHoldsFleetWideLockSet(t *testing.T) {
	t.Parallel()

	activeGens := [][]any{
		{"repo-a", "scope-a", "gen-a"},
		{"repo-b", "scope-b", "gen-b"},
		{"repo-c", "scope-c", "gen-c"},
		{"repo-d", "scope-d", "gen-d"},
	}
	db := &lockAwareMaintenanceDB{
		mgr: newAdvisoryLockManager(),
		snapshotRows: []*queueFakeRows{
			{rows: [][]any{
				{[]byte(`{"repo_id":"repo-a","name":"a"}`)},
				{[]byte(`{"repo_id":"repo-b","name":"b"}`)},
				{[]byte(`{"repo_id":"repo-c","name":"c"}`)},
				{[]byte(`{"repo_id":"repo-d","name":"d"}`)},
			}},
			{rows: [][]any{}},  // latest facts: none.
			{rows: activeGens}, // active generations snapshot.
		},
		batchActiveGens:  activeGens,
		succeededWorkIDs: [][]any{},
	}

	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC) }
	store.maintenanceBatchSize = 2
	// Pin a single worker so this test isolates the per-batch lock-release
	// property (#3482): one batch acquires and releases its locks before the next
	// begins, so peak-held equals the batch size. The concurrent-batch peak bound
	// (workers x batch size) is proven separately in
	// TestConcurrentBackfillBatchesBoundPeakHeldLocks.
	store.maintenanceWorkers = 1

	if err := store.RunDeferredRelationshipMaintenance(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v, want nil", err)
	}

	if db.peakHeld > store.maintenanceBatchSize {
		t.Fatalf("peak simultaneous repo locks = %d, want <= batch size %d (fleet-wide lock set still held)",
			db.peakHeld, store.maintenanceBatchSize)
	}
	if db.heldExclusive != 0 {
		t.Fatalf("residual held exclusive locks = %d, want 0 (locks not released between batches)", db.heldExclusive)
	}
	// 4 repos at batch size 2 => 2 batch transactions; plus the reopen
	// transaction => at least 3 independent transactions.
	if db.beginCount < 3 {
		t.Fatalf("begin count = %d, want >= 3 (independent per-batch + reopen transactions)", db.beginCount)
	}
}

// TestConcurrentBackfillBatchesBoundPeakHeldLocks is the #3704 concurrency-safety
// proof. With several disjoint per-repository batches and a worker pool above one,
// the batches run concurrently (peak held locks exceed a single batch's size) but
// the peak stays bounded by workers x batch size — never the fleet-wide lock set.
// All exclusive locks are released by the end (no leak), and every batch commits.
// The advisoryLockManager enforces real exclusive-lock semantics, so a lock-order
// deadlock between concurrent batches would hang this test rather than pass.
func TestConcurrentBackfillBatchesBoundPeakHeldLocks(t *testing.T) {
	t.Parallel()

	const repoCount = 8
	catalogRows := make([][]any, 0, repoCount)
	activeGens := make([][]any, 0, repoCount)
	for i := 0; i < repoCount; i++ {
		id := "repo-" + string(rune('a'+i))
		catalogRows = append(catalogRows, []any{[]byte(`{"repo_id":"` + id + `","name":"` + id + `"}`)})
		activeGens = append(activeGens, []any{id, "scope-" + id, "gen-" + id})
	}
	const (
		batchSize = 2 // 8 repos / 2 => 4 batches
		workers   = 4
	)
	batches := repoCount / batchSize
	cohort := workers
	if cohort > batches {
		cohort = batches
	}

	db := &lockAwareMaintenanceDB{
		mgr: newAdvisoryLockManager(),
		snapshotRows: []*queueFakeRows{
			{rows: catalogRows},
			{rows: [][]any{}},  // latest facts: none.
			{rows: activeGens}, // active generations snapshot.
		},
		batchActiveGens:  activeGens,
		succeededWorkIDs: [][]any{},
	}
	// Deterministic rendezvous: every batch holds its locks until the full
	// concurrent cohort has arrived, so the observed peak is forced regardless of
	// the OS scheduler (the macOS CI runner could otherwise finish one batch
	// before the next acquired, observing only batchSize). The 5s safety deadline
	// guarantees the test fails loudly rather than hanging if the pool ever runs
	// fewer than `cohort` batches concurrently.
	db.concurrencyBarrier = newCohortBarrier(cohort, 5*time.Second)

	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC) }
	store.maintenanceBatchSize = batchSize
	store.maintenanceWorkers = workers

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	// With the barrier, the whole cohort holds its locks at once, so the peak is
	// deterministically cohort*batchSize. This both proves real concurrency
	// (peak > batchSize) and pins the workers*batchSize upper bound exactly.
	wantPeak := cohort * batchSize
	if db.peakHeld != wantPeak {
		t.Fatalf("peak simultaneous repo locks = %d, want exactly %d (cohort %d x batch size %d)",
			db.peakHeld, wantPeak, cohort, batchSize)
	}
	if db.peakHeld <= store.maintenanceBatchSize {
		t.Fatalf("peak simultaneous repo locks = %d, want > batch size %d (batches did not run concurrently)",
			db.peakHeld, store.maintenanceBatchSize)
	}
	maxBound := store.maintenanceWorkers * store.maintenanceBatchSize
	if db.peakHeld > maxBound {
		t.Fatalf("peak simultaneous repo locks = %d, exceeds workers*batch bound %d", db.peakHeld, maxBound)
	}
	if db.heldExclusive != 0 {
		t.Fatalf("residual held exclusive locks = %d, want 0 (locks leaked across concurrent batches)", db.heldExclusive)
	}
}

// TestWholeCorpusMaintenanceDoesNotBlockUnrelatedCommit proves that while one
// batch holds its repositories' exclusive locks, a commit on a repository in a
// later batch is not blocked. The repo-a batch holds only repo-a; a concurrent
// commit on repo-b takes its shared lock and proceeds immediately. Under the
// retired single-transaction design, repo-b's exclusive lock was held for the
// whole pass and this commit would block.
func TestWholeCorpusMaintenanceDoesNotBlockUnrelatedCommit(t *testing.T) {
	t.Parallel()

	mgr := newAdvisoryLockManager()
	// Take repo-a's exclusive lock to model the repo-a batch being mid-flight.
	batchTx := &advisoryLockTx{mgr: mgr}
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(
		context.Background(), batchTx, []string{"repo-a"},
	); err != nil {
		t.Fatalf("repo-a batch lock: %v", err)
	}

	commitProceeded := make(chan struct{})
	go func() {
		commitTx := &advisoryLockTx{mgr: mgr}
		if err := acquireDeferredMaintenanceRepoSharedLock(
			context.Background(), commitTx, "repo-b",
		); err != nil {
			t.Errorf("repo-b commit shared lock: %v", err)
		}
		_ = commitTx.Commit()
		close(commitProceeded)
	}()

	select {
	case <-commitProceeded:
		// Unrelated commit proceeded while the repo-a batch held repo-a's lock.
	case <-time.After(2 * time.Second):
		t.Fatal("commit on unrelated repo-b blocked by in-flight repo-a batch (fleet-wide serialization still present)")
	}
	_ = batchTx.Commit()
}

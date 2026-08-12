// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Real-Postgres regression tests for #4586: running more than one workflow
// coordinator must not enqueue the same collector target twice.
//
// Two coordinators that tick either side of a 30-second wall-clock bucket edge
// plan the same target under different run/work-item/generation identifiers.
// Those rows are legitimately distinct, so the INSERT's ON CONFLICT DO NOTHING
// cannot collapse them. The only thing that stops both from landing is the
// pg_advisory_xact_lock in createRunWithWorkItemsIfNoOpenTargetsOnce, which
// makes the read of already-planned targets and the insert one atomic step.
//
// Run with a throwaway Postgres:
//
//	ESHU_POSTGRES_DSN='postgres://eshu:change-me@localhost:<port>/eshu?sslmode=disable' \
//	  go test ./internal/storage/postgres -run 'WorkflowGuardedRunCreate.*Live' -count=1 -v
//
// The hermetic companion (workflow_control_open_targets_lock_test.go) asserts
// the same ordering without a database, so CI still fails if the lock moves or
// disappears when no DSN is configured.

// workflowOpenTargetsLockRaceRuns is the iteration count the #4586 proof used.
// Removing the advisory lock reproduced a duplicate in 25 of 25 runs, so 25
// keeps the regression test at the sensitivity the evidence was recorded at.
const workflowOpenTargetsLockRaceRuns = 25

func workflowOpenTargetsLockLiveStore(t *testing.T) (*sql.DB, *WorkflowControlStore, context.Context) {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres #4586 coordinator N-safety proofs")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = db.Close() })

	store := NewWorkflowControlStore(SQLDB{DB: db})
	// Schema DDL gets its own deadline so cold setup under host load cannot eat
	// the proof's budget, matching awsCloudRuntimeDriftAdmissionLiveDB.
	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 2*time.Minute)
	err = store.EnsureSchema(schemaCtx)
	cancelSchema()
	if err != nil {
		t.Fatalf("ensure workflow control schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return db, store, ctx
}

// workflowOpenTargetsLockCleanup deletes the runs this test created. Work items
// cascade from workflow_runs, so this leaves no rows behind for other live
// tests sharing the database. It never truncates: a sibling live test may be
// using the same tables.
func workflowOpenTargetsLockCleanup(t *testing.T, db *sql.DB, runPrefix string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx,
			`DELETE FROM workflow_runs WHERE run_id LIKE $1 || '%'`, runPrefix); err != nil {
			t.Logf("cleanup runs %q: %v", runPrefix, err)
		}
	})
}

// workflowOpenTargetsLockPlan builds the run plus single work item one
// coordinator would plan for a 30-second bucket. bucket only moves run_id,
// work_item_id, and generation_id; every column the admission guard compares is
// identical across buckets, which is what makes the two rows a duplicate of the
// same target rather than two different targets.
func workflowOpenTargetsLockPlan(
	instanceID string,
	scopeID string,
	bucket string,
	at time.Time,
) (workflow.Run, []workflow.WorkItem) {
	runID := "oci_registry:" + instanceID + ":schedule:continuous-" + bucket
	generationID := "oci_registry:" + bucket + ":" + scopeID
	run := workflow.Run{
		RunID:              runID,
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorOCIRegistry),
		CreatedAt:          at,
		UpdatedAt:          at,
	}
	item := workflow.WorkItem{
		WorkItemID:          "oci_registry:" + instanceID + ":" + generationID,
		RunID:               runID,
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorOCIRegistry),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           at,
		UpdatedAt:           at,
	}
	return run, []workflow.WorkItem{item}
}

// TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive is
// the #4586 N-safety regression: two coordinators racing across a bucket edge
// leave exactly one claimable work item for the shared target.
//
// Deleting the pg_advisory_xact_lock from
// createRunWithWorkItemsIfNoOpenTargetsOnce, or moving it after the
// planned-target read, makes both coordinators read "nothing planned" and both
// insert. That produced a duplicate in 25 of 25 runs when measured.
func TestWorkflowGuardedRunCreateAdmitsOneTargetForConcurrentCoordinatorsLive(t *testing.T) {
	db, store, ctx := workflowOpenTargetsLockLiveStore(t)

	suffix := fmt.Sprintf("4586-race-%d", time.Now().UnixNano())
	instanceID := "collector-oci-registry-" + suffix
	runPrefix := "oci_registry:" + instanceID + ":"
	workflowOpenTargetsLockCleanup(t, db, runPrefix)

	at := time.Date(2026, time.May, 13, 15, 0, 0, 0, time.UTC)
	duplicates := 0
	for iteration := 1; iteration <= workflowOpenTargetsLockRaceRuns; iteration++ {
		// A fresh scope per iteration keeps earlier iterations' rows from
		// covering this one, so every iteration is a real race.
		scopeID := fmt.Sprintf("oci-registry://registry.invalid/%s/%d", suffix, iteration)
		runA, itemsA := workflowOpenTargetsLockPlan(instanceID, scopeID,
			fmt.Sprintf("20260513T150000Z-%d", iteration), at)
		runB, itemsB := workflowOpenTargetsLockPlan(instanceID, scopeID,
			fmt.Sprintf("20260513T150030Z-%d", iteration), at)

		var wg sync.WaitGroup
		start := make(chan struct{})
		enqueued := make([]int, 2)
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			enqueued[0], errs[0] = store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, runA, itemsA)
		}()
		go func() {
			defer wg.Done()
			<-start
			enqueued[1], errs[1] = store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, runB, itemsB)
		}()
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("iteration %d: coordinator %d: CreateRunWithWorkItemsIfNoOpenTargets() error = %v", iteration, i, err)
			}
		}

		var total, pending int
		if err := db.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE status = 'pending')
FROM workflow_work_items
WHERE collector_instance_id = $1 AND scope_id = $2`,
			instanceID, scopeID).Scan(&total, &pending); err != nil {
			t.Fatalf("iteration %d: count work items: %v", iteration, err)
		}
		if pending != 1 || total != 1 {
			duplicates++
			t.Errorf("iteration %d: rows = %d, pending = %d, want 1 and 1; two coordinators admitted the same target twice (returned enqueued %d and %d)",
				iteration, total, pending, enqueued[0], enqueued[1])
		}
		if got := enqueued[0] + enqueued[1]; got != 1 {
			t.Errorf("iteration %d: enqueued sum = %d (%d + %d), want exactly 1 admitted target",
				iteration, got, enqueued[0], enqueued[1])
		}
	}
	if duplicates > 0 {
		t.Fatalf("duplicate admission in %d of %d runs; the collector-instance planning lock is not holding",
			duplicates, workflowOpenTargetsLockRaceRuns)
	}
}

// TestWorkflowGuardedRunCreateWaitsOnPlanningLockBeforeReadingTargetsLive is the
// deterministic half of the #4586 proof. Instead of racing and counting, it
// holds the collector-instance planning lock from an outside session and checks
// that the guard waits for it before reading planned targets.
//
// The sequence is the read-then-write race, forced:
//
//  1. an outside session takes the planning lock for this collector instance;
//  2. the guard starts and must block on that lock;
//  3. while it is blocked, a competing coordinator's row is committed;
//  4. the lock is released.
//
// A guard holding the lock reads planned targets in step 4 and sees the
// competing row, so it admits nothing. A guard without the lock does its read
// in step 2 -- before the competing row exists -- and inserts a duplicate.
// Step 2 fails outright in that case: no backend ever waits on the lock.
func TestWorkflowGuardedRunCreateWaitsOnPlanningLockBeforeReadingTargetsLive(t *testing.T) {
	db, store, ctx := workflowOpenTargetsLockLiveStore(t)

	suffix := fmt.Sprintf("4586-order-%d", time.Now().UnixNano())
	instanceID := "collector-oci-registry-" + suffix
	runPrefix := "oci_registry:" + instanceID + ":"
	workflowOpenTargetsLockCleanup(t, db, runPrefix)

	at := time.Date(2026, time.May, 13, 15, 0, 0, 0, time.UTC)
	scopeID := "oci-registry://registry.invalid/" + suffix
	lateRun, lateItems := workflowOpenTargetsLockPlan(instanceID, scopeID, "20260513T150030Z", at)
	earlyRun, earlyItems := workflowOpenTargetsLockPlan(instanceID, scopeID, "20260513T150000Z", at)

	// The key comes from the production derivation, so a change to the lock's
	// key shape moves this test with it instead of silently unhooking it.
	lockKey := workflowPlanningLockKey(lateItems[0])

	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open lock-holder connection: %v", err)
	}
	defer func() { _ = holder.Close() }()
	holderTx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holder transaction: %v", err)
	}
	released := false
	defer func() {
		if !released {
			_ = holderTx.Rollback()
		}
	}()
	if _, err := holderTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
		t.Fatalf("take planning lock in outside session: %v", err)
	}

	guardDone := make(chan struct{})
	var guardEnqueued int
	var guardErr error
	go func() {
		defer close(guardDone)
		guardEnqueued, guardErr = store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, lateRun, lateItems)
	}()

	if err := waitForWorkflowPlanningLockWaiter(ctx, db, lockKey, 20*time.Second); err != nil {
		select {
		case <-guardDone:
			t.Fatalf("the guard finished (enqueued = %d, err = %v) without ever waiting on the collector-instance planning lock: it read planned targets outside the lock, which is the #4586 duplicate-admission race: %v",
				guardEnqueued, guardErr, err)
		default:
		}
		t.Fatalf("no backend waited on the collector-instance planning lock: %v", err)
	}

	// The competing coordinator commits its plan for the same target while the
	// guard is parked on the lock.
	if err := store.CreateRun(ctx, earlyRun); err != nil {
		t.Fatalf("competing coordinator: CreateRun() error = %v", err)
	}
	if err := store.EnqueueWorkItems(ctx, earlyItems); err != nil {
		t.Fatalf("competing coordinator: EnqueueWorkItems() error = %v", err)
	}

	released = true
	if err := holderTx.Rollback(); err != nil {
		t.Fatalf("release planning lock: %v", err)
	}

	select {
	case <-guardDone:
	case <-ctx.Done():
		t.Fatalf("guard did not return after the planning lock was released: %v", ctx.Err())
	}
	if guardErr != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", guardErr)
	}
	if guardEnqueued != 0 {
		t.Fatalf("enqueued = %d, want 0: the competing coordinator's open target committed before the lock was released", guardEnqueued)
	}

	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM workflow_work_items WHERE collector_instance_id = $1 AND scope_id = $2`,
		instanceID, scopeID).Scan(&total); err != nil {
		t.Fatalf("count work items: %v", err)
	}
	if total != 1 {
		t.Fatalf("work item rows = %d, want 1 for one target", total)
	}
}

// TestWorkflowGuardedRunCreateReportsRowsActuallyInsertedLive covers the #4586
// accounting gap against the index that produces it.
//
// workflow_work_items has a partial unique index on
// (collector_instance_id, scope_id, generation_id) for non-terminal
// terraform_state candidates. The admission guard compares a wider tuple that
// includes acceptance_unit_id, so it can consider two rows distinct targets
// while the index considers them one. The second row is dropped by
// ON CONFLICT DO NOTHING, and the guard used to report it as enqueued anyway.
func TestWorkflowGuardedRunCreateReportsRowsActuallyInsertedLive(t *testing.T) {
	db, store, ctx := workflowOpenTargetsLockLiveStore(t)

	suffix := fmt.Sprintf("4586-count-%d", time.Now().UnixNano())
	instanceID := "collector-tfstate-" + suffix
	runID := "terraform_state:" + instanceID + ":schedule:continuous-20260513T150000Z"
	workflowOpenTargetsLockCleanup(t, db, "terraform_state:"+instanceID+":")

	at := time.Date(2026, time.May, 13, 15, 0, 0, 0, time.UTC)
	scopeID := "state_snapshot:s3:" + suffix
	generationID := "terraform_state_candidate:s3:" + suffix
	newItem := func(acceptanceUnitID string) workflow.WorkItem {
		return workflow.WorkItem{
			WorkItemID:          runID + ":" + acceptanceUnitID,
			RunID:               runID,
			CollectorKind:       scope.CollectorTerraformState,
			CollectorInstanceID: instanceID,
			SourceSystem:        string(scope.CollectorTerraformState),
			ScopeID:             scopeID,
			AcceptanceUnitID:    acceptanceUnitID,
			SourceRunID:         generationID,
			GenerationID:        generationID,
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           at,
			UpdatedAt:           at,
		}
	}
	run := workflow.Run{
		RunID:              runID,
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          at,
		UpdatedAt:          at,
	}
	items := []workflow.WorkItem{newItem("repository:eshu-4586-a"), newItem("repository:eshu-4586-b")}

	enqueued, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM workflow_work_items WHERE collector_instance_id = $1 AND scope_id = $2`,
		instanceID, scopeID).Scan(&rows); err != nil {
		t.Fatalf("count work items: %v", err)
	}
	if rows != 1 {
		t.Fatalf("work item rows = %d, want 1: the terraform_state candidate index should have dropped the second acceptance unit; if it did not, this test no longer exercises the accounting gap", rows)
	}
	if enqueued != rows {
		t.Fatalf("enqueued = %d, actual rows = %d: the guard reported work an operator cannot find in the queue, and the coordinator's \"skipped duplicate workflow work\" log never fires (#4586)", enqueued, rows)
	}
}

// waitForWorkflowPlanningLockWaiter blocks until some backend is waiting on the
// advisory lock identified by key, or the budget runs out.
//
// pg_advisory_xact_lock(bigint) stores the key split across two 32-bit lock-tag
// fields, surfaced by pg_locks as classid (high half) and objid (low half),
// with objsubid = 1 marking the single-argument form.
func waitForWorkflowPlanningLockWaiter(
	ctx context.Context,
	db *sql.DB,
	key int64,
	budget time.Duration,
) error {
	unsigned := uint64(key) // #nosec G115 -- the lock key is an intentional bit-reinterpret; both halves are read back unsigned
	classID := int64(unsigned >> 32)
	objID := int64(unsigned & 0xFFFFFFFF)

	deadline := time.Now().Add(budget)
	for {
		var waiters int
		if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_locks
WHERE locktype = 'advisory'
  AND NOT granted
  AND classid::bigint = $1
  AND objid::bigint = $2
  AND objsubid = 1`, classID, objID).Scan(&waiters); err != nil {
			return fmt.Errorf("read pg_locks: %w", err)
		}
		if waiters > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no ungranted advisory lock on classid=%d objid=%d after %s", classID, objID, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

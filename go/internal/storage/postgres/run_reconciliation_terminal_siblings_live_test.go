// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10091 disposable PostgreSQL proof through the real reconcile, open-target guard and claim path that a terminal work item keeps its pending siblings open, that the run fails only once they drain, that every path back to pending keeps the run open, that a failed run's items stay frozen, and that concurrent workers, reconcilers and a planner never hold two open items for one target; each proof creates its own isolated bootstrapped schema; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Real-Postgres regression tests for #10091: one terminal work item must not
// close its pending siblings to the planner.
//
// Run with a throwaway Postgres:
//
//	ESHU_POSTGRES_DSN='postgres://postgres:postgres@localhost:<port>/postgres?sslmode=disable' \
//	ESHU_POSTGRES_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres -run 'TerminalSiblings' -count=1 -v

const terminalSiblingsLease = 5 * time.Minute

// terminalSiblingsRun is the fixture for one planner's view of a collector
// instance: every plan for the same instance and scopes targets the same work.
type terminalSiblingsRun struct {
	instanceID string
	scopes     []string
}

// plan builds the run and work items one planner pass would create. label moves
// run_id, work_item_id and generation_id only; every column the guard compares
// stays the same, so two plans are duplicates of one target set.
func (f terminalSiblingsRun) plan(label string, at time.Time) (workflow.Run, []workflow.WorkItem) {
	runID := f.instanceID + ":" + label
	run := workflow.Run{
		RunID:              runID,
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorGit),
		CreatedAt:          at,
		UpdatedAt:          at,
	}
	items := make([]workflow.WorkItem, 0, len(f.scopes))
	for _, scopeID := range f.scopes {
		generationID := runID + ":" + scopeID + ":generation"
		items = append(items, workflow.WorkItem{
			WorkItemID:          runID + ":" + scopeID,
			RunID:               runID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: f.instanceID,
			SourceSystem:        string(scope.CollectorGit),
			ScopeID:             scopeID,
			AcceptanceUnitID:    scopeID,
			SourceRunID:         generationID,
			GenerationID:        generationID,
			FairnessKey:         scopeID,
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           at,
			UpdatedAt:           at,
		})
	}
	return run, items
}

func (f terminalSiblingsRun) admit(
	t *testing.T,
	ctx context.Context,
	store *WorkflowControlStore,
	label string,
	at time.Time,
) workflow.RunAdmission {
	t.Helper()
	run, items := f.plan(label, at)
	admission, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets(%s) error = %v, want nil", label, err)
	}
	return admission
}

// claim takes the next eligible item for the instance. The fair-claim rotation
// picks the order, so callers act on the returned item, never on a fixed id.
func (f terminalSiblingsRun) claim(
	t *testing.T,
	ctx context.Context,
	store *WorkflowControlStore,
	at time.Time,
	claimID string,
) (workflow.WorkItem, workflow.Claim) {
	t.Helper()
	item, claim, found, err := store.ClaimNextEligible(ctx, f.selector(claimID), at, terminalSiblingsLease)
	if err != nil {
		t.Fatalf("ClaimNextEligible(%s) error = %v, want nil", claimID, err)
	}
	if !found {
		t.Fatalf("ClaimNextEligible(%s) found nothing, want a claimable sibling", claimID)
	}
	return item, claim
}

func (f terminalSiblingsRun) selector(claimID string) workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: f.instanceID,
		OwnerID:             f.instanceID + "-owner",
		ClaimID:             claimID,
	}
}

func terminalSiblingsMutation(item workflow.WorkItem, claim workflow.Claim, at time.Time) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:   item.WorkItemID,
		ClaimID:      claim.ClaimID,
		FencingToken: claim.FencingToken,
		OwnerID:      claim.OwnerID,
		ObservedAt:   at,
	}
}

func terminalSiblingsFailure(mutation workflow.ClaimMutation) workflow.ClaimMutation {
	mutation.FailureClass = "collect_failure"
	mutation.FailureMessage = "synthetic terminal failure"
	mutation.FailureTerminal = true
	return mutation
}

func terminalSiblingsReconcile(t *testing.T, ctx context.Context, store *WorkflowControlStore, at time.Time) {
	t.Helper()
	if _, err := store.ReconcileWorkflowRuns(ctx, at); err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
}

// terminalSiblingsRunEnd returns the persisted finished_at, so a proof can tell
// an open run from a finished one without trusting the status column alone.
func terminalSiblingsRunEnd(t *testing.T, ctx context.Context, db *sql.DB, runID string) sql.NullTime {
	t.Helper()
	var finishedAt sql.NullTime
	if err := db.QueryRowContext(ctx,
		`SELECT finished_at FROM workflow_runs WHERE run_id = $1`, runID).Scan(&finishedAt); err != nil {
		t.Fatalf("read finished_at of %s: %v", runID, err)
	}
	return finishedAt
}

func terminalSiblingsOpenCount(t *testing.T, ctx context.Context, db *sql.DB, runID string) int {
	t.Helper()
	var open int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM workflow_work_items
WHERE run_id = $1 AND status IN ('pending', 'claimed')`, runID).Scan(&open); err != nil {
		t.Fatalf("count open items of %s: %v", runID, err)
	}
	return open
}

// TestTerminalSiblingsStayOpenThroughReconcileGuardAndClaimLive is the #10091
// regression through the real path. Reverting the ReconcileRunProgress rule
// fails the first assertion after the terminal failure, and with only the status
// assertion removed the guard admits a duplicate of each sibling.
func TestTerminalSiblingsStayOpenThroughReconcileGuardAndClaimLive(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	base := time.Now().UTC().Truncate(time.Millisecond)
	fixture := terminalSiblingsRun{
		instanceID: "terminal-siblings-path",
		scopes:     []string{"scope-a", "scope-b", "scope-c"},
	}
	firstRunID := fixture.instanceID + ":bucket-1"

	if got := fixture.admit(t, ctx, store, "bucket-1", base); got.InsertedWorkItems != 3 {
		t.Fatalf("first admission = %+v, want 3 inserted", got)
	}

	// One item fails terminally through the real claim path.
	terminal, terminalClaim := fixture.claim(t, ctx, store, base.Add(time.Second), "claim-terminal")
	if err := store.FailClaimTerminal(ctx, terminalSiblingsFailure(
		terminalSiblingsMutation(terminal, terminalClaim, base.Add(2*time.Second)),
	)); err != nil {
		t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
	}

	terminalSiblingsReconcile(t, ctx, store, base.Add(3*time.Second))
	mustWorkflowRunStatus(t, db, firstRunID, workflow.RunStatusCollectionActive)
	if end := terminalSiblingsRunEnd(t, ctx, db, firstRunID); end.Valid {
		t.Fatalf("finished_at = %v, want NULL while siblings are pending", end.Time)
	}
	mustCompletenessStatus(t, db, firstRunID, string(scope.CollectorGit),
		string(gpphase.KeyspaceCodeEntitiesUID), string(gpphase.PhaseCanonicalNodesCommitted),
		workflow.CompletenessStatusBlocked)

	// The next planner pass finds all three targets open, the terminal one
	// included: its run has not ended.
	if got := fixture.admit(t, ctx, store, "bucket-2", base.Add(4*time.Second)); got.EligibleTargets != 0 || got.InsertedWorkItems != 0 {
		t.Fatalf("second admission = %+v, want nothing admitted while the first run is open", got)
	}

	// Siblings stay claimable by design: their work is independent of the
	// failure, and the run stays open across every intermediate state.
	second, secondClaim := fixture.claim(t, ctx, store, base.Add(5*time.Second), "claim-sibling-1")
	if second.WorkItemID == terminal.WorkItemID {
		t.Fatalf("claimed the terminal item %s again", terminal.WorkItemID)
	}
	terminalSiblingsReconcile(t, ctx, store, base.Add(6*time.Second))
	mustWorkflowRunStatus(t, db, firstRunID, workflow.RunStatusCollectionActive)
	if err := store.CompleteClaim(ctx, terminalSiblingsMutation(second, secondClaim, base.Add(7*time.Second))); err != nil {
		t.Fatalf("CompleteClaim(first sibling) error = %v, want nil", err)
	}

	third, thirdClaim := fixture.claim(t, ctx, store, base.Add(8*time.Second), "claim-sibling-2")
	terminalSiblingsReconcile(t, ctx, store, base.Add(9*time.Second))
	// No pending item is left, only a claimed one: still open.
	mustWorkflowRunStatus(t, db, firstRunID, workflow.RunStatusCollectionActive)
	if err := store.CompleteClaim(ctx, terminalSiblingsMutation(third, thirdClaim, base.Add(10*time.Second))); err != nil {
		t.Fatalf("CompleteClaim(second sibling) error = %v, want nil", err)
	}

	terminalSiblingsReconcile(t, ctx, store, base.Add(11*time.Second))
	mustWorkflowRunStatus(t, db, firstRunID, workflow.RunStatusFailed)
	failedAt := terminalSiblingsRunEnd(t, ctx, db, firstRunID)
	if !failedAt.Valid {
		t.Fatal("finished_at = NULL, want it set once the run fails")
	}

	// The run has ended, so the guard admits all three targets, the terminal
	// scope included: a terminal item does not stop planning for its scope.
	if got := fixture.admit(t, ctx, store, "bucket-3", base.Add(12*time.Second)); got.EligibleTargets != 3 || got.InsertedWorkItems != 3 {
		t.Fatalf("third admission = %+v, want all 3 targets admitted after the run failed", got)
	}

	// Failed is terminal: reconciliation no longer touches the run.
	terminalSiblingsReconcile(t, ctx, store, base.Add(13*time.Second))
	mustWorkflowRunStatus(t, db, firstRunID, workflow.RunStatusFailed)
	if end := terminalSiblingsRunEnd(t, ctx, db, firstRunID); !end.Valid || !end.Time.Equal(failedAt.Time) {
		t.Fatalf("finished_at = %+v, want it unchanged at %v", end, failedAt.Time)
	}
}

// TestTerminalSiblingsEveryPathBackToPendingKeepsRunOpenLive covers each write
// that returns a claimed item to pending. Each one requires item.status =
// 'claimed', so it can only fire before the run fails, and the run must read
// open afterwards.
func TestTerminalSiblingsEveryPathBackToPendingKeepsRunOpenLive(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	base := time.Now().UTC().Truncate(time.Millisecond)
	paths := []struct {
		name string
		// requeue returns the sibling to pending and reports when it is visible.
		requeue func(t *testing.T, item workflow.WorkItem, claim workflow.Claim, at time.Time) time.Time
	}{
		{
			name: "retryable",
			requeue: func(t *testing.T, item workflow.WorkItem, claim workflow.Claim, at time.Time) time.Time {
				t.Helper()
				mutation := terminalSiblingsMutation(item, claim, at)
				mutation.FailureClass = "collect_failure"
				mutation.FailureMessage = "synthetic retryable failure"
				if err := store.FailClaimRetryable(ctx, mutation); err != nil {
					t.Fatalf("FailClaimRetryable() error = %v, want nil", err)
				}
				return at
			},
		},
		{
			name: "release",
			requeue: func(t *testing.T, item workflow.WorkItem, claim workflow.Claim, at time.Time) time.Time {
				t.Helper()
				if err := store.ReleaseClaim(ctx, terminalSiblingsMutation(item, claim, at)); err != nil {
					t.Fatalf("ReleaseClaim() error = %v, want nil", err)
				}
				return at
			},
		},
		{
			name: "expired lease",
			requeue: func(t *testing.T, item workflow.WorkItem, _ workflow.Claim, at time.Time) time.Time {
				t.Helper()
				asOf := at.Add(terminalSiblingsLease + time.Second)
				reaped, err := store.ReapExpiredClaims(ctx, asOf, 10, time.Second)
				if err != nil {
					t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
				}
				if len(reaped) != 1 || reaped[0].WorkItemID != item.WorkItemID {
					t.Fatalf("reaped = %+v, want only %s", reaped, item.WorkItemID)
				}
				return asOf.Add(time.Second)
			},
		},
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			fixture := terminalSiblingsRun{
				instanceID: "terminal-siblings-requeue-" + path.name,
				scopes:     []string{"scope-a", "scope-b"},
			}
			runID := fixture.instanceID + ":bucket-1"
			fixture.admit(t, ctx, store, "bucket-1", base)

			terminal, terminalClaim := fixture.claim(t, ctx, store, base.Add(time.Second), fixture.instanceID+"-terminal")
			if err := store.FailClaimTerminal(ctx, terminalSiblingsFailure(
				terminalSiblingsMutation(terminal, terminalClaim, base.Add(2*time.Second)),
			)); err != nil {
				t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
			}

			sibling, siblingClaim := fixture.claim(t, ctx, store, base.Add(3*time.Second), fixture.instanceID+"-sibling")
			visibleAt := path.requeue(t, sibling, siblingClaim, base.Add(4*time.Second))
			if got := terminalSiblingsOpenCount(t, ctx, db, runID); got != 1 {
				t.Fatalf("open items = %d, want the requeued sibling", got)
			}

			terminalSiblingsReconcile(t, ctx, store, visibleAt)
			mustWorkflowRunStatus(t, db, runID, workflow.RunStatusCollectionActive)
			if got := fixture.admit(t, ctx, store, "bucket-2", visibleAt); got.EligibleTargets != 0 {
				t.Fatalf("admission = %+v, want nothing admitted while the sibling is requeued", got)
			}

			again, againClaim := fixture.claim(t, ctx, store, visibleAt.Add(time.Second), fixture.instanceID+"-again")
			if again.WorkItemID != sibling.WorkItemID {
				t.Fatalf("reclaimed %s, want the requeued sibling %s", again.WorkItemID, sibling.WorkItemID)
			}
			if err := store.CompleteClaim(ctx, terminalSiblingsMutation(again, againClaim, visibleAt.Add(2*time.Second))); err != nil {
				t.Fatalf("CompleteClaim() error = %v, want nil", err)
			}
			terminalSiblingsReconcile(t, ctx, store, visibleAt.Add(3*time.Second))
			mustWorkflowRunStatus(t, db, runID, workflow.RunStatusFailed)
		})
	}
}

// TestTerminalSiblingsFailedRunItemsStayFrozenLive is the premise of the
// interleaving argument: once a run has failed, nothing returns one of its items
// to pending, so the guard can safely stop counting them. Stale claim mutations
// and the reaper all miss, and nothing is claimable.
func TestTerminalSiblingsFailedRunItemsStayFrozenLive(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	base := time.Now().UTC().Truncate(time.Millisecond)
	fixture := terminalSiblingsRun{
		instanceID: "terminal-siblings-frozen",
		scopes:     []string{"scope-a", "scope-b"},
	}
	runID := fixture.instanceID + ":bucket-1"
	fixture.admit(t, ctx, store, "bucket-1", base)

	terminal, terminalClaim := fixture.claim(t, ctx, store, base.Add(time.Second), "claim-terminal")
	staleTerminal := terminalSiblingsMutation(terminal, terminalClaim, base.Add(2*time.Second))
	if err := store.FailClaimTerminal(ctx, terminalSiblingsFailure(staleTerminal)); err != nil {
		t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
	}
	sibling, siblingClaim := fixture.claim(t, ctx, store, base.Add(3*time.Second), "claim-sibling")
	staleSibling := terminalSiblingsMutation(sibling, siblingClaim, base.Add(4*time.Second))
	if err := store.CompleteClaim(ctx, staleSibling); err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}
	terminalSiblingsReconcile(t, ctx, store, base.Add(5*time.Second))
	mustWorkflowRunStatus(t, db, runID, workflow.RunStatusFailed)

	// A delayed worker replays its old mutations after the flip. Whether each
	// call returns an error or an idempotent ack is a separate contract; what
	// matters here is that no item returns to pending.
	for _, stale := range []workflow.ClaimMutation{staleTerminal, staleSibling} {
		retry := stale
		retry.ObservedAt = base.Add(6 * time.Second)
		retry.FailureClass = "collect_failure"
		retry.FailureMessage = "stale retry after the run failed"
		if err := store.FailClaimRetryable(ctx, retry); err != nil {
			t.Logf("stale FailClaimRetryable(%s) = %v", stale.WorkItemID, err)
		}
		release := stale
		release.ObservedAt = base.Add(7 * time.Second)
		if err := store.ReleaseClaim(ctx, release); err != nil {
			t.Logf("stale ReleaseClaim(%s) = %v", stale.WorkItemID, err)
		}
	}
	reaped, err := store.ReapExpiredClaims(ctx, base.Add(terminalSiblingsLease+time.Hour), 10, time.Second)
	if err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if len(reaped) != 0 {
		t.Fatalf("reaped = %+v, want nothing: the failed run holds no active claim", reaped)
	}
	if got := terminalSiblingsOpenCount(t, ctx, db, runID); got != 0 {
		t.Fatalf("open items = %d, want 0: a failed run's items must stay frozen", got)
	}
	_, _, found, err := store.ClaimNextEligible(ctx, fixture.selector("claim-after-flip"),
		base.Add(terminalSiblingsLease+2*time.Hour), terminalSiblingsLease)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if found {
		t.Fatal("ClaimNextEligible() found an item of a failed run, want none")
	}
}

// terminalSiblingsDuplicateTargets counts targets holding more than one open
// item. It is the invariant the guard exists to keep: one target, one open item.
func terminalSiblingsDuplicateTargets(ctx context.Context, db *sql.DB, instanceID string) (int, error) {
	var duplicates int
	err := db.QueryRowContext(ctx, `
SELECT count(*) FROM (
    SELECT scope_id FROM workflow_work_items
    WHERE collector_instance_id = $1 AND status IN ('pending', 'claimed')
    GROUP BY scope_id
    HAVING count(*) > 1
) AS duplicated`, instanceID).Scan(&duplicates)
	return duplicates, err
}

// TestTerminalSiblingsNoDuplicateUnderConcurrentLifecycleLive races the whole
// lifecycle: workers claim and finish items (one target always fails
// terminally), two reconcilers tick as two coordinators would, and a planner
// keeps re-planning the same targets. After every admission no target may hold
// two open items. The early-failed rule breaks this on the first reconcile tick.
//
// Two reconcilers can write a stale collection_active over a fresh failed,
// because the status update has no predicate on the old status. That regress is
// conservative (the guard counts the run's items as open, and those items are
// already frozen) and the next tick corrects it. A reconcile that runs out of its
// bounded 40001/40P01 retries under this contention is tolerated and counted:
// the next tick repeats it.
func TestTerminalSiblingsNoDuplicateUnderConcurrentLifecycleLive(t *testing.T) {
	const (
		workers      = 4
		reconcilers  = 2
		targetCount  = 12
		wantFailures = 3
		workPause    = 5 * time.Millisecond
		tickPause    = 3 * time.Millisecond
	)
	db, store := openWorkflowControlIntegrationStore(t)
	postgresproof.RaisePoolForConcurrentClaimers(db, workers+reconcilers+2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	scopes := make([]string, 0, targetCount)
	for i := range targetCount {
		scopes = append(scopes, fmt.Sprintf("scope-%02d", i))
	}
	fixture := terminalSiblingsRun{instanceID: "terminal-siblings-race", scopes: scopes}
	if got := fixture.admit(t, ctx, store, "plan-0", time.Now().UTC()); got.InsertedWorkItems != targetCount {
		t.Fatalf("first admission = %+v, want %d inserted", got, targetCount)
	}

	// Fail the first claimed item before the race starts, so every sibling is
	// still pending on the first reconcile tick. That makes the RED on the
	// early-failed rule reliable rather than dependent on the claim rotation.
	first, firstClaim := fixture.claim(t, ctx, store, time.Now().UTC(), "claim-first")
	failingScope := first.ScopeID
	if err := store.FailClaimTerminal(ctx, terminalSiblingsFailure(
		terminalSiblingsMutation(first, firstClaim, time.Now().UTC()),
	)); err != nil {
		t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
	}

	raceCtx, stop := context.WithCancel(ctx)
	defer stop()
	report := func(format string, args ...any) {
		if raceCtx.Err() != nil {
			return
		}
		t.Errorf(format, args...)
		stop()
	}

	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for attempt := 0; raceCtx.Err() == nil; attempt++ {
				selector := fixture.selector(fmt.Sprintf("claim-%d-%d", worker, attempt))
				selector.OwnerID = fmt.Sprintf("worker-%d", worker)
				item, claim, found, err := store.ClaimNextEligible(raceCtx, selector, time.Now().UTC(), terminalSiblingsLease)
				if err != nil {
					report("worker %d: ClaimNextEligible() error = %v", worker, err)
					return
				}
				if !found {
					time.Sleep(workPause)
					continue
				}
				time.Sleep(workPause)
				mutation := terminalSiblingsMutation(item, claim, time.Now().UTC())
				if item.ScopeID == failingScope {
					err = store.FailClaimTerminal(raceCtx, terminalSiblingsFailure(mutation))
				} else {
					err = store.CompleteClaim(raceCtx, mutation)
				}
				if err != nil {
					report("worker %d: finish %s: %v", worker, item.WorkItemID, err)
					return
				}
			}
		}()
	}

	var exhausted sync.Map
	for reconciler := range reconcilers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for raceCtx.Err() == nil {
				_, err := store.ReconcileWorkflowRuns(raceCtx, time.Now().UTC())
				switch {
				case err == nil:
				case isRetryableWorkflowReconciliationError(err):
					exhausted.Store(reconciler, true)
				default:
					report("reconciler %d: ReconcileWorkflowRuns() error = %v", reconciler, err)
					return
				}
				time.Sleep(tickPause)
			}
		}()
	}

	planner := make(chan error, 1)
	go func() {
		for plan := 1; ; plan++ {
			if raceCtx.Err() != nil {
				planner <- raceCtx.Err()
				return
			}
			run, items := fixture.plan(fmt.Sprintf("plan-%d", plan), time.Now().UTC())
			if _, err := store.CreateRunWithWorkItemsIfNoOpenTargets(raceCtx, run, items); err != nil {
				planner <- fmt.Errorf("plan %d: %w", plan, err)
				return
			}
			duplicates, err := terminalSiblingsDuplicateTargets(raceCtx, db, fixture.instanceID)
			if err != nil {
				planner <- fmt.Errorf("plan %d: count duplicates: %w", plan, err)
				return
			}
			if duplicates > 0 {
				planner <- fmt.Errorf("plan %d: %d targets hold two open items", plan, duplicates)
				return
			}
			var failed int
			if err := db.QueryRowContext(raceCtx,
				`SELECT count(*) FROM workflow_runs WHERE status = 'failed' AND run_id LIKE $1 || '%'`,
				fixture.instanceID).Scan(&failed); err != nil {
				planner <- fmt.Errorf("plan %d: count failed runs: %w", plan, err)
				return
			}
			if failed >= wantFailures {
				planner <- nil
				return
			}
			time.Sleep(tickPause)
		}
	}()

	plannerErr := <-planner
	stop()
	wg.Wait()
	if plannerErr != nil && !errors.Is(plannerErr, context.Canceled) {
		t.Fatalf("planner: %v", plannerErr)
	}
	if ctx.Err() != nil {
		t.Fatalf("race did not reach %d failed runs before its deadline: %v", wantFailures, ctx.Err())
	}
	exhausted.Range(func(key, _ any) bool {
		t.Logf("reconciler %v exhausted its bounded retry at least once; the next tick repeated it", key)
		return true
	})
}

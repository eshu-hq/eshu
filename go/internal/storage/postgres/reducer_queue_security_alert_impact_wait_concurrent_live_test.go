// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8827 disposable PostgreSQL 18 contention proof that six workers on independent connections draining twelve scopes that each hold a pending reconciliation (the older row) and a pending supply_chain_impact row never run the two domains of one scope at once, defer every reconciliation that runs first without spending an attempt, and finish with every reconciliation matched at attempt_count 1 and every producer succeeded; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local run about 4s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// TestReducerContentionGateSecurityAlertReconciliationImpactFenceUnderContention
// drains a backlog with concurrent workers. Each worker holds its own
// connection so the claim statements truly interleave. The invariants:
//
//   - one scope never has its reconciliation and its producer in flight at once
//     (the existing (scope, scope id) fence still holds under the new class);
//   - a reconciliation that runs before its producer defers without spending
//     an attempt, so every reconciliation ends matched at attempt_count 1;
//   - every row reaches succeeded: nothing is stranded, nothing dead-letters.
//
// The wall clock only bounds the test as a hang guard; it is never asserted.
func TestReducerContentionGateSecurityAlertReconciliationImpactFenceUnderContention(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	const scopes, workers = 12, 6
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)

	loaders := make(map[string]*impactWaitLoader, scopes)
	for i := 0; i < scopes; i++ {
		scope := fmt.Sprintf("scope-8827-c%02d", i)
		seedImpactWaitScope(t, ctx, db, scope, fmt.Sprintf("gen-8827-c%02d", i),
			fmt.Sprintf("recon-8827-c%02d", i), fmt.Sprintf("impact-8827-c%02d", i))
		loaders[scope] = newImpactWaitLoader()
	}
	writer := &impactWaitWriter{}

	var (
		mu        sync.Mutex
		inFlight  = map[string]int{}
		overlaps  atomic.Int64
		deferrals atomic.Int64
		remaining atomic.Int64
	)
	remaining.Store(scopes * 2)
	enter := func(scope string) {
		mu.Lock()
		defer mu.Unlock()
		inFlight[scope]++
		if inFlight[scope] > 1 {
			overlaps.Add(1)
		}
	}
	leave := func(scope string) {
		mu.Lock()
		defer mu.Unlock()
		inFlight[scope]--
	}

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		workerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
		queue := ReducerQueue{
			database: SQLDB{DB: workerDB}, LeaseOwner: fmt.Sprintf("worker-%d", w), LeaseDuration: time.Minute,
			RetryDelay: 20 * time.Millisecond, MaxAttempts: 3, Now: func() time.Time { return time.Now().UTC() },
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for remaining.Load() > 0 && ctx.Err() == nil {
				intent, claimed, err := queue.Claim(ctx)
				if err != nil {
					errs <- fmt.Errorf("claim: %w", err)
					return
				}
				if !claimed {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				enter(intent.ScopeID)
				var runErr error
				switch intent.Domain {
				case reducer.DomainSupplyChainImpact:
					time.Sleep(15 * time.Millisecond) // the producer's work
					loaders[intent.ScopeID].writeImpact(intent.ScopeID)
					runErr = queue.Ack(ctx, intent, reducercontract.Result{})
					if runErr == nil {
						remaining.Add(-1)
					}
				case reducer.DomainSecurityAlertReconciliation:
					handler := impactWaitHandler(workerDB, loaders[intent.ScopeID], writer, true, nil)
					result, handleErr := handler.Handle(ctx, intent)
					if handleErr != nil {
						if failureClassName(handleErr) != securityalert.ImpactNotReadyFailureClass {
							runErr = fmt.Errorf("handle %s: unexpected error %w", intent.ScopeID, handleErr)
						} else {
							deferrals.Add(1)
							runErr = queue.Fail(ctx, intent, handleErr)
						}
					} else if runErr = queue.Ack(ctx, intent, result); runErr == nil {
						remaining.Add(-1)
					}
				default:
					runErr = fmt.Errorf("unexpected domain %s", intent.Domain)
				}
				leave(intent.ScopeID)
				if runErr != nil {
					errs <- runErr
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if remaining.Load() != 0 {
		t.Fatalf("%d rows not drained before the hang guard fired", remaining.Load())
	}
	if overlaps.Load() != 0 {
		t.Fatalf("%d overlaps: one scope had its reconciliation and producer in flight together", overlaps.Load())
	}
	if deferrals.Load() == 0 {
		t.Fatal("no reconciliation ran before its producer: the scenario was not exercised")
	}
	calls, statuses := writer.snapshot()
	if calls != scopes || len(statuses) != scopes {
		t.Fatalf("writes = %d decisions = %d, want %d each", calls, len(statuses), scopes)
	}
	for _, status := range statuses {
		if status != securityalert.SecurityAlertReconciliationMatched {
			t.Fatalf("decision status = %s, want matched: a reconciliation gave up before its producer finished", status)
		}
	}
	var badRecon, badImpact int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE domain = 'security_alert_reconciliation' AND (status <> 'succeeded' OR attempt_count <> 1)),
       count(*) FILTER (WHERE domain = 'supply_chain_impact' AND (status <> 'succeeded' OR attempt_count <> 1))
FROM fact_work_items WHERE work_item_id LIKE '%-8827-c%'`).Scan(&badRecon, &badImpact); err != nil {
		t.Fatalf("read terminal state: %v", err)
	}
	if badRecon != 0 || badImpact != 0 {
		t.Fatalf("rows not succeeded at attempt 1: reconciliations=%d producers=%d", badRecon, badImpact)
	}
	t.Logf("drained %d scopes with %d workers; %d non-counting deferrals, 0 overlaps", scopes, workers, deferrals.Load())
}

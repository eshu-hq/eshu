// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Real-queue proof for the #7268 shared-edge target deferral on
// deployable_unit_correlation.
//
// The fake-queue tests pin the retry UPDATE and the claim CASE text; only real
// PostgreSQL shows the reclaim itself keeps attempt_count. Everything on the
// queue side is production code against the real fact_work_items DDL: the real
// ReducerQueue Claim/Fail SQL, and the real error the shared-edge writer
// returns for an absent target (sharedEdgeTargetMissCause drives
// writer.EdgeWriter.WriteEdges and wraps it as the handler does).
//
// The TestReducerContentionGate prefix puts this in the reducer contention
// gate's -run filter (.github/workflows/reducer-contention-gate.yml), which runs
// a real PostgreSQL service on every PR touching the reducer or this package.

// sharedEdgeBoundExpiredStandIn has the queue-facing contract of the reducer's
// sharedEdgeTargetWaitExceededError: retryable, no failure class, no Unwrap.
// That type is unexported in package reducer; the reducer's own
// TestDeployableUnitCorrelationTargetDeferralIsBoundedByElapsedTime proves the
// handler returns exactly this contract past the 30-minute bound, and this
// stand-in proves what the real queue does with it.
type sharedEdgeBoundExpiredStandIn struct{}

func (sharedEdgeBoundExpiredStandIn) Error() string {
	return "shared edge target still absent after 31m0s elapsed since the repair cycle began (bound 30m0s)"
}
func (sharedEdgeBoundExpiredStandIn) Retryable() bool { return true }

// TestReducerContentionGateSharedEdgeTargetDeferralKeepsItsAttemptBudget
// drives one deployable_unit_correlation row through the real queue.
//
//  1. More claim/fail cycles than MaxAttempts with the real target-miss error:
//     the row stays 'retrying' under shared_edge_target_not_ready and every
//     reclaim hands it back with attempt_count still 1. Under the old counting
//     behaviour the row dead-lettered on cycle three.
//  2. The elapsed bound trips: the handler now returns a classless retryable
//     error. The next reclaims count, and the row dead-letters at
//     MaxAttempts, so a target that never appears still fails loudly.
func TestReducerContentionGateSharedEdgeTargetDeferralKeepsItsAttemptBudget(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)
	const scopeID = "repository:acme/edge-api"
	enqueuedAt := time.Now().UTC().Add(-time.Minute)
	seedReducerFairnessScope(t, ctx, db, scopeID, enqueuedAt)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "deployable-unit-correlation-7268",
		scopeID:        scopeID,
		generationID:   "gen-fair",
		domain:         string(reducer.DomainDeployableUnitCorrelation),
		conflictDomain: "scope",
		conflictKey:    scopeID,
		sourceSystem:   "git",
		updatedAt:      enqueuedAt,
	})

	clock := time.Now().UTC()
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "shared-edge-target-proof",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Second,
		MaxRetryDelay: 2 * time.Second,
		MaxAttempts:   3,
		ClaimDomains:  []reducer.Domain{reducer.DomainDeployableUnitCorrelation},
		Now:           func() time.Time { return clock },
	}
	claim := func(phase string, cycle int) reducer.Intent {
		t.Helper()
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("%s cycle %d: Claim() error = %v", phase, cycle, err)
		}
		if !claimed {
			t.Fatalf("%s cycle %d: Claim() claimed = false, want the retrying row back", phase, cycle)
		}
		return intent
	}

	const deferCycles = 5
	for cycle := 1; cycle <= deferCycles; cycle++ {
		intent := claim("defer", cycle)
		if intent.AttemptCount != 1 {
			t.Fatalf("defer cycle %d: claimed AttemptCount = %d, want 1 frozen: %s is non-counting",
				cycle, intent.AttemptCount, reducer.SharedEdgeTargetNotReadyFailureClass)
		}
		if err := queue.Fail(ctx, intent, sharedEdgeTargetMissCause(t)); err != nil {
			t.Fatalf("defer cycle %d: Fail() error = %v", cycle, err)
		}
		status, failureClass, attemptCount := readSharedEdgeTargetProofWorkItem(t, ctx, db)
		if status != "retrying" || failureClass != reducer.SharedEdgeTargetNotReadyFailureClass || attemptCount != 1 {
			t.Fatalf("defer cycle %d: status=%q failure_class=%q attempt_count=%d, want retrying/%s/1",
				cycle, status, failureClass, attemptCount, reducer.SharedEdgeTargetNotReadyFailureClass)
		}
		clock = clock.Add(5 * time.Second)
	}

	// The bound trips. Attempts now count: 1 (frozen) -> 2 -> 3 -> dead letter.
	for cycle, wantAttempt := range []int{1, 2, 3} {
		intent := claim("bounded", cycle+1)
		if intent.AttemptCount != wantAttempt {
			t.Fatalf("bounded cycle %d: claimed AttemptCount = %d, want %d: the classless error must count",
				cycle+1, intent.AttemptCount, wantAttempt)
		}
		if err := queue.Fail(ctx, intent, sharedEdgeBoundExpiredStandIn{}); err != nil {
			t.Fatalf("bounded cycle %d: Fail() error = %v", cycle+1, err)
		}
		clock = clock.Add(5 * time.Second)
	}
	status, failureClass, attemptCount := readSharedEdgeTargetProofWorkItem(t, ctx, db)
	if status != "dead_letter" || attemptCount != 3 {
		t.Fatalf("terminal status=%q failure_class=%q attempt_count=%d, want dead_letter at 3: a target that never appears must fail loudly",
			status, failureClass, attemptCount)
	}
	if failureClass == reducer.SharedEdgeTargetNotReadyFailureClass {
		t.Fatalf("dead letter kept failure_class %q, want the counting triage class", failureClass)
	}
}

// readSharedEdgeTargetProofWorkItem reads the durable row back: the queue's
// return values and the table's contents are separate claims.
func readSharedEdgeTargetProofWorkItem(t *testing.T, ctx context.Context, db *sql.DB) (string, string, int) {
	t.Helper()

	var (
		status       string
		failureClass sql.NullString
		attemptCount int
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, failure_class, attempt_count
FROM fact_work_items
WHERE stage = 'reducer' AND domain = $1`,
		string(reducer.DomainDeployableUnitCorrelation),
	).Scan(&status, &failureClass, &attemptCount); err != nil {
		t.Fatalf("read deployable_unit_correlation work item: %v", err)
	}
	return status, failureClass.String, attemptCount
}

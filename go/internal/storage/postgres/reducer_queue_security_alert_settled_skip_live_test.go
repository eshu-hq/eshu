// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9053 disposable PostgreSQL 18 proof, on the real ReducerQueue claim/fail/ack SQL, the real readiness/producer.ImpactStore, and the real SecurityAlertReconciliationHandler, that a reconciliation whose same-generation supply_chain_impact work already succeeded writes its genuine no-match on the first attempt (one claim, terminal succeeded at attempt 1, no impact_wait_outcome), and that a reconciliation whose producer finishes late still waits without spending an attempt and matches once the finding lands; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local run about 3s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// Real-queue convergence proof for the #9053 settled skip, in both
// directions. The queue, the readiness store, and the handler are production
// code against real PostgreSQL; the fact loader and the manifest bridge are
// the same stand-ins as the #8827 fence proof in
// reducer_queue_security_alert_impact_wait_live_test.go. No SQL, index, or
// queue state machine changes: both directions exercise the shipped claim,
// fail, ack, and ImpactStore statements and assert attempt accounting only.

// TestReducerContentionGateSecurityAlertReconciliationSettledSkipsTheWait
// proves a settled producer finishes in one attempt: with the impact row
// already succeeded, the first reconciliation claim writes unmatched with no
// impact_wait_outcome and acks at attempt 1.
func TestReducerContentionGateSecurityAlertReconciliationSettledSkipsTheWait(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	scope, generation := "scope-9053-settled", "gen-9053-settled"
	reconID, impactID := "recon-9053-settled", "impact-9053-settled"
	seedImpactWaitScope(t, ctx, db, scope, generation, reconID, impactID)
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, impactID); err != nil {
		t.Fatalf("set producer succeeded: %v", err)
	}

	clock := time.Now().UTC()
	now := func() time.Time { return clock }
	queue := runtimeFenceQueue(db, "recon-worker", reducer.DomainSecurityAlertReconciliation, now)
	loader, writer := newImpactWaitLoader(), &impactWaitWriter{}
	handler := impactWaitHandler(db, loader, writer, true, now)

	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("Claim() claimed=%v err=%v, want the settled reconciliation at once", claimed, err)
	}
	if intent.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1: a settled producer writes on the first run", intent.AttemptCount)
	}
	result, handleErr := handler.Handle(ctx, intent)
	if handleErr != nil {
		t.Fatalf("Handle() error = %v, want the genuine no-match write instead of a counting defer", handleErr)
	}
	if strings.Contains(result.EvidenceSummary, "impact_wait=") {
		t.Fatalf("summary %q names a give-up for a settled producer", result.EvidenceSummary)
	}
	if err := queue.Ack(ctx, intent, result); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	calls, statuses := writer.snapshot()
	if calls != 1 || len(statuses) != 1 || statuses[0] != securityalert.SecurityAlertReconciliationUnmatched {
		t.Fatalf("writes = %d statuses = %v, want exactly one unmatched decision", calls, statuses)
	}
	if outcomes := writer.outcomeSnapshot(); len(outcomes) != 1 || outcomes[0] != "" {
		t.Fatalf("write impact wait outcomes = %q, want one empty genuine no-match", outcomes)
	}
	if status, _, attempts := readRuntimeFenceItem(t, ctx, db, reconID); status != "succeeded" || attempts != 1 {
		t.Fatalf("reconciliation terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// TestReducerContentionGateSecurityAlertReconciliationLateProducerKeepsItsWait
// proves the other direction: while the producer is in flight the
// reconciliation defers without spending an attempt, and once the producer
// writes its finding and acks, the next claim matches at attempt 1.
func TestReducerContentionGateSecurityAlertReconciliationLateProducerKeepsItsWait(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	scope, generation := "scope-9053-late", "gen-9053-late"
	reconID, impactID := "recon-9053-late", "impact-9053-late"
	seedImpactWaitScope(t, ctx, db, scope, generation, reconID, impactID)

	clock := time.Now().UTC()
	now := func() time.Time { return clock }
	reconQueue := runtimeFenceQueue(db, "recon-worker", reducer.DomainSecurityAlertReconciliation, now)
	impactQueue := runtimeFenceQueue(db, "impact-worker", reducer.DomainSupplyChainImpact, now)
	loader, writer := newImpactWaitLoader(), &impactWaitWriter{}
	handler := impactWaitHandler(db, loader, writer, true, now)

	for cycle := 1; cycle <= 2; cycle++ {
		intent, claimed, err := reconQueue.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("cycle %d: Claim() claimed=%v err=%v, want the deferred row back", cycle, claimed, err)
		}
		if intent.AttemptCount != 1 {
			t.Fatalf("cycle %d: AttemptCount = %d, want 1 frozen under %s", cycle, intent.AttemptCount, securityalert.ImpactNotReadyFailureClass)
		}
		_, handleErr := handler.Handle(ctx, intent)
		if got := failureClassName(handleErr); got != securityalert.ImpactNotReadyFailureClass {
			t.Fatalf("cycle %d: Handle() error = %v (class %q), want the impact-not-ready deferral", cycle, handleErr, got)
		}
		if err := reconQueue.Fail(ctx, intent, handleErr); err != nil {
			t.Fatalf("cycle %d: Fail() error = %v", cycle, err)
		}
		if status, _, attempts := readRuntimeFenceItem(t, ctx, db, reconID); status != "retrying" || attempts != 1 {
			t.Fatalf("cycle %d: reconciliation row = %s/attempts=%d, want retrying/1", cycle, status, attempts)
		}
		clock = clock.Add(10 * time.Second)
	}

	impactIntent, claimed, err := impactQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("impact Claim() = %v, %v", claimed, err)
	}
	loader.writeImpact(scope)
	if err := impactQueue.Ack(ctx, impactIntent, reducercontract.Result{}); err != nil {
		t.Fatalf("impact Ack() error = %v", err)
	}
	clock = clock.Add(10 * time.Second)
	intent, claimed, err := reconQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("post-producer Claim() = %v, %v", claimed, err)
	}
	result, handleErr := handler.Handle(ctx, intent)
	if handleErr != nil {
		t.Fatalf("post-producer Handle() error = %v, want the match once the finding exists", handleErr)
	}
	if strings.Contains(result.EvidenceSummary, "impact_wait=") {
		t.Fatalf("summary %q names a give-up although the producer finished first", result.EvidenceSummary)
	}
	if err := reconQueue.Ack(ctx, intent, result); err != nil {
		t.Fatalf("reconciliation Ack() error = %v", err)
	}
	calls, statuses := writer.snapshot()
	if calls != 1 || len(statuses) != 1 || statuses[0] != securityalert.SecurityAlertReconciliationMatched {
		t.Fatalf("writes = %d statuses = %v, want exactly one matched decision", calls, statuses)
	}
	if status, _, attempts := readRuntimeFenceItem(t, ctx, db, reconID); status != "succeeded" || attempts != 1 {
		t.Fatalf("reconciliation terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8827 disposable PostgreSQL 18 proof, on the real ReducerQueue claim/fail/ack SQL, the real readiness/producer.ImpactStore, and the real SecurityAlertReconciliationHandler, that a reconciliation waiting on its own scope generation's supply_chain_impact work keeps attempt_count frozen at 1 through more cycles than MaxAttempts and past the handler's own cap, is fenced while the producer is claimed, matches once the producer acks, and that without the fence or after the producer dead-letters it still gives up visibly after three counting attempts; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local run about 3s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// Real-queue proofs for the #8827 reconciliation impact fence. The queue, the
// readiness store, and the handler are production code against real
// PostgreSQL. Two things are stand-ins: the fact loader (it returns the impact
// finding only after the producer "writes" it, the order the production
// supply_chain_impact handler has: write, then Ack) and the manifest bridge
// (the reducer-root extractor cannot be imported from here, so a fixed
// consumption supplies the dependency evidence).
//
// The TestReducerContentionGate prefix puts these in the reducer contention
// gate's -run filter (.github/workflows/reducer-contention-gate.yml).

const (
	impactWaitRepo    = "repo://github/acme/api"
	impactWaitPackage = "npm://registry.npmjs.org/pending-provider-lib"
)

type impactWaitLoader struct {
	mu            sync.Mutex
	impactWritten map[string]bool
}

func newImpactWaitLoader() *impactWaitLoader {
	return &impactWaitLoader{impactWritten: map[string]bool{}}
}

func (l *impactWaitLoader) writeImpact(scopeID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.impactWritten[scopeID] = true
}

func (l *impactWaitLoader) alerts() []facts.Envelope {
	return []facts.Envelope{{
		FactID: "alert-8827", ScopeID: impactWaitRepo, GenerationID: "generation-1",
		FactKind: facts.SecurityAlertRepositoryAlertFactKind, SchemaVersion: facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": impactWaitRepo, "provider": "github_dependabot", "provider_alert_number": int64(15),
			"provider_state": "open", "package_id": impactWaitPackage, "package_name": "pending-provider-lib",
			"ecosystem": "npm", "manifest_path": "package-lock.json",
			"cve_ids": []string{"CVE-2026-9680"}, "ghsa_ids": []string{"GHSA-synthetic-9680"},
			"vulnerable_range": "<8.9.1", "patched_version": "8.9.1",
		},
	}}
}

func (l *impactWaitLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.alerts(), nil
}

func (l *impactWaitLoader) ListFactsByKind(context.Context, string, string, []string) ([]facts.Envelope, error) {
	return l.alerts(), nil
}

// ListActiveSecurityAlertReconciliationFacts returns the impact finding only
// once the scope's producer wrote it. The handler passes no scope, so the
// loader reports for any scope whose producer wrote.
func (l *impactWaitLoader) ListActiveSecurityAlertReconciliationFacts(
	context.Context,
	securityalert.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.impactWritten) == 0 {
		return nil, nil
	}
	return []facts.Envelope{{
		FactID: "impact-8827", ScopeID: impactWaitRepo, GenerationID: "generation-1",
		FactKind: facts.ReducerSupplyChainImpactFindingFactKind, ObservedAt: time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": impactWaitRepo, "package_id": impactWaitPackage, "cve_id": "CVE-2026-9680",
			"advisory_id": "GHSA-synthetic-9680", "impact_status": "affected_exact",
		},
	}}, nil
}

func impactWaitConsumptions(
	[]securityalert.ProviderSecurityAlert,
	[]facts.Envelope,
) []securityalert.SecurityAlertConsumption {
	return []securityalert.SecurityAlertConsumption{{
		FactID: "consume-8827", EvidenceKind: "manifest", RepositoryID: impactWaitRepo, PackageID: impactWaitPackage,
		RelativePath: "package-lock.json", ObservedVersion: "8.9.0", ObservedAt: time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
	}}
}

type impactWaitWriter struct {
	mu        sync.Mutex
	calls     int
	statuses  []securityalert.SecurityAlertReconciliationStatus
	intentIDs []string
	outcomes  []string
}

func (w *impactWaitWriter) WriteSecurityAlertReconciliations(
	_ context.Context,
	write securityalert.SecurityAlertReconciliationWrite,
) (securityalert.SecurityAlertReconciliationWriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.intentIDs = append(w.intentIDs, write.IntentID)
	w.outcomes = append(w.outcomes, write.ImpactWaitOutcome)
	for _, decision := range write.Decisions {
		w.statuses = append(w.statuses, decision.Status)
	}
	return securityalert.SecurityAlertReconciliationWriteResult{CanonicalWrites: len(write.Decisions)}, nil
}

func (w *impactWaitWriter) snapshot() (int, []securityalert.SecurityAlertReconciliationStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls, append([]securityalert.SecurityAlertReconciliationStatus(nil), w.statuses...)
}

// outcomeSnapshot returns the ImpactWaitOutcome of every write (#8956).
func (w *impactWaitWriter) outcomeSnapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.outcomes...)
}

// impactWaitRetryable is a transient failure the producer's queue retries on
// the counting path.
type impactWaitRetryable struct{}

func (impactWaitRetryable) Error() string   { return "transient producer failure" }
func (impactWaitRetryable) Retryable() bool { return true }

func impactWaitHandler(
	db *sql.DB,
	loader *impactWaitLoader,
	writer *impactWaitWriter,
	fenced bool,
	now func() time.Time,
) securityalert.SecurityAlertReconciliationHandler {
	handler := securityalert.SecurityAlertReconciliationHandler{
		FactLoader: loader, Writer: writer, ExtractManifestConsumptions: impactWaitConsumptions, Now: now,
	}
	if fenced {
		handler.ImpactProducerReadiness = producerreadiness.ImpactStore{DB: SQLDB{DB: db}}
	}
	return handler
}

// seedImpactWaitScope registers one scope generation holding the pending
// reconciliation row and the pending supply_chain_impact row of the same
// projection, the shape the projector enqueues.
func seedImpactWaitScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID, reconID, impactID string) {
	t.Helper()
	seedImpactProducerScope(t, ctx, db, scopeID, generationID)
	now := time.Now().UTC()
	// The reconciliation is the older row, so the queue offers it first: the
	// order in which the burn happened.
	for _, item := range []struct {
		id, domain string
		age        time.Duration
	}{
		{impactID, string(reducer.DomainSupplyChainImpact), time.Minute},
		{reconID, string(reducer.DomainSecurityAlertReconciliation), 2 * time.Minute},
	} {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: item.id, scopeID: scopeID, generationID: generationID, domain: item.domain,
			conflictDomain: "scope", conflictKey: scopeID, sourceSystem: "security_alert", updatedAt: now.Add(-item.age),
		})
	}
}

func failureClassName(err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}

// TestReducerContentionGateSecurityAlertReconciliationImpactFenceKeepsItsAttemptBudget
// is the #8827 regression on the real queue. Reconciliation is claimed FIRST
// while the impact row is pending, then while it is claimed (the reconciliation
// is fenced out, not burned), then while it is retrying on the counting path.
// Across five reconciliation cycles, more than MaxAttempts and past the
// handler's own cap of three, attempt_count stays frozen at 1 and nothing is
// written. Once the producer writes its finding and acks, the next claim
// matches the alert and acks at attempt 1.
func TestReducerContentionGateSecurityAlertReconciliationImpactFenceKeepsItsAttemptBudget(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	scope, generation := "scope-8827-fence", "gen-8827-fence"
	seedImpactWaitScope(t, ctx, db, scope, generation, "recon-8827-fence", "impact-8827-fence")

	clock := time.Now().UTC()
	now := func() time.Time { return clock }
	reconQueue := runtimeFenceQueue(db, "recon-worker", reducer.DomainSecurityAlertReconciliation, now)
	impactQueue := runtimeFenceQueue(db, "impact-worker", reducer.DomainSupplyChainImpact, now)
	loader, writer := newImpactWaitLoader(), &impactWaitWriter{}
	handler := impactWaitHandler(db, loader, writer, true, now)

	var impactIntent reducercontract.Intent
	for cycle := 1; cycle <= 5; cycle++ {
		switch cycle {
		case 2:
			// The producer is claimed: the reconciliation must be fenced out of
			// the claim entirely, not claimed and burned.
			intent, claimed, err := impactQueue.Claim(ctx)
			if err != nil || !claimed {
				t.Fatalf("cycle %d: impact Claim() = %v, %v", cycle, claimed, err)
			}
			impactIntent = intent
			if _, fenced, err := reconQueue.Claim(ctx); err != nil || fenced {
				t.Fatalf("cycle %d: reconciliation Claim() claimed=%v err=%v while the producer is claimed, want fenced out", cycle, fenced, err)
			}
			// The producer fails transiently and goes to counting retry.
			if err := impactQueue.Fail(ctx, impactIntent, impactWaitRetryable{}); err != nil {
				t.Fatalf("cycle %d: impact Fail() error = %v", cycle, err)
			}
			clock = clock.Add(10 * time.Second)
		}
		intent, claimed, err := reconQueue.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("cycle %d: reconciliation Claim() claimed=%v err=%v, want the deferred row back", cycle, claimed, err)
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
		status, class, attempts := readRuntimeFenceItem(t, ctx, db, "recon-8827-fence")
		if status != "retrying" || class != securityalert.ImpactNotReadyFailureClass || attempts != 1 {
			t.Fatalf("cycle %d: reconciliation row = %s/%s/attempts=%d, want retrying/%s/1", cycle, status, class, attempts, securityalert.ImpactNotReadyFailureClass)
		}
		if calls, _ := writer.snapshot(); calls != 0 {
			t.Fatalf("cycle %d: writer calls = %d, want 0 while the producer is in flight", cycle, calls)
		}
		clock = clock.Add(10 * time.Second)
	}

	// The producer finishes: claim it through the real queue, write the
	// finding, then ack (production order).
	impactIntent, claimed, err := impactQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("final impact Claim() = %v, %v", claimed, err)
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
	result, err := handler.Handle(ctx, intent)
	if err != nil {
		t.Fatalf("post-producer Handle() error = %v, want the match once the finding exists", err)
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
	if status, _, attempts := readRuntimeFenceItem(t, ctx, db, "recon-8827-fence"); status != "succeeded" || attempts != 1 {
		t.Fatalf("reconciliation terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// TestReducerContentionGateSecurityAlertReconciliationGivesUpVisiblyWithoutAFence
// pins the counting path on the real queue, the behavior #8827 describes and
// the one that remains when no producer is in flight. With no fence wired and
// the producer still pending (the pre-fix behavior), and with the fence wired
// but the producer dead-lettered (nothing left to wait for), the
// reconciliation spends attempts 1 and 2 on counting deferrals and on attempt 3
// writes unmatched, now naming the give-up on the result. With the fence wired
// and the producer succeeded (#8956, #9053) the first attempt writes the
// genuine no-match at once: unmatched, but no give-up on the result or the
// write, terminal succeeded at attempt 1.
func TestReducerContentionGateSecurityAlertReconciliationGivesUpVisiblyWithoutAFence(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	cases := []struct {
		name         string
		fenced       bool
		impactStatus string
		wantOutcome  string
		// settledFirst marks the case the #9053 skip finishes on the first
		// attempt instead of the counting path.
		settledFirst bool
	}{
		{"no fence, producer pending (pre-fix behavior)", false, "pending", securityalert.ImpactWaitGaveUpUnmatched, false},
		{"fence wired, producer dead-lettered", true, "dead_letter", securityalert.ImpactWaitGaveUpUnmatched, false},
		{"fence wired, producer succeeded (genuine no-match)", true, "succeeded", "", true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			scope, generation := "scope-8827-giveup", "gen-8827-giveup"
			reconID, impactID := "recon-8827-giveup", "impact-8827-giveup"
			seedImpactWaitScope(t, ctx, db, scope, generation, reconID, impactID)
			if tc.impactStatus != "pending" {
				if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = $2, failure_class = 'terminal',
				        failure_message = 'dead' WHERE work_item_id = $1`, impactID, tc.impactStatus); err != nil {
					t.Fatalf("case %d: set producer %s: %v", i, tc.impactStatus, err)
				}
			}
			clock := time.Now().UTC()
			now := func() time.Time { return clock }
			queue := runtimeFenceQueue(db, "recon-worker", reducer.DomainSecurityAlertReconciliation, now)
			loader, writer := newImpactWaitLoader(), &impactWaitWriter{}
			handler := impactWaitHandler(db, loader, writer, tc.fenced, now)

			if tc.settledFirst {
				intent, claimed, err := queue.Claim(ctx)
				if err != nil || !claimed {
					t.Fatalf("settled Claim() claimed=%v err=%v, want the write on the first attempt", claimed, err)
				}
				if intent.AttemptCount != 1 {
					t.Fatalf("settled AttemptCount = %d, want 1", intent.AttemptCount)
				}
				result, handleErr := handler.Handle(ctx, intent)
				if handleErr != nil {
					t.Fatalf("settled Handle() error = %v, want the genuine no-match write", handleErr)
				}
				if strings.Contains(result.EvidenceSummary, "impact_wait=") {
					t.Fatalf("settled summary %q names a give-up for a settled producer", result.EvidenceSummary)
				}
				if err := queue.Ack(ctx, intent, result); err != nil {
					t.Fatalf("settled Ack() error = %v", err)
				}
				calls, statuses := writer.snapshot()
				if calls != 1 || len(statuses) != 1 || statuses[0] != securityalert.SecurityAlertReconciliationUnmatched {
					t.Fatalf("settled writes = %d statuses = %v, want one unmatched decision", calls, statuses)
				}
				if outcomes := writer.outcomeSnapshot(); len(outcomes) != 1 || outcomes[0] != "" {
					t.Fatalf("settled write impact wait outcomes = %q, want one empty genuine no-match", outcomes)
				}
				if status, _, attempts := readRuntimeFenceItem(t, ctx, db, reconID); status != "succeeded" || attempts != 1 {
					t.Fatalf("settled reconciliation terminal = %s attempts=%d, want succeeded at 1", status, attempts)
				}
				return
			}
			for attempt := 1; attempt <= 3; attempt++ {
				intent, claimed, err := queue.Claim(ctx)
				if err != nil || !claimed {
					t.Fatalf("attempt %d: Claim() claimed=%v err=%v", attempt, claimed, err)
				}
				if intent.AttemptCount != attempt {
					t.Fatalf("attempt %d: AttemptCount = %d, want %d on the counting path", attempt, intent.AttemptCount, attempt)
				}
				result, handleErr := handler.Handle(ctx, intent)
				if attempt < 3 {
					if got := failureClassName(handleErr); got != "security_alert_reconciliation_waiting_for_impact" {
						t.Fatalf("attempt %d: Handle() error = %v (class %q), want the counting deferral", attempt, handleErr, got)
					}
					if err := queue.Fail(ctx, intent, handleErr); err != nil {
						t.Fatalf("attempt %d: Fail() error = %v", attempt, err)
					}
					clock = clock.Add(time.Hour)
					continue
				}
				if handleErr != nil {
					t.Fatalf("attempt 3: Handle() error = %v, want the give-up write", handleErr)
				}
				if tc.wantOutcome != "" && !strings.Contains(result.EvidenceSummary, "impact_wait="+tc.wantOutcome) {
					t.Fatalf("attempt 3: summary %q must name the %s reason", result.EvidenceSummary, tc.wantOutcome)
				}
				if tc.wantOutcome == "" && strings.Contains(result.EvidenceSummary, "impact_wait=") {
					t.Fatalf("attempt 3: summary %q names a give-up for a settled producer", result.EvidenceSummary)
				}
				if err := queue.Ack(ctx, intent, result); err != nil {
					t.Fatalf("attempt 3: Ack() error = %v", err)
				}
			}
			calls, statuses := writer.snapshot()
			if calls != 1 || len(statuses) != 1 || statuses[0] != securityalert.SecurityAlertReconciliationUnmatched {
				t.Fatalf("writes = %d statuses = %v, want one unmatched decision", calls, statuses)
			}
			if outcomes := writer.outcomeSnapshot(); len(outcomes) != 1 || outcomes[0] != tc.wantOutcome {
				t.Fatalf("write impact wait outcomes = %q, want [%q]", outcomes, tc.wantOutcome)
			}
			if status, _, attempts := readRuntimeFenceItem(t, ctx, db, reconID); status != "succeeded" || attempts != 3 {
				t.Fatalf("reconciliation terminal = %s attempts=%d, want succeeded at 3", status, attempts)
			}
		})
	}
}

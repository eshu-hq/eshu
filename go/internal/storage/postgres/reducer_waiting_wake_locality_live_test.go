// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11114 producer-terminal wake locality: a foreign scope's supply_chain_impact Ack leaves a same-scope reconciliation wait parked while its own scope's Ack reopens it, and a catalog runtime wait (cross-scope provider) still reopens on a workload_materialization Ack in a different repository scope; disposable database per test, about 10 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// impactNotReadyCause self-classifies as the #8827 reconciliation impact
// fence miss, parking through the narrowed same-scope producer row.
type impactNotReadyCause struct{}

func (impactNotReadyCause) Error() string   { return "security alert reconciliation impact not ready" }
func (impactNotReadyCause) Retryable() bool { return true }
func (impactNotReadyCause) FailureClass() string {
	return securityalert.ImpactNotReadyFailureClass
}

// parkNarrowedWaiter claims one waiter row of the given domain and fails it
// with a self-classifying narrowed cause, as the handler deferral does. It
// returns the parked row state and fails the test unless exactly one wait
// row exists.
func parkNarrowedWaiter(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	clock *time.Time,
	id string,
	domain reducercontract.Domain,
	cause error,
) waitingRowState {
	t.Helper()
	queue, intent := claimWaitingRow(t, ctx, db, clock, domain)
	if intent.IntentID != id {
		t.Fatalf("claimed %q, want %q", intent.IntentID, id)
	}
	if err := queue.Fail(ctx, intent, cause); err != nil {
		t.Fatalf("fail %s into wait: %v", id, err)
	}
	state := readWaitingRow(t, ctx, db, id)
	if state.status != "retrying" || state.attempts != 1 || state.waitRows != 1 {
		t.Fatalf("%s parked = %+v, want retrying at attempt 1 with one wait row", id, state)
	}
	return state
}

// ackProducer seeds, claims, and acks one producer row through the real
// single-row Ack.
func ackProducer(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	clock *time.Time,
	id, scopeID, genID string,
	domain reducercontract.Domain,
) {
	t.Helper()
	addOwnGateItem(t, ctx, db, id, scopeID, genID, domain, *clock)
	queue, intent := claimWaitingRow(t, ctx, db, clock, domain)
	if intent.IntentID != id {
		t.Fatalf("claimed producer %q, want %q", intent.IntentID, id)
	}
	if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack producer %s: %v", id, err)
	}
}

// TestProducerTerminalWakeForeignImpactAckLeavesReconciliationParkedLive is
// the #11114 theory proof. Scope A's reconciliation waits on its own scope
// generation's supply_chain_impact row (#8827: the probe reads the waiter's
// own scope and generation). Scope B's impact Ack must leave A parked: the
// wait row stays, and visible_at and attempt_count do not move. Scope A's
// own impact Ack must reopen it at once. RED before #11114: the cross-scope
// leg reopened every scope-bound wait naming supply_chain_impact in any
// scope, so B's Ack reopened A.
func TestProducerTerminalWakeForeignImpactAckLeavesReconciliationParkedLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	const scopeA, scopeB = "waiting-11114-impact-a", "waiting-11114-impact-b"
	addOwnGateGenerationSeed(t, ctx, base, scopeA, "a0", "a1", clock)
	addOwnGateGenerationSeed(t, ctx, base, scopeB, "b0", "b1", clock)
	addOwnGateItem(t, ctx, base, "recon-11114-a", scopeA, "a1", reducercontract.DomainSecurityAlertReconciliation, clock)
	parked := parkNarrowedWaiter(t, ctx, base, &clock, "recon-11114-a",
		reducercontract.DomainSecurityAlertReconciliation, impactNotReadyCause{})

	clock = clock.Add(time.Minute)
	ackProducer(t, ctx, base, &clock, "impact-11114-b", scopeB, "b1", reducercontract.DomainSupplyChainImpact)
	after := readWaitingRow(t, ctx, base, "recon-11114-a")
	if after.waitRows != 1 || after.status != "retrying" || after.attempts != parked.attempts ||
		!after.visibleAt.Time.Equal(parked.visibleAt.Time) {
		t.Fatalf("scope B's impact Ack moved scope A's parked reconciliation:\nparked = %+v\nafter  = %+v", parked, after)
	}

	clock = clock.Add(time.Minute)
	ackProducer(t, ctx, base, &clock, "impact-11114-a", scopeA, "a1", reducercontract.DomainSupplyChainImpact)
	reopened := readWaitingRow(t, ctx, base, "recon-11114-a")
	if reopened.waitRows != 0 || reopened.status != "retrying" || reopened.attempts != parked.attempts ||
		!reopened.visibleAt.Time.Equal(clock) {
		t.Fatalf("scope A's own impact Ack did not reopen it at once: %+v (wake at %s)", reopened, clock)
	}
}

// TestProducerTerminalWakeCatalogRuntimeCrossScopeLive is the #11114
// lost-wake guard. A catalog correlation parks on RuntimeNotReady keyed to
// its own catalog scope, but its fence reads the workload_materialization
// rows of the correlated repository scopes, so the keyed wake can never
// match. A workload_materialization Ack in a different repository scope must
// still reopen it through the cross-scope leg (#7816, #11053).
func TestProducerTerminalWakeCatalogRuntimeCrossScopeLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	const catalogScope, repoScope = "waiting-11114-catalog", "waiting-11114-repo"
	addOwnGateGenerationSeed(t, ctx, base, catalogScope, "c0", "c1", clock)
	addOwnGateGenerationSeed(t, ctx, base, repoScope, "r0", "r1", clock)
	addOwnGateItem(t, ctx, base, "catalog-11114", catalogScope, "c1", reducercontract.DomainServiceCatalogCorrelation, clock)
	parked := parkNarrowedWaiter(t, ctx, base, &clock, "catalog-11114",
		reducercontract.DomainServiceCatalogCorrelation, runtimeNotReadyCause{})

	clock = clock.Add(time.Minute)
	ackProducer(t, ctx, base, &clock, "workload-11114", repoScope, "r1", reducercontract.DomainWorkloadMaterialization)
	reopened := readWaitingRow(t, ctx, base, "catalog-11114")
	if reopened.waitRows != 0 || reopened.status != "retrying" || reopened.attempts != parked.attempts ||
		!reopened.visibleAt.Time.Equal(clock) {
		t.Fatalf("workload_materialization Ack in another repository scope did not reopen the catalog wait: %+v (wake at %s)", reopened, clock)
	}
}

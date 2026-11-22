// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11228 audit-resolved signal: a parked wait whose provider committed without a wake is counted reopened by failure_class, and a wait whose queue row already left the set-aside (backstop claim) is counted reconciled; disposable database per test; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestWaitingAuditResolvedCounterLive pins the #11228 operator signal. A
// lost wake (the generation activated, the wait row survived) resolves
// through the audit and counts
// eshu_dp_reducer_waiting_audit_resolved_total{outcome="reopened"} for its
// failure_class. A wait whose queue row the backstop already claimed counts
// outcome="reconciled". Without the counter the lost-wake class shows only as
// an unlabeled audit wake statement.
func TestWaitingAuditResolvedCounterLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	scopeID := "waiting-scope-audit-resolved"
	seedWaitingScope(t, ctx, base, scopeID, "ar-g0", "ar-g1", clock)
	class := reducercontract.GenerationActivationNotReadyFailureClass
	// Each wait has its own key: the audit counts a sibling on an
	// already-woken key as reopened without re-reading its queue row.
	plant := func(id, genID, status string) {
		key := waitingScopeGenerationKey(scopeID, genID)
		addOwnGateItem(t, ctx, base, id, scopeID, genID, reducer.DomainDeployableUnitCorrelation, clock)
		if _, err := base.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at)
VALUES ($1, 'generation_active', $2::jsonb, $3, $4, $5, $6, $7, $7)`,
			id, key, scopeID, genID, string(reducer.DomainDeployableUnitCorrelation), class, clock); err != nil {
			t.Fatalf("plant wait %s: %v", id, err)
		}
		if _, err := base.ExecContext(ctx, `
UPDATE fact_work_items SET status = $2, failure_class = $3, visible_at = $4 WHERE work_item_id = $1`,
			id, status, class, clock.Add(WaitingBackstopDelay)); err != nil {
			t.Fatalf("park queue row %s: %v", id, err)
		}
	}
	// The activation commits with no wake (both waits are planted after it):
	// the lost-wake state the issue names.
	activatePendingGen(t, ctx, base, scopeID, "ar-g1", &clock)
	plant("ar-lost", "ar-g1", "retrying")
	// The backstop elapsed and a reducer claimed the row: it left the
	// set-aside while its wait row survived.
	plant("ar-backstop", "ar-g0", "running")

	instruments, reader := newEnqueueInstruments(t)
	reopened, reconciled, _, err := AuditWaitingItems(ctx, SQLDB{DB: base}, instruments,
		clock, DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("AuditWaitingItems() error = %v", err)
	}
	if reopened != 1 || reconciled != 1 {
		t.Fatalf("audit reopened=%d reconciled=%d, want 1 and 1", reopened, reconciled)
	}
	rm := collectAckParkMetrics(t, reader)
	for _, outcome := range []string{telemetry.WaitingWakeOutcomeReopened, telemetry.WaitingAuditOutcomeReconciled} {
		got := counterWithLabels(rm, fallbackResolvedMetric, map[string]string{
			"failure_class": class, "outcome": outcome,
			telemetry.MetricDimensionWaitingWakeSite: WaitingWakeSiteInTx,
		})
		if got != 1 {
			t.Fatalf("fallback_resolved{failure_class=%s,wake_site=in_tx,outcome=%s} = %d, want 1", class, outcome, got)
		}
	}
}

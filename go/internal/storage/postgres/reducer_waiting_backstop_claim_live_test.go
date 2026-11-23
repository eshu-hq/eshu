// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11228 backstop exit signal: a parked wait claimed at the 10-minute backstop with its wait row still present counts outcome=backstop by failure_class and the class's registered wake_site, through Claim and ClaimBatch; a row woken on one key with a sibling wait row left, and a row whose wait row is gone, do not; disposable database per subtest; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fallbackResolvedMetric is the #11228 counter of waits a fallback (the
// audit or the backstop) resolved instead of a wake.
const fallbackResolvedMetric = "eshu_dp_reducer_waiting_fallback_resolved_total"

// backstopClaimFixture is one parked generation_active waiter on its own
// disposable database.
type backstopClaimFixture struct {
	base  *sql.DB
	clock time.Time
	id    string
	queue ReducerQueue
}

// newParkedWaiter seeds a pending generation, claims one reducer row on it,
// and parks it through the production Fail.
func newParkedWaiter(t *testing.T, ctx context.Context, scopeID string) *backstopClaimFixture {
	t.Helper()
	base, _ := openWaitingDB(t, ctx)
	f := &backstopClaimFixture{base: base, clock: time.Now().UTC().Truncate(time.Second), id: scopeID + "-waiter"}
	seedWaitingScope(t, ctx, base, scopeID, scopeID+"-g0", scopeID+"-g1", f.clock)
	addOwnGateItem(t, ctx, base, f.id, scopeID, scopeID+"-g1", reducer.DomainDeployableUnitCorrelation, f.clock)
	f.queue = newOwnGateQueue(base, &f.clock, reducer.DomainDeployableUnitCorrelation)
	intent, ok, err := f.queue.Claim(ctx)
	if err != nil || !ok || intent.IntentID != f.id {
		t.Fatalf("claim waiter: intent=%q ok=%v err=%v", intent.IntentID, ok, err)
	}
	if err := f.queue.Fail(ctx, intent, notYetActive(scopeID, scopeID+"-g1")); err != nil {
		t.Fatalf("park waiter: %v", err)
	}
	if got := readWaitingRow(t, ctx, base, f.id); got.waitRows != 1 || got.status != "retrying" {
		t.Fatalf("parked waiter = %+v, want retrying with one wait row", got)
	}
	return f
}

// claimWith claims through Claim or ClaimBatch on a queue wired to
// instruments, and returns the claimed ids.
func (f *backstopClaimFixture) claimWith(t *testing.T, ctx context.Context, batch bool, instruments *telemetry.Instruments) []string {
	t.Helper()
	queue := f.queue
	queue.Instruments = instruments
	if batch {
		intents, err := queue.ClaimBatch(ctx, 4)
		if err != nil {
			t.Fatalf("ClaimBatch() error = %v", err)
		}
		ids := make([]string, 0, len(intents))
		for _, intent := range intents {
			ids = append(ids, intent.IntentID)
		}
		return ids
	}
	intent, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !ok {
		return nil
	}
	return []string{intent.IntentID}
}

// backstopCount reads the backstop outcome for the generation_active class
// on its registered in_tx wake site.
func backstopCount(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
},
) int64 {
	t.Helper()
	rm := collectAckParkMetrics(t, reader)
	return counterWithLabels(rm, fallbackResolvedMetric, map[string]string{
		"failure_class":                          reducercontract.GenerationActivationNotReadyFailureClass,
		telemetry.MetricDimensionWaitingWakeSite: WaitingWakeSiteInTx,
		"outcome":                                "backstop",
	})
}

// TestWaitingBackstopClaimCounterLive pins the second exit of ruling
// decision 4: a wait the backstop released (claimed with its wait row still
// present, never woken) is counted outcome=backstop, distinct from the
// audit's reopened and reconciled exits. A row reopened by a wake on one key
// while a sibling wait row survives is not a backstop exit, nor is a row
// whose wait row is already gone.
func TestWaitingBackstopClaimCounterLive(t *testing.T) {
	ctx := context.Background()
	for _, batch := range []bool{false, true} {
		name := "claim"
		if batch {
			name = "claim_batch"
		}
		t.Run(name+"/backstop_with_wait_row", func(t *testing.T) {
			f := newParkedWaiter(t, ctx, "backstop-hit-"+name)
			f.clock = f.clock.Add(WaitingBackstopDelay + time.Second)
			instruments, reader := newEnqueueInstruments(t)
			if ids := f.claimWith(t, ctx, batch, instruments); len(ids) != 1 || ids[0] != f.id {
				t.Fatalf("claimed %v at the backstop, want [%s]", ids, f.id)
			}
			if got := backstopCount(t, reader); got != 1 {
				t.Fatalf("backstop count = %d, want 1: the row was claimed at the backstop with its wait row present", got)
			}
		})
		t.Run(name+"/woken_sibling_wait_row_left", func(t *testing.T) {
			f := newParkedWaiter(t, ctx, "backstop-sib-"+name)
			// A second wait row on another key, as a multi-key wait leaves
			// behind when one key's provider wakes it.
			if _, err := f.base.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at)
SELECT work_item_id, waits_for_kind, $2::jsonb, scope_id, generation_id, domain, failure_class,
    first_deferred_at, updated_at
FROM reducer_waiting_items WHERE work_item_id = $1`,
				f.id, waitingScopeGenerationKey("backstop-sib-"+name, "other")); err != nil {
				t.Fatalf("plant sibling wait row: %v", err)
			}
			key := waitingScopeGenerationKey("backstop-sib-"+name, "backstop-sib-"+name+"-g1")
			if _, _, err := wakeWaitingByKey(ctx, SQLDB{DB: f.base}, nil, WaitingWakeSiteInTx,
				WaitingKindGenerationActive, key, f.clock); err != nil {
				t.Fatalf("wake one key: %v", err)
			}
			if got := readWaitingRow(t, ctx, f.base, f.id); got.waitRows != 1 {
				t.Fatalf("wait rows after one key's wake = %d, want the sibling left", got.waitRows)
			}
			instruments, reader := newEnqueueInstruments(t)
			if ids := f.claimWith(t, ctx, batch, instruments); len(ids) != 1 || ids[0] != f.id {
				t.Fatalf("claimed %v after the wake, want [%s]", ids, f.id)
			}
			if got := backstopCount(t, reader); got != 0 {
				t.Fatalf("backstop count = %d, want 0: a wake reopened the row", got)
			}
		})
		t.Run(name+"/backstop_without_wait_row", func(t *testing.T) {
			f := newParkedWaiter(t, ctx, "backstop-gone-"+name)
			if _, err := f.base.ExecContext(ctx,
				`DELETE FROM reducer_waiting_items WHERE work_item_id = $1`, f.id); err != nil {
				t.Fatalf("drop wait row: %v", err)
			}
			f.clock = f.clock.Add(WaitingBackstopDelay + time.Second)
			instruments, reader := newEnqueueInstruments(t)
			if ids := f.claimWith(t, ctx, batch, instruments); len(ids) != 1 || ids[0] != f.id {
				t.Fatalf("claimed %v at the backstop, want [%s]", ids, f.id)
			}
			if got := backstopCount(t, reader); got != 0 {
				t.Fatalf("backstop count = %d, want 0: no wait row was present at the claim", got)
			}
		})
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10876 winners-watermark wake proofs: a keyed winners tick reopens advisory waits parked on the watermark without the audit running, and an audit-reopened wait converges through the next tick with its bound covered instead of stranding at the drain deadline; disposable database per test, about 60 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// winnersWatermarkTestCause drives the real Fail path into the
// advisory_group_winners_watermark_not_ready park, mirroring what the
// advisory-group handler returns when the winners watermark has not passed
// the intent's bound. ExpectedWait only changes the log level, so the park
// behavior is identical without it.
type winnersWatermarkTestCause struct {
	watermark time.Time
	required  time.Time
}

func (e winnersWatermarkTestCause) Error() string {
	return "advisory group projection waiting for supply chain impact winners rebuilt at " +
		e.watermark.UTC().Format(time.RFC3339) + " to reach the intent enqueue time " +
		e.required.UTC().Format(time.RFC3339)
}

func (winnersWatermarkTestCause) Retryable() bool { return true }

func (winnersWatermarkTestCause) FailureClass() string {
	return advisorygroup.WinnersWatermarkNotReadyFailureClass
}

// seedWinnersWatermark stamps the singleton watermark row, simulating the
// maintainer state the gate left behind: a stale rebuild the advisory waits
// were parked against.
func seedWinnersWatermark(t *testing.T, ctx context.Context, db *sql.DB, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)
VALUES (1, $1)
ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at`, at); err != nil {
		t.Fatalf("seed winners watermark: %v", err)
	}
}

// readWinnersWatermark reads the committed singleton stamp.
func readWinnersWatermark(t *testing.T, ctx context.Context, db *sql.DB) time.Time {
	t.Helper()
	var at time.Time
	if err := db.QueryRowContext(ctx, `
SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1`).Scan(&at); err != nil {
		t.Fatalf("read winners watermark: %v", err)
	}
	return at.UTC()
}

// parkAdvisoryOnWinnersWatermark claims one advisory row and fails it with
// the winners-watermark cause, returning the parked intent. It mirrors the
// gate: the handler parks against a stale watermark.
func parkAdvisoryOnWinnersWatermark(
	t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, id string,
) reducercontract.Intent {
	t.Helper()
	queue, intent := claimWaitingRow(t, ctx, db, clock, reducercontract.DomainAdvisoryGroupProjection)
	if intent.IntentID != id {
		t.Fatalf("claimed %q, want %q", intent.IntentID, id)
	}
	if intent.EnqueuedAt.IsZero() || intent.CycleStartedAt.IsZero() {
		t.Fatalf("claimed intent carries no times: enqueued=%v cycle=%v", intent.EnqueuedAt, intent.CycleStartedAt)
	}
	watermark := readWinnersWatermark(t, ctx, db)
	if err := queue.Fail(ctx, intent, winnersWatermarkTestCause{watermark: watermark, required: intent.EnqueuedAt}); err != nil {
		t.Fatalf("fail into winners wait: %v", err)
	}
	got := readWaitingRow(t, ctx, db, id)
	if got.waitRows != 1 {
		t.Fatalf("wait rows = %d, want 1 parked", got.waitRows)
	}
	if !got.failureClass.Valid || got.failureClass.String != advisorygroup.WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("failure class = %q, want the winners-watermark class", got.failureClass.String)
	}
	return intent
}

// winnersSatisfactionForClaimed runs the shared satisfaction predicate for a
// freshly claimed advisory intent the way the handler and the audit do,
// deriving the wait key from the claimed intent itself.
func winnersSatisfactionForClaimed(
	t *testing.T, ctx context.Context, db *sql.DB, intent reducercontract.Intent,
) bool {
	t.Helper()
	entry := WaitingDeferralByClass()[advisorygroup.WinnersWatermarkNotReadyFailureClass]
	keyJSON, ok := waitingKeyFor(intent, entry)
	if !ok {
		t.Fatalf("no wait key for claimed intent %+v", intent)
	}
	satisfied, err := waitingSatisfiedForIntent(ctx, SQLDB{DB: db}, intent, entry, keyJSON, time.Now().UTC())
	if err != nil {
		t.Fatalf("satisfaction recheck: %v", err)
	}
	return satisfied
}

// TestWinnersKeyedTickWakesParkedAdvisoryWaitLive replays the golden-corpus
// gate miss: advisory waits parked against a stale winners watermark while
// keyed resweeps commit every 30 s. RED before the fix: the keyed tick
// stamps a covering watermark but wakes nothing, so the wait rows survive
// the tick. Fixed: the tick reopens them in-transaction with the new stamp
// as their bound, so the next claim's handler recheck passes.
func TestWinnersKeyedTickWakesParkedAdvisoryWaitLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	scopeID := "waiting-winners-scope"
	genID := "waiting-winners-g1"
	seedOwnGateScope(t, ctx, base, scopeID, genID, clock)
	const id = "wait-winners-keyed"
	addOwnGateItem(t, ctx, base, id, scopeID, genID, reducercontract.DomainAdvisoryGroupProjection, clock)

	// The gate state: a stale watermark the advisory handler parked against.
	seedWinnersWatermark(t, ctx, base, clock.Add(-time.Minute))
	intent := parkAdvisoryOnWinnersWatermark(t, ctx, base, &clock, id)
	parkedBound := intent.CycleStartedAt
	if intent.EnqueuedAt.After(parkedBound) {
		parkedBound = intent.EnqueuedAt
	}

	// One keyed tick on an empty dirty log: it stamps a fresh watermark past
	// the parked bound. No audit runs anywhere in this test.
	store := NewSupplyChainImpactWinnersStore(SQLDB{DB: base})
	result, err := store.ResweepWinnersKeyed(ctx)
	if err != nil {
		t.Fatalf("keyed tick: %v", err)
	}
	if result.Skipped {
		t.Fatal("keyed tick skipped: another resweep is in flight in a single-tick test")
	}
	stamp := readWinnersWatermark(t, ctx, base)
	if !stamp.After(parkedBound) {
		t.Fatalf("tick stamp %v does not cover parked bound %v", stamp, parkedBound)
	}

	// The wait must be gone and the queue row claimable: the wake ran in the
	// tick's transaction, not in a later audit.
	got := readWaitingRow(t, ctx, base, id)
	if got.waitRows != 0 {
		t.Fatalf("wait rows = %d after the keyed tick, want 0 (woken in-tx, no audit ran)", got.waitRows)
	}
	if got.status != "retrying" {
		t.Fatalf("status = %q after the wake, want retrying", got.status)
	}

	// The woken row's new bound must be covered by the stamp that woke it:
	// the wake aligns the cycle start to the covering watermark instead of
	// the wake wall time, so the handler recheck passes on the next claim
	// instead of re-parking with a bound past the stamp.
	clock = time.Now().UTC()
	_, reclaimed := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainAdvisoryGroupProjection)
	if reclaimed.IntentID != id {
		t.Fatalf("reclaimed %q, want %q", reclaimed.IntentID, id)
	}
	if reclaimed.CycleStartedAt.After(stamp) {
		t.Fatalf("reclaimed cycle start %v is past the waking stamp %v: the handler would re-park", reclaimed.CycleStartedAt, stamp)
	}
	if !winnersSatisfactionForClaimed(t, ctx, base, reclaimed) {
		t.Fatalf("woken intent not satisfied at stamp %v with cycle start %v: the handler would re-park", stamp, reclaimed.CycleStartedAt)
	}
}

// TestWinnersAuditReopenedWaitConvergesOnNextTickLive replays the exact gate
// path: the 5-minute audit reopened 4 parked advisory waits, yet all 4 were
// still residual at the drain deadline. The audit's reopen moves the cycle
// start to the audit time, which no stamp covers, so the reclaim re-parks;
// convergence needs the next keyed tick to both stamp past the new bound and
// wake the re-parked wait with that stamp as its bound.
func TestWinnersAuditReopenedWaitConvergesOnNextTickLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	scopeID := "waiting-winners-audit-scope"
	genID := "waiting-winners-audit-g1"
	seedOwnGateScope(t, ctx, base, scopeID, genID, clock)
	const id = "wait-winners-audit"
	addOwnGateItem(t, ctx, base, id, scopeID, genID, reducercontract.DomainAdvisoryGroupProjection, clock)

	seedWinnersWatermark(t, ctx, base, clock.Add(-time.Minute))
	parkAdvisoryOnWinnersWatermark(t, ctx, base, &clock, id)

	// Ticks advance the stamp the advisory waits on (as in the gate, where
	// every keyed resweep committed). Stamp directly: the audit half of this
	// proof must not depend on the tick wake under test.
	covering := time.Now().UTC()
	seedWinnersWatermark(t, ctx, base, covering)

	// The 5-minute audit pass: the provider committed, so it reopens.
	reopened, reconciled, missed, err := AuditWaitingItems(
		ctx, SQLDB{DB: base}, nil, time.Now().UTC(), DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if reopened != 1 || reconciled != 0 || missed != 0 {
		t.Fatalf("audit = reopened %d reconciled %d missed %d, want 1/0/0", reopened, reconciled, missed)
	}
	if got := readWaitingRow(t, ctx, base, id); got.waitRows != 0 {
		t.Fatalf("wait rows = %d after the audit, want 0", got.waitRows)
	}

	// The reclaim runs before any new stamp covers the audit time, so the
	// handler re-parks (the gate's residual): the bound moved to the audit
	// reopen while the watermark did not.
	clock = time.Now().UTC()
	_, reclaimed := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainAdvisoryGroupProjection)
	if reclaimed.IntentID != id {
		t.Fatalf("reclaimed %q, want %q", reclaimed.IntentID, id)
	}
	if winnersSatisfactionForClaimed(t, ctx, base, reclaimed) {
		t.Fatalf("audit-reopened intent unexpectedly satisfied: the gate proof needs the re-park half")
	}
	reclaimQueue := newOwnGateQueue(base, &clock, reducercontract.DomainAdvisoryGroupProjection)
	if err := reclaimQueue.Fail(ctx, reclaimed, winnersWatermarkTestCause{
		watermark: readWinnersWatermark(t, ctx, base), required: reclaimed.CycleStartedAt,
	}); err != nil {
		t.Fatalf("re-park after audit reopen: %v", err)
	}
	reparked := readWaitingRow(t, ctx, base, id)
	if reparked.waitRows != 1 {
		t.Fatalf("wait rows = %d after the reclaim Fail, want the gate's re-park (1)", reparked.waitRows)
	}

	// The next keyed tick converges it: the stamp passes the re-parked bound
	// and the in-tx wake aligns the cycle start to that stamp, so the final
	// claim's handler recheck passes instead of stranding to the deadline.
	store := NewSupplyChainImpactWinnersStore(SQLDB{DB: base})
	if _, err := store.ResweepWinnersKeyed(ctx); err != nil {
		t.Fatalf("converging keyed tick: %v", err)
	}
	final := readWinnersWatermark(t, ctx, base)
	if got := readWaitingRow(t, ctx, base, id); got.waitRows != 0 {
		t.Fatalf("wait rows = %d after the converging tick, want 0", got.waitRows)
	}
	clock = time.Now().UTC()
	_, converged := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainAdvisoryGroupProjection)
	if converged.IntentID != id {
		t.Fatalf("converged claim %q, want %q", converged.IntentID, id)
	}
	if !winnersSatisfactionForClaimed(t, ctx, base, converged) {
		t.Fatalf("re-parked intent not satisfied at stamp %v with cycle start %v: still residual", final, converged.CycleStartedAt)
	}
}

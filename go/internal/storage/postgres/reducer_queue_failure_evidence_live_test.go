// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// failureEvidenceProofWinners is a stub advisory-group winners reader stuck
// at one old watermark: the dead-maintainer shape the #11075 stale clock
// must surface in bounded attempts.
type failureEvidenceProofWinners struct {
	watermark time.Time
}

func (w failureEvidenceProofWinners) QualifyingAdvisoryGroupWinners(_ context.Context, _ string) ([]advisorygroup.WinnerRow, error) {
	return nil, nil
}

func (w failureEvidenceProofWinners) AdvisoryGroupWinnersWatermark(_ context.Context) (time.Time, error) {
	return w.watermark, nil
}

// failureEvidenceProofWriter fails any reconcile call: the proof never
// reaches the graph because the watermark never passes the bound.
type failureEvidenceProofWriter struct{}

func (failureEvidenceProofWriter) ReconcileAdvisoryGroup(_ context.Context, _ advisorygroup.Group) (advisorygroup.WriteStats, error) {
	return advisorygroup.WriteStats{}, nil
}

// TestReducerQueueClaimCarriesFailureEvidenceLive is the #11075 phase-2 live
// proof that the deferral clock survives the queue: enqueue, claim, defer
// through the production advisory-group handler, Fail, re-claim, and defer
// again. The re-claimed intent must carry the first deferral's marker in its
// failure evidence, the attempt count must stay frozen under the non-counting
// class, the second verdict must still defer not_ready (the clock restarts
// from the first deferral, not the second claim), and a verdict past 30
// minutes of post-claim deferral must read stale.
func TestReducerQueueClaimCarriesFailureEvidenceLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the failure-evidence proof")
	}
	// Fail-closed disposable opt-in: this proof owns its database.
	reducerFairnessProofOptIn(t, dsn)
	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)

	const scopeID = "failure-evidence-11075"
	const generationID = "gen-1"
	seedFailureEvidenceScope(t, ctx, db, scopeID, generationID)

	// Fixed queue clock: the test time-travels past the 10-minute park
	// backstop exactly as wall-clock expiry would, without touching the
	// row. The wake path would reset the cycle; the backstop path preserves
	// it, which is the sustained-deferral shape under test.
	queueClock := time.Now().UTC().Truncate(time.Second)
	firstDeferral := queueClock
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "failure-evidence-proof-reducer",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Millisecond,
		MaxAttempts:   3,
		ClaimDomains:  []reducercontract.Domain{reducercontract.DomainAdvisoryGroupProjection},
		Now:           func() time.Time { return queueClock },
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducercontract.DomainAdvisoryGroupProjection,
		EntityKey:    "advisory_group:CVE-2026-liveproof",
		Reason:       "supply_chain_impact_winners_touch",
		FactID:       "fact-11075",
		SourceSystem: "vulnerability_intelligence",
	}}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("first Claim() claimed = false, want the enqueued advisory intent")
	}
	if intent.Failure != nil {
		t.Fatalf("first claim Failure = %+v, want nil for a row with no prior verdict", intent.Failure)
	}

	// The watermark sits an hour behind the freshly enqueued row: the
	// production handler defers, and the marker dates the first deferral.
	handler := advisorygroup.Handler{
		Winners: failureEvidenceProofWinners{watermark: firstDeferral.Add(-time.Hour)},
		Writer:  failureEvidenceProofWriter{},
		Now:     func() time.Time { return firstDeferral },
	}
	_, handlerErr := handler.Handle(ctx, intent)
	if handlerErr == nil {
		t.Fatal("handler on a behind watermark: want a deferral error")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(handlerErr, &classified) {
		t.Fatalf("deferral error does not classify a failure: %v", handlerErr)
	}
	if classified.FailureClass() != advisorygroup.WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("first verdict class = %q, want not_ready", classified.FailureClass())
	}
	if err := queue.Fail(ctx, intent, handlerErr); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	// Travel past the 10-minute park backstop: the row becomes claimable
	// again in the same cycle, attempt frozen, failure evidence intact.
	queueClock = queueClock.Add(11 * time.Minute)
	intent2, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("second Claim() claimed = false, want the retrying row back past the park backstop")
	}
	if intent2.AttemptCount != intent.AttemptCount {
		t.Fatalf("re-claim attempt count = %d, want %d (non-counting class freezes attempts)", intent2.AttemptCount, intent.AttemptCount)
	}
	if intent2.Failure == nil {
		t.Fatal("re-claim Failure is nil, want the prior deferral's failure evidence")
	}
	if intent2.Failure.FailureClass != advisorygroup.WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("re-claim failure class = %q, want not_ready", intent2.Failure.FailureClass)
	}
	if !strings.Contains(intent2.Failure.Details, "winners-watermark-deferral: first-deferred-at=") {
		t.Fatalf("re-claim failure details miss the deferral marker: %q", intent2.Failure.Details)
	}

	// One minute of post-claim deferral: still not_ready. The clock did not
	// restart at the second claim.
	handler.Now = func() time.Time { return firstDeferral.Add(time.Minute) }
	if _, err := handler.Handle(ctx, intent2); !reducercontract.IsExpectedWait(err) {
		t.Fatalf("second verdict 1min past first deferral: want expected wait, got %v", err)
	}

	// Thirty-one minutes of post-claim deferral on a stuck watermark: stale,
	// retryable, out of the non-counting class — the dead maintainer
	// surfaces visibly instead of waiting in silence.
	handler.Now = func() time.Time { return firstDeferral.Add(31 * time.Minute) }
	_, staleErr := handler.Handle(ctx, intent2)
	if staleErr == nil {
		t.Fatal("verdict 31min past first deferral: want a stale error")
	}
	if reducercontract.IsExpectedWait(staleErr) {
		t.Fatalf("31min post-claim deferral: want out of expected wait, got %v", staleErr)
	}
	classified = nil
	if !errors.As(staleErr, &classified) {
		t.Fatalf("stale error does not classify a failure: %v", staleErr)
	}
	if classified.FailureClass() != advisorygroup.WinnersWatermarkStaleFailureClass {
		t.Fatalf("third verdict class = %q, want stale", classified.FailureClass())
	}

	// Dead-maintainer full loop (#11075 Decision 2): the stale verdict is
	// counting, so driving it through Fail must burn the attempt budget and
	// land the row in dead_letter within MaxAttempts counting evaluations —
	// bounded and visible instead of silent. The ledger: claim 1 evaluated
	// not_ready (non-counting, attempt frozen at 1); every stale Fail below
	// retries while attemptCount < MaxAttempts and the last one
	// dead-letters. RED-first note: asserting dead_letter right after this
	// first stale Fail fails with status='retrying' — the budget genuinely
	// burns one counting evaluation at a time.
	if err := queue.Fail(ctx, intent2, staleErr); err != nil {
		t.Fatalf("Fail(first stale) error = %v", err)
	}
	var firstStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, intent2.IntentID).Scan(&firstStatus); err != nil {
		t.Fatalf("first stale Fail status: %v", err)
	}
	if firstStatus != "retrying" {
		t.Fatalf("after first stale Fail status = %q, want retrying (budget unspent)", firstStatus)
	}
	staleVerdicts := 0
	for {
		staleVerdicts++
		if staleVerdicts > queue.MaxAttempts {
			t.Fatalf("stale loop ran %d iterations without dead-lettering (MaxAttempts=%d)", staleVerdicts, queue.MaxAttempts)
		}
		// Past the backstop: the stale class parks on the winners_watermark
		// wait like the not_ready class, so the row is retrying on the
		// 10-minute backstop; travel past it so the claim is deterministic
		// and the handler clock stays past the bound.
		queueClock = queueClock.Add(11 * time.Minute)
		intentN, claimed, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("stale-loop Claim() #%d error = %v", staleVerdicts, err)
		}
		if !claimed {
			t.Fatalf("stale-loop Claim() #%d claimed = false, want the retrying row", staleVerdicts)
		}
		if wantAttempt := intent2.AttemptCount + staleVerdicts; intentN.AttemptCount != wantAttempt {
			t.Fatalf("stale-loop claim #%d attempt count = %d, want %d (stale is counting)", staleVerdicts, intentN.AttemptCount, wantAttempt)
		}
		if intentN.Failure == nil || !strings.Contains(intentN.Failure.Details, "winners-watermark-deferral: first-deferred-at=") {
			t.Fatalf("stale-loop claim #%d lost the deferral marker: %+v", staleVerdicts, intentN.Failure)
		}
		_, staleErrN := handler.Handle(ctx, intentN)
		if staleErrN == nil || reducercontract.IsExpectedWait(staleErrN) {
			t.Fatalf("stale-loop verdict #%d: want out-of-expected-wait stale, got %v", staleVerdicts, staleErrN)
		}
		classified = nil
		if !errors.As(staleErrN, &classified) {
			t.Fatalf("stale-loop error #%d does not classify a failure: %v", staleVerdicts, staleErrN)
		}
		if classified.FailureClass() != advisorygroup.WinnersWatermarkStaleFailureClass {
			t.Fatalf("stale-loop verdict #%d class = %q, want stale", staleVerdicts, classified.FailureClass())
		}
		if err := queue.Fail(ctx, intentN, staleErrN); err != nil {
			t.Fatalf("stale-loop Fail() #%d error = %v", staleVerdicts, err)
		}
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, intentN.IntentID).Scan(&status); err != nil {
			t.Fatalf("stale-loop status #%d: %v", staleVerdicts, err)
		}
		if status == "dead_letter" {
			break
		}
		if status != "retrying" {
			t.Fatalf("stale-loop status #%d = %q, want retrying or dead_letter", staleVerdicts, status)
		}
	}
	// intent2's stale Fail plus the loop's: exactly MaxAttempts counting
	// stale evaluations to reach dead_letter on the stuck watermark.
	if total := staleVerdicts + 1; total != queue.MaxAttempts {
		t.Fatalf("counting stale verdicts through Fail = %d, want MaxAttempts=%d to dead_letter", total, queue.MaxAttempts)
	}
}

func seedFailureEvidenceScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, base, generationID)
	exec(`
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload
) VALUES ($1, $2, 'snapshot', $3, $3, 'active', $3, '{}'::jsonb)`,
		generationID, scopeID, base)
}

// TestWinnersStaleWaitParksWakesAndDrainsLive is the consumer-side proof for
// the stale-wait park: the production handler's stale verdict (30 minutes of
// post-claim deferral on a stuck watermark) must park on the winners_watermark
// wait with the 10-minute backstop instead of churning the retry timer, the
// resweep commit must wake it in-transaction, and the reclaimed intent must
// converge through the production handler to success instead of re-queuing.
//
// RED before the park fix: the stale Fail takes the timer path, so the row
// retries on the short backoff with no wait row, burns its counting attempts
// while a long resweep holds the watermark, and poisons as dead_letter. The
// park assertions below fail on that tree with visible_at on the millisecond
// backoff and zero wait rows.
func TestWinnersStaleWaitParksWakesAndDrainsLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale-wait drain proof")
	}
	// Fail-closed disposable opt-in: this proof owns its database.
	reducerFairnessProofOptIn(t, dsn)
	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)

	const scopeID = "stale-wait-drain-proof"
	const generationID = "gen-1"
	seedFailureEvidenceScope(t, ctx, db, scopeID, generationID)

	queueClock := time.Now().UTC().Truncate(time.Second)
	firstDeferral := queueClock
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "stale-wait-drain-proof-reducer",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Millisecond,
		MaxAttempts:   3,
		ClaimDomains:  []reducercontract.Domain{reducercontract.DomainAdvisoryGroupProjection},
		Now:           func() time.Time { return queueClock },
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducercontract.DomainAdvisoryGroupProjection,
		EntityKey:    "advisory_group:CVE-2026-stale-drain",
		Reason:       "supply_chain_impact_winners_touch",
		FactID:       "fact-stale-drain",
		SourceSystem: "vulnerability_intelligence",
	}}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("first Claim() claimed = false, want the enqueued advisory intent")
	}

	// The watermark sits an hour behind: the first verdict defers not_ready
	// and parks, dating the cycle's first deferral.
	handler := advisorygroup.Handler{
		Winners: failureEvidenceProofWinners{watermark: firstDeferral.Add(-time.Hour)},
		Writer:  failureEvidenceProofWriter{},
		Now:     func() time.Time { return firstDeferral },
	}
	_, handlerErr := handler.Handle(ctx, intent)
	if handlerErr == nil {
		t.Fatal("handler on a behind watermark: want a deferral error")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(handlerErr, &classified) {
		t.Fatalf("deferral error does not classify a failure: %v", handlerErr)
	}
	if classified.FailureClass() != advisorygroup.WinnersWatermarkNotReadyFailureClass {
		t.Fatalf("first verdict class = %q, want not_ready", classified.FailureClass())
	}
	if err := queue.Fail(ctx, intent, handlerErr); err != nil {
		t.Fatalf("Fail(first) error = %v", err)
	}

	// Thirty-one minutes of post-claim deferral on the stuck watermark: the
	// verdict reclassifies to the counting stale class.
	queueClock = queueClock.Add(11 * time.Minute)
	intent2, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("second Claim() claimed = false, want the retrying row back past the park backstop")
	}
	handler.Now = func() time.Time { return firstDeferral.Add(31 * time.Minute) }
	_, staleErr := handler.Handle(ctx, intent2)
	if staleErr == nil {
		t.Fatal("handler 31min past first deferral: want a stale error")
	}
	if reducercontract.IsExpectedWait(staleErr) {
		t.Fatalf("31min past first deferral: want out of expected wait, got %v", staleErr)
	}
	classified = nil
	if !errors.As(staleErr, &classified) {
		t.Fatalf("stale error does not classify a failure: %v", staleErr)
	}
	if classified.FailureClass() != advisorygroup.WinnersWatermarkStaleFailureClass {
		t.Fatalf("second verdict class = %q, want stale", classified.FailureClass())
	}
	if err := queue.Fail(ctx, intent2, staleErr); err != nil {
		t.Fatalf("Fail(stale) error = %v", err)
	}

	// The stale wait must park on the winners_watermark wait with the
	// 10-minute backstop, not churn on the millisecond retry timer: the
	// failure class stays stale (still counting), the visible horizon is the
	// backstop, and exactly one wait row names the watermark wait.
	stale := readWaitingRow(t, ctx, db, intent2.IntentID)
	if stale.status != "retrying" {
		t.Fatalf("stale status = %q, want retrying (budget unspent)", stale.status)
	}
	if !stale.failureClass.Valid || stale.failureClass.String != advisorygroup.WinnersWatermarkStaleFailureClass {
		t.Fatalf("stale failure class = %v, want %q", stale.failureClass, advisorygroup.WinnersWatermarkStaleFailureClass)
	}
	if !stale.visibleAt.Valid {
		t.Fatal("stale visible_at is NULL, want the 10-minute backstop")
	}
	if backstopIn := stale.visibleAt.Time.Sub(queueClock); backstopIn < 9*time.Minute || backstopIn > 11*time.Minute {
		t.Fatalf("stale visible_at horizon = %v, want ~10m (the backstop, not the retry timer)", backstopIn)
	}
	if stale.waitRows != 1 {
		t.Fatalf("stale wait rows = %d, want 1 parked on the winners watermark", stale.waitRows)
	}
	if !stale.waitKind.Valid || stale.waitKind.String != string(WaitingKindWinnersWatermark) {
		t.Fatalf("stale wait kind = %v, want winners_watermark", stale.waitKind)
	}

	// A keyed tick commits a covering watermark past the parked bound. Its
	// in-transaction wake must reopen the stale wait with the covering stamp
	// as the new bound.
	store := NewSupplyChainImpactWinnersStore(SQLDB{DB: db})
	result, err := store.ResweepWinnersKeyed(ctx)
	if err != nil {
		t.Fatalf("keyed tick: %v", err)
	}
	if result.Skipped {
		t.Fatal("keyed tick skipped: another resweep is in flight in a single-tick test")
	}
	bound := intent2.CycleStartedAt
	if intent2.EnqueuedAt.After(bound) {
		bound = intent2.EnqueuedAt
	}
	stamp := readWinnersWatermark(t, ctx, db)
	if !stamp.After(bound) {
		t.Fatalf("tick stamp %v does not cover parked stale bound %v", stamp, bound)
	}
	if got := readWaitingRow(t, ctx, db, intent2.IntentID); got.waitRows != 0 {
		t.Fatalf("wait rows = %d after the keyed tick, want 0 (stale wait woken in-tx)", got.waitRows)
	}

	// The reclaim's bound is the covering stamp, so the production handler
	// converges to success against the real winners reader instead of
	// re-queuing another wait: the backlog drains.
	queueClock = time.Now().UTC()
	reclaimQueue := newOwnGateQueue(db, &queueClock, reducercontract.DomainAdvisoryGroupProjection)
	reclaimed, ok, err := reclaimQueue.Claim(ctx)
	if err != nil {
		t.Fatalf("drain Claim() error = %v", err)
	}
	if !ok || reclaimed.IntentID != intent2.IntentID {
		t.Fatalf("drain Claim() = %q, want %q", reclaimed.IntentID, intent2.IntentID)
	}
	if reclaimed.CycleStartedAt.After(stamp) {
		t.Fatalf("reclaimed cycle start %v is past the waking stamp %v: the handler would re-park", reclaimed.CycleStartedAt, stamp)
	}
	drainHandler := advisorygroup.Handler{
		Winners: FactStore{database: SQLDB{DB: db}},
		Writer:  failureEvidenceProofWriter{},
	}
	drainResult, err := drainHandler.Handle(ctx, reclaimed)
	if err != nil {
		t.Fatalf("handler after the stale wake: want success (drain), got %v", err)
	}
	if drainResult.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("handler after the stale wake: status = %q, want succeeded", drainResult.Status)
	}
}

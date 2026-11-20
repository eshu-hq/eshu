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

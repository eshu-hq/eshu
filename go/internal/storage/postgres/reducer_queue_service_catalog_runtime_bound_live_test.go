// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// Real-queue elapsed-bound proofs for the #7816 runtime fence (amendments A1,
// A2, and A3). The bound runs per repository from the newest in-flight
// producer row's last-workable time, GREATEST(COALESCE(reopened_at,
// created_at), COALESCE(last_attempt_at, created_at), COALESCE(visible_at,
// created_at), COALESCE(claim_until, created_at)), as the real producer
// readiness store reads it; the catalog row's age never decides. Every
// producer row here is shaped by production SQL only: Claim (on a clock set
// in the past), Heartbeat, Fail through the real retry path (counting and
// non-counting classes), ReopenSucceeded, and Ack.

// runtimeFenceRetryError is a retryable, counting producer failure, the shape
// a workload_materialization handler returns on a transient graph error.
type runtimeFenceRetryError struct{}

func (runtimeFenceRetryError) Error() string        { return "transient graph write failure" }
func (runtimeFenceRetryError) Retryable() bool      { return true }
func (runtimeFenceRetryError) FailureClass() string { return "workload_materialization_transient" }

// runtimeFenceCorpusNotReadyError is the producer's own corpus-fence
// deferral: the non-counting workload_materialization_resolution_not_ready
// class, which retries without touching created_at or reopened_at.
type runtimeFenceCorpusNotReadyError struct{}

func (runtimeFenceCorpusNotReadyError) Error() string   { return "relationship corpus not ready" }
func (runtimeFenceCorpusNotReadyError) Retryable() bool { return true }
func (runtimeFenceCorpusNotReadyError) FailureClass() string {
	return reducer.WorkloadMaterializationResolutionNotReadyFailureClass
}

// setRuntimeFenceProducerCreated moves the producer row's enqueue time
// createdAgo into the past, as if it had been enqueued then.
func setRuntimeFenceProducerCreated(t *testing.T, ctx context.Context, p *runtimeFenceProof, createdAgo time.Duration, status string) {
	t.Helper()
	at := time.Now().UTC().Add(-createdAgo)
	if _, err := p.db.ExecContext(ctx,
		`UPDATE fact_work_items SET created_at = $2, updated_at = $2, status = $3 WHERE work_item_id = $1`,
		runtimeFenceProducerItem, at, status); err != nil {
		t.Fatalf("age producer row: %v", err)
	}
}

// runtimeFenceProducerAttempt shapes the producer row through the real queue:
// enqueued createdAgo, claimed claimedAgo on a clock set that far back, then,
// when cause is non-nil, failed through the real retry path with a backoff of
// exactly retryDelay. A nil cause leaves the row claimed with a lease of
// lease, measured from the claim, and returns the claimed intent. Fail checks the lease against the database
// clock, so a failing case claimed far in the past needs a lease that still
// holds now; the retry clears it.
func runtimeFenceProducerAttempt(
	t *testing.T,
	ctx context.Context,
	p *runtimeFenceProof,
	createdAgo, claimedAgo, retryDelay, lease time.Duration,
	cause error,
) reducer.Intent {
	t.Helper()
	setRuntimeFenceProducerCreated(t, ctx, p, createdAgo, "pending")
	claimClock := time.Now().UTC().Add(-claimedAgo)
	producerQueue := runtimeFenceQueue(p.db, "workload-worker", reducer.DomainWorkloadMaterialization,
		func() time.Time { return claimClock })
	producerQueue.LeaseDuration = lease
	producerQueue.RetryDelay, producerQueue.MaxRetryDelay = retryDelay, retryDelay
	intent, claimed, err := producerQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("producer Claim() = %v, %v", claimed, err)
	}
	if cause == nil {
		return intent
	}
	if err := producerQueue.Fail(ctx, intent, cause); err != nil {
		t.Fatalf("producer Fail() error = %v", err)
	}
	if status, _, _ := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceProducerItem); status != "retrying" {
		t.Fatalf("producer row status = %s, want retrying", status)
	}
	requireRuntimeFenceRetryAnchor(t, ctx, p)
	return intent
}

// requireRuntimeFenceRetryAnchor is amendment A3 proof c: a retry leaves no
// stale lease behind. claim_until is NULL after the real retry path, so the
// store's anchor is the later of the last claim and the scheduled
// visibility.
func requireRuntimeFenceRetryAnchor(t *testing.T, ctx context.Context, p *runtimeFenceProof) {
	t.Helper()
	var claimUntil sql.NullTime
	var want time.Time
	if err := p.db.QueryRowContext(ctx,
		`SELECT claim_until, GREATEST(last_attempt_at, visible_at) FROM fact_work_items WHERE work_item_id = $1`,
		runtimeFenceProducerItem).Scan(&claimUntil, &want); err != nil {
		t.Fatalf("read retried producer row: %v", err)
	}
	if claimUntil.Valid {
		t.Fatalf("retried producer claim_until = %s, want NULL", claimUntil.Time)
	}
	states, err := producerreadiness.Store{DB: SQLDB{DB: p.db}}.RuntimeProducerStates(ctx,
		[]string{runtimeFenceRepoID}, []reducer.Domain{reducer.DomainWorkloadMaterialization})
	if err != nil {
		t.Fatalf("RuntimeProducerStates() error = %v", err)
	}
	if got := states[runtimeFenceRepoID].NewestInFlightWorkableAt; !got.Equal(want) {
		t.Fatalf("retried producer anchor = %s, want max(last_attempt_at, visible_at) = %s", got, want)
	}
}

// claimAndHandleRuntimeFence claims the catalog row and runs the real handler.
func claimAndHandleRuntimeFence(t *testing.T, ctx context.Context, p *runtimeFenceProof, queue ReducerQueue) (reducer.Intent, error) {
	t.Helper()
	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("catalog Claim() = %v, %v", claimed, err)
	}
	_, handleErr := p.handler(p.db).Handle(ctx, intent)
	return intent, handleErr
}

// requireRuntimeFenceDeferral asserts a non-counting deferral that wrote and
// read nothing, then fails the row back to the queue and checks it kept its
// attempt budget.
func requireRuntimeFenceDeferral(t *testing.T, ctx context.Context, p *runtimeFenceProof, queue ReducerQueue, intent reducer.Intent, handleErr error) {
	t.Helper()
	var classified interface{ FailureClass() string }
	if !errors.As(handleErr, &classified) || classified.FailureClass() != servicecatalog.RuntimeNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want the runtime readiness deferral (materialization writes %v)",
			handleErr, runtimeRowCounts(p.materialization.snapshot()))
	}
	if p.correlation.calls != 0 || len(p.materialization.snapshot()) != 0 || p.graph.reads.Load() != 0 {
		t.Fatalf("correlation=%d materialization=%d graph reads=%d, want 0 on a deferral",
			p.correlation.calls, len(p.materialization.snapshot()), p.graph.reads.Load())
	}
	if err := queue.Fail(ctx, intent, handleErr); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	status, class, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceCatalogItem)
	if status != "retrying" || class != servicecatalog.RuntimeNotReadyFailureClass || attempts != 1 {
		t.Fatalf("catalog row = %s/%s/attempts=%d, want retrying/%s/1", status, class, attempts, servicecatalog.RuntimeNotReadyFailureClass)
	}
}

// requireRuntimeFenceCommit asserts the handler proceeded past the bound,
// committed the (empty) family it read, and that the Ack succeeds.
func requireRuntimeFenceCommit(t *testing.T, ctx context.Context, p *runtimeFenceProof, queue ReducerQueue, intent reducer.Intent, handleErr error) {
	t.Helper()
	if handleErr != nil {
		t.Fatalf("Handle() error = %v, want nil: every in-flight producer is past its bound", handleErr)
	}
	if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("Ack() after bound_exceeded error = %v", err)
	}
	if writes := p.materialization.snapshot(); len(writes) != 1 || len(writes[0].Runtime) != 0 {
		t.Fatalf("materialization runtime rows = %v, want one write with the empty family read past the bound", runtimeRowCounts(writes))
	}
	if status, _, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "succeeded" || attempts != 1 {
		t.Fatalf("catalog terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

// TestReducerContentionGateServiceCatalogRuntimeFenceBoundsOnProducerNotCatalogAge
// is proof 4a on the real queue: a catalog row created 45 minutes ago meets a
// producer row enqueued one minute ago. The handler defers with nothing
// written; the consumer's age does not open the bound.
func TestReducerContentionGateServiceCatalogRuntimeFenceBoundsOnProducerNotCatalogAge(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	p := newRuntimeFenceProof(t, ctx, dsn, 45*time.Minute)
	queue := runtimeFenceQueue(p.db, "catalog-old", reducer.DomainServiceCatalogCorrelation, nil)
	intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
	requireRuntimeFenceDeferral(t, ctx, p, queue, intent, handleErr)
}

// TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerIsWorked
// is amendment A2 proofs a, c (first half), and d (second half): a producer
// enqueued 45 minutes ago that a worker is still touching keeps a young
// catalog row deferring. Each attempt refreshes last_attempt_at or
// visible_at, so a row polling the producer's own corpus fence, a row in a
// counting backoff that ends in the future, and a row claimed one minute ago
// are never past the bound. Bounding on created_at committed an empty family
// in all three.
func TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerIsWorked(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	enqueued := 45 * time.Minute
	for _, tc := range []struct {
		name                     string
		claimedAgo, delay, lease time.Duration
		cause                    error
	}{
		// a: corpus-fence retry, polled 2 min ago, visible again 1 min ago.
		{"corpus fence polled 2 min ago", 2 * time.Minute, time.Minute, 10 * time.Minute, runtimeFenceCorpusNotReadyError{}},
		// c: counting retry, attempted 1 min ago, visible in 10 min.
		{"counting backoff until now+10 min", time.Minute, 11 * time.Minute, 10 * time.Minute, runtimeFenceRetryError{}},
		// d: claimed 1 min ago, lease still live.
		{"claimed 1 min ago", time.Minute, time.Second, 10 * time.Minute, nil},
		// A3 b: claimed 30 min ago, lease expired 29 min ago, no heartbeat.
		{"lease expired 29 min ago", 30 * time.Minute, time.Second, time.Minute, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
			runtimeFenceProducerAttempt(t, ctx, p, enqueued, tc.claimedAgo, tc.delay, tc.lease, tc.cause)
			queue := runtimeFenceQueue(p.db, "catalog-young", reducer.DomainServiceCatalogCorrelation, nil)
			intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
			requireRuntimeFenceDeferral(t, ctx, p, queue, intent, handleErr)
		})
	}
}

// TestReducerContentionGateServiceCatalogRuntimeFenceConvergesAtTheElapsedBound
// is the no-wedge proof (amendment A2 proofs b, c second half, d first half,
// e first half, and the ack half of 4e): a producer row no worker has touched
// for 31 minutes or more cannot hold a one-minute-old catalog row. Past the
// bound the handler commits what it reads (here an empty family: the
// producer never wrote) and the Ack succeeds.
func TestReducerContentionGateServiceCatalogRuntimeFenceConvergesAtTheElapsedBound(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name                                 string
		createdAgo, claimedAgo, delay, lease time.Duration
		cause                                error
		neverClaimed                         bool
	}{
		// e: pending, never claimed, enqueued 31 min ago.
		{name: "pending never claimed 31 min", createdAgo: 31 * time.Minute, neverClaimed: true},
		// b: corpus-fence retry starved: polled 46 min ago, visible 45 min ago.
		{
			name: "corpus fence starved 45 min", createdAgo: 46 * time.Minute, claimedAgo: 46 * time.Minute,
			delay: time.Minute, lease: time.Hour, cause: runtimeFenceCorpusNotReadyError{},
		},
		// c: counting retry visible 31 min ago, unclaimed since.
		{
			name: "counting retry visible 31 min unclaimed", createdAgo: 45 * time.Minute, claimedAgo: 32 * time.Minute,
			delay: time.Minute, lease: time.Hour, cause: runtimeFenceRetryError{},
		},
		// A3 b: claimed 32 min ago, lease expired 31 min ago, no heartbeat.
		{
			name: "lease expired 31 min ago", createdAgo: 40 * time.Minute, claimedAgo: 32 * time.Minute,
			delay: time.Second, lease: time.Minute,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
			if tc.neverClaimed {
				setRuntimeFenceProducerCreated(t, ctx, p, tc.createdAgo, "pending")
			} else {
				runtimeFenceProducerAttempt(t, ctx, p, tc.createdAgo, tc.claimedAgo, tc.delay, tc.lease, tc.cause)
			}
			queue := runtimeFenceQueue(p.db, "catalog-young", reducer.DomainServiceCatalogCorrelation, nil)
			intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
			requireRuntimeFenceCommit(t, ctx, p, queue, intent, handleErr)
		})
	}
}

// runtimeFenceProducerLease reads the producer row's last claim and lease.
func runtimeFenceProducerLease(t *testing.T, ctx context.Context, p *runtimeFenceProof) (time.Time, time.Time) {
	t.Helper()
	var lastAttempt, claimUntil time.Time
	if err := p.db.QueryRowContext(ctx,
		`SELECT last_attempt_at, claim_until FROM fact_work_items WHERE work_item_id = $1`,
		runtimeFenceProducerItem).Scan(&lastAttempt, &claimUntil); err != nil {
		t.Fatalf("read producer lease: %v", err)
	}
	return lastAttempt, claimUntil
}

// TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerLeaseIsLive
// is amendment A3 proofs a and d. Heartbeat writes only claim_until and
// updated_at, so a producer run that started 31 minutes ago and still
// heartbeats has last_attempt_at 31 minutes old; the A2 anchor read it as
// past the bound and committed an empty family. claim_until is the fourth
// anchor term: a live lease keeps the catalog deferring. A second owner that
// reclaims an expired lease refreshes last_attempt_at and claim_until, so it
// defers again.
func TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerLeaseIsLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()

	t.Run("claimed 31 min ago, heartbeated now", func(t *testing.T) {
		p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
		// The lease must still be live for the heartbeat to be accepted, as
		// it is for a worker that heartbeats every 30 s.
		claimClock := time.Now().UTC().Add(-31 * time.Minute)
		producerIntent := runtimeFenceProducerAttempt(t, ctx, p, 45*time.Minute, 31*time.Minute, time.Second,
			31*time.Minute+30*time.Second, nil)
		heartbeat := runtimeFenceQueue(p.db, "workload-worker", reducer.DomainWorkloadMaterialization, nil)
		if err := heartbeat.Heartbeat(ctx, producerIntent); err != nil {
			t.Fatalf("producer Heartbeat() error = %v", err)
		}
		lastAttempt, claimUntil := runtimeFenceProducerLease(t, ctx, p)
		if lastAttempt.After(claimClock.Add(time.Minute)) || time.Until(claimUntil) < 30*time.Second {
			t.Fatalf("producer row last_attempt_at %s ago, claim_until in %s; want about 31 min ago and about 1 min ahead",
				time.Since(lastAttempt), time.Until(claimUntil))
		}
		queue := runtimeFenceQueue(p.db, "catalog-young", reducer.DomainServiceCatalogCorrelation, nil)
		intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
		requireRuntimeFenceDeferral(t, ctx, p, queue, intent, handleErr)
	})

	t.Run("reclaimed by a second owner after the lease expired", func(t *testing.T) {
		p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
		// The first owner claimed 40 min ago and died: its lease expired 39
		// min ago.
		runtimeFenceProducerAttempt(t, ctx, p, 45*time.Minute, 40*time.Minute, time.Second, time.Minute, nil)
		second := runtimeFenceQueue(p.db, "workload-worker-b", reducer.DomainWorkloadMaterialization, nil)
		second.LeaseDuration = 10 * time.Minute
		if _, claimed, err := second.Claim(ctx); err != nil || !claimed {
			t.Fatalf("second owner Claim() = %v, %v, want the expired lease reclaimed", claimed, err)
		}
		lastAttempt, claimUntil := runtimeFenceProducerLease(t, ctx, p)
		if time.Since(lastAttempt) > time.Minute || time.Until(claimUntil) < 9*time.Minute {
			t.Fatalf("reclaimed producer last_attempt_at %s ago, claim_until in %s; want now and about 10 min ahead",
				time.Since(lastAttempt), time.Until(claimUntil))
		}
		queue := runtimeFenceQueue(p.db, "catalog-young", reducer.DomainServiceCatalogCorrelation, nil)
		intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
		requireRuntimeFenceDeferral(t, ctx, p, queue, intent, handleErr)
	})
}

// TestReducerContentionGateServiceCatalogRuntimeFenceReopenedProducerWaits is
// amendment A2 proof e (second half) on the real queue: the producer row
// succeeded two hours ago and ReopenSucceeded reopened it one minute ago.
// reopened_at (and the reopen's visible_at) start a fresh bound, so even a
// 45-minute-old catalog row defers.
func TestReducerContentionGateServiceCatalogRuntimeFenceReopenedProducerWaits(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	p := newRuntimeFenceProof(t, ctx, dsn, 45*time.Minute)
	setRuntimeFenceProducerCreated(t, ctx, p, 2*time.Hour, "succeeded")
	oneMinuteAgo := func() time.Time { return time.Now().UTC().Add(-time.Minute) }
	producerQueue := runtimeFenceQueue(p.db, "workload-reopen", reducer.DomainWorkloadMaterialization, oneMinuteAgo)
	reopened, err := producerQueue.ReopenSucceeded(ctx, runtimeFenceProducerItem)
	if err != nil || !reopened {
		t.Fatalf("ReopenSucceeded() = %v, %v, want the succeeded producer row reopened", reopened, err)
	}
	queue := runtimeFenceQueue(p.db, "catalog-old", reducer.DomainServiceCatalogCorrelation, nil)
	intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
	requireRuntimeFenceDeferral(t, ctx, p, queue, intent, handleErr)
}

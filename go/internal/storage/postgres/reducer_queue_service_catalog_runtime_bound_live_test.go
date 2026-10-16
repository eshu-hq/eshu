// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// Real-queue elapsed-bound proofs for the #7816 runtime fence (amendment A1).
// The bound is measured from the producer row's own cycle start,
// COALESCE(reopened_at, created_at), as the real producer readiness store
// reads it; the catalog row's age never decides. Every queue transition here
// is production SQL: Claim, Fail (retry), ReopenSucceeded, and Ack.

// runtimeFenceRetryError is a retryable, counting producer failure, the shape
// a workload_materialization handler returns on a transient graph error.
type runtimeFenceRetryError struct{}

func (runtimeFenceRetryError) Error() string        { return "transient graph write failure" }
func (runtimeFenceRetryError) Retryable() bool      { return true }
func (runtimeFenceRetryError) FailureClass() string { return "workload_materialization_transient" }

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

// TestReducerContentionGateServiceCatalogRuntimeFenceBoundsOnProducerCycle is
// proof 4a on the real queue: a catalog row created 45 minutes ago meets a
// producer row whose cycle started one minute ago. The handler defers with
// nothing written; the consumer's age does not open the bound.
func TestReducerContentionGateServiceCatalogRuntimeFenceBoundsOnProducerCycle(t *testing.T) {
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

// TestReducerContentionGateServiceCatalogRuntimeFenceConvergesAtTheElapsedBound
// is proof 4b and the ack half of 4e: a producer wedged in flight for 31
// minutes from its own cycle start cannot hold a one-minute-old catalog row.
// The retrying case goes through the real retry path, which moves updated_at
// to one minute ago but leaves created_at at 31 minutes. Past the bound the
// handler commits what it reads (here an empty family: the producer never
// wrote) and the Ack succeeds.
func TestReducerContentionGateServiceCatalogRuntimeFenceConvergesAtTheElapsedBound(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	wedged := 31 * time.Minute

	for _, name := range []string{"pending", "retrying"} {
		t.Run(name, func(t *testing.T) {
			p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
			setRuntimeFenceProducerCreated(t, ctx, p, wedged, "pending")
			if name == "retrying" {
				oneMinuteAgo := func() time.Time { return time.Now().UTC().Add(-time.Minute) }
				producerQueue := runtimeFenceQueue(p.db, "workload-worker", reducer.DomainWorkloadMaterialization, oneMinuteAgo)
				// The clock runs a minute behind, so the lease must outlast it.
				producerQueue.LeaseDuration = 10 * time.Minute
				producerIntent, claimed, err := producerQueue.Claim(ctx)
				if err != nil || !claimed {
					t.Fatalf("producer Claim() = %v, %v", claimed, err)
				}
				if err := producerQueue.Fail(ctx, producerIntent, runtimeFenceRetryError{}); err != nil {
					t.Fatalf("producer Fail() error = %v", err)
				}
				var status string
				var created, updated time.Time
				if err := p.db.QueryRowContext(ctx,
					`SELECT status, created_at, updated_at FROM fact_work_items WHERE work_item_id = $1`,
					runtimeFenceProducerItem).Scan(&status, &created, &updated); err != nil {
					t.Fatalf("read producer row: %v", err)
				}
				if status != "retrying" || time.Since(created) < wedged || time.Since(updated) > 2*time.Minute {
					t.Fatalf("producer row = %s created %s ago updated %s ago, want retrying, created >= %s, updated about 1m ago",
						status, time.Since(created), time.Since(updated), wedged)
				}
			}
			queue := runtimeFenceQueue(p.db, "catalog-young", reducer.DomainServiceCatalogCorrelation, nil)
			intent, handleErr := claimAndHandleRuntimeFence(t, ctx, p, queue)
			if handleErr != nil {
				t.Fatalf("Handle() error = %v, want nil: the producer is past its own bound", handleErr)
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
		})
	}
}

// TestReducerContentionGateServiceCatalogRuntimeFenceReopenedProducerWaits is
// proof 4d on the real queue: the producer row succeeded two hours ago and
// ReopenSucceeded reopened it one minute ago. reopened_at starts a fresh
// producer cycle, so even a 45-minute-old catalog row defers.
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

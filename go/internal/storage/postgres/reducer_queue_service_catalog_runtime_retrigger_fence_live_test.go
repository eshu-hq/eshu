// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8169 disposable PostgreSQL 18 proof that a pending repo_dependency RUNS_ON intent holds the service catalog runtime fence with nothing written, and that two concurrent workload_materialization ACKs on separate connections reopen the catalog row once and produce one catalog run. Local run about 3s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// TestReducerContentionGateServiceCatalogFenceWaitsOnRunsOnIntent is proof
// (c): the workload producer is terminal but a repo_dependency RUNS_ON intent
// for the repository is still pending, so the catalog defers with nothing
// written. Once the repo_dependency runner marks the intent completed and its
// workload replay acks, the catalog commits the runtime rows.
func TestReducerContentionGateServiceCatalogFenceWaitsOnRunsOnIntent(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)
	p.addProducer(t, ctx, "wm-checkout-8169")
	p.runProducer(t, ctx, p.db, "workload-worker")

	const intentID = "repo-dependency-runs-on-8169"
	if _, err := p.db.ExecContext(ctx, `
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, payload, created_at
) VALUES ($1, 'repo_dependency', 'runs_on:repo-checkout->platform:k8s', $2, $3, $3, 'run-8169', $4,
    jsonb_build_object('repo_id', $3::text, 'relationship_type', 'RUNS_ON', 'platform_id', 'platform:k8s'), $5)`,
		intentID, runtimeFenceRepoScope, runtimeFenceRepoID, runtimeFenceRepoGen, p.clock.Add(-time.Minute),
	); err != nil {
		t.Fatalf("seed RUNS_ON intent: %v", err)
	}

	catalogQueue := p.queue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation)
	intent, claimed, err := catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("catalog Claim() claimed=%v err=%v", claimed, err)
	}
	_, handleErr := p.handler(p.db).Handle(ctx, intent)
	var classified interface{ FailureClass() string }
	if !errors.As(handleErr, &classified) || classified.FailureClass() != servicecatalog.RuntimeNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want the runtime readiness deferral while the RUNS_ON intent is pending", handleErr)
	}
	if generations := p.serviceGenerations(t, ctx, retriggerCheckoutSvc); len(generations) != 0 {
		t.Fatalf("service generations = %v, want none written while the fence is open", generations)
	}
	if err := catalogQueue.Fail(ctx, intent, handleErr); err != nil {
		t.Fatalf("catalog Fail() error = %v", err)
	}

	// The repo_dependency runner's order: write RUNS_ON, replay the workload
	// materialization, then mark the intents completed.
	producerQueue := p.queue(p.db, "replayer", reducer.DomainWorkloadMaterialization)
	if reopened, err := producerQueue.ReopenSucceeded(ctx, "wm-checkout-8169"); err != nil || !reopened {
		t.Fatalf("ReopenSucceeded() = %v, %v", reopened, err)
	}
	if _, err := NewSharedIntentStore(SQLDB{DB: p.db}).MarkIntentsCompleted(ctx, []string{intentID}, p.clock); err != nil {
		t.Fatalf("MarkIntentsCompleted() error = %v", err)
	}
	p.runProducer(t, ctx, p.db, "workload-worker")
	p.clock = p.clock.Add(2 * time.Second)

	p.runCatalog(t, ctx, "catalog run after the RUNS_ON intent completed")
	g := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerCheckoutSvc))
	if g == "" || p.runtimeSnapshotCount(t, ctx, g) != 2 {
		t.Fatalf("checkout active generation %q runtime rows = %d, want 2", g, p.runtimeSnapshotCount(t, ctx, g))
	}
}

// TestReducerContentionGateServiceCatalogConcurrentAcksReopenOnce is proof
// (d) and the concurrency proof: two workload_materialization rows of the
// repository ack at the same time on separate connections against one
// succeeded catalog row. Neither ack fails or deadlocks, the catalog row is
// reopened exactly once, and one catalog run commits one new generation.
func TestReducerContentionGateServiceCatalogConcurrentAcksReopenOnce(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)
	p.runCatalog(t, ctx, "first catalog run (no producer)")
	g1 := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerCheckoutSvc))

	owners := []string{"workload-worker-a", "workload-worker-b"}
	intents := make([]reducer.Intent, len(owners))
	queues := make([]ReducerQueue, len(owners))
	for i, owner := range owners {
		p.addProducer(t, ctx, "wm-checkout-8169-"+owner)
		conn := openReducerFairnessClaimerDB(t, ctx, p.dsn, p.schema)
		queues[i] = p.queue(conn, owner, reducer.DomainWorkloadMaterialization)
		intent, claimed, err := queues[i].Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("%s Claim() claimed=%v err=%v", owner, claimed, err)
		}
		intents[i] = intent
	}
	p.graph.committed.Store(true)

	var reopenedAt time.Time
	var wg sync.WaitGroup
	errs := make(chan error, len(owners))
	start := make(chan struct{})
	for i := range owners {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- queues[i].Ack(ctx, intents[i], reducer.Result{})
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent workload Ack() error = %v", err)
		}
	}
	if err := p.db.QueryRowContext(ctx,
		`SELECT reopened_at FROM fact_work_items WHERE work_item_id = $1 AND status = 'pending'`,
		runtimeFenceCatalogItem).Scan(&reopenedAt); err != nil {
		t.Fatalf("catalog row not pending after two acks: %v", err)
	}

	p.runCatalog(t, ctx, "single re-triggered catalog run")
	catalogQueue := p.queue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation)
	if _, claimed, err := catalogQueue.Claim(ctx); err != nil || claimed {
		t.Fatalf("second catalog Claim() claimed=%v err=%v, want nothing left after one run", claimed, err)
	}
	checkout := p.serviceGenerations(t, ctx, retriggerCheckoutSvc)
	if len(checkout) != 2 || checkout[g1] != "superseded" {
		t.Fatalf("checkout generations = %v, want G1 superseded plus exactly one new generation", checkout)
	}
}

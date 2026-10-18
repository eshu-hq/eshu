// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8169 disposable PostgreSQL 18 proof that a workload_materialization ACK re-triggers the service_catalog_correlation rows whose decisions name its repository: a succeeded row reopens, a claimed row is flagged for replay, and the next catalog run commits a generation carrying the runtime family. Local run about 3s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerContentionGateServiceCatalogRetriggerRepairsNoProducerGeneration
// is proof (a): the catalog commits generation G1 at no_producer (empty
// runtime family), then the repository's workload_materialization row appears,
// runs, and acks through the real queue. The ack reopens the catalog row, the
// next run commits G2 with both runtime rows and supersedes G1, and the
// unchanged repository's service keeps its generation with no runtime delta.
func TestReducerContentionGateServiceCatalogRetriggerRepairsNoProducerGeneration(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)

	p.runCatalog(t, ctx, "first catalog run (no producer)")
	g1 := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerCheckoutSvc))
	b1 := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerBillingSvc))
	if g1 == "" || b1 == "" || p.runtimeSnapshotCount(t, ctx, g1) != 0 {
		t.Fatalf("G1 checkout=%q billing=%q runtime rows=%d, want two active generations and an empty runtime family",
			g1, b1, p.runtimeSnapshotCount(t, ctx, g1))
	}

	p.addProducer(t, ctx, "wm-checkout-8169")
	p.runProducer(t, ctx, p.db, "workload-worker")
	if status, _ := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "pending" {
		t.Fatalf("catalog row after the workload ack = %s, want pending (re-triggered)", status)
	}

	p.runCatalog(t, ctx, "re-triggered catalog run")
	checkout := p.serviceGenerations(t, ctx, retriggerCheckoutSvc)
	g2 := retriggerActiveGeneration(checkout)
	if g2 == "" || g2 == g1 || checkout[g1] != "superseded" {
		t.Fatalf("checkout generations = %v, want G1 %s superseded and a new active G2", checkout, g1)
	}
	if n := p.runtimeSnapshotCount(t, ctx, g2); n != 2 {
		t.Fatalf("G2 runtime rows = %d, want 2", n)
	}
	if added := p.runtimeAdded(t, ctx, retriggerCheckoutSvc, g1); added != 2 {
		t.Fatalf("checkout runtime added since G1 = %d, want 2", added)
	}
	billing := p.serviceGenerations(t, ctx, retriggerBillingSvc)
	if len(billing) != 1 || retriggerActiveGeneration(billing) != b1 {
		t.Fatalf("billing generations = %v, want only %s, unchanged", billing, b1)
	}
	if added := p.runtimeAdded(t, ctx, retriggerBillingSvc, b1); added != 0 {
		t.Fatalf("billing runtime added = %d, want 0 for the unchanged repository", added)
	}
}

// TestReducerContentionGateServiceCatalogRetriggerFlagsClaimedRow is proof
// (b): the catalog row is claimed, and its run already read the empty runtime
// family, when the workload ack lands. The ack sets the replay flag, the
// catalog's own ack becomes pending through the migration 093 trigger, and the
// next run commits the runtime rows.
func TestReducerContentionGateServiceCatalogRetriggerFlagsClaimedRow(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)
	catalogQueue := p.queue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation)
	intent, claimed, err := catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("catalog Claim() claimed=%v err=%v", claimed, err)
	}
	if _, err := p.handler(p.db).Handle(ctx, intent); err != nil {
		t.Fatalf("catalog Handle() error = %v", err)
	}
	g1 := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerCheckoutSvc))

	p.addProducer(t, ctx, "wm-checkout-8169")
	p.runProducer(t, ctx, p.db, "workload-worker")
	if status, flagged := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "claimed" || !flagged {
		t.Fatalf("catalog row during its run = %s flagged=%v, want claimed and flagged for replay", status, flagged)
	}
	if err := catalogQueue.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("catalog Ack() error = %v", err)
	}
	if status, flagged := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "pending" || flagged {
		t.Fatalf("catalog row after its ack = %s flagged=%v, want pending and unflagged", status, flagged)
	}

	p.runCatalog(t, ctx, "replayed catalog run")
	checkout := p.serviceGenerations(t, ctx, retriggerCheckoutSvc)
	g2 := retriggerActiveGeneration(checkout)
	if g2 == g1 || checkout[g1] != "superseded" || p.runtimeSnapshotCount(t, ctx, g2) != 2 {
		t.Fatalf("checkout generations = %v (G1 %s), want a new active G2 with 2 runtime rows", checkout, g1)
	}
}

// TestReducerContentionGateServiceCatalogRetriggerFlagsFirstRunWithoutDecisions
// covers the case the decision-fact map cannot see: a first catalog run that
// is claimed but has written no reducer_service_catalog_correlation decision
// yet when the workload ack lands. Its repository set is unknown, so the ack
// must flag it conservatively; its ack then returns it to pending and the
// replay commits the runtime rows.
func TestReducerContentionGateServiceCatalogRetriggerFlagsFirstRunWithoutDecisions(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)
	catalogQueue := p.queue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation)
	intent, claimed, err := catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("catalog Claim() claimed=%v err=%v", claimed, err)
	}
	var decisions int
	if err := p.db.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_records WHERE fact_kind = 'reducer_service_catalog_correlation'`,
	).Scan(&decisions); err != nil || decisions != 0 {
		t.Fatalf("decision facts before the first run = %d (err %v), want 0", decisions, err)
	}

	p.addProducer(t, ctx, "wm-checkout-8169")
	p.runProducer(t, ctx, p.db, "workload-worker")
	if status, flagged := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "claimed" || !flagged {
		t.Fatalf("first-run catalog row after the workload ack = %s flagged=%v, want claimed and flagged", status, flagged)
	}
	if _, err := p.handler(p.db).Handle(ctx, intent); err != nil {
		t.Fatalf("catalog Handle() error = %v", err)
	}
	if err := catalogQueue.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("catalog Ack() error = %v", err)
	}
	if status, _ := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "pending" {
		t.Fatalf("first-run catalog row after its ack = %s, want pending (replay owed)", status)
	}
	p.runCatalog(t, ctx, "replayed first run")
	g := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerCheckoutSvc))
	if n := p.runtimeSnapshotCount(t, ctx, g); n != 2 {
		t.Fatalf("checkout active generation runtime rows = %d, want 2", n)
	}
}

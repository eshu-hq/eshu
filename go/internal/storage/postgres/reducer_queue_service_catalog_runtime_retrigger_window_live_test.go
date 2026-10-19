// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8169 disposable PostgreSQL 18 regression proof that a workload_materialization ACK landing between a catalog re-run's runtime read and its decision write flags that in-flight run, even when the scope's older decisions do not name the repository; the run's ACK returns it to pending instead of committing an empty runtime family. Local run about 2s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// retriggerBillingOnlyLoader serves only the billing service's catalog facts,
// so a first catalog run writes decisions that name repo-billing and never
// repo-checkout.
type retriggerBillingOnlyLoader struct{ retriggerFactLoader }

func (l retriggerBillingOnlyLoader) ListFacts(ctx context.Context, scopeID, generationID string) ([]facts.Envelope, error) {
	all, err := l.retriggerFactLoader.ListFacts(ctx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	var out []facts.Envelope
	for _, envelope := range all {
		if strings.HasSuffix(envelope.FactID, retriggerBillingSvc) {
			out = append(out, envelope)
		}
	}
	return out, nil
}

// retriggerWindowLoader runs hook once, right after the catalog run's runtime
// read returns and before the handler writes its decisions: the window the
// in-flight re-trigger branch must cover.
type retriggerWindowLoader struct {
	inner *runtimeFenceGraph
	once  sync.Once
	hook  func()
}

func (w *retriggerWindowLoader) GetRuntimeInstancesForRepos(
	ctx context.Context,
	repositoryIDs []string,
) (map[string][]servicecatalog.ServiceRuntimeInstance, error) {
	out, err := w.inner.GetRuntimeInstancesForRepos(ctx, repositoryIDs)
	w.once.Do(w.hook)
	return out, err
}

// TestReducerContentionGateServiceCatalogRetriggerFlagsRunThatNewlyNamesRepo
// is the #8169 review regression (F1): the catalog scope's first run names
// only billing. The row is reopened and the re-run newly names checkout, which
// has no producer row at fence time. The checkout workload row appears, runs,
// and ACKs after the re-run's (empty) runtime read and before its decision
// write. The scope's committed decisions do not name checkout, so a rule keyed
// on decisions misses the run; the claimed/running branch must be global. The
// run's own ACK must return it to pending, and the replay commits both
// runtime rows.
func TestReducerContentionGateServiceCatalogRetriggerFlagsRunThatNewlyNamesRepo(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)
	catalogQueue := p.queue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation)

	intent, claimed, err := catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("first catalog Claim() claimed=%v err=%v", claimed, err)
	}
	first := p.handler(p.db)
	first.FactLoader = retriggerBillingOnlyLoader{}
	if _, err := first.Handle(ctx, intent); err != nil {
		t.Fatalf("first catalog Handle() error = %v", err)
	}
	if err := catalogQueue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("first catalog Ack() error = %v", err)
	}
	if reopened, err := catalogQueue.ReopenSucceeded(ctx, runtimeFenceCatalogItem); err != nil || !reopened {
		t.Fatalf("ReopenSucceeded() reopened=%v err=%v, want the catalog row reopened", reopened, err)
	}

	intent, claimed, err = catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("second catalog Claim() claimed=%v err=%v", claimed, err)
	}
	second := p.handler(p.db)
	second.RuntimeInstanceLoader = &retriggerWindowLoader{inner: p.graph, hook: func() {
		p.addProducer(t, ctx, "wm-checkout-window-8169")
		p.runProducer(t, ctx, p.db, "workload-worker")
	}}
	if _, err := second.Handle(ctx, intent); err != nil {
		t.Fatalf("second catalog Handle() error = %v", err)
	}
	if status, flagged := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "claimed" || !flagged {
		t.Fatalf("catalog row after the in-window workload ack = %s flagged=%v, want claimed and flagged for replay",
			status, flagged)
	}
	if err := catalogQueue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("second catalog Ack() error = %v", err)
	}
	if status, _ := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "pending" {
		t.Fatalf("catalog row after its ack = %s, want pending: the run committed runtime read before the producer ack", status)
	}

	p.runCatalog(t, ctx, "replayed catalog run")
	g := retriggerActiveGeneration(p.serviceGenerations(t, ctx, retriggerCheckoutSvc))
	if n := p.runtimeSnapshotCount(t, ctx, g); n != 2 {
		t.Fatalf("checkout active generation runtime rows = %d, want 2 after the replay", n)
	}
}

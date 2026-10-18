// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7816 disposable PostgreSQL 18 proof that a workload_materialization producer acking between the runtime fence check and the read still defers the catalog intent, and that three catalog workers racing one producer on separate connections never commit an empty runtime family; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local runs about 1s each on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// completingReadiness wraps the real store and, right after the readiness
// query answers, lets the producer finish (graph write, then Ack) before the
// handler acts on the answer: the "producer completes between the fence
// check and the read" interleave.
type completingReadiness struct {
	store    producerreadiness.Store
	complete func()
	once     sync.Once
}

func (c *completingReadiness) RuntimeProducerStates(
	ctx context.Context,
	repoIDs []string,
	domains []reducercontract.Domain,
) (map[string]servicecatalog.RuntimeProducerState, error) {
	states, err := c.store.RuntimeProducerStates(ctx, repoIDs, domains)
	c.once.Do(c.complete)
	return states, err
}

// TestReducerContentionGateServiceCatalogRuntimeFenceProducerCompletesMidCheck
// is proof 5a. The fence saw the producer in flight; the producer then
// commits and acks before the handler acts. The handler still defers on the
// answer it captured (it never reads runtime evidence after an in-flight
// answer), and the next claim reads the committed instances. Checking after
// the read instead would have let an empty read pass.
func TestReducerContentionGateServiceCatalogRuntimeFenceProducerCompletesMidCheck(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
	producerQueue := runtimeFenceQueue(p.db, "workload-worker", reducer.DomainWorkloadMaterialization, nil)
	producerIntent, claimed, err := producerQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("producer Claim() = %v, %v", claimed, err)
	}
	handler := p.handler(p.db)
	handler.RuntimeProducerReadiness = &completingReadiness{
		store: producerreadiness.Store{DB: SQLDB{DB: p.db}},
		complete: func() {
			p.graph.committed.Store(true)
			if err := producerQueue.Ack(ctx, producerIntent, reducer.Result{}); err != nil {
				t.Errorf("producer Ack() error = %v", err)
			}
		},
	}
	catalogQueue := runtimeFenceQueue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation, nil)
	intent, claimed, err := catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("catalog Claim() = %v, %v", claimed, err)
	}
	_, handleErr := handler.Handle(ctx, intent)
	var classified interface{ FailureClass() string }
	if !errors.As(handleErr, &classified) || classified.FailureClass() != servicecatalog.RuntimeNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want the deferral captured before the producer finished", handleErr)
	}
	if reads := p.graph.reads.Load(); reads != 0 {
		t.Fatalf("graph reads = %d, want 0 on a pass whose fence answered in flight", reads)
	}
	if err := catalogQueue.Fail(ctx, intent, handleErr); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if _, err := p.db.ExecContext(ctx, `UPDATE fact_work_items SET next_attempt_at = NULL, visible_at = NULL WHERE work_item_id = $1`,
		runtimeFenceCatalogItem); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	intent, claimed, err = catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("retry Claim() = %v, %v", claimed, err)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("retry Handle() error = %v, want nil after the producer acked", err)
	}
	if writes := p.materialization.snapshot(); len(writes) != 1 || len(writes[0].Runtime) != 2 {
		t.Fatalf("materialization runtime rows = %v, want one write with 2 rows", runtimeRowCounts(writes))
	}
}

// TestReducerContentionGateServiceCatalogRuntimeFenceConcurrentWorkers is
// proof 5b. Three catalog workers and one producer worker, each on its own
// Postgres connection, race on the real queue. Whatever the interleaving, no
// service generation commits with an empty runtime family, exactly one
// generation commits, the catalog row never spends its retry budget, and
// both rows end succeeded.
func TestReducerContentionGateServiceCatalogRuntimeFenceConcurrentWorkers(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)

	producerDB := openReducerFairnessClaimerDB(t, ctx, dsn, p.schema)
	catalogDBs := []*sql.DB{
		openReducerFairnessClaimerDB(t, ctx, dsn, p.schema),
		openReducerFairnessClaimerDB(t, ctx, dsn, p.schema),
		openReducerFairnessClaimerDB(t, ctx, dsn, p.schema),
	}
	var deferrals atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	wg.Add(1)
	go func() {
		defer wg.Done()
		db := producerDB
		queue := runtimeFenceQueue(db, "workload-worker", reducer.DomainWorkloadMaterialization, nil)
		for ctx.Err() == nil {
			intent, claimed, err := queue.Claim(ctx)
			if err != nil {
				errs <- err
				return
			}
			if !claimed {
				time.Sleep(time.Millisecond)
				continue
			}
			time.Sleep(time.Duration(rand.IntN(30)) * time.Millisecond) // graph write time
			p.graph.committed.Store(true)
			if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
				errs <- err
			}
			return
		}
	}()
	var done sync.Once
	finished := make(chan struct{})
	for worker, db := range catalogDBs {
		wg.Add(1)
		go func(worker int, db *sql.DB) {
			defer wg.Done()
			queue := runtimeFenceQueue(db, "catalog-worker", reducer.DomainServiceCatalogCorrelation, nil)
			queue.LeaseOwner = fmt.Sprintf("%s-%c", queue.LeaseOwner, 'a'+worker)
			queue.RetryDelay = time.Millisecond
			handler := p.handler(db)
			for {
				select {
				case <-finished:
					return
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				default:
				}
				intent, claimed, err := queue.Claim(ctx)
				if err != nil {
					errs <- err
					return
				}
				if !claimed {
					time.Sleep(time.Millisecond)
					continue
				}
				if _, handleErr := handler.Handle(ctx, intent); handleErr != nil {
					deferrals.Add(1)
					if err := queue.Fail(ctx, intent, handleErr); err != nil {
						errs <- err
						return
					}
					continue
				}
				if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
					errs <- err
					return
				}
				done.Do(func() { close(finished) })
				return
			}
		}(worker, db)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("worker error: %v", err)
	}
	t.Logf("catalog deferrals before the producer acked: %d", deferrals.Load())
	writes := p.materialization.snapshot()
	if len(writes) != 1 || len(writes[0].Runtime) != 2 {
		t.Fatalf("materialization runtime rows = %v, want exactly one write with 2 rows: an empty runtime family committed", runtimeRowCounts(writes))
	}
	for _, item := range []string{runtimeFenceCatalogItem, runtimeFenceProducerItem} {
		if status, _, attempts := readRuntimeFenceItem(t, ctx, p.db, item); status != "succeeded" || attempts != 1 {
			t.Fatalf("%s terminal = %s attempts=%d, want succeeded at 1", item, status, attempts)
		}
	}
}

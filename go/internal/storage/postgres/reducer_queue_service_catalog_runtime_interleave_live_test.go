// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7816 disposable PostgreSQL 18 proof that a workload_materialization producer acking between the runtime fence check and the read still defers the catalog intent, and that three catalog workers racing one producer on separate connections produce complete mock materialization calls and settle the real queue, including one owed replay; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local runs about 1s each on postgres:18; wall time is logged, not gated
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
			if err := producerQueue.Ack(ctx, producerIntent, reducercontract.Result{}); err != nil {
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
// materialization call omits a runtime instance. One producer acknowledgement
// can require one catalog replay, so this proof checks every call and the
// durable queue state after all workers finish.
func TestReducerContentionGateServiceCatalogRuntimeFenceConcurrentWorkers(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	t.Run("unforced", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)

		producerDB := openReducerFairnessClaimerDB(t, ctx, p.targetDSN)
		catalogDBs := []*sql.DB{
			openReducerFairnessClaimerDB(t, ctx, p.targetDSN),
			openReducerFairnessClaimerDB(t, ctx, p.targetDSN),
			openReducerFairnessClaimerDB(t, ctx, p.targetDSN),
		}
		var deferrals atomic.Int64
		var wg sync.WaitGroup
		successfulWorkers := make(chan int, len(catalogDBs))
		var traceMu sync.Mutex
		var trace []string
		record := func(worker int, phase string) {
			traceMu.Lock()
			trace = append(trace, fmt.Sprintf("worker-%d:%s", worker, phase))
			traceMu.Unlock()
		}
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
				if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
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
					record(worker, "claimed")
					if _, handleErr := handler.Handle(ctx, intent); handleErr != nil {
						var classified interface{ FailureClass() string }
						if !errors.As(handleErr, &classified) || classified.FailureClass() != servicecatalog.RuntimeNotReadyFailureClass {
							errs <- fmt.Errorf("catalog worker %d unexpected Handle error: %w", worker, handleErr)
							return
						}
						deferrals.Add(1)
						record(worker, "deferred:"+classified.FailureClass())
						if err := queue.Fail(ctx, intent, handleErr); err != nil {
							errs <- err
							return
						}
						record(worker, "failed")
						continue
					}
					record(worker, "handled")
					if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
						errs <- err
						return
					}
					record(worker, "acked")
					successfulWorkers <- worker
					done.Do(func() { close(finished) })
					return
				}
			}(worker, db)
		}
		wg.Wait()
		close(errs)
		close(successfulWorkers)
		for err := range errs {
			t.Fatalf("worker error: %v", err)
		}
		t.Logf("catalog deferrals before the producer acked: %d", deferrals.Load())
		owners := make([]int, 0, 2)
		for worker := range successfulWorkers {
			owners = append(owners, worker)
		}
		writes := p.materialization.snapshot()
		if status, _, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceProducerItem); status != "succeeded" || attempts != 1 {
			t.Fatalf("%s terminal = %s attempts=%d, want succeeded at 1", runtimeFenceProducerItem, status, attempts)
		}
		// #8169 can return the first successful catalog ACK to pending. A peer
		// may drain that one replay before the first worker reports completion.
		status, _, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceCatalogItem)
		switch {
		case status == "succeeded" && attempts == 1:
			if len(writes) != len(owners) || len(owners) < 1 || len(owners) > 2 {
				t.Fatalf("catalog writes=%d successful workers=%v, want one execution plus at most one replay", len(writes), owners)
			}
			if len(owners) == 2 {
				var reopenedAt sql.NullTime
				if err := p.db.QueryRowContext(ctx, `SELECT reopened_at FROM fact_work_items WHERE work_item_id = $1`, runtimeFenceCatalogItem).Scan(&reopenedAt); err != nil {
					t.Fatalf("read catalog replay marker: %v", err)
				}
				if !reopenedAt.Valid || owners[0] == owners[1] {
					t.Fatalf("two catalog writes lack an owed peer replay: workers=%v reopened=%v", owners, reopenedAt.Valid)
				}
			}
		case status == "pending" && attempts == 0:
			if len(owners) != 1 {
				t.Fatalf("catalog pending replay has successful workers %v, want one", owners)
			}
			assertRuntimeFenceWrites(t, writes, 1)
			queue := runtimeFenceQueue(p.db, "catalog-replay", reducer.DomainServiceCatalogCorrelation, nil)
			intent, claimed, err := queue.Claim(ctx)
			if err != nil || !claimed {
				t.Fatalf("replay Claim() claimed=%v err=%v", claimed, err)
			}
			if _, err := p.handler(p.db).Handle(ctx, intent); err != nil {
				t.Fatalf("replay Handle() error = %v", err)
			}
			if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
				t.Fatalf("replay Ack() error = %v", err)
			}
			writes = p.materialization.snapshot()
		default:
			t.Fatalf("%s terminal = %s attempts=%d, want succeeded at 1 or pending at 0 (re-triggered)", runtimeFenceCatalogItem, status, attempts)
		}
		wantWrites := 1
		if status == "pending" || len(owners) == 2 {
			wantWrites = 2
		}
		assertRuntimeFenceWrites(t, writes, wantWrites)
		assertRuntimeFenceQueueSettled(t, ctx, p.db)
		t.Logf("catalog successful workers=%v total complete materialization calls=%d trace=%v", owners, len(writes), trace)
	})
	t.Run("peer_drained_replay", func(t *testing.T) {
		testPeerDrainedRuntimeFence(t, dsn)
	})
}

// testPeerDrainedRuntimeFence forces the producer ACK to flag an in-flight
// catalog claim, then lets a different worker consume the owed replay before
// the first worker reports completion. It uses the same queue and fixture as
// the unforced contention proof.
func testPeerDrainedRuntimeFence(t *testing.T, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
	producerDB := openReducerFairnessClaimerDB(t, ctx, p.targetDSN)
	catalogDBs := []*sql.DB{
		openReducerFairnessClaimerDB(t, ctx, p.targetDSN),
		openReducerFairnessClaimerDB(t, ctx, p.targetDSN),
		openReducerFairnessClaimerDB(t, ctx, p.targetDSN),
	}
	type claimResult struct {
		worker  int
		claimed bool
		err     error
	}
	firstClaimed := make(chan claimResult, 1)
	releaseFirst := make(chan struct{})
	firstAcked := make(chan error, 1)
	finishFirst := make(chan struct{})
	firstStopped := make(chan struct{})
	go func() {
		defer close(firstStopped)
		queue := runtimeFenceQueue(catalogDBs[0], "catalog-worker-a", reducer.DomainServiceCatalogCorrelation, nil)
		intent, claimed, err := queue.Claim(ctx)
		firstClaimed <- claimResult{worker: 0, claimed: claimed, err: err}
		if err != nil || !claimed {
			return
		}
		select {
		case <-releaseFirst:
		case <-ctx.Done():
			return
		}
		if _, err := p.handler(catalogDBs[0]).Handle(ctx, intent); err != nil {
			firstAcked <- fmt.Errorf("first Handle: %w", err)
			return
		}
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			firstAcked <- fmt.Errorf("first Ack: %w", err)
			return
		}
		firstAcked <- nil
		select {
		case <-finishFirst:
		case <-ctx.Done():
		}
	}()
	select {
	case result := <-firstClaimed:
		if result.err != nil || !result.claimed {
			t.Fatalf("first catalog Claim() claimed=%v err=%v", result.claimed, result.err)
		}
	case <-ctx.Done():
		t.Fatal("first catalog Claim timed out")
	}

	producer := runtimeFenceQueue(producerDB, "workload-worker", reducer.DomainWorkloadMaterialization, nil)
	producerIntent, claimed, err := producer.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("producer Claim() claimed=%v err=%v", claimed, err)
	}
	p.graph.committed.Store(true)
	if err := producer.Ack(ctx, producerIntent, reducercontract.Result{}); err != nil {
		t.Fatalf("producer Ack() error = %v", err)
	}
	var status string
	var replayRequired bool
	var attempts int
	if err := p.db.QueryRowContext(ctx, `SELECT status, cross_scope_replay_required, attempt_count
		FROM fact_work_items WHERE work_item_id = $1`, runtimeFenceCatalogItem,
	).Scan(&status, &replayRequired, &attempts); err != nil {
		t.Fatalf("read flagged catalog: %v", err)
	}
	if status != "claimed" && status != "running" || !replayRequired || attempts != 1 {
		t.Fatalf("catalog after producer ACK = %s replay=%v attempts=%d, want in-flight flagged at 1", status, replayRequired, attempts)
	}
	close(releaseFirst)
	select {
	case err := <-firstAcked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("first catalog Ack timed out")
	}
	if err := p.db.QueryRowContext(ctx, `SELECT status, cross_scope_replay_required, attempt_count
		FROM fact_work_items WHERE work_item_id = $1`, runtimeFenceCatalogItem,
	).Scan(&status, &replayRequired, &attempts); err != nil {
		t.Fatalf("read pending replay: %v", err)
	}
	if status != "pending" || replayRequired || attempts != 0 {
		t.Fatalf("catalog after first Ack = %s replay=%v attempts=%d, want pending false at 0", status, replayRequired, attempts)
	}

	startPeers := make(chan struct{})
	peerResults := make(chan claimResult, 2)
	for worker := 1; worker < 3; worker++ {
		go func(worker int) {
			select {
			case <-startPeers:
			case <-ctx.Done():
				peerResults <- claimResult{worker: worker, err: ctx.Err()}
				return
			}
			queue := runtimeFenceQueue(catalogDBs[worker], fmt.Sprintf("catalog-worker-%d", worker), reducer.DomainServiceCatalogCorrelation, nil)
			intent, claimed, err := queue.Claim(ctx)
			if err == nil && claimed {
				_, err = p.handler(catalogDBs[worker]).Handle(ctx, intent)
				if err == nil {
					err = queue.Ack(ctx, intent, reducercontract.Result{})
				}
			}
			peerResults <- claimResult{worker: worker, claimed: claimed, err: err}
		}(worker)
	}
	close(startPeers)
	peerClaims := 0
	peerOwner := -1
	for range 2 {
		select {
		case result := <-peerResults:
			if result.err != nil {
				t.Fatalf("peer %d replay: %v", result.worker, result.err)
			}
			if result.claimed {
				peerClaims++
				peerOwner = result.worker
			}
		case <-ctx.Done():
			t.Fatal("peer replay timed out")
		}
	}
	close(finishFirst)
	<-firstStopped
	if peerClaims != 1 {
		t.Fatalf("peer replay claims = %d, want exactly one", peerClaims)
	}
	if status, _, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceProducerItem); status != "succeeded" || attempts != 1 {
		t.Fatalf("producer after replay = %s attempts=%d, want succeeded at 1", status, attempts)
	}
	var leaseOwner sql.NullString
	var claimUntil sql.NullTime
	if err := p.db.QueryRowContext(ctx, `SELECT status, cross_scope_replay_required, attempt_count, lease_owner, claim_until
		FROM fact_work_items WHERE work_item_id = $1`, runtimeFenceCatalogItem,
	).Scan(&status, &replayRequired, &attempts, &leaseOwner, &claimUntil); err != nil {
		t.Fatalf("read drained catalog: %v", err)
	}
	if status != "succeeded" || replayRequired || attempts != 1 || leaseOwner.Valid || claimUntil.Valid {
		t.Fatalf("catalog after peer replay = %s replay=%v attempts=%d lease=%v until=%v, want succeeded at 1 without replay or lease",
			status, replayRequired, attempts, leaseOwner.Valid, claimUntil.Valid)
	}
	t.Logf("forced replay: worker 0 claimed and ACKed after producer flagged it; worker %d claimed and ACKed the pending replay", peerOwner)
	writes := p.materialization.snapshot()
	assertRuntimeFenceWrites(t, writes, 2)
	assertRuntimeFenceQueueSettled(t, ctx, p.db)
}

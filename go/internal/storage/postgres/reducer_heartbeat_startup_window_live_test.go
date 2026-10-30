// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9545 reducer lease-expiry startup-window proof: the immediate pre-heartbeat extends the 150 ms startup claim before the executor enters, so a concurrent reclaim sweep never sees an expired claim_until while the worker holds the intent; a rejected pre-heartbeat fails as executor-never-entered rather than a lease lost mid-execution; disposable PostgreSQL databases through postgresproof with the production claim token; enrolled in the live-postgres-readiness runner; TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres 4.77s on local PostgreSQL 18.6 on 2026-10-30
//eshu:readiness:env ESHU_REDUCER_HEARTBEAT_PROOF_DISPOSABLE ESHU_REDUCER_HEARTBEAT_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// heartbeatProofStartupLease is the lease of the claim that starts the proof.
// It stands in for a short lease that is already close to expiry when the
// worker picks the intent up: the immediate pre-heartbeat must extend it
// before it runs out, or the reclaim sweep sees it expire while the worker
// holds the intent.
const heartbeatProofStartupLease = 150 * time.Millisecond

// TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres proves the
// #4447 fix's runtime effect against real Postgres, not just the in-memory
// Service.Run unit test. Set ESHU_REDUCER_HEARTBEAT_PROOF_DSN to an
// administrative DSN (database postgres) and
// ESHU_REDUCER_HEARTBEAT_PROOF_DISPOSABLE=1 to run it on a disposable, fully
// bootstrapped database. It is skipped only when the DSN is unset. With the
// DSN set, postgresproof fails the test before any write unless the opt-in is
// 1 and the DSN names the postgres administrative database.
//
// Claim, heartbeat and ack are production SQL; the scope, generation and
// pending work item are seeded with the sibling test helpers.
// reducer.Service.Run claims the intent through ReducerQueue.Claim with a
// 150 ms lease, so the claim carries the production claim token
// (last_attempt_at, Intent.ClaimedAt) that the token-checked heartbeat
// requires (#6634). A second ReducerQueue with the same lease owner and a
// 30 s lease is the Heartbeater and the WorkSink. The
// executor blocks well past the startup lease, and HeartbeatInterval is one
// hour, so only the immediate pre-heartbeat can extend claim_until in time. A
// concurrent reclaim sweep, modeling another worker's expired-lease recovery
// (#4464), polls claim_until of the in-flight row against the database clock.
//
//   - Patched: the pre-heartbeat extends claim_until before Executor.Execute
//     runs, the sweep never sees an expired lease, and the intent is acked.
//   - Unpatched (no immediate pre-heartbeat): the sweep sees the startup lease
//     expire while the executor holds the intent.
//   - Rejected pre-heartbeat (for example a claim without the token, the
//     #9545 break): Service.Run skips the executor, and the test reports that
//     the executor never entered rather than a lease lost mid-execution.
//
// The in-memory TestServiceRunPreHeartbeatsImmediatelyOnClaim in
// internal/reducer/service_heartbeat_test.go proves the unpatched service
// waits for the ticker; this proof checks the same fix end to end.
func TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_REDUCER_HEARTBEAT_PROOF_DSN"))
	if adminDSN == "" {
		t.Skip("set ESHU_REDUCER_HEARTBEAT_PROOF_DSN and ESHU_REDUCER_HEARTBEAT_PROOF_DISPOSABLE=1 to run the reducer lease heartbeat startup-window proof")
	}
	ctx, db := postgresproof.OpenDisposableDatabase(t, adminDSN,
		os.Getenv("ESHU_REDUCER_HEARTBEAT_PROOF_DISPOSABLE"), 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply heartbeat proof schema: %v", err)
	}
	db.SetMaxOpenConns(4)

	const leaseOwner = "worker-startup-window-proof"
	const workItemID = "reducer_startup_window_proof"
	seedReducerAckReclaimScope(t, ctx, db, "repository:9545")
	seedReducerAckReclaimGeneration(t, ctx, db, "repository:9545", "generation:9545")
	seedReducerAckReclaimWorkItem(t, ctx, db, workItemID, "repository:9545", "generation:9545")

	claimQueue := ReducerQueue{database: SQLDB{DB: db}, LeaseOwner: leaseOwner, LeaseDuration: heartbeatProofStartupLease}
	workerQueue := ReducerQueue{database: SQLDB{DB: db}, LeaseOwner: leaseOwner, LeaseDuration: 30 * time.Second}

	sweepStop := make(chan struct{})
	sweep := startReclaimSweep(ctx, db, workItemID, sweepStop)
	executor := &blockingProofExecutor{release: make(chan struct{}), entered: make(chan struct{})}
	service := reducer.Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        claimQueue,
		Executor:          executor,
		WorkSink:          workerQueue,
		Heartbeater:       workerQueue,
		HeartbeatInterval: time.Hour, // the ticker must not fire during this test
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}
	runDone := make(chan error, 1)
	go func() { runDone <- service.Run(ctx) }()

	select {
	case <-sweep.observed:
		close(executor.release)
		<-runDone
		close(sweepStop)
		<-sweep.done
		select {
		case <-executor.entered:
			t.Fatal("reclaim sweep observed an expired claim_until while the executor held the intent: the lease-expiry startup window is not closed")
		default:
			t.Fatal("the startup lease expired and the executor never entered: the pre-heartbeat was rejected or never ran, so Service.Run skipped the intent")
		}
	case <-time.After(3 * time.Second):
		close(sweepStop)
		<-sweep.done
		if sweep.err != nil {
			t.Fatalf("reclaim sweep stopped after %d reads: %v", sweep.reads, sweep.err)
		}
		if sweep.reads == 0 {
			t.Fatal("reclaim sweep never read the work item, so it could not have seen an expired lease")
		}
		select {
		case <-executor.entered:
		default:
			t.Fatal("the executor never entered within 3s: Service.Run did not run the claimed intent")
		}
		close(executor.release)
		if err := <-runDone; err != nil {
			t.Fatalf("Service.Run() error = %v", err)
		}
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("query final row state: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", status)
	}
}

// reclaimSweep is a running poll of one work item. reads and err are safe to
// read only after done closes.
type reclaimSweep struct {
	observed chan struct{} // closed once an expired in-flight lease is seen
	done     chan struct{} // closed when the poll goroutine exits
	reads    int           // successful reads of the work item
	err      error         // the read error that stopped the poll, if any
}

// startReclaimSweep polls the work item every 20 ms until stop closes. It
// closes observed once if the row is in flight (claimed or running, as the
// production queue treats it) with a claim_until already behind the database
// clock, the state an expired-lease reclaim would act on. A read error stops
// the poll and is kept in err; a sweep read error or a sweep that never
// reads fails the test.
func startReclaimSweep(ctx context.Context, db *sql.DB, workItemID string, stop <-chan struct{}) *reclaimSweep {
	sweep := &reclaimSweep{observed: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(sweep.done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var expired bool
				if err := db.QueryRowContext(ctx, `
					SELECT COALESCE(status IN ('claimed', 'running') AND claim_until < clock_timestamp(), false)
					FROM fact_work_items WHERE work_item_id = $1
				`, workItemID).Scan(&expired); err != nil {
					sweep.err = err
					return
				}
				sweep.reads++
				if expired {
					close(sweep.observed)
					return
				}
			}
		}
	}()
	return sweep
}

// blockingProofExecutor closes entered when Execute starts and blocks until
// release closes.
type blockingProofExecutor struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (b *blockingProofExecutor) Execute(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return reducercontract.Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: reducer.ResultStatusSucceeded}, nil
	case <-ctx.Done():
		return reducercontract.Result{}, ctx.Err()
	}
}

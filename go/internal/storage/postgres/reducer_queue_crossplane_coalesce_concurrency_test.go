// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerClaimCoalesceConcurrentClaimersLive drains a three-generation
// backlog with two claimers on independent connections: the stale rows are
// superseded exactly once, exactly one carrier execution is leased, and no
// claim errors or deadlocks.
func TestReducerClaimCoalesceConcurrentClaimersLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}
	ctx := context.Background()
	db, schemaName := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
	base := time.Date(2026, time.November, 7, 8, 0, 0, 0, time.UTC)
	domain := string(reducercontract.DomainCrossplaneSatisfiedByMaterialization)

	seedCoalesceScope(t, ctx, db, "scope-co-conc", 4, base)
	for i := 0; i < 4; i++ {
		insertCoalesceWorkItem(t, ctx, db, coalesceRow{
			id: "co-conc-g" + string(rune('0'+i)), generationID: "g" + string(rune('0'+i)), status: "pending",
		}, "scope-co-conc", domain, base)
	}

	const claimers = 2
	queues := make([]ReducerQueue, claimers)
	for i := range queues {
		queues[i] = ReducerQueue{
			database:      SQLDB{DB: openSupersedeReadinessClaimerDB(t, ctx, dsn, schemaName)},
			LeaseOwner:    "coalesce-concurrent",
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return base.Add(5 * time.Hour) },
		}
	}
	start := make(chan struct{})
	errs := make(chan error, claimers)
	var wg sync.WaitGroup
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(q ReducerQueue) {
			defer wg.Done()
			<-start
			for range 6 {
				_, claimed, err := q.Claim(ctx)
				if err != nil {
					errs <- err
					return
				}
				if !claimed {
					return
				}
			}
		}(queues[i])
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Claim() error = %v", err)
	}

	var superseded, claimed int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE status = 'superseded'),
       count(*) FILTER (WHERE status = 'claimed')
FROM fact_work_items WHERE work_item_id LIKE 'co-conc-%'`).Scan(&superseded, &claimed); err != nil {
		t.Fatalf("count coalesce outcomes: %v", err)
	}
	if superseded != 3 || claimed != 1 {
		t.Fatalf("coalesce outcomes superseded=%d claimed=%d, want 3 superseded and exactly 1 carrier claimed", superseded, claimed)
	}
}

// TestReducerClaimCoalesceCarrierClaimedBeforeLockLive is the EvalPlanQual
// interleave: session X holds the stale row's lock uncommitted so the sweep
// waits on it; meanwhile another worker leases the carrier (the only real
// transition out of pending — dead-letter writes require
// claimed/running) and commits. Carrier membership is membership in the
// lock step's output, decided by row-self status quals the recheck
// re-evaluates on the newest row version, so the claimed carrier never
// enters the locked set and the stale row survives as the #10106 rebind
// fallback. The merged #10320 snapshot-EXISTS probe retired the stale row
// in this interleave; #10325 proved that unsound (an in-flight carrier can
// still dead-letter, and superseded is unreplayable) and replaced it with
// the lock-step form this test now pins.
func TestReducerClaimCoalesceCarrierClaimedBeforeLockLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}
	ctx := context.Background()
	db, schemaName := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
	base := time.Date(2026, time.November, 7, 8, 0, 0, 0, time.UTC)
	domain := string(reducercontract.DomainCrossplaneSatisfiedByMaterialization)

	seedCoalesceScope(t, ctx, db, "scope-co-epq", 3, base)
	insertCoalesceWorkItem(t, ctx, db, coalesceRow{id: "co-epq-g0", generationID: "g0", status: "pending"}, "scope-co-epq", domain, base)
	insertCoalesceWorkItem(t, ctx, db, coalesceRow{id: "co-epq-g1", generationID: "g1", status: "pending"}, "scope-co-epq", domain, base)

	holder := openSupersedeReadinessClaimerDB(t, ctx, dsn, schemaName)
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = 'co-epq-g0'`); err != nil {
		t.Fatalf("holder lock stale row: %v", err)
	}
	var holderPID int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatalf("holder pid: %v", err)
	}

	sweeperDB := openSupersedeReadinessClaimerDB(t, ctx, dsn, schemaName)
	var sweepPID int
	if err := sweeperDB.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&sweepPID); err != nil {
		t.Fatalf("sweeper pid: %v", err)
	}
	queue := ReducerQueue{
		database:      SQLDB{DB: sweeperDB},
		LeaseOwner:    "coalesce-epq",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return base.Add(4 * time.Hour) },
	}
	claimDone := make(chan error, 1)
	go func() {
		_, _, err := queue.Claim(ctx)
		claimDone <- err
	}()
	if blockers := waitBlocked(t, ctx, db, sweepPID); len(blockers) != 1 || blockers[0] != holderPID {
		t.Fatalf("sweep blocked by %v, want holder pid %d", blockers, holderPID)
	}

	// Another worker leases the carrier and commits while the sweep waits.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'co-epq-other', claim_until = $1
WHERE work_item_id = 'co-epq-g1'`, base.Add(5*time.Hour)); err != nil {
		t.Fatalf("lease carrier: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	if err := <-claimDone; err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	// Both rows must survive: the carrier stays claimed under the other
	// worker's lease (its claimed version never entered the locked
	// carrier set), and the stale row stays pending — the claim defers
	// behind the carrier's lease on the shared conflict domain — as the
	// rebind fallback if that carrier later dead-letters. Superseding it
	// here is the #10320 snapshot-probe outcome #10325 removed.
	if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, "co-epq-g1"); status != "claimed" {
		t.Fatalf("carrier status = %q, want claimed (debt holder must survive the prune)", status)
	}
	if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, "co-epq-g0"); status != "pending" {
		t.Fatalf("stale row status = %q, want pending (no locked carrier; the rebind fallback must survive)", status)
	}
}

// TestReducerCoalesceBacklogDrainCountsLive measures the redundant-work
// bound on the real queue: S scopes x G queued generations of pending
// Crossplane rows drain through Claim+Ack. Each claim lease is one handler
// execution against the active snapshot, so executions must equal scopes
// (not rows), with rows-scopes one-time supersede writes. Before #10269
// the same drain leased every row (S*G executions); the RED run of
// TestReducerClaimCoalescesStaleCrossplaneRowsLive observed the oldest
// stale row claimed with the rest left pending.
func TestReducerCoalesceBacklogDrainCountsLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}
	ctx := context.Background()
	db, _ := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
	base := time.Date(2026, time.November, 7, 8, 0, 0, 0, time.UTC)
	domain := string(reducercontract.DomainCrossplaneSatisfiedByMaterialization)

	const scopes, generations = 20, 5
	for s := range scopes {
		scopeID := "scope-drain-" + string(rune('a'+s))
		seedCoalesceScope(t, ctx, db, scopeID, generations, base)
		for g := range generations {
			insertCoalesceWorkItem(t, ctx, db, coalesceRow{
				id:           scopeID + "-w" + string(rune('0'+g)),
				generationID: "g" + string(rune('0'+g)),
				status:       "pending",
			}, scopeID, domain, base)
		}
	}

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "coalesce-drain",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return base.Add(6 * time.Hour) },
	}
	executions := 0
	for range scopes * generations {
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if !claimed {
			break
		}
		executions++
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			t.Fatalf("Ack(%s) error = %v", intent.IntentID, err)
		}
	}
	if executions != scopes {
		t.Fatalf("drain executions = %d, want %d (one per scope, not per row)", executions, scopes)
	}
	var superseded int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE work_item_id LIKE 'scope-drain-%' AND status = 'superseded'`).Scan(&superseded); err != nil {
		t.Fatalf("count superseded: %v", err)
	}
	if want := scopes * (generations - 1); superseded != want {
		t.Fatalf("superseded rows = %d, want %d (rows - scopes)", superseded, want)
	}
	t.Logf("backlog drain: scopes=%d generations=%d rows=%d executions=%d superseded=%d",
		scopes, generations, scopes*generations, executions, superseded)
}

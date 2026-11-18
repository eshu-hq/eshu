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
// backlog with two claimers on independent connections: exactly one carrier
// execution is leased and no claim errors or deadlocks. Since #10863 the
// claims only fence the stale rows; one sweep pass afterwards supersedes
// every stale row except the newest pending one, which stays as the
// rebind fallback while the carrier is in flight (a claimed carrier never
// counts for the sweep either).
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

	countOutcomes := func() (superseded, claimed, pending int) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE status = 'superseded'),
       count(*) FILTER (WHERE status = 'claimed'),
       count(*) FILTER (WHERE status = 'pending')
FROM fact_work_items WHERE work_item_id LIKE 'co-conc-%'`).Scan(&superseded, &claimed, &pending); err != nil {
			t.Fatalf("count coalesce outcomes: %v", err)
		}
		return superseded, claimed, pending
	}
	if superseded, claimed, pending := countOutcomes(); superseded != 0 || claimed != 1 || pending != 3 {
		t.Fatalf("post-claim outcomes superseded=%d claimed=%d pending=%d, want 0/1/3 (claims fence, never terminalize)",
			superseded, claimed, pending)
	}
	if _, err := queues[0].SupersedeStaleReducerGenerations(ctx, 100); err != nil {
		t.Fatalf("SupersedeStaleReducerGenerations() error = %v", err)
	}
	if superseded, claimed, pending := countOutcomes(); superseded != 2 || claimed != 1 || pending != 1 {
		t.Fatalf("post-sweep outcomes superseded=%d claimed=%d pending=%d, want 2/1/1 (newest pending stays as fallback)",
			superseded, claimed, pending)
	}
}

// TestReducerClaimCoalesceCarrierClaimedBeforeLockLive pins the #10325
// carrier rule under the #10863 split: another worker leases the carrier
// and commits before the sweep's lock step runs. Carrier membership is
// membership in the lock step's output, decided by row-self status quals,
// so the claimed carrier never enters the locked set and the stale row
// survives the sweep as the #10106 rebind fallback; the claim gate fences
// it only while that carrier is in flight. When the carrier later
// dead-letters, the fence lifts and the stale row is claimable again —
// the outcome the merged #10320 snapshot-EXISTS probe destroyed by
// terminalizing the fallback against an in-flight carrier.
func TestReducerClaimCoalesceCarrierClaimedBeforeLockLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}
	ctx := context.Background()
	db, _ := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
	base := time.Date(2026, time.November, 7, 8, 0, 0, 0, time.UTC)
	domain := string(reducercontract.DomainCrossplaneSatisfiedByMaterialization)

	seedCoalesceScope(t, ctx, db, "scope-co-epq", 3, base)
	insertCoalesceWorkItem(t, ctx, db, coalesceRow{id: "co-epq-g0", generationID: "g0", status: "pending"}, "scope-co-epq", domain, base)
	insertCoalesceWorkItem(t, ctx, db, coalesceRow{id: "co-epq-g1", generationID: "g1", status: "pending"}, "scope-co-epq", domain, base)

	// Another worker leases the carrier and commits before the sweep runs.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'co-epq-other', claim_until = $1
WHERE work_item_id = 'co-epq-g1'`, base.Add(5*time.Hour)); err != nil {
		t.Fatalf("lease carrier: %v", err)
	}

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "coalesce-epq",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return base.Add(4 * time.Hour) },
	}
	if swept, err := queue.SupersedeStaleReducerGenerations(ctx, 100); err != nil || swept != 0 {
		t.Fatalf("SupersedeStaleReducerGenerations = %d rows err %v, want 0 (claimed carrier never counts)", swept, err)
	}
	// The gate fences the stale row while the carrier is in flight, so the
	// claim hands out nothing on this conflict key.
	if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
		t.Fatalf("Claim() = claimed %v err %v, want no hand-out while the carrier is leased", claimed, err)
	}
	if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, "co-epq-g1"); status != "claimed" {
		t.Fatalf("carrier status = %q, want claimed (debt holder must survive the prune)", status)
	}
	if status, _ := readReducerSupersedeReadinessWorkItemStatus(t, ctx, db, "co-epq-g0"); status != "pending" {
		t.Fatalf("stale row status = %q, want pending (no locked carrier; the rebind fallback must survive)", status)
	}

	// The carrier dead-letters: the fence lifts and the stale row becomes
	// the claimable rebind fallback again.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'dead_letter', lease_owner = NULL, claim_until = NULL
WHERE work_item_id = 'co-epq-g1'`); err != nil {
		t.Fatalf("dead-letter carrier: %v", err)
	}
	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed || intent.IntentID != "co-epq-g0" {
		t.Fatalf("Claim() after carrier dead-letter = %q claimed %v err %v, want the stale fallback co-epq-g0",
			intent.IntentID, claimed, err)
	}
}

// TestReducerCoalesceBacklogDrainCountsLive measures the redundant-work
// bound on the real queue: S scopes x G queued generations of pending
// Crossplane rows drain through Claim+Ack with the periodic sweep ticking
// between claims (the production pairing since #10863). Each claim lease
// is one handler execution against the active snapshot, so executions must
// equal scopes (not rows), with rows-scopes one-time supersede writes.
// Before #10269 the same drain leased every row (S*G executions); the RED
// run of TestReducerClaimCoalescesStaleCrossplaneRowsLive observed the
// oldest stale row claimed with the rest left pending.
func TestReducerCoalesceBacklogDrainCountsLive(t *testing.T) {
	dsn := reducerSupersedeReadinessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_SUPERSEDE_READINESS_DSN or ESHU_POSTGRES_DSN to run the coalesce proof")
	}
	ctx := context.Background()
	db, _ := openReducerSupersedeReadinessDBWithSchema(t, ctx, dsn)
	// Ack's lease fence compares claim_until with the database's real
	// clock_timestamp(), so anchor the fixture clock at real time (a fixed
	// past date makes every Ack read as an expired lease).
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
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
		// The production sweeper ticks independently of claims; one pass
		// per drain step is its fastest-cadence form.
		if _, err := queue.SupersedeStaleReducerGenerations(ctx, 1000); err != nil {
			t.Fatalf("SupersedeStaleReducerGenerations() error = %v", err)
		}
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

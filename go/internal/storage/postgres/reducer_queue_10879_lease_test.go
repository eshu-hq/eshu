// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// leaseTestRetryableError drives the retry branch of failIntent: Retryable()
// true keeps the row live-adjacent (retrying) instead of dead-lettering it.
type leaseTestRetryableError struct{}

func (leaseTestRetryableError) Error() string   { return "lease test retryable" }
func (leaseTestRetryableError) Retryable() bool { return true }

// setupLeaseTestSchema builds a disposable queue schema including the #10879
// lease table. It fails RED until migration 322 exists.
func setupLeaseTestSchema(
	t *testing.T,
	ctx context.Context,
) (reducerClaimBenchmarkConn, string) {
	t.Helper()
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN to run the 10879 lease tests")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlConn.Close() })
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("reducer_queue_10879_lease_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create lease test schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("fact_work_item_leases")); err != nil {
		t.Fatalf("apply lease table migration: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("fact_work_item_leases_backfill")); err != nil {
		t.Fatalf("apply lease backfill migration: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("cross_scope_completion_queue")); err != nil {
		t.Fatalf("create completion events table: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("reducer_waiting_items")); err != nil {
		t.Fatalf("create waiting items table: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set lease test search_path: %v", err)
	}
	return conn, schemaName
}

// leaseTestQueue returns a ReducerQueue bound to the disposable schema.
func leaseTestQueue(conn reducerClaimBenchmarkConn, owner string) ReducerQueue {
	return ReducerQueue{
		database:      conn,
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		ClaimDomains:  []reducercontract.Domain{reducercontract.DomainSupplyChainImpact},
		MaxAttempts:   3,
	}
}

// liveLeaseRows reports every lease-table row: work_item_id -> owner.
func liveLeaseRows(
	t *testing.T,
	ctx context.Context,
	conn reducerClaimBenchmarkConn,
) map[string]string {
	t.Helper()
	rows, err := conn.QueryContext(ctx,
		`SELECT work_item_id, lease_owner FROM fact_work_item_leases`)
	if err != nil {
		t.Fatalf("list lease rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			t.Fatalf("scan lease row: %v", err)
		}
		out[id] = owner
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate lease rows: %v", err)
	}
	return out
}

// claimOneSupplyChainItem claims a single seeded supply-chain row.
func claimOneSupplyChainItem(
	t *testing.T,
	ctx context.Context,
	queue ReducerQueue,
) reducercontract.Intent {
	t.Helper()
	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed {
		t.Fatal("claim returned no work, want one seeded row")
	}
	return intent
}

// TestReducerQueue10879LeaseTableBackfillsLiveLeases proves migration 322
// creates the lease table with the production shape and backfills exactly the
// live reducer leases, leaving terminal rows out.
func TestReducerQueue10879LeaseTableBackfillsLiveLeases(t *testing.T) {
	ctx := context.Background()
	conn, _ := setupLeaseTestSchema(t, ctx)

	var fillfactor sql.NullString
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT reloptions::text FROM pg_class WHERE relname = 'fact_work_item_leases'`,
	).Scan(&fillfactor); err != nil {
		t.Fatalf("read lease table reloptions: %v", err)
	}
	if !fillfactor.Valid || fillfactor.String != "{fillfactor=70}" {
		t.Fatalf("lease table reloptions = %v, want {fillfactor=70}", fillfactor.String)
	}

	// Plant one live and one terminal row, then re-apply the migration: the
	// backfill must pick up exactly the live lease. Re-applying also proves
	// the migration is idempotent on a database that already has the table.
	now := time.Now().UTC()
	until := now.Add(time.Minute)
	if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes (
	    scope_id, scope_kind, source_system, source_key, parent_scope_id,
	    collector_kind, partition_key, observed_at, ingested_at, status,
	    active_generation_id, payload
	) VALUES ('scope-backfill-1', 'repository', 'git', 'backfill-repo-1', NULL,
	    'git', 'backfill-repo-1', $1, $1, 'active', 'generation-backfill-1', '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert backfill scope: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO scope_generations (
	    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
	    ingested_at, status, activated_at, superseded_at, payload
	) VALUES ('generation-backfill-1', 'scope-backfill-1', 'snapshot', 'backfill',
	    $1, $1, 'active', $1, NULL, '{}'::jsonb)`, now); err != nil {
		t.Fatalf("insert backfill generation: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, status,
	    attempt_count, lease_owner, claim_until, last_attempt_at, payload,
	    created_at, updated_at
	) VALUES
	    ('backfill-live-1', 'scope-backfill-1', 'generation-backfill-1', 'reducer',
	     'supply_chain_impact', 'claimed', 1, 'backfill-owner',
	     $2, $1, '{}'::jsonb, $1, $1),
	    ('backfill-done-1', 'scope-backfill-1', 'generation-backfill-1', 'reducer',
	     'supply_chain_impact', 'succeeded', 1, NULL, NULL, NULL, '{}'::jsonb, $1, $1)`, now, until); err != nil {
		t.Fatalf("insert backfill rows: %v", err)
	}
	for _, name := range []string{"fact_work_item_leases", "fact_work_item_leases_backfill"} {
		if _, err := conn.ExecContext(ctx, MigrationSQL(name)); err != nil {
			t.Fatalf("re-apply lease migration %s: %v", name, err)
		}
	}

	var live, total int
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT count(*), (SELECT count(*) FROM fact_work_items
		   WHERE stage = 'reducer' AND status IN ('claimed', 'running'))
		 FROM fact_work_item_leases`).Scan(&live, &total); err != nil {
		t.Fatalf("count lease rows: %v", err)
	}
	if live != 1 || total != 1 {
		t.Fatalf("lease rows = %d, live main leases = %d, want 1 and 1", live, total)
	}
	var owner string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT lease_owner FROM fact_work_item_leases WHERE work_item_id = 'backfill-live-1'`,
	).Scan(&owner); err != nil {
		t.Fatalf("read backfilled lease: %v", err)
	}
	if owner != "backfill-owner" {
		t.Fatalf("backfilled lease owner = %q, want backfill-owner", owner)
	}
}

// TestReducerQueue10879ClaimInsertsLeaseMirror proves a claim writes the
// lease row with the exact fenced triple the main row carries.
func TestReducerQueue10879ClaimInsertsLeaseMirror(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue := leaseTestQueue(conn, "lease-mirror-claim")
	intent := claimOneSupplyChainItem(t, ctx, queue)

	leases := liveLeaseRows(t, ctx, conn)
	owner, ok := leases[intent.IntentID]
	if !ok {
		t.Fatalf("no lease row for claimed %q", intent.IntentID)
	}
	if owner != "lease-mirror-claim" {
		t.Fatalf("lease owner = %q, want lease-mirror-claim", owner)
	}

	var mainOwner string
	var mainUntil, leaseUntil time.Time
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT w.lease_owner, w.claim_until, l.claim_until
		   FROM fact_work_items w JOIN fact_work_item_leases l USING (work_item_id)
		   WHERE w.work_item_id = $1`, intent.IntentID,
	).Scan(&mainOwner, &mainUntil, &leaseUntil); err != nil {
		t.Fatalf("read mirrored lease: %v", err)
	}
	if !mainUntil.Equal(leaseUntil) {
		t.Fatalf("main claim_until %v != lease claim_until %v", mainUntil, leaseUntil)
	}
}

// TestReducerQueue10879HeartbeatRefreshesLeaseMirror proves a heartbeat
// extends the lease row in the same statement as the main row.
func TestReducerQueue10879HeartbeatRefreshesLeaseMirror(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue := leaseTestQueue(conn, "lease-mirror-beat")
	intent := claimOneSupplyChainItem(t, ctx, queue)

	if first := liveLeaseRows(t, ctx, conn)[intent.IntentID]; first == "" {
		t.Fatalf("no lease row after claim for %q", intent.IntentID)
	}
	if err := queue.Heartbeat(ctx, intent); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if got := liveLeaseRows(t, ctx, conn)[intent.IntentID]; got != "lease-mirror-beat" {
		t.Fatalf("lease owner after heartbeat = %q, want lease-mirror-beat", got)
	}
	var mainUntil, leaseUntil time.Time
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT w.claim_until, l.claim_until FROM fact_work_items w
		   JOIN fact_work_item_leases l USING (work_item_id)
		   WHERE w.work_item_id = $1`, intent.IntentID,
	).Scan(&mainUntil, &leaseUntil); err != nil {
		t.Fatalf("read refreshed lease: %v", err)
	}
	if !mainUntil.Equal(leaseUntil) {
		t.Fatalf("main claim_until %v != lease claim_until %v after heartbeat", mainUntil, leaseUntil)
	}
}

// TestReducerQueue10879TerminalOpsClearLeaseMirror proves ack, retry, and
// dead-letter remove the lease row in the same statement, leaving no orphan.
func TestReducerQueue10879TerminalOpsClearLeaseMirror(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}

	ackQueue := leaseTestQueue(conn, "lease-mirror-ack")
	ackIntent := claimOneSupplyChainItem(t, ctx, ackQueue)
	if err := ackQueue.Ack(ctx, ackIntent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}

	retryQueue := leaseTestQueue(conn, "lease-mirror-retry")
	retryIntent := claimOneSupplyChainItem(t, ctx, retryQueue)
	if err := retryQueue.Fail(ctx, retryIntent, leaseTestRetryableError{}); err != nil {
		t.Fatalf("retry fail: %v", err)
	}

	deadQueue := leaseTestQueue(conn, "lease-mirror-dead")
	deadIntent := claimOneSupplyChainItem(t, ctx, deadQueue)
	if err := deadQueue.Fail(ctx, deadIntent, errors.New("permanent lease test failure")); err != nil {
		t.Fatalf("dead-letter fail: %v", err)
	}

	for id, op := range map[string]string{
		ackIntent.IntentID: "ack", retryIntent.IntentID: "retry", deadIntent.IntentID: "dead-letter",
	} {
		if _, ok := liveLeaseRows(t, ctx, conn)[id]; ok {
			t.Fatalf("orphan lease row survives %s for %q", op, id)
		}
	}
	var orphans int
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_work_item_leases l
		  LEFT JOIN fact_work_items w USING (work_item_id)
		  WHERE w.work_item_id IS NULL
		     OR NOT (w.stage = 'reducer' AND w.status IN ('claimed', 'running'))`,
	).Scan(&orphans); err != nil {
		t.Fatalf("count orphan leases: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("orphan lease rows = %d, want 0", orphans)
	}
}

// TestReducerQueue10879StaleWorkerFenceHoldsOnMirror proves a stale worker's
// heartbeat and ack are still rejected after the holder's lease expired and
// another owner reclaimed it, and the lease row belongs to the new owner.
func TestReducerQueue10879StaleWorkerFenceHoldsOnMirror(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}

	holder := leaseTestQueue(conn, "lease-stale-holder")
	stale := claimOneSupplyChainItem(t, ctx, holder)

	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_items SET claim_until = clock_timestamp() - interval '1 minute'
		  WHERE work_item_id = $1`, stale.IntentID); err != nil {
		t.Fatalf("expire hold on main: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_item_leases SET claim_until = clock_timestamp() - interval '1 minute'
		  WHERE work_item_id = $1`, stale.IntentID); err != nil {
		t.Fatalf("expire hold on mirror: %v", err)
	}

	thief := leaseTestQueue(conn, "lease-stale-thief")
	fresh, claimed, err := thief.Claim(ctx)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if !claimed {
		t.Fatal("reclaim returned no work, want the expired row")
	}

	if err := holder.Heartbeat(ctx, stale); !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("stale heartbeat err = %v, want ErrReducerClaimRejected", err)
	}
	if err := holder.Ack(ctx, stale, reducercontract.Result{}); !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("stale ack err = %v, want ErrReducerClaimRejected", err)
	}
	if got := liveLeaseRows(t, ctx, conn)[fresh.IntentID]; got != "lease-stale-thief" {
		t.Fatalf("lease owner after reclaim = %q, want lease-stale-thief", got)
	}
}

// TestReducerQueue10879OnePerGroupLeaseUnique proves the lease table refuses
// a second live lease on one conflict key and the mapper still defers the
// conflict instead of surfacing a raw unique violation.
func TestReducerQueue10879OnePerGroupLeaseUnique(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue := leaseTestQueue(conn, "lease-unique-owner")
	intent := claimOneSupplyChainItem(t, ctx, queue)

	var domain, scope string
	var key sql.NullString
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT conflict_domain, conflict_key, scope_id FROM fact_work_items WHERE work_item_id = $1`,
		intent.IntentID).Scan(&domain, &key, &scope); err != nil {
		t.Fatalf("read conflict key: %v", err)
	}
	_, err := conn.ExecContext(ctx,
		`INSERT INTO fact_work_item_leases (
		   work_item_id, stage, scope_id, conflict_domain, conflict_key,
		   lease_owner, claim_until, last_attempt_at
		 ) VALUES ('lease-unique-dup', 'reducer', $1, $2, $3, 'other-owner',
		   clock_timestamp() + interval '1 minute', clock_timestamp())`,
		scope, domain, key)
	if err == nil {
		t.Fatal("duplicate lease insert succeeded, want unique violation")
	}
	if !isReducerLiveLeaseConflict(err) {
		t.Fatalf("duplicate lease err not mapped to defer: %v", err)
	}

	var live int
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_work_item_leases`).Scan(&live); err != nil {
		t.Fatalf("count leases: %v", err)
	}
	if live != 1 {
		t.Fatalf("lease rows = %d, want exactly the claimed one", live)
	}
}

// shippedBackfillSQL reads the committed 323 migration file, and
// shippedReconcileSQL the committed 324 file, so the test exercises the
// exact bytes bootstrap runs, not copies.
func shippedBackfillSQL(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("migrations/323_fact_work_item_leases_backfill.sql")
	if err != nil {
		t.Fatalf("read shipped 323 backfill: %v", err)
	}
	return string(raw)
}

func shippedReconcileSQL(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("migrations/324_fact_work_item_leases_reconcile.sql")
	if err != nil {
		t.Fatalf("read shipped 324 reconcile: %v", err)
	}
	return string(raw)
}

// TestReducerQueue10879BackfillReconcilesTerminalizedRows proves the shipped
// 323+324 sequence removes mirror rows whose main row terminalized after
// the scan (the backfill race): a live row copied by the scan, then
// finished without mirror cleanup, survives a 323-only re-run (the RED
// half) and is removed by 324, while live rows keep their mirror entries
// and the same-key sibling stays claimable.
func TestReducerQueue10879BackfillReconcilesTerminalizedRows(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue := leaseTestQueue(conn, "backfill-race-owner")

	orphans := func() int {
		t.Helper()
		var n int
		if err := conn.conn.QueryRowContext(ctx,
			`SELECT count(*) FROM fact_work_item_leases l
			  LEFT JOIN fact_work_items w USING (work_item_id)
			  WHERE w.work_item_id IS NULL
			     OR NOT (w.stage = 'reducer' AND w.status IN ('claimed', 'running'))`,
		).Scan(&n); err != nil {
			t.Fatalf("count orphan leases: %v", err)
		}
		return n
	}

	// Hand-seed a same-key pair on a real scope/identity quad: a live row
	// the scan will copy plus a pending sibling old enough to be the
	// deterministic oldest claim candidate (seed tail is 2026-10-19).
	var scopeID, generationID, domain, conflictDomain string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT scope_id, generation_id, domain, conflict_domain
		 FROM fact_work_items WHERE domain = 'supply_chain_impact' LIMIT 1`,
	).Scan(&scopeID, &generationID, &domain, &conflictDomain); err != nil {
		t.Fatalf("read seed quad: %v", err)
	}
	old := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES
	    ('backfill-race-terminalized', $1, $2, 'reducer', $3, $4,
	     'backfill-race-k', 'claimed', 1, 'backfill-race-owner',
	     $5, $5, '{}'::jsonb, $5, $5),
	    ('backfill-race-sibling', $1, $2, 'reducer', $3, $4,
	     'backfill-race-k', 'pending', 1, NULL,
	     NULL, NULL, '{}'::jsonb, $6, $6)`,
		scopeID, generationID, domain, conflictDomain, now, old); err != nil {
		t.Fatalf("seed same-key pair: %v", err)
	}

	backfill := shippedBackfillSQL(t)
	reconcile := shippedReconcileSQL(t)
	if _, err := conn.ExecContext(ctx, backfill); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if rows := liveLeaseRows(t, ctx, conn); rows["backfill-race-terminalized"] == "" {
		t.Fatalf("first backfill missed the live row: %v", rows)
	}

	// The race: the main row finishes after the scan copied it, the way a
	// main-only writer would (no mirror cleanup).
	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_items
		 SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
		     last_attempt_at = NULL
		 WHERE work_item_id = 'backfill-race-terminalized'`); err != nil {
		t.Fatalf("terminalize main row: %v", err)
	}

	// 323 alone cannot reconcile: the orphan survives a backfill re-run.
	// This is the RED half — it pins the defect 324 exists to fix.
	if _, err := conn.ExecContext(ctx, backfill); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if n := orphans(); n != 1 {
		t.Fatalf("orphan lease rows after 323-only re-run = %d, want 1", n)
	}

	if _, err := conn.ExecContext(ctx, reconcile); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n := orphans(); n != 0 {
		t.Fatalf("orphan lease rows after 324 reconcile = %d, want 0", n)
	}

	// No wedge: the same-key sibling must now be claimable.
	got, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("post-reconcile claim err = %v", err)
	}
	if !claimed {
		t.Fatal("post-reconcile claim deferred, want the sibling")
	}
	if got.IntentID != "backfill-race-sibling" {
		t.Fatalf("post-reconcile claim took %q, want backfill-race-sibling", got.IntentID)
	}
}

// TestReducerQueue10879ReconcileCleansPreexistingOrphan covers databases
// that already applied 323 before 324 existed: a planted orphan is cleaned
// by running only 324, and the wedged key becomes claimable again.
func TestReducerQueue10879ReconcileCleansPreexistingOrphan(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue := leaseTestQueue(conn, "backfill-race-owner")

	var scopeID, generationID, domain, conflictDomain string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT scope_id, generation_id, domain, conflict_domain
		 FROM fact_work_items WHERE domain = 'supply_chain_impact' LIMIT 1`,
	).Scan(&scopeID, &generationID, &domain, &conflictDomain); err != nil {
		t.Fatalf("read seed quad: %v", err)
	}
	old := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES
	    ('backfill-race-terminalized', $1, $2, 'reducer', $3, $4,
	     'backfill-race-k', 'claimed', 1, 'backfill-race-owner',
	     $5, $5, '{}'::jsonb, $5, $5),
	    ('backfill-race-sibling', $1, $2, 'reducer', $3, $4,
	     'backfill-race-k', 'pending', 1, NULL,
	     NULL, NULL, '{}'::jsonb, $6, $6)`,
		scopeID, generationID, domain, conflictDomain, now, old); err != nil {
		t.Fatalf("seed same-key pair: %v", err)
	}

	// The old world: 323 ran, then the main row finished without cleanup.
	if _, err := conn.ExecContext(ctx, shippedBackfillSQL(t)); err != nil {
		t.Fatalf("323 backfill: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_items
		 SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
		     last_attempt_at = NULL
		 WHERE work_item_id = 'backfill-race-terminalized'`); err != nil {
		t.Fatalf("terminalize main row: %v", err)
	}

	// Only 324 runs now: it must clean the pre-existing orphan.
	if _, err := conn.ExecContext(ctx, shippedReconcileSQL(t)); err != nil {
		t.Fatalf("324 reconcile: %v", err)
	}
	var n int
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_work_item_leases l
		  LEFT JOIN fact_work_items w USING (work_item_id)
		  WHERE w.work_item_id IS NULL
		     OR NOT (w.stage = 'reducer' AND w.status IN ('claimed', 'running'))`,
	).Scan(&n); err != nil {
		t.Fatalf("count orphan leases: %v", err)
	}
	if n != 0 {
		t.Fatalf("orphan lease rows after 324-only run = %d, want 0", n)
	}
	got, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("post-reconcile claim err = %v", err)
	}
	if !claimed || got.IntentID != "backfill-race-sibling" {
		t.Fatalf("post-reconcile claim = (%q, %v), want (backfill-race-sibling, true)", got.IntentID, claimed)
	}
}

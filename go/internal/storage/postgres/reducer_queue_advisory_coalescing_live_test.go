// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10566 disposable PostgreSQL proof for the advisory-coalescing re-homing design: a concurrent enqueue+retouch storm over one synthetic-slot group converges to one row with one insert and one live lease (A-1), the claim-time supersede sweep retires real stale rows while never touching the synthetic active-generation row and N touches over M groups leave M rows with the synthetic genesis absent from the real retention-candidate selection (A-2), and fail-after-touch then retry reclaims the same slot with the winners-watermark bound covering the second touch (A-3); seeded rows carry a unique prefix with cleanup on both paths; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type coalescing10566Retryable struct{ msg string }

func (e coalescing10566Retryable) Error() string   { return e.msg }
func (e coalescing10566Retryable) Retryable() bool { return true }

func coalescing10566SeedScope(t *testing.T, ctx context.Context, db *sql.DB, scope, kind string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload) VALUES ($1,$2,'synthetic',$1,'synthetic',$1,NOW(),NOW(),'active','{}'::jsonb) ON CONFLICT (scope_id) DO NOTHING`, scope, kind)
	if err != nil {
		t.Fatalf("seed scope %s: %v", scope, err)
	}
}

func coalescing10566SeedGeneration(t *testing.T, ctx context.Context, db *sql.DB, gen, scope, status string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload) VALUES ($1,$2,'synthetic',NOW(),NOW(),$3,NOW(),'{}'::jsonb) ON CONFLICT (generation_id) DO NOTHING`, gen, scope, status)
	if err != nil {
		t.Fatalf("seed generation %s: %v", gen, err)
	}
	if status == "active" {
		if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, gen); err != nil {
			t.Fatalf("point active %s: %v", scope, err)
		}
	}
}

func coalescing10566Intent(scope, gen, group string) runtime.ReducerIntent {
	return runtime.ReducerIntent{
		ScopeID:      scope,
		GenerationID: gen,
		Domain:       reducercontract.DomainAdvisoryGroupProjection,
		EntityKey:    "advisory_group:" + group,
		Reason:       "supply_chain_impact_winners_touch",
		SourceSystem: "synthetic",
		Payload:      map[string]any{"group_id": group},
	}
}

// coalescing10566Touch models one origin pass touching a group under the
// re-homed design: same synthetic scope/generation identity, enqueue plus
// retouch, no production enqueuer change.
func coalescing10566Touch(t *testing.T, ctx context.Context, q ReducerQueue, scope, gen, group string) (int, bool) {
	t.Helper()
	intent := coalescing10566Intent(scope, gen, group)
	res, err := q.Enqueue(ctx, []runtime.ReducerIntent{intent})
	if err != nil {
		t.Fatalf("touch enqueue %s: %v", group, err)
	}
	ok, err := q.RetouchWorkItem(ctx, reducerWorkItemID(intent))
	if err != nil {
		t.Fatalf("touch retouch %s: %v", group, err)
	}
	return res.Count, ok
}

func coalescing10566Status(t *testing.T, ctx context.Context, db *sql.DB, id string) string {
	t.Helper()
	var s string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id=$1`, id).Scan(&s); err != nil {
		t.Fatalf("read status %s: %v", id, err)
	}
	return s
}

// TestAdvisoryCoalescingLifecycleLive proves the #10566 A-1/A-2/A-3 sketches
// against real PostgreSQL using synthetic slot VALUES only. It takes no
// position on the still-open terminal-slot question (a touch landing on a
// failed or dead-letter slot is dropped by the current retouch set; the
// design amendment records the required reopen): every scenario below starts
// from a live slot and stays inside the live set.
func TestAdvisoryCoalescingLifecycleLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the advisory coalescing lifecycle proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	synth := "coalesce-10566-synth-" + suffix
	genesis := synth + "-genesis"
	realScope := "coalesce-10566-real-" + suffix
	realGen1 := realScope + "-g1"
	realGen2 := realScope + "-g2"
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer ccancel()
		for _, q := range []string{
			`DELETE FROM fact_work_items WHERE scope_id LIKE 'coalesce-10566-%'`,
			`DELETE FROM scope_generations WHERE scope_id LIKE 'coalesce-10566-%'`,
			`DELETE FROM ingestion_scopes WHERE scope_id LIKE 'coalesce-10566-%'`,
		} {
			if _, err := db.ExecContext(cctx, q); err != nil {
				t.Errorf("cleanup %s: %v", q, err)
			}
		}
	})

	coalescing10566SeedScope(t, ctx, db, synth, "synthetic")
	coalescing10566SeedGeneration(t, ctx, db, genesis, synth, "active")
	coalescing10566SeedScope(t, ctx, db, realScope, "vulnerability_intelligence")
	coalescing10566SeedGeneration(t, ctx, db, realGen1, realScope, "active")

	mkQueue := func(owner string) ReducerQueue {
		q := NewReducerQueue(SQLDB{DB: db}, owner, time.Minute)
		q.MaxAttempts = 10
		q.RetryDelay = 100 * time.Millisecond
		q.ClaimDomains = []reducercontract.Domain{reducercontract.DomainAdvisoryGroupProjection}
		return q
	}
	drainAll := func(q ReducerQueue, bound int) {
		t.Helper()
		for i := 0; i < bound; i++ {
			intent, ok, err := q.Claim(ctx)
			if err != nil {
				t.Fatalf("drain claim: %v", err)
			}
			if !ok {
				return
			}
			if err := q.Ack(ctx, intent, reducercontract.Result{}); err != nil {
				t.Fatalf("drain ack: %v", err)
			}
		}
		t.Fatalf("drain bound exhausted")
	}
	enq := mkQueue("coalesce-enq-" + suffix)

	// A-1: 16 concurrent touches of one group converge to one row admitted
	// once, and two concurrent claimers yield exactly one live lease.
	g1 := "GHSA-COALESCE-STORM"
	g1id := reducerWorkItemID(coalescing10566Intent(synth, genesis, g1))
	const stormN = 16
	var insertedTotal atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < stormN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n, _ := coalescing10566Touch(t, ctx, mkQueue(fmt.Sprintf("storm-%s-%d", suffix, i)), synth, genesis, g1)
			insertedTotal.Add(int64(n))
		}(i)
	}
	wg.Wait()
	var g1count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id=$1`, g1id).Scan(&g1count); err != nil {
		t.Fatalf("count storm row: %v", err)
	}
	if g1count != 1 || insertedTotal.Load() != 1 {
		t.Fatalf("A-1 storm: rows=%d inserted=%d, want 1/1", g1count, insertedTotal.Load())
	}
	c1, c2 := mkQueue("claimer-1-"+suffix), mkQueue("claimer-2-"+suffix)
	type claimRes struct {
		intent reducercontract.Intent
		q      ReducerQueue
		ok     bool
		err    error
	}
	ch := make(chan claimRes, 2)
	go func() { intent, ok, err := c1.Claim(ctx); ch <- claimRes{intent, c1, ok, err} }()
	go func() { intent, ok, err := c2.Claim(ctx); ch <- claimRes{intent, c2, ok, err} }()
	r1, r2 := <-ch, <-ch
	if r1.ok == r2.ok || r1.err != nil || r2.err != nil {
		t.Fatalf("A-1 one-lease: r1=(%v,%v) r2=(%v,%v)", r1.ok, r1.err, r2.ok, r2.err)
	}
	winner := r1
	if r2.ok {
		winner = r2
	}
	if !winner.ok || winner.intent.IntentID != g1id {
		t.Fatalf("A-1 winner holds %q, want storm row %q", winner.intent.IntentID, g1id)
	}
	if err := winner.q.Ack(ctx, winner.intent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack storm row: %v", err)
	}
	if s := coalescing10566Status(t, ctx, db, g1id); s != "succeeded" {
		t.Fatalf("A-1 storm row status = %s, want succeeded", s)
	}

	// A-2: advancing the real scope retires its stale row in the claim
	// sweep while the synthetic row on the always-active genesis survives.
	g2synth := "GHSA-COALESCE-HOMED"
	g2synthID := reducerWorkItemID(coalescing10566Intent(synth, genesis, g2synth))
	coalescing10566Touch(t, ctx, enq, synth, genesis, g2synth)
	g2 := "GHSA-COALESCE-STALE"
	g2realID := reducerWorkItemID(coalescing10566Intent(realScope, realGen1, g2))
	coalescing10566Touch(t, ctx, enq, realScope, realGen1, g2)
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations SET status='superseded', superseded_at=NOW() - INTERVAL '2 hours' WHERE generation_id=$1`, realGen1); err != nil {
		t.Fatalf("retire gen1: %v", err)
	}
	coalescing10566SeedGeneration(t, ctx, db, realGen2, realScope, "active")
	drainAll(mkQueue("drainer-"+suffix), 10)
	if s := coalescing10566Status(t, ctx, db, g2realID); s != "superseded" {
		t.Fatalf("A-2 real stale row = %s, want superseded", s)
	}
	if s := coalescing10566Status(t, ctx, db, g2synthID); s == "superseded" {
		t.Fatalf("A-2 synthetic row superseded: host-generation guard failed")
	}
	var genesisStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM scope_generations WHERE generation_id=$1`, genesis).Scan(&genesisStatus); err != nil {
		t.Fatalf("read genesis: %v", err)
	}
	if genesisStatus != "active" {
		t.Fatalf("A-2 genesis = %s, want active", genesisStatus)
	}

	// A-2 retention half: the shipped candidate selection picks up the aged
	// real superseded generation and never names the active genesis.
	rows, err := db.QueryContext(ctx, generationRetentionCandidateQuery, time.Now().Add(-time.Hour), 0, 10, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("retention candidates: %v", err)
	}
	selected := map[string]bool{}
	for rows.Next() {
		var scopeID, generationID, scopeKind, sourceSHA, freshness string
		var supersededAt, observedAt time.Time
		var uncovered, isDelta, deltaProtected bool
		if err := rows.Scan(&scopeID, &generationID, &scopeKind, &supersededAt, &observedAt, &uncovered, &sourceSHA, &isDelta, &freshness, &deltaProtected); err != nil {
			_ = rows.Close()
			t.Fatalf("scan candidate: %v", err)
		}
		selected[generationID] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatalf("iterate candidates: %v", err)
	}
	_ = rows.Close()
	if !selected[realGen1] {
		t.Fatalf("A-2 retention: aged real superseded generation not selected (selected=%v)", selected)
	}
	if selected[genesis] {
		t.Fatalf("A-2 retention: active genesis selected as prune candidate")
	}

	// A-2 cap: repeated touches over many groups leave one row per group.
	for i := 0; i < 25; i++ {
		for k := 0; k < 3; k++ {
			coalescing10566Touch(t, ctx, enq, synth, genesis, fmt.Sprintf("GHSA-COALESCE-CAP-%02d", i))
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id=$1`, synth).Scan(&n); err != nil {
		t.Fatalf("count synth rows: %v", err)
	}
	if n != 2+25 {
		t.Fatalf("A-2 cap rows = %d, want 27 (storm + homed + 25 groups)", n)
	}
	drainAll(mkQueue("settle-"+suffix), 60)

	// A-3: fail-after-touch then retry reclaims the same slot with the
	// watermark bound covering the second touch, then succeeds.
	g3 := "GHSA-COALESCE-RETRY"
	g3id := reducerWorkItemID(coalescing10566Intent(synth, genesis, g3))
	coalescing10566Touch(t, ctx, enq, synth, genesis, g3)
	w := mkQueue("worker-" + suffix)
	first, ok, err := w.Claim(ctx)
	if err != nil || !ok || first.IntentID != g3id {
		t.Fatalf("g3 claim1: %v %v", ok, err)
	}
	if err := w.Fail(ctx, first, coalescing10566Retryable{"coalescing proof retryable"}); err != nil {
		t.Fatalf("g3 fail: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	t2 := time.Now().UTC()
	coalescing10566Touch(t, ctx, enq, synth, genesis, g3)
	var second reducercontract.Intent
	deadline := time.Now().Add(20 * time.Second)
	for {
		intent, ok, err := w.Claim(ctx)
		if err != nil {
			t.Fatalf("g3 claim2: %v", err)
		}
		if ok {
			second = intent
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("g3 claim2: retry never became due")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if second.IntentID != g3id {
		t.Fatalf("A-3 retry reclaimed %s, want same slot %s", second.IntentID, g3id)
	}
	if second.CycleStartedAt.Before(t2.Add(-2 * time.Second)) {
		t.Fatalf("A-3 bound %v before second touch %v", second.CycleStartedAt, t2)
	}
	if err := w.Ack(ctx, second, reducercontract.Result{}); err != nil {
		t.Fatalf("g3 ack: %v", err)
	}
	if s := coalescing10566Status(t, ctx, db, g3id); s != "succeeded" {
		t.Fatalf("A-3 g3 = %s, want succeeded", s)
	}
}

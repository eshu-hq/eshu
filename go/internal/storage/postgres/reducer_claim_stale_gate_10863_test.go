// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerClaimQueriesGateStaleWithoutSweeping pins the #10863 claim
// contract: neither claim statement (nor the funnel census) terminalizes
// stale rows — no sweep UPDATE, no blocking lock pass — while both still
// fence stale-generation rows out of hand-out through the read-only gate
// set, rechecked on the locked rows in the batch claim's lock arms.
func TestReducerClaimQueriesGateStaleWithoutSweeping(t *testing.T) {
	t.Parallel()

	gate := reducerClaimStaleGateCTEs()
	for name, query := range map[string]string{
		"single": claimReducerWorkQuery,
		"batch":  claimReducerWorkBatchQuery,
		"funnel": reducerClaimBatchFunnelQuery,
	} {
		if !strings.Contains(query, gate) {
			t.Fatalf("%s query does not compose reducerClaimStaleGateCTEs() verbatim", name)
		}
		for _, banned := range []string{
			"UPDATE fact_work_items AS stale",
			"FOR NO KEY UPDATE OF stale",
			"superseded_stale_reducer_locked",
			"superseded_stale_reducer_generations",
		} {
			if strings.Contains(query, banned) {
				t.Fatalf("%s query still carries the in-claim supersede sweep (%q); #10863 moved it out", name, banned)
			}
		}
		if !strings.Contains(query, "FROM stale_reducer_candidates AS superseded") {
			t.Fatalf("%s query does not fence candidates on stale_reducer_candidates", name)
		}
	}
	// The batch claim rechecks the gate on the actual locked rows in both
	// lock arms, next to the EvalPlanQual lease rechecks.
	if got := strings.Count(claimReducerWorkBatchQuery, "FROM stale_reducer_candidates AS gate_stale"); got != 2 {
		t.Fatalf("batch claim lock-arm gate rechecks = %d, want 2 (reserved + rest)", got)
	}
	for _, want := range []string{
		"stale_reducer_candidates AS MATERIALIZED (",
		// #11234: the gate is driven from the prefiltered work rows.
		"stale_gate_rows AS MATERIALIZED (",
		"FROM stale_gate_rows AS stale",
		"stale.status IN ('pending', 'retrying')",
		"stale.generation_id <> scope.active_generation_id",
		"stale_generation.ingested_at < scope.active_ingested_at",
		supplyChainFanOutDebtSpareSQL,
		"carrier.status IN ('pending', 'retrying', 'claimed', 'running')",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("stale gate CTEs missing %q:\n%s", want, gate)
		}
	}
}

// TestSupersedeStaleReducerGenerationsQueryShape pins the standalone
// periodic sweep statement (#10863): the shipped sweep's predicate, carrier
// arm, and terminal UPDATE, bounded by a LIMIT and safe under concurrent
// sweepers through SKIP LOCKED.
func TestSupersedeStaleReducerGenerationsQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		supersedeInactiveReducerGenerationsPredicate,
		supersedeInactiveReducerCarrierArmSQL,
		supersededCrossplaneNewestCarrierCTE,
		"UPDATE fact_work_items AS stale",
		"status = 'superseded'",
		"LIMIT $3",
		"FOR NO KEY UPDATE OF stale SKIP LOCKED",
		// Keyset cursor over the locked window: progress is locked rows
		// plus the greatest locked id, never the superseded count alone,
		// or a spared head starves every row behind it.
		`stale.work_item_id COLLATE "C" > $4::text`,
		"(SELECT count(*) FROM superseded_stale_reducer_locked)",
		`max(work_item_id COLLATE "C") FROM superseded_stale_reducer_locked`,
		priorFailureStaleSQL,
	} {
		if !strings.Contains(supersedeStaleReducerGenerationsQuery, want) {
			t.Fatalf("standalone sweep query missing %q", want)
		}
	}
}

// seedStaleGateScope plants one scope whose active generation is
// gen-active (ingested at now) with an older superseded generation
// gen-old (ingested a day earlier), for the #10863 gate proofs.
func seedStaleGateScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, now time.Time) {
	t.Helper()
	seedGateScope(t, ctx, db, scopeID, "gen-active-"+scopeID, now)
	mustExecGateArgs(t, ctx, db, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'fair', $3, $3, 'superseded', $3, $4, '{}'::jsonb)`,
		"gen-old-"+scopeID, scopeID, now.Add(-24*time.Hour), now)
}

func staleGateQueue(db *sql.DB, owner string, now time.Time) ReducerQueue {
	return ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Hour,
		Now:           func() time.Time { return now },
	}
}

func staleGateRowStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("read status of %q: %v", workItemID, err)
	}
	return status
}

func intentIDSet(intents []reducercontract.Intent) map[string]bool {
	out := map[string]bool{}
	for _, in := range intents {
		out[in.IntentID] = true
	}
	return out
}

// TestReducerClaimStaleGateRefusesStaleBeforeSweepLive is acceptance 4 for
// #10863 on the built code: with the sweep out of the claim, a
// stale-generation row is refused at hand-out by both claim paths and stays
// pending (the claim no longer terminalizes it) until the standalone sweep
// runs; the sweep then terminalizes exactly that row.
func TestReducerClaimStaleGateRefusesStaleBeforeSweepLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale-gate live proof")
	}
	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	scopeID := "scope-stale-gate"
	seedStaleGateScope(t, ctx, db, scopeID, now)
	insert := func(id, gen string, updated time.Time) {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: gen,
			domain: "deployment_mapping", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: updated,
		})
	}
	// The stale row is the oldest, so an unfenced claim hands it out first.
	insert("gate-stale-1", "gen-old-"+scopeID, now.Add(-time.Hour))
	insert("gate-fresh-1", "gen-active-"+scopeID, now)

	queue := staleGateQueue(db, "gate-owner", now.Add(2*time.Hour))
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	got := intentIDSet(intents)
	if got["gate-stale-1"] {
		t.Fatalf("ClaimBatch handed out stale row gate-stale-1 before any sweep: %v", got)
	}
	if !got["gate-fresh-1"] || len(intents) != 1 {
		t.Fatalf("ClaimBatch = %v, want exactly gate-fresh-1", got)
	}
	if status := staleGateRowStatus(t, ctx, db, "gate-stale-1"); status != "pending" {
		t.Fatalf("stale row status after claim = %q, want pending (claim must not terminalize)", status)
	}
	// Single claim also refuses it: fresh row is leased, stale is fenced.
	if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
		t.Fatalf("single Claim after batch = claimed %v err %v, want no hand-out", claimed, err)
	}

	swept, err := queue.SupersedeStaleReducerGenerations(ctx, 100)
	if err != nil {
		t.Fatalf("SupersedeStaleReducerGenerations: %v", err)
	}
	if swept != 1 {
		t.Fatalf("sweep superseded %d rows, want 1", swept)
	}
	if status := staleGateRowStatus(t, ctx, db, "gate-stale-1"); status != "superseded" {
		t.Fatalf("stale row status after sweep = %q, want superseded", status)
	}
	if status := staleGateRowStatus(t, ctx, db, "gate-fresh-1"); status != "claimed" {
		t.Fatalf("fresh row status after sweep = %q, want claimed (untouched)", status)
	}
	if again, err := queue.SupersedeStaleReducerGenerations(ctx, 100); err != nil || again != 0 {
		t.Fatalf("second sweep = %d rows err %v, want 0", again, err)
	}
}

// TestReducerClaimStaleGateSparesLive covers the gate's deliberate spares:
// a stale supply_chain_impact row still owing fan-out (#10060) and a sole
// stale Crossplane row (#10106 rebind) stay claimable, while a stale
// Crossplane row with a newer pending carrier is fenced (#10269).
func TestReducerClaimStaleGateSparesLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale-gate spares proof")
	}
	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	scopeID := "scope-stale-spares"
	seedStaleGateScope(t, ctx, db, scopeID, now)
	insert := func(id, gen, domain string) {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: gen,
			domain: domain, conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now,
		})
	}
	insert("spare-fanout", "gen-old-"+scopeID, "supply_chain_impact")
	mustExecGateArgs(t, ctx, db,
		`UPDATE fact_work_items SET payload = payload || '{"fanout_owed":"true"}'::jsonb WHERE work_item_id='spare-fanout'`)
	insert("spare-crossplane-sole", "gen-old-"+scopeID, "crossplane_satisfied_by_materialization")

	scope2 := "scope-stale-carrier"
	seedStaleGateScope(t, ctx, db, scope2, now)
	insert2 := func(id, gen string) {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scope2, generationID: gen,
			domain: "crossplane_satisfied_by_materialization", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now,
		})
	}
	insert2("fenced-crossplane-stale", "gen-old-"+scope2)
	insert2("carrier-crossplane-new", "gen-active-"+scope2)

	queue := staleGateQueue(db, "spare-owner", now.Add(2*time.Hour))
	intents, err := queue.ClaimBatch(ctx, 16)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	got := intentIDSet(intents)
	for _, want := range []string{"spare-fanout", "spare-crossplane-sole", "carrier-crossplane-new"} {
		if !got[want] {
			t.Fatalf("ClaimBatch missing spared row %q: %v", want, got)
		}
	}
	if got["fenced-crossplane-stale"] {
		t.Fatalf("ClaimBatch handed stale Crossplane row with a newer carrier: %v", got)
	}
}

// TestReducerStaleSweepBoundedConcurrentLive proves the standalone sweep is
// batch-bounded and safe under several concurrent sweepers: bounded passes
// from concurrent queues supersede every stale row exactly once in total.
func TestReducerStaleSweepBoundedConcurrentLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale sweep concurrency proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	scopeID := "scope-stale-sweep"
	seedStaleGateScope(t, ctx, db, scopeID, now)
	const staleRows = 10
	for i := 0; i < staleRows; i++ {
		id := fmt.Sprintf("sweep-stale-%02d", i)
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: "gen-old-" + scopeID,
			domain: "deployment_mapping", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now,
		})
	}

	const sweepers = 4
	var (
		mu    sync.Mutex
		total int
		wg    sync.WaitGroup
	)
	for i := 0; i < sweepers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sdb := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			q := staleGateQueue(sdb, fmt.Sprintf("sweeper-%d", i), now.Add(2*time.Hour))
			for pass := 0; pass < 10; pass++ {
				n, err := q.SupersedeStaleReducerGenerations(ctx, 3)
				if err != nil {
					t.Errorf("sweep pass: %v", err)
					return
				}
				if n > 3 {
					t.Errorf("sweep pass superseded %d rows, want <= limit 3", n)
					return
				}
				mu.Lock()
				total += n
				mu.Unlock()
				if n == 0 {
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if total != staleRows {
		t.Fatalf("concurrent sweeps superseded %d rows in total, want %d exactly once", total, staleRows)
	}
	var superseded int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_work_items WHERE work_item_id LIKE 'sweep-stale-%' AND status='superseded'`).Scan(&superseded); err != nil {
		t.Fatalf("count superseded: %v", err)
	}
	if superseded != staleRows {
		t.Fatalf("superseded rows = %d, want %d", superseded, staleRows)
	}
}

// TestReducerClaimStaleGateStormExpiredHoldLive is acceptance 5 for
// #10863: concurrent batch claims over a backlog mixing fresh rows, an
// expired lease awaiting reclaim, and stale-generation rows deliver every
// fresh row exactly once, reclaim the expired hold exactly once, and never
// deliver a stale row.
func TestReducerClaimStaleGateStormExpiredHoldLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the stale-gate storm proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	scopeID := "scope-stale-storm"
	seedStaleGateScope(t, ctx, db, scopeID, now)
	const freshRows = 32
	for i := 0; i < freshRows; i++ {
		id := fmt.Sprintf("storm-fresh-%02d", i)
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: "gen-active-" + scopeID,
			domain: "deployment_mapping", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("storm-stale-%02d", i)
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID: id, scopeID: scopeID, generationID: "gen-old-" + scopeID,
			domain: "deployment_mapping", conflictDomain: reducerConflictDomainScope,
			conflictKey: id + "-key", updatedAt: now.Add(-time.Hour),
		})
	}
	// Expired hold: claimed on the active generation with a lapsed lease.
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "storm-expired-hold", scopeID: scopeID, generationID: "gen-active-" + scopeID,
		domain: "deployment_mapping", conflictDomain: reducerConflictDomainScope,
		conflictKey: "storm-expired-hold-key", updatedAt: now,
	})
	claimAt := now.Add(2 * time.Hour)
	mustExecGateArgs(t, ctx, db,
		`UPDATE fact_work_items SET status='claimed', lease_owner='dead-owner', claim_until=$1 WHERE work_item_id='storm-expired-hold'`,
		now.Add(time.Hour))

	const claimers = 8
	var (
		mu      sync.Mutex
		claimed = map[string]int{}
		start   = make(chan struct{})
		wg      sync.WaitGroup
	)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cdb := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			q := staleGateQueue(cdb, fmt.Sprintf("storm-gate-%d", i), claimAt)
			<-start
			for r := 0; r < 20; r++ {
				intents, err := q.ClaimBatch(ctx, 8)
				if err != nil {
					t.Errorf("ClaimBatch: %v", err)
					return
				}
				mu.Lock()
				for _, in := range intents {
					claimed[in.IntentID]++
				}
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for id, n := range claimed {
		if strings.HasPrefix(id, "storm-stale-") {
			t.Fatalf("stale row %q delivered %d times during the storm", id, n)
		}
		if n != 1 {
			t.Fatalf("work item %q delivered %d times, want exactly once", id, n)
		}
	}
	if len(claimed) != freshRows+1 {
		t.Fatalf("storm delivered %d distinct items, want %d fresh + 1 reclaimed hold", len(claimed), freshRows)
	}
	if claimed["storm-expired-hold"] != 1 {
		t.Fatalf("expired hold delivered %d times, want exactly 1 reclaim", claimed["storm-expired-hold"])
	}
	var stalePending int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_work_items WHERE work_item_id LIKE 'storm-stale-%' AND status='pending'`).Scan(&stalePending); err != nil {
		t.Fatalf("count stale pending: %v", err)
	}
	if stalePending != 8 {
		t.Fatalf("stale rows still pending after storm = %d, want 8 (fenced, not terminalized by claims)", stalePending)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Soak wave helpers for TestSupplyChainImpactWinnersStagingAutovacuumSoakLive
// (see that test's header for the frozen bounds and RED/GREEN invocations).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// soakReadVacuumStats samples depth, heap, dead tuples, and the daemon's own
// counters for the staging table. It never fails the test itself; the caller
// grades the series against the pre-registered bounds.
func soakReadVacuumStats(t *testing.T, ctx context.Context, database *sql.DB, start time.Time) soakVacuumStats {
	t.Helper()
	var s soakVacuumStats
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_winners_dirty_staging`).Scan(&s.depth); err != nil {
		t.Fatalf("soak read staging depth: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT pg_total_relation_size('supply_chain_impact_winners_dirty_staging')`).Scan(&s.heapBytes); err != nil {
		t.Fatalf("soak read staging heap: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT n_dead_tup, last_autovacuum, autovacuum_count FROM pg_stat_user_tables WHERE relname = 'supply_chain_impact_winners_dirty_staging'`).Scan(&s.deadTup, &s.lastVacuum, &s.vacCount); err != nil {
		t.Fatalf("soak read vacuum stats: %v", err)
	}
	s.elapsed = time.Since(start)
	return s
}

// soakSetupScope creates one conflict domain (scope, generation, claimed work
// item) for a wave. Scope rows carry a NULL active generation so the
// activation capture trigger stages nothing.
func soakSetupScope(t *testing.T, ctx context.Context, database *sql.DB, wave, scope int) (scopeID, genID, itemID string, claimedAt time.Time) {
	t.Helper()
	scopeID = fmt.Sprintf("scope:soak:%d:%d", wave, scope)
	genID = fmt.Sprintf("gen:soak:%d:%d", wave, scope)
	itemID = fmt.Sprintf("item:soak:%d:%d", wave, scope)
	claimedAt = time.Now().UTC()
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1, 'test', 'test', 'test', 'test', 'test', now(), now(), 'active', NULL, '{}')`, scopeID); err != nil {
		t.Fatalf("soak setup scope %s: %v", scopeID, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
		VALUES ($1, $2, 'test', now(), now(), 'active', '{}')`, genID, scopeID); err != nil {
		t.Fatalf("soak setup generation %s: %v", genID, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
		(work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, lease_owner, claim_until, last_attempt_at, created_at, updated_at, payload)
		VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'claimed', 1, 'soak', now() + interval '10 minutes', $4, now(), now(), '{}')`,
		itemID, scopeID, genID, claimedAt); err != nil {
		t.Fatalf("soak setup work item %s: %v", itemID, err)
	}
	return scopeID, genID, itemID, claimedAt
}

// soakPayloadFor builds the finding payload the production writer stores.
func soakPayloadFor(t *testing.T, pkg string) string {
	t.Helper()
	doc, err := json.Marshal(map[string]string{
		"package_id": pkg, "repository_id": "repo:soak",
		"subject_digest": "digest:soak", "cve_id": "CVE-2026-1110", "advisory_id": "CVE-2026-1110",
	})
	if err != nil {
		t.Fatalf("soak marshal finding payload: %v", err)
	}
	return string(doc)
}

// soakRunPass executes one production-shape writer transaction: conflict-domain
// lock, finding upsert, capture, owed marker, replace-set retract. No retry:
// any error, including 40P01, fails the gate loudly. It returns the staged
// mark count (fact_upsert captures plus fact_retract marks).
func soakRunPass(t *testing.T, ctx context.Context, database *sql.DB, failed *atomic.Int64, w int, token int64, scopeID, genID, itemID string, claimedAt time.Time, hot []string, tail []string) int64 {
	t.Helper()
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}
	abort := func(step string, err error) int64 {
		if isDeadlock(err) {
			t.Errorf("soak writer txn hit a lock-order cycle (40P01) at %s: %v", step, err)
		}
		failed.Add(1)
		return 0
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return abort("begin", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	lockKey := facts.ReducerSupplyChainImpactFindingFactKind + "\x1f" + scopeID + "\x1f" + genID
	if _, err := tx.ExecContext(ctx, landingInterleaveLockSQL, lockKey); err != nil {
		return abort("conflict-domain lock", err)
	}
	now := time.Now().UTC()
	pkgs := append(append([]string{}, hot...), tail...)
	rows := make([]factwrite.VersionedRow, 0, len(pkgs))
	keep := make([]string, 0, len(pkgs))
	for i, pkg := range pkgs {
		factID := fmt.Sprintf("fact:soak:%s:%d:%d", scopeID, token, i)
		keep = append(keep, factID)
		rows = append(rows, factwrite.VersionedRow{
			FactID: factID, ScopeID: scopeID, GenerationID: genID,
			FactKind:      facts.ReducerSupplyChainImpactFindingFactKind,
			StableFactKey: factID, SchemaVersion: facts.ReducerDerivedSchemaVersionV1,
			CollectorKind: "test", SourceConfidence: "high", SourceSystem: "test",
			SourceFactKey: factID, ObservedAt: now, IngestedAt: now,
			IsTombstone: false, Payload: soakPayloadFor(t, pkg), FencingToken: token,
		})
	}
	if err := factwrite.BatchInsertVersionedFacts(ctx, tx, rows); err != nil {
		return abort("finding upsert", err)
	}
	repos := make([]string, len(pkgs))
	digests := make([]string, len(pkgs))
	cves := make([]string, len(pkgs))
	for i := range pkgs {
		repos[i], digests[i], cves[i] = "repo:soak", "digest:soak", "CVE-2026-1110"
	}
	res, err := tx.ExecContext(ctx, captureWinnersDirtyFactsSQL, pkgs, repos, digests, cves, cves)
	if err != nil {
		return abort("capture", err)
	}
	upsertMarks, err := res.RowsAffected()
	if err != nil {
		return abort("count capture", err)
	}
	owed, err := tx.ExecContext(ctx, markSupplyChainImpactFanOutOwedQuery, itemID, "soak", claimedAt)
	if err != nil {
		return abort("publish owed marker", err)
	}
	if n, err := owed.RowsAffected(); err != nil || n != 1 {
		return abort("publish owed marker", fmt.Errorf("owed marker touched %d rows, err %v", n, err))
	}
	// The retract CTE returns (tombstoned, marked); only the staged marks
	// feed conservation.
	var tombstoned, retractMarks int64
	if err := tx.QueryRowContext(ctx, landingInterleaveRetractSQL,
		facts.ReducerSupplyChainImpactFindingFactKind, scopeID, genID, keep, token).Scan(&tombstoned, &retractMarks); err != nil {
		return abort("retract", err)
	}
	_ = tombstoned
	if err := tx.Commit(); err != nil {
		if isDeadlock(err) {
			t.Errorf("soak writer txn commit hit a lock-order cycle (40P01): %v", err)
		}
		failed.Add(1)
		return 0
	}
	committed = true
	return upsertMarks + retractMarks
}

// soakDrainWave collapses staging until the worker reports no rows, bounded
// by soakDrainLimit. Any leftover row after the bound KILLs the soak.
func soakDrainWave(t *testing.T, ctx context.Context, database *sql.DB, wave int) {
	t.Helper()
	for i := 0; i < soakDrainLimit; i++ {
		tx, err := (SQLDB{DB: database}).Begin(ctx)
		if err != nil {
			t.Fatalf("soak wave %d begin drain: %v", wave, err)
		}
		outcome, err := collapseWinnersStaging(ctx, tx, 0)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("soak wave %d drain: %v", wave, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("soak wave %d commit drain: %v", wave, err)
		}
		if !outcome.split && outcome.rows == 0 {
			return
		}
	}
	t.Fatalf("soak wave %d: staging did not drain after %d collapses", wave, soakDrainLimit)
}

// soakCanary runs the unchanged-code contended-upsert cell the quiet lane
// uses: 4 writers sharing 162 hot keys. It returns the 4-writer p95.
func soakCanary(t *testing.T, ctx context.Context, database *sql.DB) time.Duration {
	t.Helper()
	const writers, iters = 4, 25
	keys := make([]string, 0, hotkeyShimHotKeys)
	for g := 0; g < hotkeyShimHotKeys; g++ {
		keys = append(keys, fmt.Sprintf("pkg:soak-canary:%03d", g))
	}
	repos := make([]string, len(keys))
	digests := make([]string, len(keys))
	cves := make([]string, len(keys))
	for i := range keys {
		repos[i], digests[i], cves[i] = "repo:soak-canary", "digest:soak-canary", "CVE-2026-1111"
	}
	var mu sync.Mutex
	var durs []time.Duration
	var failed atomic.Int64
	var workers sync.WaitGroup
	start := make(chan struct{})
	for w := 0; w < writers; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for i := -2; i < iters; i++ {
				begin := time.Now()
				_, err := database.ExecContext(ctx, hotkeyShimDirectUpsertSQL, keys, repos, digests, cves, cves)
				elapsed := time.Since(begin)
				if err != nil {
					failed.Add(1)
					return
				}
				if i >= 0 {
					mu.Lock()
					durs = append(durs, elapsed)
					mu.Unlock()
				}
			}
		}()
	}
	close(start)
	workers.Wait()
	if failed.Load() != 0 {
		t.Fatalf("soak canary VOID: %d accessor(s) failed; the host or database is unfit, no numbers published", failed.Load())
	}
	return winnersDirtyLatencyPercentile(durs, 0.95)
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const supportsFenceWriterSetting = `SELECT set_config('eshu.supply_chain_impact_supports_writer', 'supports_v1', true)`

func supportsDirtyScopes(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports_dirty`).Scan(&n); err != nil {
		t.Fatalf("count dirty scopes: %v", err)
	}
	return n
}

// supportsOldWriterInsert inserts one finding fact the way a reducer without the
// maintainer would: no writer setting. With setting set, it models the new
// writer, which carries the setting for its whole transaction.
func supportsOldWriterInsert(t *testing.T, ctx context.Context, db *sql.DB, factID, kind string, withSetting bool) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if withSetting {
		if _, err := tx.ExecContext(ctx, supportsFenceWriterSetting); err != nil {
			t.Fatalf("set writer setting: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, now(), now(), FALSE,
    '{"cve_id":"CVE-2026-0001","package_id":"pkg:npm/old@1","repository_id":"repository:old","impact_status":"affected_exact"}'::jsonb, 1)`,
		factID, replaceSetLiveScope, replaceSetLiveGeneration, kind); err != nil {
		t.Fatalf("insert %s: %v", factID, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestSupplyChainImpactSupportsOldWriterFenceLive proves the old-writer fence: a
// finding fact written without the maintainer's transaction setting marks its
// scope dirty and the table not ready; the backfill drains the scope and the
// table becomes ready again. A write that carries the setting (the new writer),
// a fact of another kind, and a real writer pass mark nothing.
func TestSupplyChainImpactSupportsOldWriterFenceLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 2}
	writer := replaceSetWriter(db)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:10880:fence-seed",
		replaceSetFinding("repository:r_fence_1"), replaceSetFinding("repository:r_fence_2"))); err != nil {
		t.Fatalf("seed pass: %v", err)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("the real writer marked %d scopes dirty, want 0: it must carry the maintainer setting", n)
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("ready after the backfill = %v (err %v), want true", ready, err)
	}

	// Neither a setting-carrying write nor another kind marks anything.
	supportsOldWriterInsert(t, ctx, db, "fact:fence:new-writer", "reducer_supply_chain_impact_finding", true)
	supportsOldWriterInsert(t, ctx, db, "fact:fence:other-kind", "vulnerability.cve", false)
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("a writer-setting insert or another kind marked %d scopes dirty, want 0", n)
	}

	// An old writer's insert marks the scope and the table not ready, even
	// though the marker is complete.
	supportsOldWriterInsert(t, ctx, db, "fact:fence:old-writer", "reducer_supply_chain_impact_finding", false)
	if n := supportsDirtyScopes(t, ctx, db); n != 1 {
		t.Fatalf("an old writer's insert left %d dirty scopes, want 1", n)
	}
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || ready {
		t.Fatalf("ready with a dirty scope = %v (err %v), want false", ready, err)
	}
	// An old writer's tombstone update marks the scope as well.
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); err != nil {
		t.Fatalf("clear dirty: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'fact:fence:new-writer'`); err != nil {
		t.Fatalf("old tombstone: %v", err)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 1 {
		t.Fatalf("an old writer's tombstone update left %d dirty scopes, want 1", n)
	}

	// The backfill drains the dirty scope (dirty, then drained, then ready) and
	// the support of the old writer's fact exists while the tombstoned fact has none.
	for step := 0; step < 50 && supportsDirtyScopes(t, ctx, db) > 0; step++ {
		if _, err := backfill.RunStep(ctx); err != nil {
			t.Fatalf("drain step %d: %v", step, err)
		}
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("%d scopes still dirty after draining", n)
	}
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("ready after the drain = %v (err %v), want true", ready, err)
	}
	supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "after draining the old writer's scope")
	var oldSupports int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE fact_id = 'fact:fence:old-writer'`).Scan(&oldSupports); err != nil || oldSupports != 1 {
		t.Fatalf("the old writer's fact has %d supports (err %v), want 1", oldSupports, err)
	}
}

// TestSupplyChainImpactSupportsMarkerWaitsForTheFenceLive proves the marker is
// written only when the fence is empty: a dirty scope that appears before the
// verification keeps the table not ready until it is drained, and the marker
// completes in the same step that finds the fence empty.
func TestSupplyChainImpactSupportsMarkerWaitsForTheFenceLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 100}
	if _, err := replaceSetWriter(db).WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:10880:fence-marker",
		replaceSetFinding("repository:r_marker_1"))); err != nil {
		t.Fatalf("seed pass: %v", err)
	}
	supportsOldWriterInsert(t, ctx, db, "fact:marker:old-writer", "reducer_supply_chain_impact_finding", false)
	if n := supportsDirtyScopes(t, ctx, db); n != 1 {
		t.Fatalf("dirty scopes = %d, want 1", n)
	}
	for step := 0; step < 50; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		if report.Complete {
			if n := supportsDirtyScopes(t, ctx, db); n != 0 {
				t.Fatalf("the marker completed with %d scopes still dirty", n)
			}
			supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "after the marker")
			return
		}
	}
	t.Fatal("the backfill never completed")
}

// TestSupplyChainImpactSupportsFreshInstallMarksImmediatelyLive proves an empty
// database needs no sweep: the first steps find nothing to back up, verify, and
// set the marker.
func TestSupplyChainImpactSupportsFreshInstallMarksImmediatelyLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store}
	if steps := supportsBackfillRunToCompletion(t, ctx, backfill); steps > 2 {
		t.Fatalf("a fresh install took %d steps, want at most 2", steps)
	}
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("ready on a fresh install = %v (err %v), want true", ready, err)
	}
}

// TestSupplyChainImpactSupportsResumeAfterKillLive proves the cursor survives a
// process death: a new backfill value, with no memory of the first, continues
// from the stored cursor and finishes, and a second full sweep over a reset
// marker rewrites no support row.
func TestSupplyChainImpactSupportsResumeAfterKillLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	first := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 2}
	for i := 0; i < 3; i++ {
		if report, err := first.RunStep(ctx); err != nil || report.Complete {
			t.Fatalf("step %d = %+v (err %v), want incomplete", i, report, err)
		}
	}
	var cursor string
	if err := db.QueryRowContext(ctx, `SELECT cursor_fact_id FROM supply_chain_impact_finding_supports_backfill`).Scan(&cursor); err != nil || cursor == "" {
		t.Fatalf("stored cursor = %q (err %v), want the last fact the killed process finished", cursor, err)
	}
	second := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 2}
	supportsBackfillRunToCompletion(t, ctx, second)
	supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "after the resumed sweep")
	supportsLiveAssertMirror(t, ctx, db, replaceSetSecondScope, replaceSetSecondGeneration, "after the resumed sweep, second scope")

	before := supportsLiveXminSum(t, ctx, db)
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports_backfill
SET completed_at = NULL, cursor_scope_id = '', cursor_generation_id = '', cursor_fact_id = ''`); err != nil {
		t.Fatalf("reset marker: %v", err)
	}
	supportsBackfillRunToCompletion(t, ctx, second)
	if after := supportsLiveXminSum(t, ctx, db); after != before {
		t.Fatalf("a replayed sweep rewrote supports (xmin sum %d to %d)", before, after)
	}
}

// TestSupplyChainImpactSupportsMarkerAboveTheBinaryIsNotReadyLive proves a
// stored marker for a higher contract version (a newer binary ran the sweep)
// reads as not ready to this binary: its reads must answer 503.
func TestSupplyChainImpactSupportsMarkerAboveTheBinaryIsNotReadyLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	store := postgres.SQLDB{DB: db}
	supportsBackfillRunToCompletion(t, ctx, postgres.SupplyChainImpactSupportsBackfill{DB: store})
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports_backfill (marker_name, contract_version, completed_at)
VALUES ('supply_chain_impact_finding_supports_v99', 99, now())`); err != nil {
		t.Fatalf("plant a newer marker: %v", err)
	}
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || ready {
		t.Fatalf("ready with a marker above this binary's version = %v (err %v), want false", ready, err)
	}
	_ = supplychaincore.SupplyChainImpactWriteSupersededFailureClass
}

// TestSupplyChainImpactSupportsOperatorScopeWriteLive pins the census answer to
// "who writes operator-scope finding facts": the same impact writer
// (commitSupplyChainImpactRows, the only BatchInsertVersionedFacts call that
// writes the finding kind) runs the operator pass, so its transaction's supports
// write and fence setting cover the operator scope without a hook in the
// suppression store. A finding written for the operator scope lands in supports
// with that scope id, and marks nothing dirty.
func TestSupplyChainImpactSupportsOperatorScopeWriteLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	const scope, generation = "operator:vulnerability_suppressions", "generation:10880:operator"
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('operator:vulnerability_suppressions', 'vulnerability_intelligence', 'eshu_policy', 'operator', 'synthetic', 'operator', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('generation:10880:operator', 'operator:vulnerability_suppressions', 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`,
		`UPDATE ingestion_scopes SET active_generation_id = 'generation:10880:operator' WHERE scope_id = 'operator:vulnerability_suppressions'`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed operator scope: %v", err)
		}
	}
	write := replaceSetWrite("intent:10880:operator-pass", replaceSetFinding("repository:r_operator_1"))
	write.ScopeID, write.GenerationID = scope, generation
	if _, err := replaceSetWriter(db).WriteSupplyChainImpactFindings(ctx, write); err != nil {
		t.Fatalf("operator pass: %v", err)
	}
	supportsLiveAssertMirror(t, ctx, db, scope, generation, "operator-scope pass")
	if got := len(supportsLiveSupportIDs(t, ctx, db, scope, generation)); got != 1 {
		t.Fatalf("operator-scope supports = %d, want 1", got)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("the operator pass marked %d scopes dirty, want 0", n)
	}
}

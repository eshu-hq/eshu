// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// supportsVerifySteps runs backfill steps until stop returns true for a report,
// failing after 500 steps, and returns every report it saw.
func supportsVerifySteps(
	t *testing.T, ctx context.Context, b postgres.SupplyChainImpactSupportsBackfill,
	stop func(postgres.SupplyChainImpactSupportsBackfillReport) bool,
) []postgres.SupplyChainImpactSupportsBackfillReport {
	t.Helper()
	var reports []postgres.SupplyChainImpactSupportsBackfillReport
	for step := 0; step < 500; step++ {
		report, err := b.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step %d: %v", step, err)
		}
		reports = append(reports, report)
		if stop(report) {
			return reports
		}
	}
	t.Fatal("the backfill did not reach the expected state in 500 steps")
	return reports
}

func supportsVerifyOutcomes(reports []postgres.SupplyChainImpactSupportsBackfillReport) map[string]int {
	out := map[string]int{}
	for _, report := range reports {
		if report.VerifySlice != "" {
			out[report.VerifySlice]++
		}
	}
	return out
}

func supportsVerifyIsComplete(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
	return r.Complete
}

// supportsVerifyCursor reads the stored verification cursor and whether it
// belongs to the current sweep.
func supportsVerifyCursor(t *testing.T, ctx context.Context, db *sql.DB) (cursor [3]string, current bool, sweeps int) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE(verify_cursor_scope_id, ''), COALESCE(verify_cursor_generation_id, ''),
       COALESCE(verify_cursor_fact_id, ''), COALESCE(verify_sweep = sweeps, FALSE), sweeps
FROM supply_chain_impact_finding_supports_backfill`).Scan(&cursor[0], &cursor[1], &cursor[2], &current, &sweeps); err != nil {
		t.Fatalf("read the verification cursor: %v", err)
	}
	return cursor, current, sweeps
}

// supportsVerifyKeyAfter reports whether key (scope, generation, fact) sorts
// after the cursor in the database's own order, the order the slices walk.
func supportsVerifyKeyAfter(t *testing.T, ctx context.Context, db *sql.DB, key, cursor [3]string) bool {
	t.Helper()
	var after bool
	if err := db.QueryRowContext(ctx, `SELECT ($1::text, $2::text, $3::text) > ($4::text, $5::text, $6::text)`,
		key[0], key[1], key[2], cursor[0], cursor[1], cursor[2]).Scan(&after); err != nil {
		t.Fatalf("compare keys: %v", err)
	}
	return after
}

// supportsVerifyPairs returns the two seeded pairs in the order the slices walk.
func supportsVerifyPairs(t *testing.T, ctx context.Context, db *sql.DB) (first, last [2]string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT DISTINCT scope_id, generation_id FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
ORDER BY scope_id, generation_id`)
	if err != nil {
		t.Fatalf("list pairs: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var pairs [][2]string
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			t.Fatalf("scan pair: %v", err)
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil || len(pairs) != 2 {
		t.Fatalf("pairs = %v (err %v), want the two seeded pairs", pairs, err)
	}
	return pairs[0], pairs[1]
}

// supportsVerifyWritePair runs the real writer for one pair with the given
// repositories: a replace-set write, so a repository left out is retracted
// (its fact tombstoned, its support removed in the same transaction) and a new
// one is inserted with its support.
func supportsVerifyWritePair(t *testing.T, ctx context.Context, db *sql.DB, pair [2]string, intent string, repositories ...string) {
	t.Helper()
	findings := []supplychaincore.SupplyChainImpactFinding{}
	for _, repository := range repositories {
		findings = append(findings, replaceSetFinding(repository))
	}
	write := replaceSetWrite(intent, findings...)
	write.ScopeID, write.GenerationID = pair[0], pair[1]
	if _, err := replaceSetWriter(db).WriteSupplyChainImpactFindings(ctx, write); err != nil {
		t.Fatalf("writer pass %s: %v", intent, err)
	}
}

func supportsVerifyRepositories(n int) []string {
	out := make([]string, 0, n)
	for j := 0; j < n; j++ {
		out = append(out, fmt.Sprintf("repository:r_10880_bf_%d", j))
	}
	return out
}

// supportsVerifyAssertClean checks the end state every verification test
// requires: the marker set, no dirty scope, both pairs mirrored, and no support
// without a live fact.
func supportsVerifyAssertClean(t *testing.T, ctx context.Context, db *sql.DB, step string) {
	t.Helper()
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, postgres.SQLDB{DB: db}); err != nil || !ready {
		t.Fatalf("%s: ready = %v (err %v), want true", step, ready, err)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 0 {
		t.Fatalf("%s: %d dirty scopes under a set marker", step, n)
	}
	for _, pair := range [][2]string{
		{replaceSetLiveScope, replaceSetLiveGeneration},
		{replaceSetSecondScope, replaceSetSecondGeneration},
	} {
		supportsLiveAssertMirror(t, ctx, db, pair[0], pair[1], step+" "+pair[0])
	}
	var strays int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_supports s
LEFT JOIN fact_records f ON f.fact_id = s.fact_id AND NOT f.is_tombstone WHERE f.fact_id IS NULL`).Scan(&strays); err != nil || strays != 0 {
		t.Fatalf("%s: supports without a live fact = %d (err %v), want 0", step, strays, err)
	}
}

// TestSupplyChainImpactSupportsVerifyCatchesALateOrphanLive is the seeded
// RED/GREEN pair of the orphan half against the real slice statement. With
// one-row slices the verification starts; then a real writer retracts a finding
// of the pair the slices reach last and inserts one into the first pair, between
// slices. In the planted run an orphan support (one a writer without the
// maintainer would leave) is put back for the retracted fact, after the cursor:
// a later slice must catch it, remove it under the pair's lock, and only then
// set the marker. The control run must see no orphan slice. Neither run may see
// a gap: the writer keeps supports current between slices.
func TestSupplyChainImpactSupportsVerifyCatchesALateOrphanLive(t *testing.T) {
	for _, planted := range []bool{true, false} {
		t.Run(fmt.Sprintf("planted=%v", planted), func(t *testing.T) {
			ctx, db := openPreC2ReplaceSetLiveDB(t)
			replaceSetSeedSecondScope(t, ctx, db)
			supportsBackfillSeed(t, ctx, db)
			backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 1000, VerifySliceSize: 1}
			first, last := supportsVerifyPairs(t, ctx, db)
			before := supportsVerifySteps(t, ctx, backfill, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
				return r.VerifySlice == telemetry.SupplyChainImpactSupportsVerifySliceAdvance
			})
			lastLive := len(supportsLiveFactIDs(t, ctx, db, last[0], last[1]))
			supportsVerifyWritePair(t, ctx, db, last, "intent:10880:verify-retract", supportsVerifyRepositories(lastLive)[:lastLive-1]...)
			firstLive := len(supportsLiveFactIDs(t, ctx, db, first[0], first[1]))
			supportsVerifyWritePair(t, ctx, db, first, "intent:10880:verify-insert",
				append(supportsVerifyRepositories(firstLive), "repository:r_10880_bf_new")...)
			if planted {
				var orphan [3]string
				orphan[0], orphan[1] = last[0], last[1]
				if err := db.QueryRowContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id,
    finding_id, has_payload_finding_id, priority_score, source_confidence, suppression_state, payload)
SELECT fact_id, 'orphan', scope_id, generation_id, 'orphan', 0, 0, 'reported', 'active', '{}'::jsonb
FROM fact_records
WHERE is_tombstone = TRUE AND scope_id = $1 AND generation_id = $2
  AND fact_id NOT IN (SELECT fact_id FROM supply_chain_impact_finding_supports)
ORDER BY fact_id DESC LIMIT 1
RETURNING fact_id`, last[0], last[1]).Scan(&orphan[2]); err != nil {
					t.Fatalf("plant the orphan: %v", err)
				}
				cursor, current, _ := supportsVerifyCursor(t, ctx, db)
				if !current || !supportsVerifyKeyAfter(t, ctx, db, orphan, cursor) {
					t.Fatalf("the planted orphan %v is not after the verification cursor %v (current %v): not a late slice", orphan, cursor, current)
				}
			}
			after := supportsVerifySteps(t, ctx, backfill, supportsVerifyIsComplete)
			outcomes := supportsVerifyOutcomes(append(before, after...))
			wantOrphans := 0
			if planted {
				wantOrphans = 1
			}
			if outcomes[telemetry.SupplyChainImpactSupportsVerifySliceOrphan] != wantOrphans ||
				outcomes[telemetry.SupplyChainImpactSupportsVerifySliceGap] != 0 ||
				outcomes[telemetry.SupplyChainImpactSupportsVerifySliceAdvance] < 2 ||
				outcomes[telemetry.SupplyChainImpactSupportsVerifySliceLast] != 1 {
				t.Fatalf("slice outcomes = %v, want %d orphan, no gap, at least 2 advances and 1 last", outcomes, wantOrphans)
			}
			supportsVerifyAssertClean(t, ctx, db, "after the verification")
		})
	}
}

// TestSupplyChainImpactSupportsVerifyRestartsOnALateMissLive is the seeded
// RED/GREEN pair of the missing half: after the verification started, the
// support of the last live finding in slice order is deleted (an old binary's
// write path in a rolling upgrade). A later slice must find it, restart the
// sweep and drop the verification cursor; the marker is set only after the
// restarted sweep refills it. The control run completes with no gap.
func TestSupplyChainImpactSupportsVerifyRestartsOnALateMissLive(t *testing.T) {
	for _, planted := range []bool{true, false} {
		t.Run(fmt.Sprintf("planted=%v", planted), func(t *testing.T) {
			ctx, db := openPreC2ReplaceSetLiveDB(t)
			replaceSetSeedSecondScope(t, ctx, db)
			supportsBackfillSeed(t, ctx, db)
			backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 1000, VerifySliceSize: 1}
			before := supportsVerifySteps(t, ctx, backfill, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
				return r.VerifySlice == telemetry.SupplyChainImpactSupportsVerifySliceAdvance
			})
			_, _, sweepsBefore := supportsVerifyCursor(t, ctx, db)
			if planted {
				var missing [3]string
				if err := db.QueryRowContext(ctx, `
DELETE FROM supply_chain_impact_finding_supports WHERE fact_id = (
    SELECT fact_id FROM fact_records
    WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
    ORDER BY scope_id DESC, generation_id DESC, fact_id DESC LIMIT 1)
RETURNING scope_id, generation_id, fact_id`).Scan(&missing[0], &missing[1], &missing[2]); err != nil {
					t.Fatalf("drop the last support: %v", err)
				}
				cursor, current, _ := supportsVerifyCursor(t, ctx, db)
				if !current || !supportsVerifyKeyAfter(t, ctx, db, missing, cursor) {
					t.Fatalf("the dropped support %v is not after the verification cursor %v: not a late slice", missing, cursor)
				}
			}
			gapSeen := false
			after := supportsVerifySteps(t, ctx, backfill, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
				if r.VerifySlice == telemetry.SupplyChainImpactSupportsVerifySliceGap && !gapSeen {
					gapSeen = true
					if r.Missing != 1 || r.Complete {
						t.Fatalf("gap step = %+v, want one missing support and no marker", r)
					}
					if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, postgres.SQLDB{DB: db}); ready {
						t.Fatal("ready over a gap")
					}
					if _, current, sweeps := supportsVerifyCursor(t, ctx, db); current || sweeps != sweepsBefore+1 {
						t.Fatalf("after the gap: cursor current %v, sweeps %d, want a dropped cursor and sweeps %d", current, sweeps, sweepsBefore+1)
					}
				}
				return r.Complete
			})
			outcomes := supportsVerifyOutcomes(append(before, after...))
			if gaps := outcomes[telemetry.SupplyChainImpactSupportsVerifySliceGap]; (gaps == 1) != planted || gaps > 1 {
				t.Fatalf("slice outcomes = %v, want exactly one gap only in the planted run", outcomes)
			}
			supportsVerifyAssertClean(t, ctx, db, "after the verification")
		})
	}
}

// TestSupplyChainImpactSupportsVerifyResumesAfterKillLive proves the
// verification cursor survives a process death: a new backfill value continues
// after the stored cursor (its first slice moves the cursor forward, it never
// starts over), keeps the sweep, and finishes.
func TestSupplyChainImpactSupportsVerifyResumesAfterKillLive(t *testing.T) {
	ctx, db := openPreC2ReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	first := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 1000, VerifySliceSize: 1}
	advances := 0
	supportsVerifySteps(t, ctx, first, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
		if r.VerifySlice == telemetry.SupplyChainImpactSupportsVerifySliceAdvance {
			advances++
		}
		return advances == 2
	})
	stored, current, sweeps := supportsVerifyCursor(t, ctx, db)
	if !current || stored[2] == "" {
		t.Fatalf("stored cursor = %v (current %v), want the end of the killed process's last slice", stored, current)
	}
	second := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 1000, VerifySliceSize: 1}
	report, err := second.RunStep(ctx)
	if err != nil || report.VerifySlice != telemetry.SupplyChainImpactSupportsVerifySliceAdvance {
		t.Fatalf("first step after the restart = %+v (err %v), want an advance slice", report, err)
	}
	resumed, _, sweepsAfter := supportsVerifyCursor(t, ctx, db)
	if !supportsVerifyKeyAfter(t, ctx, db, resumed, stored) || sweepsAfter != sweeps {
		t.Fatalf("cursor after the restart = %v (sweeps %d), want past %v in sweep %d: the verification started over", resumed, sweepsAfter, stored, sweeps)
	}
	supportsVerifySteps(t, ctx, second, supportsVerifyIsComplete)
	supportsVerifyAssertClean(t, ctx, db, "after the resumed verification")
}

// TestSupplyChainImpactSupportsVerifyWaitsForAnOldWriterLive proves an old
// writer between slices (a finding fact written without the maintainer, in a
// pair the verification already passed) keeps the marker unset: the fence marks
// the scope, a step drains it, and the marker is set only with the fence empty
// and the old writer's fact backed by a support.
func TestSupplyChainImpactSupportsVerifyWaitsForAnOldWriterLive(t *testing.T) {
	ctx, db := openPreC2ReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 1000, VerifySliceSize: 1}
	before := supportsVerifySteps(t, ctx, backfill, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
		return r.VerifySlice == telemetry.SupplyChainImpactSupportsVerifySliceAdvance
	})
	supportsOldWriterInsert(t, ctx, db, "fact:verify:old-writer", "reducer_supply_chain_impact_finding", false)
	cursor, current, _ := supportsVerifyCursor(t, ctx, db)
	oldKey := [3]string{replaceSetLiveScope, replaceSetLiveGeneration, "fact:verify:old-writer"}
	if !current || supportsVerifyKeyAfter(t, ctx, db, oldKey, cursor) {
		t.Fatalf("the old writer's key %v is not behind the verification cursor %v (current %v): the slices have not passed it", oldKey, cursor, current)
	}
	if n := supportsDirtyScopes(t, ctx, db); n != 1 {
		t.Fatalf("dirty scopes after the old writer = %d, want 1", n)
	}
	drains := 0
	after := supportsVerifySteps(t, ctx, backfill, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
		drains += r.Drained
		if r.Complete && r.DirtyScopes != 0 {
			t.Fatalf("marker set with %d dirty scopes", r.DirtyScopes)
		}
		return r.Complete
	})
	if drains != 1 {
		t.Fatalf("drains = %d, want 1", drains)
	}
	if gaps := supportsVerifyOutcomes(append(before, after...))[telemetry.SupplyChainImpactSupportsVerifySliceGap]; gaps != 0 {
		t.Fatalf("gap slices = %d, want 0: the drain, not a restart, must repair the old writer's scope", gaps)
	}
	supportsVerifyAssertClean(t, ctx, db, "after the old writer drained")
}

// TestSupplyChainImpactSupportsVerifyResetMarkerStartsOverLive proves a
// completed verification leaves no cursor behind. With the marker's completed_at
// reset and no sweep bump (the reset the fence and fleet-gate live tests use),
// the next verification starts at the first key of the key space and checks
// every interval again before the marker is set.
func TestSupplyChainImpactSupportsVerifyResetMarkerStartsOverLive(t *testing.T) {
	ctx, db := openPreC2ReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 1000, VerifySliceSize: 1}
	supportsVerifySteps(t, ctx, backfill, supportsVerifyIsComplete)
	var anyVerify bool
	if err := db.QueryRowContext(ctx, `
SELECT verify_cursor_scope_id IS NOT NULL OR verify_cursor_generation_id IS NOT NULL
    OR verify_cursor_fact_id IS NOT NULL OR verify_sweep IS NOT NULL
FROM supply_chain_impact_finding_supports_backfill`).Scan(&anyVerify); err != nil || anyVerify {
		t.Fatalf("verification columns after completion: set %v (err %v), want all NULL", anyVerify, err)
	}
	_, _, sweeps := supportsVerifyCursor(t, ctx, db)
	if _, err := db.ExecContext(ctx, `UPDATE supply_chain_impact_finding_supports_backfill
SET completed_at = NULL, cursor_scope_id = '', cursor_generation_id = '', cursor_fact_id = ''`); err != nil {
		t.Fatalf("reset marker: %v", err)
	}
	reports := supportsVerifySteps(t, ctx, backfill, func(r postgres.SupplyChainImpactSupportsBackfillReport) bool {
		return r.VerifySlice != "" || r.Complete
	})
	if last := reports[len(reports)-1]; last.Complete || last.VerifySlice != telemetry.SupplyChainImpactSupportsVerifySliceAdvance {
		t.Fatalf("first slice after the reset = %+v, want an advance (not the last slice of the old cursor)", last)
	}
	var first [3]string
	if err := db.QueryRowContext(ctx, `
SELECT scope_id, generation_id, fact_id FROM (
    SELECT scope_id, generation_id, fact_id FROM fact_records
    WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
    UNION ALL
    SELECT scope_id, generation_id, fact_id FROM supply_chain_impact_finding_supports
) AS k ORDER BY scope_id, generation_id, fact_id LIMIT 1`).Scan(&first[0], &first[1], &first[2]); err != nil {
		t.Fatalf("first key: %v", err)
	}
	cursor, current, sweepsAfter := supportsVerifyCursor(t, ctx, db)
	if !current || cursor != first || sweepsAfter != sweeps {
		t.Fatalf("cursor after the first slice = %v (current %v, sweeps %d), want the first key %v in sweep %d", cursor, current, sweepsAfter, first, sweeps)
	}
	supportsVerifySteps(t, ctx, backfill, supportsVerifyIsComplete)
	supportsVerifyAssertClean(t, ctx, db, "after the reset verification")
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Lab proofs for the C1k fill and the lazy stored read (#10880, arbiter ruling 1.3 and
// 1.6, T6). They run at a scale an environment variable sets and print what they
// measured; on a shared server the WAL delta includes every other writer, so run them on
// a quiet disposable one. Defaults are small enough for any disposable database.
//
//	ESHU_LAB_FILL_ROWS       supports to seed and fill (default 5000)
//	ESHU_LAB_FILL_PAIR_ROWS  supports per scope generation (default 2000)
//	ESHU_LAB_FILL_REPORT     path to write the JSON report to (optional)
//	ESHU_LAB_PLAN_DIR        directory to write the EXPLAIN plan texts to (optional)

func labInt(t *testing.T, name string, def int) int {
	t.Helper()
	raw := labEnv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		t.Fatalf("%s=%q, want a positive integer", name, raw)
	}
	return v
}

func labScopeGeneration(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) {
	t.Helper()
	supportsExec(t, ctx, db, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
  observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, now(), now(), 'active', '{}'::jsonb)`, scope)
	supportsExec(t, ctx, db, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, generation, scope)
	supportsExec(t, ctx, db, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generation, scope)
}

type labStats struct {
	WALLSN                       string
	WALRecords, WALFPI, WALBytes int64
	TupUpd, TupHotUpd, DeadTup   int64
	TableBytes, IndexBytes       int64
}

func labRead(t *testing.T, ctx context.Context, db *sql.DB) labStats {
	t.Helper()
	// Statistics are flushed lazily; force a flush so the deltas are complete.
	supportsExec(t, ctx, db, `SELECT pg_stat_force_next_flush()`)
	time.Sleep(1500 * time.Millisecond)
	var s labStats
	if err := db.QueryRowContext(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&s.WALLSN); err != nil {
		t.Fatalf("read the WAL position: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT wal_records, wal_fpi, wal_bytes::bigint FROM pg_stat_wal`).Scan(&s.WALRecords, &s.WALFPI, &s.WALBytes); err != nil {
		t.Fatalf("read pg_stat_wal: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT n_tup_upd, n_tup_hot_upd, n_dead_tup FROM pg_stat_user_tables WHERE relname = 'supply_chain_impact_finding_supports'`).Scan(&s.TupUpd, &s.TupHotUpd, &s.DeadTup); err != nil {
		t.Fatalf("read pg_stat_user_tables: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT pg_relation_size('supply_chain_impact_finding_supports'), pg_indexes_size('supply_chain_impact_finding_supports')`).Scan(&s.TableBytes, &s.IndexBytes); err != nil {
		t.Fatalf("read relation sizes: %v", err)
	}
	return s
}

// TestSupplyChainImpactSupportsStableKeyFillLabLive is the T6 harness: it seeds
// ESHU_LAB_FILL_ROWS keyless supports across pairs of ESHU_LAB_FILL_PAIR_ROWS, runs the
// fill to completion with the production chunk size, and reports the WAL bytes per
// filled row (the log position delta, and pg_stat_wal), the HOT update share, the dead
// tuples, the wall time and the per-step time (the pair lock is held for one step). It
// asserts only that every fillable support was filled; the numbers are the result.
func TestSupplyChainImpactSupportsStableKeyFillLabLive(t *testing.T) {
	rows := labInt(t, "ESHU_LAB_FILL_ROWS", 5000)
	pairRows := labInt(t, "ESHU_LAB_FILL_PAIR_ROWS", 2000)
	ctx, db := openReplaceSetLiveDB(t)
	ctx, cancel := context.WithTimeout(ctx, 4*time.Hour)
	defer cancel()

	pairs := (rows + pairRows - 1) / pairRows
	for i := 0; i < pairs; i++ {
		scope := fmt.Sprintf("scope:c1k:lab:%06d", i)
		generation := scope + ":g1"
		labScopeGeneration(t, ctx, db, scope, generation)
		n := pairRows
		if i == pairs-1 {
			n = rows - pairRows*(pairs-1)
		}
		fillSeedRows(t, ctx, db, scope, generation, fmt.Sprintf("lab%06d:", i), n) // fact ids are unique across pairs
	}
	// The supports are the writer's derivation of the facts, keys cleared; a rewrite
	// then packs the table the way a long-lived one is packed (no dead tuples or free
	// space from the clearing), so the HOT share below is not skewed by the seed.
	fillSeedSupports(t, ctx, db, "lab")
	supportsExec(t, ctx, db, `VACUUM FULL supply_chain_impact_finding_supports`)
	supportsExec(t, ctx, db, `ANALYZE supply_chain_impact_finding_supports`)
	supportsExec(t, ctx, db, `ANALYZE fact_records`)
	supportsExec(t, ctx, db, `CHECKPOINT`)
	seeded := fillCount(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE stable_fact_key IS NULL AND fact_id LIKE 'lab%'`)

	before := labRead(t, ctx, db)
	fill := postgres.SupplyChainImpactStableKeyFill{DB: postgres.SQLDB{DB: db}}
	started := time.Now()
	var filled int64
	var steps int
	var maxStep, sumStep time.Duration
	for {
		stepStarted := time.Now()
		report, err := fill.RunStep(ctx)
		if err != nil {
			t.Fatalf("step %d: %v", steps, err)
		}
		d := time.Since(stepStarted)
		steps++
		sumStep += d
		if report.Filled > 0 && d > maxStep {
			maxStep = d
		}
		filled += report.Filled
		if report.Complete {
			break
		}
	}
	wall := time.Since(started)
	after := labRead(t, ctx, db)

	if filled != int64(seeded) {
		t.Fatalf("filled %d of %d seeded keyless supports", filled, seeded)
	}
	var walBytes float64
	if err := db.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::float8`, after.WALLSN, before.WALLSN).Scan(&walBytes); err != nil {
		t.Fatalf("diff the WAL positions: %v", err)
	}
	upd, hot := after.TupUpd-before.TupUpd, after.TupHotUpd-before.TupHotUpd
	report := map[string]any{
		"rows_seeded": seeded, "rows_filled": filled, "pairs": pairs, "pair_rows": pairRows, "steps": steps,
		"wall_seconds": wall.Seconds(), "avg_step_ms": float64(sumStep.Milliseconds()) / float64(steps), "max_fill_step_ms": maxStep.Milliseconds(),
		"wal_lsn_bytes": walBytes, "wal_lsn_bytes_per_row": walBytes / float64(filled),
		"pg_stat_wal_bytes": after.WALBytes - before.WALBytes, "pg_stat_wal_bytes_per_row": float64(after.WALBytes-before.WALBytes) / float64(filled),
		"wal_records": after.WALRecords - before.WALRecords, "wal_fpi": after.WALFPI - before.WALFPI,
		"tup_upd": upd, "tup_hot_upd": hot, "hot_share": float64(hot) / float64(max(upd, 1)),
		"dead_tup_after": after.DeadTup, "table_bytes": after.TableBytes, "index_bytes": after.IndexBytes,
		"note": "WAL deltas include every writer on the server: run on a quiet disposable database",
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("T6 fill report:\n%s", encoded)
	if path := labEnv("ESHU_LAB_FILL_REPORT"); path != "" {
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			t.Fatalf("write the report: %v", err)
		}
	}
}

var labPKeyLoops = regexp.MustCompile(`fact_records_pkey on fact_records twin.*loops=(\d+)`)

// TestSupplyChainImpactSupportsLazyStoredReadPlanLive is the lazy-twin proof of ruling
// 1.3: for a pair whose supports all carry their key the twin subplan is never executed
// (no fact_records probe), and for a pair whose keys are NULL it probes the primary key
// once per row; both answers equal the stored read over the fact rows. The plan texts are
// written to ESHU_LAB_PLAN_DIR when set.
func TestSupplyChainImpactSupportsLazyStoredReadPlanLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 C1 lazy stored read plan proof")
	rows := labInt(t, "ESHU_LAB_FILL_PAIR_ROWS", 2000)
	const filledScope, filledGen = "scope:c1:plan:filled", "scope:c1:plan:filled:g1"
	const nullScope, nullGen = "scope:c1:plan:null", "scope:c1:plan:null:g1"
	for _, p := range [][2]string{{filledScope, filledGen}, {nullScope, nullGen}} {
		labScopeGeneration(t, h.ctx, h.db, p[0], p[1])
		fillSeedRows(t, h.ctx, h.db, p[0], p[1], "plan:"+p[0]+":", rows) // fact ids are unique across pairs
	}
	fillSeedSupports(t, h.ctx, h.db, "plan:")
	// The filled pair: every support carries the twin's key.
	supportsExec(t, h.ctx, h.db, `
UPDATE supply_chain_impact_finding_supports AS s SET stable_fact_key = f.stable_fact_key
FROM fact_records AS f WHERE f.fact_id = s.fact_id AND s.scope_id = $1`, filledScope)
	supportsExec(t, h.ctx, h.db, `ANALYZE supply_chain_impact_finding_supports`)
	supportsExec(t, h.ctx, h.db, `ANALYZE fact_records`)

	query := supplychaincore.ProofStoredFindingsQuery()
	plans := map[string]string{}
	for name, p := range map[string][2]string{"filled": {filledScope, filledGen}, "null": {nullScope, nullGen}} {
		plans[name] = h.explain("EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query, p[0], p[1])
		if dir := labEnv("ESHU_LAB_PLAN_DIR"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, "stored_read_"+name+".txt"), []byte(plans[name]+"\n"), 0o644); err != nil {
				t.Fatalf("write the %s plan: %v", name, err)
			}
		}
	}
	if !strings.Contains(plans["filled"], "SubPlan") || !strings.Contains(plans["filled"], "never executed") {
		t.Errorf("filled keys: the twin subplan is missing or ran; want a SubPlan that was never executed:\n%s", plans["filled"])
	}
	if m := labPKeyLoops.FindStringSubmatch(plans["filled"]); m != nil {
		t.Errorf("filled keys: fact_records_pkey was probed (loops=%s), want no probe:\n%s", m[1], plans["filled"])
	}
	m := labPKeyLoops.FindStringSubmatch(plans["null"])
	if m == nil {
		t.Fatalf("NULL keys: no fact_records_pkey probe in the plan:\n%s", plans["null"])
	}
	if loops, _ := strconv.Atoi(m[1]); loops != rows {
		t.Errorf("NULL keys: %d primary-key probes, want one per row (%d)", loops, rows)
	}

	// The answers equal the stored read over the fact rows for the same pair.
	for _, p := range [][2]string{{filledScope, filledGen}, {nullScope, nullGen}} {
		legacy := scanStoredRead(t, h.ctx, h.db, supportsLegacyStoredRead, p[0], p[1])
		supports := scanStoredRead(t, h.ctx, h.db, query, p[0], p[1])
		if len(legacy) != rows || len(supports) != rows {
			t.Fatalf("pair %s: legacy read %d rows, supports read %d rows, want %d each", p[0], len(legacy), len(supports), rows)
		}
		for id, l := range legacy {
			s := supports[id]
			l.Token, s.Token = 0, 0
			if l != s {
				t.Fatalf("pair %s fact %s: supports read %+v != legacy read %+v", p[0], id, s, l)
			}
		}
	}
}

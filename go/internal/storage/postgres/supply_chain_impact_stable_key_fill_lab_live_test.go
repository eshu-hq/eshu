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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
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
//	ESHU_LAB_PAYLOAD_BYTES   pad each fact payload (and so each support's) to about this many
//	                         bytes (default 0; QA is about 9,000 per supports row)
//	ESHU_LAB_BUILD_REPORT    path to write the index build report to (optional)
//	ESHU_LAB_KEEP_DB=1       keep the seeded database (also ESHU_PROOF_KEEP_DB=1, which the
//	                         clone helper reads); its name is printed
//	ESHU_LAB_ALLOW_BIG=1     run above the disk estimate cap (below)
//
// Disk estimate: rows x (2 x payload + 6 KB of row, index and TOAST overhead) x 1.5 for WAL and index
// build temp space. At 300,000 rows and a 9,000-byte payload that is about 11 GB; a run whose estimate
// is above 25 GB refuses unless ESHU_LAB_ALLOW_BIG=1.
//
// The lab host has about 36 GB of free disk and a supports row with its fact is roughly 9 KB, so the
// lab scale is capped at 300,000 rows: a larger ESHU_LAB_FILL_ROWS fails unless
// ESHU_LAB_FILL_ALLOW_LARGE=1 is set. The test deadline scales with the row count (5 minutes plus one
// second per 1,000 rows); the go test -timeout stays the caller's. The seed is set-based: one INSERT
// per pair for the facts, then one derivation of the supports.

// labMaxRows is the lab scale cap (see the file comment).
const labMaxRows = 300000

// labDiskCapBytes is the estimated disk a lab run may use without ESHU_LAB_ALLOW_BIG=1.
const labDiskCapBytes = 25 << 30

// labDiskGuard refuses a run whose disk estimate (see the file comment) is above the cap.
func labDiskGuard(t *testing.T, rows, payloadBytes int) {
	t.Helper()
	estimate := float64(rows) * float64(2*payloadBytes+6*1024) * 1.5
	t.Logf("lab disk estimate: %.1f GB for %d rows at a %d-byte payload", estimate/(1<<30), rows, payloadBytes)
	if estimate > labDiskCapBytes && labEnv("ESHU_LAB_ALLOW_BIG") != "1" {
		t.Fatalf("estimated %.1f GB is over the %d GB lab cap; set ESHU_LAB_ALLOW_BIG=1 to run anyway", estimate/(1<<30), labDiskCapBytes>>30)
	}
}

// labKeepDB reports whether the seeded database is kept, and prints its name. The keep itself is
// the clone helper's (ESHU_PROOF_KEEP_DB=1); ESHU_LAB_KEEP_DB=1 is the lab spelling.
func labKeepDB(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var name string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatalf("read the database name: %v", err)
	}
	t.Logf("lab database: %s", name)
	if labEnv("ESHU_LAB_KEEP_DB") == "1" && labEnv("ESHU_PROOF_KEEP_DB") != "1" {
		t.Logf("ESHU_LAB_KEEP_DB=1 only keeps the database when ESHU_PROOF_KEEP_DB=1 is also set (the clone helper reads that one); this run drops %s", name)
	}
}

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

// labInt0 is labInt with a default of 0, which the variable may also be set to.
func labInt0(t *testing.T, name string) int {
	t.Helper()
	raw := labEnv(name)
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		t.Fatalf("%s=%q, want a non-negative integer", name, raw)
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
	if rows > labMaxRows && labEnv("ESHU_LAB_FILL_ALLOW_LARGE") != "1" {
		t.Fatalf("ESHU_LAB_FILL_ROWS=%d is over the %d-row lab cap (about 9 KB a row on a host with 36 GB free); set ESHU_LAB_FILL_ALLOW_LARGE=1 to override", rows, labMaxRows)
	}
	payload := labInt0(t, "ESHU_LAB_PAYLOAD_BYTES")
	labDiskGuard(t, rows, payload)
	_, db := openReplaceSetLiveDB(t)
	// openReplaceSetLiveDB's context is a fixed 5 min (#11488); the seed and the fill scale with the rows.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute+time.Duration(rows)*time.Millisecond)
	defer cancel()
	labKeepDB(t, ctx, db)

	pairs := (rows + pairRows - 1) / pairRows
	for i := 0; i < pairs; i++ {
		scope := fmt.Sprintf("scope:c1k:lab:%06d", i)
		generation := scope + ":g1"
		labScopeGeneration(t, ctx, db, scope, generation)
		n := pairRows
		if i == pairs-1 {
			n = rows - pairRows*(pairs-1)
		}
		fillSeedRowsPadded(t, ctx, db, scope, generation, fmt.Sprintf("lab%06d:", i), n, payload) // fact ids are unique across pairs
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

// indexBuildSQL returns the exact statement of migration 354 from the embedded migrations,
// not a copy, so the build the proof times is the build the roll runs.
func indexBuildSQL(t *testing.T) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(def.Path, "354_supply_chain_impact_supports_keyed_probe_idx.sql") {
			return def.SQL
		}
	}
	t.Fatal("migration 354 is not among the embedded migrations")
	return ""
}

// TestSupplyChainImpactSupportsKeyedProbeIndexBuildLabLive is the lab build proof of migration 354
// (ruling 2.2): it seeds ESHU_LAB_FILL_ROWS supports (payload padded to ESHU_LAB_PAYLOAD_BYTES), drops
// the index if the bootstrap built it, runs the migration's CREATE INDEX CONCURRENTLY text, and
// reports the build seconds, the WAL written (log position delta, so run it on a quiet server), the
// temp bytes (pg_stat_database delta), the index size and whether the index is valid and ready.
// ESHU_LAB_BUILD_REPORT writes the JSON.
func TestSupplyChainImpactSupportsKeyedProbeIndexBuildLabLive(t *testing.T) {
	rows := labInt(t, "ESHU_LAB_FILL_ROWS", 5000)
	pairRows := labInt(t, "ESHU_LAB_FILL_PAIR_ROWS", 2000)
	payload := labInt0(t, "ESHU_LAB_PAYLOAD_BYTES")
	if rows > labMaxRows && labEnv("ESHU_LAB_ALLOW_BIG") != "1" {
		t.Fatalf("ESHU_LAB_FILL_ROWS=%d is over the %d-row lab cap; set ESHU_LAB_ALLOW_BIG=1 to override", rows, labMaxRows)
	}
	labDiskGuard(t, rows, payload)
	_, db := openReplaceSetLiveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+time.Duration(rows)*2*time.Millisecond)
	defer cancel()
	labKeepDB(t, ctx, db)

	pairs := (rows + pairRows - 1) / pairRows
	for i := 0; i < pairs; i++ {
		scope := fmt.Sprintf("scope:c1k:build:%06d", i)
		generation := scope + ":g1"
		labScopeGeneration(t, ctx, db, scope, generation)
		n := pairRows
		if i == pairs-1 {
			n = rows - pairRows*(pairs-1)
		}
		fillSeedRowsPadded(t, ctx, db, scope, generation, fmt.Sprintf("bld%06d:", i), n, payload)
	}
	if err := postgres.ProofSyncSupportsFromFacts(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("derive supports from facts: %v", err)
	}
	supportsExec(t, ctx, db, `DROP INDEX IF EXISTS supply_chain_impact_finding_supports_keyed_probe_idx`)
	supportsExec(t, ctx, db, `VACUUM (ANALYZE) supply_chain_impact_finding_supports`)
	supportsExec(t, ctx, db, `CHECKPOINT`)

	statement := indexBuildSQL(t)
	before := labRead(t, ctx, db)
	var tempBefore int64
	if err := db.QueryRowContext(ctx, `SELECT temp_bytes FROM pg_stat_database WHERE datname = current_database()`).Scan(&tempBefore); err != nil {
		t.Fatalf("read temp bytes: %v", err)
	}
	started := time.Now()
	supportsExec(t, ctx, db, statement)
	build := time.Since(started)
	after := labRead(t, ctx, db)
	var tempAfter, indexBytes int64
	var valid, ready bool
	if err := db.QueryRowContext(ctx, `SELECT temp_bytes FROM pg_stat_database WHERE datname = current_database()`).Scan(&tempAfter); err != nil {
		t.Fatalf("read temp bytes: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT pg_relation_size(c.oid), i.indisvalid, i.indisready
FROM pg_class AS c JOIN pg_index AS i ON i.indexrelid = c.oid
WHERE c.relname = 'supply_chain_impact_finding_supports_keyed_probe_idx'`).Scan(&indexBytes, &valid, &ready); err != nil {
		t.Fatalf("read the built index: %v", err)
	}
	var walBytes float64
	if err := db.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::float8`, after.WALLSN, before.WALLSN).Scan(&walBytes); err != nil {
		t.Fatalf("diff the WAL positions: %v", err)
	}
	if !valid || !ready {
		t.Fatalf("the built index is valid=%v ready=%v, want both", valid, ready)
	}
	report := map[string]any{
		"rows": rows, "pair_rows": pairRows, "payload_bytes": payload, "build_seconds": build.Seconds(),
		"wal_lsn_bytes": walBytes, "temp_bytes": tempAfter - tempBefore, "index_bytes": indexBytes,
		"table_bytes": after.TableBytes, "indisvalid": valid, "indisready": ready,
		"statement": "the embedded migration 354, one CREATE INDEX CONCURRENTLY",
		"note":      "WAL delta includes every writer on the server: run on a quiet disposable database",
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("migration 354 build report:\n%s", encoded)
	if path := labEnv("ESHU_LAB_BUILD_REPORT"); path != "" {
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			t.Fatalf("write the report: %v", err)
		}
	}
}

// TestSupplyChainImpactSupportsKeyedProbePlanLive is the plan proof of ruling 15 (ii): with
// migration 354 built from the embedded text and the table analyzed, the keyed narrow
// probe the winners maintainer runs chooses supply_chain_impact_finding_supports_keyed_probe_idx
// with all five identifier equalities as index conditions and no Seq Scan on supports, under no
// planner override (no enable_* setting, no plan_cache_mode change). The statement is
// postgres.WinnersProofKeyedProbeSQL, rendered from the code. The plan text goes to ESHU_LAB_PLAN_DIR.
// ESHU_LAB_FILL_ROWS sets the seed (default 3,000; the lab scale is 300,000).
func TestSupplyChainImpactSupportsKeyedProbePlanLive(t *testing.T) {
	rows := labInt(t, "ESHU_LAB_FILL_ROWS", 3000)
	pairRows := labInt(t, "ESHU_LAB_FILL_PAIR_ROWS", 2000)
	payload := labInt0(t, "ESHU_LAB_PAYLOAD_BYTES")
	if rows > labMaxRows && labEnv("ESHU_LAB_ALLOW_BIG") != "1" {
		t.Fatalf("ESHU_LAB_FILL_ROWS=%d is over the %d-row lab cap; set ESHU_LAB_ALLOW_BIG=1 to override", rows, labMaxRows)
	}
	labDiskGuard(t, rows, payload)
	_, db := openReplaceSetLiveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+time.Duration(rows)*2*time.Millisecond)
	defer cancel()
	labKeepDB(t, ctx, db)

	pairs := (rows + pairRows - 1) / pairRows
	for i := 0; i < pairs; i++ {
		scope := fmt.Sprintf("scope:c1k:probe:%06d", i)
		generation := scope + ":g1"
		labScopeGeneration(t, ctx, db, scope, generation)
		n := pairRows
		if i == pairs-1 {
			n = rows - pairRows*(pairs-1)
		}
		fillSeedRowsPadded(t, ctx, db, scope, generation, fmt.Sprintf("prb%06d:", i), n, payload)
	}
	if err := postgres.ProofSyncSupportsFromFacts(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("derive supports from facts: %v", err)
	}
	supportsExec(t, ctx, db, `DROP INDEX IF EXISTS supply_chain_impact_finding_supports_keyed_probe_idx`)
	supportsExec(t, ctx, db, indexBuildSQL(t))
	supportsExec(t, ctx, db, `VACUUM (ANALYZE) supply_chain_impact_finding_supports`)

	// The probe reads its identifiers from the session table the keyed tick builds; build the
	// same shape on one connection and plan the statement inside a transaction (the statement
	// is CREATE TEMP TABLE ... ON COMMIT DROP).
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open a connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	idents := postgres.WinnersProofKeyedIdentsTable()
	for _, stmt := range []string{
		`CREATE TEMP TABLE ` + idents + ` (package_id text, repository_id text, subject_digest text, c0 text, advisory_id text,
  age_captured_at timestamptz, age_seq bigint) ON COMMIT DROP`,
		`INSERT INTO ` + idents + `
SELECT 'pkg:npm/p' || (i % 50), 'repository:r' || (i % 100), '', 'CVE-2026-' || i, '', now(), i
FROM generate_series(1, 5) AS i`,
		`ANALYZE ` + idents,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare the identifier table: %v\n%s", err, stmt)
		}
	}
	rowsOut, err := tx.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+postgres.WinnersProofKeyedProbeSQL())
	if err != nil {
		t.Fatalf("explain the keyed probe: %v", err)
	}
	var lines []string
	for rowsOut.Next() {
		var line string
		if err := rowsOut.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rowsOut.Close(); err != nil {
		t.Fatalf("close plan: %v", err)
	}
	plan := strings.Join(lines, "\n")
	if dir := labEnv("ESHU_LAB_PLAN_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "keyed_probe.txt"), []byte(plan+"\n"), 0o644); err != nil {
			t.Fatalf("write the plan: %v", err)
		}
	}
	if problems := keyedProbePlanProblems(plan, keyedProbeIndexName, keyedProbeEqualities); len(problems) != 0 {
		t.Fatalf("the keyed probe plan is wrong: %v\n%s", problems, plan)
	}
}

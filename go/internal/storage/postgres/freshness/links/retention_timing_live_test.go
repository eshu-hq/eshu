// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// Retention timing (P8 of arbiter ruling arb-7127-3d; PB4 and PB7 of
// arb-7127-3d-b). The test uses only API that also exists before the fixes it
// measures, so the same file builds the base binary. run lines carry the
// WAL and checkpointer deltas around the prune (arb-7127-3d-e PE2). Driven by
// docs/internal/evidence/7127-ledger-retention-timing.sh.
const (
	timingDSNEnv      = "ESHU_RETENTION_TIMING_DSN"      // admin DSN
	timingModeEnv     = "ESHU_RETENTION_TIMING_MODE"     // build | run | drain | lockprobe | explain
	timingFixtureEnv  = "ESHU_RETENTION_TIMING_FIXTURE"  // see timingFixtures
	timingFixturesEnv = "ESHU_RETENTION_TIMING_FIXTURES" // build: comma-separated
	timingLabelEnv    = "ESHU_RETENTION_TIMING_LABEL"
	timingRowLimitEnv = "ESHU_RETENTION_TIMING_ROW_LIMIT"
	// timingExpectNarrowEnv makes lockprobe fail unless only tscope-00 and
	// tscope-00-g0 are locked while the delete runs (arb-7127-3d-c PC1 (b)).
	timingExpectNarrowEnv = "ESHU_RETENTION_TIMING_EXPECT_NARROW"
)

// timingFixture is one retention fixture: scopes of 26 generations (the first
// prunable ones old enough to prune, the rest inside the window, g25 active),
// 400 content facts per generation, and optionally the ledger: one activation
// per generation and a chain of links of 100 deltas, one of which, in
// tscope-00, carries bigLinkRows deltas.
type timingFixture struct {
	scopes, prunable int
	ledger           bool
	bigLinkRows      int
	// bigLinkGen is the newer generation of the big link, whose prior is
	// bigLinkGen-1. The big link's candidate must be tscope-00's oldest
	// generation for PB4; tscope-00-g0 is made the oldest candidate overall.
	bigLinkGen int
}

var timingFixtures = map[string]timingFixture{
	// P8: 20 scopes, 40 candidates, (g2 -> g1) of 250,000 rows names g1.
	"empty": {scopes: 20, prunable: 2},
	"link":  {scopes: 20, prunable: 2, ledger: true, bigLinkRows: 250000, bigLinkGen: 2},
	// PB4: 25 scopes, 100 candidates, (g1 -> g0) names the oldest candidate.
	"big771":  {scopes: 25, prunable: 4, ledger: true, bigLinkRows: 771201, bigLinkGen: 1},
	"big1542": {scopes: 25, prunable: 4, ledger: true, bigLinkRows: 1542402, bigLinkGen: 1},
}

func (f timingFixture) statements() []string {
	scope := `'tscope-' || lpad(s::text, 2, '0')`
	stmts := []string{
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status, active_generation_id)
SELECT %[1]s, 'repository', 'git', 'r' || s, 'git', 'r' || s, now(), now(), 'active', %[1]s || '-g25'
FROM generate_series(0, %[2]d) AS s`, scope, f.scopes-1),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, superseded_at)
SELECT %[1]s || '-g' || g, %[1]s, 'snapshot', now() - interval '30 days', now() - interval '30 days',
       CASE WHEN g = 25 THEN 'active' ELSE 'superseded' END,
       now() - interval '30 days' + g * interval '1 hour',
       CASE WHEN g = 25 THEN NULL
            WHEN s = 0 AND g = 0 THEN now() - interval '21 days'
            WHEN g < %[3]d THEN now() - interval '20 days' + g * interval '1 minute'
            ELSE now() - interval '10 minutes' + g * interval '1 second' END
FROM generate_series(0, %[2]d) AS s, generate_series(0, 25) AS g`, scope, f.scopes-1, f.prunable),
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    source_uri, observed_at, ingested_at, is_tombstone, payload)
SELECT gen.generation_id || '/' || i, gen.scope_id, gen.generation_id, 'content_entity', 'ent:' || i, 'git', 'ent:' || i,
       'f' || (i / 40) || '.go', now(), now(), FALSE,
       jsonb_build_object('repo_id', gen.scope_id, 'entity_id', 'e' || i, 'body', repeat('x', 300))
FROM scope_generations AS gen, generate_series(1, 400) AS i`,
	}
	if f.ledger {
		stmts = append(stmts,
			`INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT scope_id, generation_id, NULL, 'sweeper', COALESCE(activated_at, observed_at) FROM scope_generations ORDER BY generation_id`,
			fmt.Sprintf(`INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
    files_keys, content_entities_keys, facts_keys, computed_at)
SELECT %[1]s, %[1]s || '-g' || g, %[1]s || '-g' || (g - 1), 'incremental', 1,
       CASE WHEN s = 0 AND g = %[3]d THEN %[4]d ELSE 100 END, 0, 400, 0, now()
FROM generate_series(0, %[2]d) AS s, generate_series(1, 25) AS g`, scope, f.scopes-1, f.bigLinkGen, f.bigLinkRows),
			`INSERT INTO changed_since_link_deltas (scope_id, generation_id, prior_generation_id, fact_category, classification,
    stable_fact_key, prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
SELECT link.scope_id, link.generation_id, link.prior_generation_id, 'content_entities', 'updated',
       'content_entity:content-entity:' || md5(link.generation_id || r), 'content_entity', 'content_entity',
       sha256(('p' || r)::bytea), sha256(('c' || link.generation_id || r)::bytea), FALSE
FROM changed_since_links AS link, generate_series(1, link.delta_rows::int) AS r`,
			`INSERT INTO changed_since_link_bucket_counts (scope_id, generation_id, prior_generation_id, fact_category,
    classification, key_count)
SELECT scope_id, generation_id, prior_generation_id, 'content_entities', 'updated', delta_rows FROM changed_since_links`)
	}
	return append(stmts, `ANALYZE`)
}

func (f timingFixture) bigLinkQuery() string {
	return fmt.Sprintf(`SELECT count(*) FROM changed_since_links WHERE generation_id = 'tscope-00-g%d'`+
		` AND prior_generation_id = 'tscope-00-g%d'`, f.bigLinkGen, f.bigLinkGen-1)
}

func timingTemplateName(fixture string) string { return "eshu_rt_template_" + fixture }

func timingPolicy(rowLimit int) postgres.GenerationRetentionPolicy {
	return postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 0, MaxSupersededAge: time.Hour, BatchGenerationLimit: 100,
		BatchRowLimit: rowLimit, PolicyScope: "global", PolicyRevision: "7127-pr3d-timing",
	}
}

// TestRetentionTimingP8 builds template fixtures, or runs retention on a fresh
// clone of one and prints TIMING lines. run: one batch; its transaction's
// duration bounds how long it holds the scope and candidate generation rows
// FOR UPDATE (taken by its first real statement, released at commit). drain:
// batches until one prunes nothing, one line per batch.
func TestRetentionTimingP8(t *testing.T) {
	admin := strings.TrimSpace(os.Getenv(timingDSNEnv))
	if admin == "" {
		t.Skipf("set %s to run the retention timing harness", timingDSNEnv)
	}
	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer func() { _ = adminDB.Close() }()
	ctx := t.Context()
	switch mode := os.Getenv(timingModeEnv); mode {
	case "build":
		for _, name := range strings.Split(os.Getenv(timingFixturesEnv), ",") {
			fixture, ok := timingFixtures[name]
			if !ok {
				t.Fatalf("unknown fixture %q", name)
			}
			template := timingTemplateName(name)
			_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+template+` WITH (FORCE)`)
			if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+template); err != nil {
				t.Fatalf("create %s: %v", template, err)
			}
			db, err := sql.Open("pgx", withDatabase(t, admin, template))
			if err != nil {
				t.Fatalf("open %s: %v", template, err)
			}
			if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
				t.Fatalf("bootstrap %s: %v", template, err)
			}
			for _, statement := range fixture.statements() {
				if _, err := db.ExecContext(ctx, statement); err != nil {
					t.Fatalf("fixture %s: %v\n%s", name, err, statement)
				}
			}
			_ = db.Close()
		}
	case "run", "drain":
		name := os.Getenv(timingFixtureEnv)
		fixture := timingFixtures[name]
		rowLimit, _ := strconv.Atoi(os.Getenv(timingRowLimitEnv))
		if rowLimit <= 0 {
			rowLimit = 10_000_000
		}
		clone := fmt.Sprintf("eshu_rt_run_%d", time.Now().UnixNano())
		if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+clone+` TEMPLATE `+timingTemplateName(name)); err != nil {
			t.Fatalf("clone: %v", err)
		}
		defer dropClone(t, admin, clone)
		db, err := sql.Open("pgx", withDatabase(t, admin, clone))
		if err != nil {
			t.Fatalf("open clone: %v", err)
		}
		defer func() { _ = db.Close() }()
		// Warm the clone the same way for every binary.
		_, _ = db.ExecContext(ctx, `SELECT count(*) FROM fact_records`)
		store := postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: db})
		for pass := 1; ; pass++ {
			var bigBefore, bigAfter int64
			if fixture.ledger {
				_ = db.QueryRowContext(ctx, fixture.bigLinkQuery()).Scan(&bigBefore)
			}
			walBefore := readWALStats(t, db)
			result, err := store.PruneSupersededGenerations(ctx, timingPolicy(rowLimit))
			if err != nil {
				t.Fatalf("prune: %v", err)
			}
			wal := readWALStats(t, db).since(walBefore)
			if fixture.ledger {
				_ = db.QueryRowContext(ctx, fixture.bigLinkQuery()).Scan(&bigAfter)
			}
			var remainingDeltas, remainingCandidates int64
			_ = db.QueryRowContext(ctx, `SELECT count(*) FROM changed_since_link_deltas`).Scan(&remainingDeltas)
			_ = db.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations WHERE superseded_at < now() - interval '1 hour'`).Scan(&remainingCandidates)
			var counted int64
			for table, n := range result.RowsPruned {
				if table != "scope_generations" && table != "shared_projection_unroutable_intents" {
					counted += n
				}
			}
			line, _ := json.Marshal(map[string]any{
				"label": os.Getenv(timingLabelEnv), "mode": mode, "fixture": name, "row_limit": rowLimit, "pass": pass,
				"duration_ms": result.Duration.Milliseconds(), "generations_pruned": result.GenerationsPruned,
				"rows_pruned": result.RowsPruned, "rows_deleted": counted, "skipped": result.Skipped,
				"big_link_deleted": bigBefore == 1 && bigAfter == 0, "remaining_deltas": remainingDeltas,
				"remaining_candidates": remainingCandidates, "wal": wal,
			})
			fmt.Println("TIMING " + string(line))
			if mode == "run" || result.GenerationsPruned == 0 || pass >= 50 {
				break
			}
		}
	case "lockprobe":
		runLockProbe(t, admin, adminDB)
	case "explain":
		runExplain(t, admin, adminDB)
	default:
		t.Fatalf("set %s to build, run, drain, lockprobe or explain", timingModeEnv)
	}
}

// walStats is one reading of the cluster's WAL and checkpointer counters
// (arbiter ruling arb-7127-3d-e PE2). They are cluster-wide. A backend
// flushes its WAL counters when it goes idle, before it reports ready for the
// next query, at most once a second; lsn is the WAL insert position and does
// not wait for that flush.
type walStats struct {
	records, fpi, bytes, buffersFull, lsn  int64
	timed, requested, done, buffersWritten int64
}

func readWALStats(t *testing.T, db *sql.DB) walStats {
	t.Helper()
	var s walStats
	if err := db.QueryRowContext(t.Context(), `SELECT w.wal_records, w.wal_fpi, w.wal_bytes::bigint, w.wal_buffers_full,
       pg_wal_lsn_diff(pg_current_wal_insert_lsn(), '0/0')::bigint,
       c.num_timed, c.num_requested, c.num_done, c.buffers_written
FROM pg_stat_wal AS w, pg_stat_checkpointer AS c`).Scan(&s.records, &s.fpi, &s.bytes, &s.buffersFull, &s.lsn,
		&s.timed, &s.requested, &s.done, &s.buffersWritten); err != nil {
		t.Fatalf("read WAL stats: %v", err)
	}
	return s
}

func (s walStats) since(before walStats) map[string]int64 {
	return map[string]int64{
		"wal_records": s.records - before.records, "wal_fpi": s.fpi - before.fpi, "wal_bytes": s.bytes - before.bytes,
		"wal_buffers_full": s.buffersFull - before.buffersFull, "lsn_bytes": s.lsn - before.lsn,
		"ckpt_timed": s.timed - before.timed, "ckpt_requested": s.requested - before.requested,
		"ckpt_done": s.done - before.done, "ckpt_buffers_written": s.buffersWritten - before.buffersWritten,
	}
}

// cloneTemplate clones a timing template into a fresh database and drops it
// when the test ends.
func cloneTemplate(t *testing.T, admin string, adminDB *sql.DB, fixture string) *sql.DB {
	t.Helper()
	clone := fmt.Sprintf("eshu_rt_run_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(t.Context(), `CREATE DATABASE `+clone+` TEMPLATE `+timingTemplateName(fixture)); err != nil {
		t.Fatalf("clone: %v", err)
	}
	db, err := sql.Open("pgx", withDatabase(t, admin, clone))
	if err != nil {
		t.Fatalf("open clone: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		dropClone(t, admin, clone)
	})
	_, _ = db.ExecContext(t.Context(), `SELECT count(*) FROM fact_records`)
	return db
}

// dropClone drops a timing clone on a connection and a context of its own
// (arbiter ruling arb-7127-3d-e). The caller's are not safe here: t.Context()
// is cancelled just before Cleanup functions run, and TestRetentionTimingP8's
// deferred adminDB.Close runs before them too, so a drop through either leaks
// the clone. A failed drop fails the test.
func dropClone(t *testing.T, admin, clone string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Errorf("drop clone %s: open admin: %v", clone, err)
		return
	}
	defer func() { _ = adminDB.Close() }()
	if _, err := adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+clone+` WITH (FORCE)`); err != nil {
		t.Errorf("drop clone %s: %v", clone, err)
	}
}

// nowaitHeld reports whether a FOR UPDATE NOWAIT on one row fails with 55P03
// (lock_not_available): the row is held by another transaction.
func nowaitHeld(t *testing.T, db *sql.DB, query, id string) bool {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin probe: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), query, id)
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "55P03") {
		return true
	}
	t.Fatalf("probe %s: %v", id, err)
	return false
}

// runLockProbe is PC1 (b) of arbiter ruling arb-7127-3d-c: with the prune of
// the big link in flight (seen in pg_stat_activity), FOR UPDATE NOWAIT on
// every scope row and every candidate generation row reports which rows the
// prune holds.
func runLockProbe(t *testing.T, admin string, adminDB *sql.DB) {
	ctx := t.Context()
	db := cloneTemplate(t, admin, adminDB, os.Getenv(timingFixtureEnv))
	var dbName string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatalf("database name: %v", err)
	}
	scopes := queryIDs(t, db, `SELECT scope_id FROM ingestion_scopes ORDER BY 1`)
	candidates := queryIDs(t, db, `SELECT generation_id FROM scope_generations WHERE superseded_at < now() - interval '1 hour' ORDER BY 1`)
	rowLimit, _ := strconv.Atoi(os.Getenv(timingRowLimitEnv))
	if rowLimit <= 0 {
		rowLimit = 100000
	}
	done := make(chan error, 1)
	go func() {
		_, err := postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: db}).PruneSupersededGenerations(ctx, timingPolicy(rowLimit))
		done <- err
	}()
	// pg_stat_activity keeps the first track_activity_query_size bytes
	// (1024 by default); the delta_rows CTE is inside them and is only in
	// the ledger delete.
	inFlight := func() bool {
		var n int
		_ = adminDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = $1 AND state = 'active' AND query LIKE '%delta_rows AS MATERIALIZED%' AND pid <> pg_backend_pid()`, dbName).Scan(&n)
		return n > 0
	}
	deadline := time.Now().Add(2 * time.Minute)
	for !inFlight() {
		select {
		case err := <-done:
			t.Fatalf("the prune ended (%v) before its ledger delete was seen", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the ledger delete never appeared in pg_stat_activity")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var heldScopes, heldGenerations []string
	for _, id := range scopes {
		if nowaitHeld(t, db, `SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR UPDATE NOWAIT`, id) {
			heldScopes = append(heldScopes, id)
		}
	}
	for _, id := range candidates {
		if nowaitHeld(t, db, `SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR UPDATE NOWAIT`, id) {
			heldGenerations = append(heldGenerations, id)
		}
	}
	stillRunning := inFlight()
	if err := <-done; err != nil {
		t.Fatalf("prune: %v", err)
	}
	line, _ := json.Marshal(map[string]any{
		"label": os.Getenv(timingLabelEnv), "mode": "lockprobe", "fixture": os.Getenv(timingFixtureEnv),
		"probed_scopes": len(scopes), "probed_generations": len(candidates),
		"held_scopes": heldScopes, "held_generations": heldGenerations, "delete_still_running": stillRunning,
	})
	fmt.Println("TIMING " + string(line))
	if os.Getenv(timingExpectNarrowEnv) != "" {
		if !stillRunning {
			t.Fatal("the delete finished before the probes did; the probe proves nothing")
		}
		if strings.Join(heldScopes, ",") != "tscope-00" || strings.Join(heldGenerations, ",") != "tscope-00-g0" {
			t.Fatalf("held scopes %v and generations %v while the delete ran, want only tscope-00 and tscope-00-g0", heldScopes, heldGenerations)
		}
	}
}

func queryIDs(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("query ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// runExplain is PC2 (iii) of arbiter ruling arb-7127-3d-c: EXPLAIN (ANALYZE,
// BUFFERS) of the shipped ledger delete for tscope-00-g0 on a clone of the
// fixture, with the retention transaction's work_mem, rolled back. It prints
// the whole JSON plan on a PLAN line (arb-7127-3d-e PE2) and the statement's
// shared hit and read blocks and its temp blocks on the TIMING line.
func runExplain(t *testing.T, admin string, adminDB *sql.DB) {
	ctx := t.Context()
	db := cloneTemplate(t, admin, adminDB, os.Getenv(timingFixtureEnv))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL work_mem = '64MB'`); err != nil {
		t.Fatalf("work_mem: %v", err)
	}
	var plan string
	if err := tx.QueryRowContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+linksfreshnessstore.RetentionPruneQueryForTest,
		[]string{"tscope-00"}, []string{"tscope-00-g0"}).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var doc []map[string]any
	if err := json.Unmarshal([]byte(plan), &doc); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	// The whole plan, on one line, for the driver to keep (arb-7127-3d-e PE2).
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(plan)); err != nil {
		t.Fatalf("compact plan: %v", err)
	}
	fmt.Println("PLAN " + compact.String())
	top, _ := doc[0]["Plan"].(map[string]any)
	var sharedBuffers string
	_ = db.QueryRowContext(ctx, `SHOW shared_buffers`).Scan(&sharedBuffers)
	line, _ := json.Marshal(map[string]any{
		"label": os.Getenv(timingLabelEnv), "mode": "explain", "fixture": os.Getenv(timingFixtureEnv),
		"shared_buffers": sharedBuffers, "execution_ms": doc[0]["Execution Time"],
		"shared_hit": top["Shared Hit Blocks"], "shared_read": top["Shared Read Blocks"],
		"temp_read": top["Temp Read Blocks"], "temp_written": top["Temp Written Blocks"],
	})
	fmt.Println("TIMING " + string(line))
}

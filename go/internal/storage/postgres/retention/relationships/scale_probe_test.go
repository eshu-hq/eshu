// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

const (
	// probeScaleEnv turns the probe on and sets its size. Scale 1 seeds 12,000
	// scopes with 24,000 live generations (720,000 evidence and 240,000
	// candidate rows), then 10,000 orphan generations of 40 evidence and 12
	// candidate rows; scale 5 is five times that.
	probeScaleEnv = "ESHU_RELATIONSHIP_RETENTION_PROBE"
	// probeLayoutEnv picks how the orphan rows sit in the heap: together (the
	// default; a generation's rows are contiguous), scattered (interleaved by
	// generation id) or random.
	probeLayoutEnv = "ESHU_RELATIONSHIP_RETENTION_PROBE_LAYOUT"
	// probeStatementTimeout is the production census bound, so an EXPLAIN that
	// would have failed a real pass fails the probe.
	probeStatementTimeout = "60s"
	// probeRowCap is the sweep's default row cap.
	probeRowCap = 100_000
)

// probeSettings reads the scale and the orphan layout, skipping the test when
// no scale is set.
func probeSettings(t *testing.T) (scale int, layout string) {
	t.Helper()
	text := os.Getenv(probeScaleEnv)
	if text == "" {
		t.Skipf("set %s=<scale> (1 is about 1.1 million evidence rows) to run the measurement probe", probeScaleEnv)
	}
	scale, err := strconv.Atoi(text)
	if err != nil || scale < 1 {
		t.Fatalf("%s=%q, want a positive integer scale", probeScaleEnv, text)
	}
	layout = os.Getenv(probeLayoutEnv)
	switch layout {
	case "", "together", "scattered", "random":
	default:
		t.Fatalf("%s=%q, want together, scattered or random", probeLayoutEnv, layout)
	}
	return scale, layout
}

// probeOrphanRows returns the FROM clause that fixes where the orphan rows land
// in the heap. Together walks a generation's rows in a row; scattered
// interleaves generations (row 1 of every generation, then row 2, and so on);
// random shuffles them.
func probeOrphanRows(layout, generations, rows string) string {
	switch layout {
	case "scattered":
		return rows + ", " + generations + " ORDER BY i, k"
	case "random":
		return rows + ", " + generations + " ORDER BY random()"
	default:
		return generations + ", " + rows
	}
}

// probeExec runs one seed statement and logs how long it took.
func (f *liveFixture) probeExec(ctx context.Context, t *testing.T, query string, args ...any) {
	t.Helper()
	started := time.Now()
	if _, err := f.db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %.80q: %v", query, err)
	}
	t.Logf("seed %.60q took %s", strings.Join(strings.Fields(query), " "), time.Since(started).Round(time.Millisecond))
}

// probeScalar reads one text value.
func (f *liveFixture) probeScalar(ctx context.Context, t *testing.T, query string, args ...any) string {
	t.Helper()
	var value string
	if err := f.db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("scalar %.60q: %v", query, err)
	}
	return value
}

// probeExplain logs EXPLAIN (ANALYZE, BUFFERS) of the statement inside a
// transaction that always rolls back, under the production census bound.
func (f *liveFixture) probeExplain(ctx context.Context, t *testing.T, label, statement string, args ...any) {
	t.Helper()
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '`+probeStatementTimeout+`'`); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	rows, err := tx.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, SETTINGS) `+statement, args...)
	if err != nil {
		t.Fatalf("explain %s: %v", label, err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain %s: %v", label, err)
		}
		lines = append(lines, line)
	}
	t.Logf("EXPLAIN %s\n%s", label, strings.Join(lines, "\n"))
}

// probeVacuum refreshes the planner statistics and the visibility map, as a
// production table would have them after autovacuum.
func (f *liveFixture) probeVacuum(ctx context.Context, t *testing.T, tables ...string) {
	t.Helper()
	for _, table := range tables {
		f.probeExec(ctx, t, `VACUUM (ANALYZE) `+table)
	}
}

// probeSeedLive seeds the steady-state corpus: every scope has a pointed-at and
// a superseded generation, each with a scope generation row, 30 evidence rows
// and 10 candidate rows.
func (f *liveFixture) probeSeedLive(ctx context.Context, t *testing.T, scale int) {
	t.Helper()
	live := 12000 * scale
	f.probeExec(ctx, t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope-'||n, 'repository', 'git', 'key-'||n, 'git', 'p-'||n, now(), now(), 'active', 'live-'||n||'-a', '{}'::jsonb FROM generate_series(1, $1::int) n`, live)
	f.probeExec(ctx, t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'live-'||n||'-'||g, 'scope-'||n, 'snapshot', now(), now(), CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, $1::int) n, (VALUES ('a'),('s')) v(g)`, live)
	f.probeExec(ctx, t, `INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at, activated_at)
SELECT 'live-'||n||'-'||g, 'scope-'||n, 'run', CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END, now(), now()
FROM generate_series(1, $1::int) n, (VALUES ('a'),('s')) v(g)`, live)
	f.probeExec(ctx, t, `INSERT INTO relationship_evidence_facts (evidence_id, generation_id, evidence_kind, relationship_type, source_repo_id, target_repo_id, source_entity_id, target_entity_id, confidence, rationale, details, observed_at)
SELECT 'live-'||n||'-'||g||'-ev-'||i, 'live-'||n||'-'||g, 'terraform', 'DEPENDS_ON', 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 0.9,
       'seed rationale text of moderate length for the row', jsonb_build_object('path', 'modules/x/main.tf', 'line', i), now()
FROM generate_series(1, $1::int) n, (VALUES ('a'),('s')) v(g), generate_series(1, 30) i`, live)
	f.probeExec(ctx, t, `INSERT INTO relationship_candidates (candidate_id, generation_id, source_repo_id, target_repo_id, source_entity_id, target_entity_id, relationship_type, confidence, evidence_count, rationale, details)
SELECT 'live-'||n||'-'||g||'-c-'||i, 'live-'||n||'-'||g, 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 'DEPENDS_ON', 0.8, 1, 'seed rationale text', '{}'::jsonb
FROM generate_series(1, $1::int) n, (VALUES ('a'),('s')) v(g), generate_series(1, 10) i`, live)
	f.probeVacuum(ctx, t, "ingestion_scopes", "scope_generations", "relationship_generations",
		"relationship_evidence_facts", "relationship_candidates")
	t.Logf("steady-state corpus: evidence=%s candidates=%s generations=%s scope_generations=%s scopes=%s",
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM relationship_evidence_facts`),
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM relationship_candidates`),
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM relationship_generations`),
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM scope_generations`),
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM ingestion_scopes`))
}

// probeSeedOrphans adds 10,000 orphan generations per scale unit, laid out as
// the layout asks.
func (f *liveFixture) probeSeedOrphans(ctx context.Context, t *testing.T, scale int, layout string) {
	t.Helper()
	orphans := 10000 * scale
	f.probeExec(ctx, t, `INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at, activated_at)
SELECT 'orph-'||lpad(k::text, 7, '0'), 'run-scope-'||(k%50), 'run', 'superseded', now(), NULL FROM generate_series(1, $1::int) k`, orphans)
	f.probeExec(ctx, t, `INSERT INTO relationship_evidence_facts (evidence_id, generation_id, evidence_kind, relationship_type, source_repo_id, target_repo_id, source_entity_id, target_entity_id, confidence, rationale, details, observed_at)
SELECT 'orph-'||lpad(k::text, 7, '0')||'-ev-'||i, 'orph-'||lpad(k::text, 7, '0'), 'terraform', 'DEPENDS_ON', 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 0.9,
       'seed rationale text of moderate length for the row', jsonb_build_object('path', 'modules/x/main.tf', 'line', i), now()
FROM `+probeOrphanRows(layout, `generate_series(1, $1::int) k`, `generate_series(1, 40) i`), orphans)
	f.probeExec(ctx, t, `INSERT INTO relationship_candidates (candidate_id, generation_id, source_repo_id, target_repo_id, source_entity_id, target_entity_id, relationship_type, confidence, evidence_count, rationale, details)
SELECT 'orph-'||lpad(k::text, 7, '0')||'-c-'||i, 'orph-'||lpad(k::text, 7, '0'), 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 'DEPENDS_ON', 0.8, 1, 'seed rationale text', '{}'::jsonb
FROM `+probeOrphanRows(layout, `generate_series(1, $1::int) k`, `generate_series(1, 12) i`), orphans)
	f.probeVacuum(ctx, t, "relationship_generations", "relationship_evidence_facts", "relationship_candidates")
	for _, relation := range []string{
		"relationship_evidence_facts", "relationship_evidence_generation_idx",
		"relationship_candidates", "relationship_candidates_generation_idx",
		"relationship_generations", "scope_generations", "ingestion_scopes",
	} {
		t.Logf("size %s = %s", relation,
			f.probeScalar(ctx, t, `SELECT pg_size_pretty(pg_total_relation_size($1::regclass))`, relation))
	}
	t.Logf("layout=%q with orphans: evidence=%s candidates=%s generations=%s", layout,
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM relationship_evidence_facts`),
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM relationship_candidates`),
		f.probeScalar(ctx, t, `SELECT count(*)::text FROM relationship_generations`))
}

// probeCensus logs the three census statements' plans, then three whole
// census-only passes (the first one is cold).
func (f *liveFixture) probeCensus(ctx context.Context, t *testing.T, state string) {
	t.Helper()
	limit := relationshipretention.CensusListLimit + 1
	f.probeExplain(ctx, t, state+" census evidence", relationshipretention.CensusEvidenceSQL, limit)
	f.probeExplain(ctx, t, state+" census candidates", relationshipretention.CensusCandidatesSQL, limit)
	f.probeExplain(ctx, t, state+" census generations", relationshipretention.CensusGenerationsSQL, limit)
	for pass := 1; pass <= 3; pass++ {
		result, err := relationshipretention.Sweep(ctx, f.database, relationshipretention.Options{})
		if err != nil {
			t.Fatalf("census: %v", err)
		}
		t.Logf("%s census-only pass %d: census %s, listed %v saturated=%v", state, pass,
			result.Census.Duration.Round(time.Millisecond), result.Census.ByTable, result.Census.Saturated)
	}
}

// probeDeletePlans logs the plans of one batch of 50 orphan generations: the
// classify statement and the three deletes (rolled back).
func (f *liveFixture) probeDeletePlans(ctx context.Context, t *testing.T) {
	t.Helper()
	ids := make([]string, 0, 50)
	for k := 1; k <= 50; k++ {
		ids = append(ids, fmt.Sprintf("orph-%07d", k))
	}
	f.probeExplain(ctx, t, "delete evidence, 50 generations", relationshipretention.DeleteEvidenceSQL, ids)
	f.probeExplain(ctx, t, "delete candidates, 50 generations", relationshipretention.DeleteCandidatesSQL, ids)
	f.probeExplain(ctx, t, "delete generations, 50 generations", relationshipretention.DeleteGenerationsSQL, ids)
	f.probeExplain(ctx, t, "classify, 50 generations", relationshipretention.ClassifySQL, ids)
}

// probeWAL is the WAL position and the pg_stat_wal counters at one instant.
type probeWAL struct {
	lsn     string
	records int64
	images  int64
	bytes   string
}

func (f *liveFixture) probeWALNow(ctx context.Context, t *testing.T) probeWAL {
	t.Helper()
	wal := probeWAL{lsn: f.probeScalar(ctx, t, `SELECT pg_current_wal_insert_lsn()::text`)}
	if err := f.db.QueryRowContext(ctx, `SELECT wal_records, wal_fpi, wal_bytes::text FROM pg_stat_wal`).
		Scan(&wal.records, &wal.images, &wal.bytes); err != nil {
		t.Fatalf("pg_stat_wal: %v", err)
	}
	return wal
}

// probeBatchDurations reads the committed batches' durations from the sweep's
// JSON log lines.
func probeBatchDurations(logs string) []float64 {
	var durations []float64
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "batch committed") {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if duration, ok := entry["duration_ms"].(float64); ok {
			durations = append(durations, duration)
		}
	}
	sort.Float64s(durations)
	return durations
}

// probeSweepPasses runs two row-capped passes with deletes on and accounts the
// WAL each one writes. A CHECKPOINT precedes the first so every page it touches
// pays a full page image, the worst case the runbook sizes against; the second
// follows directly.
func (f *liveFixture) probeSweepPasses(ctx context.Context, t *testing.T) {
	t.Helper()
	for pass := 1; pass <= 2; pass++ {
		if pass == 1 {
			f.probeExec(ctx, t, `CHECKPOINT`)
		}
		before := f.probeWALNow(ctx, t)
		var logs bytes.Buffer
		started := time.Now()
		result, err := relationshipretention.Sweep(ctx, f.database, relationshipretention.Options{
			Deletes: true, RowCap: probeRowCap, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		})
		wall := time.Since(started)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		walBytes := f.probeScalar(ctx, t, `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), $1::pg_lsn)::bigint::text`, before.lsn)
		after := f.probeWALNow(ctx, t)
		var rows int64
		for _, n := range result.RowsSwept {
			rows += n
		}
		t.Logf("pass %d (checkpoint first: %v): %d rows, %d generations, %d batches, capped=%q, wall %s, census %s, sweep %s, skipped %v",
			pass, pass == 1, rows, result.GenerationsSwept, result.Batches, result.Capped, wall.Round(time.Millisecond),
			result.Census.Duration.Round(time.Millisecond), result.SweepDuration.Round(time.Millisecond), result.Skipped)
		t.Logf("pass %d WAL: lsn diff %s bytes (%s per row), pg_stat_wal records +%d, fpi +%d, bytes %s -> %s",
			pass, walBytes, probeBytesPerRow(walBytes, rows), after.records-before.records,
			after.images-before.images, before.bytes, after.bytes)
		if durations := probeBatchDurations(logs.String()); len(durations) > 0 {
			t.Logf("pass %d batch durations ms: n=%d p50=%.1f p95=%.1f max=%.1f", pass, len(durations),
				durations[len(durations)/2], durations[int(float64(len(durations))*0.95)], durations[len(durations)-1])
		}
	}
}

// probeBytesPerRow divides the WAL bytes by the rows a pass deleted.
func probeBytesPerRow(walBytes string, rows int64) string {
	if rows == 0 {
		return "n/a"
	}
	n, _ := strconv.ParseInt(walBytes, 10, 64)
	return fmt.Sprintf("%.0f", float64(n)/float64(rows))
}

// probeHubBatch seeds 50 orphan generations of 1,000 evidence rows (4.5% of the
// scale 1 table) and logs the evidence delete's plan, to see the sequential scan
// flip the #10820 purge note describes.
func (f *liveFixture) probeHubBatch(ctx context.Context, t *testing.T) {
	t.Helper()
	f.probeExec(ctx, t, `INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at)
SELECT 'hub-'||lpad(k::text, 4, '0'), 'run-hub', 'run', 'superseded', now() FROM generate_series(1, 50) k`)
	f.probeExec(ctx, t, `INSERT INTO relationship_evidence_facts (evidence_id, generation_id, evidence_kind, relationship_type, source_repo_id, target_repo_id, source_entity_id, target_entity_id, confidence, rationale, details, observed_at)
SELECT 'hub-'||lpad(k::text, 4, '0')||'-ev-'||i, 'hub-'||lpad(k::text, 4, '0'), 'terraform', 'DEPENDS_ON', 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 0.9,
       'seed rationale text of moderate length for the row', jsonb_build_object('path', 'modules/x/main.tf', 'line', i), now()
FROM generate_series(1, 50) k, generate_series(1, 1000) i`)
	f.probeVacuum(ctx, t, "relationship_evidence_facts")
	hub := make([]string, 0, 50)
	for k := 1; k <= 50; k++ {
		hub = append(hub, fmt.Sprintf("hub-%04d", k))
	}
	f.probeExplain(ctx, t, "delete evidence, 50 hub generations (50,000 rows)", relationshipretention.DeleteEvidenceSQL, hub)
}

// TestRelationshipRetentionScaleProbe is the measurement behind the
// docs/internal/evidence/10823-relationship-retention.md note. It is a manual
// probe, not a proof, and is deliberately not a *_live_test.go file: it skips
// unless ESHU_RELATIONSHIP_RETENTION_PROBE names a scale, so no CI job runs it.
// Run it against a disposable PostgreSQL (ESHU_POSTGRES_DSN and
// ESHU_POSTGRES_DISPOSABLE=1) with -v so the figures print:
//
//	ESHU_RELATIONSHIP_RETENTION_PROBE=1 ESHU_RELATIONSHIP_RETENTION_PROBE_LAYOUT=random \
//	  go test -p 1 -count=1 -v -timeout 40m -run TestRelationshipRetentionScaleProbe \
//	  ./internal/storage/postgres/retention/relationships/
//
// Scale 1 takes about a minute; scale 5 about four. Timings are logged, never
// asserted: the host is shared and a timing assertion would flake.
func TestRelationshipRetentionScaleProbe(t *testing.T) {
	scale, layout := probeSettings(t)
	f := openLiveFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	for _, setting := range []string{
		"server_version", "wal_level", "full_page_writes", "wal_compression", "shared_buffers",
		"checkpoint_timeout", "max_wal_size", "synchronous_commit", "fsync",
	} {
		t.Logf("setting %s = %s", setting, f.probeScalar(ctx, t, `SELECT current_setting($1)`, setting))
	}
	f.probeSeedLive(ctx, t, scale)
	f.probeCensus(ctx, t, "steady-state")
	f.probeSeedOrphans(ctx, t, scale, layout)
	f.probeCensus(ctx, t, "orphan-abundant")
	f.probeDeletePlans(ctx, t)
	f.probeSweepPasses(ctx, t)
	f.probeHubBatch(ctx, t)
	remaining, err := relationshipretention.Sweep(ctx, f.database, relationshipretention.Options{})
	if err != nil {
		t.Fatalf("census after the passes: %v", err)
	}
	t.Logf("remaining orphans per the census: %v", remaining.Census.ByTable)
}

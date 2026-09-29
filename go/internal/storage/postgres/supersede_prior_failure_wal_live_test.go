// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Deterministic write-cost proof for #7320 (F1). Wall time on a shared host is
// noise; the bytes a statement writes are not. For each of the five supersede
// writers this runs the shipped statement ("after") and the same statement with
// the fold cut out ("before", derived from the shipped constant) over the same
// freshly seeded rows, and reads what the whole statement did from
// pg_stat_statements and the WAL position: WAL records, bytes and full-page
// images, shared blocks hit, read, dirtied and written, TOAST rows inserted and
// heap updates, all per superseded row. It also prints EXPLAIN (ANALYZE,
// BUFFERS, WAL, VERBOSE) per plan node for the two claim statements.
//
// Two regimes: "steady" (the rows were dirtied since the last checkpoint, so
// pages carry no fresh full-page image) and "post_checkpoint" (CHECKPOINT first,
// so the first touch of each page writes a full-page image, the worst case).
//
// Opt in with ESHU_7320_COST_PROOF=1 and the claim-proof DSN. The server must
// preload pg_stat_statements (shared_preload_libraries=pg_stat_statements,
// pg_stat_statements.track=all) with the extension created in the proof
// database, and the role must be allowed to CHECKPOINT. Turn autovacuum off so
// its WAL does not land in the measurement.

// walCall is one execution of a statement: its arguments.
type walCall struct{ args []any }

// walCase is one writer under measurement: how to seed rows for it and the
// calls to make. fold is the fragment constant its alias embeds.
type walCase struct {
	name  string
	text  string
	fold  string
	seed  func(t *testing.T, database *sql.DB, rows, bytes int)
	calls func(rows int) []walCall
	// capRows limits how many rows a per-row writer is seeded with, since it
	// makes one call per row; 0 means no limit.
	capRows int
}

// walSample is everything one statement run wrote.
type walSample struct {
	calls, rows, superseded, withPrior        float64
	walRecords, walBytes, walFPI, lsnBytes    float64
	hit, read, dirtied, written, tempWritten  float64
	toastInserts, heapUpdates, heapHotUpdates float64
	execMillis                                float64
	// reclaimed counts rows the statement moved to retrying under the
	// projector_stale_scope_reclaim marker (#7388).
	reclaimed float64
}

// costSeedScopes seeds n scopes, each with an old and a newer generation and
// one leased projector work row on the old one carrying a retry's failure.
func costSeedScopes(t *testing.T, database *sql.DB, n, detailBytes int, oldGen, newGen string) {
	t.Helper()
	costReset(t, database)
	costExec(t, database,
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'scope-'||i, 'repository','git','scope-'||i,'git','scope-'||i, now(), now(), 'active' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g1','scope-'||i,'push', now()-interval '2 hours', now()-interval '2 hours','%s' FROM generate_series(1,%d) i`, oldGen, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g2','scope-'||i,'push', now()-interval '1 hour', now()-interval '1 hour','%s' FROM generate_series(1,%d) i`, newGen, n),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, lease_owner, claim_until, failure_class, failure_message, failure_details, payload, created_at, updated_at)
SELECT 'projector_scope-'||i||'_g1','scope-'||i,'scope-'||i||'-g1','projector','source_local','claimed',3, now()-interval '90 minutes','cost-worker', now()+interval '1 hour','projection_retryable','retry', %s,'{}'::jsonb, now()-interval '2 hours', now()-interval '90 minutes' FROM generate_series(1,%d) i`, costDetailExpr(detailBytes), n),
		`VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`)
}

// costSeedAckScope seeds one scope with n old failed generations, each holding
// a dead-lettered work row, and one newer pending generation: Ack of the newer
// one supersedes all n rows in a single statement.
func costSeedAckScope(t *testing.T, database *sql.DB, n, detailBytes int) {
	t.Helper()
	costReset(t, database)
	costExec(t, database,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('scope-1','repository','git','scope-1','git','scope-1', now(), now(), 'active')`,
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-1-g'||i,'scope-1','push', now()-interval '2 hours', now()-interval '2 hours','failed' FROM generate_series(1,%d) i`, n),
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('scope-1-cur','scope-1','push', now()-interval '1 hour', now()-interval '1 hour','pending')`,
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, failure_class, failure_message, failure_details, payload, created_at, updated_at)
SELECT 'projector_scope-1_g'||i,'scope-1','scope-1-g'||i,'projector','source_local','dead_letter',3, now()-interval '90 minutes','graph_write_timeout','timed out', %s,'{}'::jsonb, now()-interval '2 hours', now()-interval '90 minutes' FROM generate_series(1,%d) i`, costDetailExpr(detailBytes), n),
		`VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`)
}

// walCases are the five writers (six statements: the reducer has a claim and a
// batch claim).
func walCases() []walCase {
	one := func(args func(now time.Time) []any) func(int) []walCall {
		return func(int) []walCall { return []walCall{{args(time.Now().UTC())}} }
	}
	perScope := func(rows int) []walCall {
		now := time.Now().UTC()
		calls := make([]walCall, rows)
		for i := range calls {
			scopeID := fmt.Sprintf("scope-%d", i+1)
			calls[i] = walCall{[]any{now, scopeID, scopeID + "-g1", "cost-worker", 3}}
		}
		return calls
	}
	return []walCase{
		{
			name: "projector_claim_sweep", text: claimProjectorWorkQuery, fold: priorFailureStaleSQL, seed: costSeedProjector,
			calls: one(func(now time.Time) []any { return []any{now, "cost", now.Add(time.Minute), ""} }),
		},
		{
			name: "reducer_claim", text: claimReducerWorkQuery, fold: priorFailureStaleSQL, seed: costSeedReducer,
			calls: one(func(now time.Time) []any { return costReducerArgs(now, 0) }),
		},
		{
			name: "reducer_claim_batch", text: claimReducerWorkBatchQuery, fold: priorFailureStaleSQL, seed: costSeedReducer,
			calls: one(func(now time.Time) []any { return costReducerArgs(now, 4) }),
		},
		{
			name: "projector_ack_obsolete", text: supersedeProjectorObsoleteGenerationsQuery, fold: priorFailureStaleSQL,
			seed: costSeedAckScope, calls: one(func(now time.Time) []any { return []any{now, "scope-1", "scope-1-cur"} }),
		},
		{
			name: "projector_heartbeat", text: supersedeRunningProjectorWorkQuery, fold: priorFailureWorkSQL, capRows: 500,
			seed: func(t *testing.T, d *sql.DB, n, b int) { costSeedScopes(t, d, n, b, "pending", "pending") }, calls: perScope,
		},
		{
			name: "projector_ack_refusal", text: markProjectorAckSupersededQuery, fold: priorFailureWorkSQL, capRows: 500,
			seed: func(t *testing.T, d *sql.DB, n, b int) { costSeedScopes(t, d, n, b, "superseded", "active") }, calls: perScope,
		},
	}
}

// walMeasure runs the calls of one statement variant in a transaction it rolls
// back and returns what the whole run wrote.
func walMeasure(ctx context.Context, t *testing.T, database *sql.DB, text string, calls []walCall, checkpoint bool) walSample {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("wal conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if checkpoint {
		if _, err := conn.ExecContext(ctx, `CHECKPOINT`); err != nil {
			t.Fatalf("checkpoint: %v", err)
		}
	}
	var discard int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT public.pg_stat_statements_reset()) x`).Scan(&discard); err != nil {
		t.Fatalf("pg_stat_statements is not usable (the server needs shared_preload_libraries=pg_stat_statements and CREATE EXTENSION in the proof database): %v", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var lsn string
	if err := tx.QueryRowContext(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn); err != nil {
		t.Fatalf("lsn: %v", err)
	}
	for _, c := range calls {
		rows, err := tx.QueryContext(ctx, text, c.args...)
		if err != nil {
			t.Fatalf("statement: %v", err)
		}
		for rows.Next() {
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("statement rows: %v", err)
		}
		_ = rows.Close()
	}
	var s walSample
	if err := tx.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), $1::pg_lsn)::float8`, lsn).Scan(&s.lsnBytes); err != nil {
		t.Fatalf("lsn diff: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(pg_stat_get_xact_tuples_inserted(c.reltoastrelid), 0)::float8,
       COALESCE(pg_stat_get_xact_tuples_updated(c.oid), 0)::float8,
       COALESCE(pg_stat_get_xact_tuples_hot_updated(c.oid), 0)::float8
FROM pg_class c WHERE c.oid = to_regclass('fact_work_items')`).Scan(&s.toastInserts, &s.heapUpdates, &s.heapHotUpdates); err != nil {
		t.Fatalf("xact stats: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE status = 'superseded')::float8,
       count(*) FILTER (WHERE status = 'superseded' AND failure_details LIKE '%"prior_failure"%')::float8,
       count(*) FILTER (WHERE status = 'retrying' AND failure_class = 'projector_stale_scope_reclaim')::float8
FROM fact_work_items`).Scan(&s.superseded, &s.withPrior, &s.reclaimed); err != nil {
		t.Fatalf("count: %v", err)
	}
	_ = tx.Rollback()
	if err := conn.QueryRowContext(ctx, `
SELECT COALESCE(sum(calls),0)::float8, COALESCE(sum(rows),0)::float8, COALESCE(sum(wal_records),0)::float8,
       COALESCE(sum(wal_bytes),0)::float8, COALESCE(sum(wal_fpi),0)::float8, COALESCE(sum(shared_blks_hit),0)::float8,
       COALESCE(sum(shared_blks_read),0)::float8, COALESCE(sum(shared_blks_dirtied),0)::float8,
       COALESCE(sum(shared_blks_written),0)::float8, COALESCE(sum(temp_blks_written),0)::float8,
       COALESCE(sum(total_exec_time),0)::float8
FROM public.pg_stat_statements
WHERE query ~ '^\s*(WITH|UPDATE)\s' AND query LIKE '%fact_work_items%' AND dbid = (SELECT oid FROM pg_database WHERE datname = current_database())`).
		Scan(&s.calls, &s.rows, &s.walRecords, &s.walBytes, &s.walFPI, &s.hit, &s.read, &s.dirtied, &s.written, &s.tempWritten, &s.execMillis); err != nil {
		t.Fatalf("read pg_stat_statements: %v", err)
	}
	return s
}

var planNodePattern = regexp.MustCompile(`^\s*(?:->\s+)?([A-Z][A-Za-z ]*?(?: on \S+)?)\s+\(cost=`)

// walExplain runs EXPLAIN (ANALYZE, BUFFERS, WAL, VERBOSE) of one statement in
// a rolled-back transaction and returns, per plan node that wrote WAL, the node
// with its Buffers and WAL lines, plus the top node's Buffers and the timings.
func walExplain(ctx context.Context, t *testing.T, database *sql.DB, text string, args []any) string {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, VERBOSE) "+text, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	node, buffers := "", ""
	first := true
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		trim := strings.TrimSpace(line)
		switch {
		case planNodePattern.MatchString(line):
			node, buffers = planNodePattern.FindStringSubmatch(line)[1], ""
			first = true
		case strings.HasPrefix(trim, "Buffers:") && buffers == "":
			buffers = trim
			if first && strings.HasPrefix(node, "Update") == false && len(out) == 0 {
				out = append(out, fmt.Sprintf("  top %s: %s", node, trim))
			}
		case strings.HasPrefix(trim, "WAL:"):
			out = append(out, fmt.Sprintf("  %s: %s | %s", node, buffers, trim))
		case strings.HasPrefix(trim, "Planning Time"), strings.HasPrefix(trim, "Execution Time"):
			out = append(out, "  "+trim)
		}
	}
	return "\n" + strings.Join(out, "\n")
}

func perRow(v, rows float64) float64 {
	if rows == 0 {
		return 0
	}
	return v / rows
}

// walLine formats one sample per superseded row.
func walLine(s walSample) string {
	r := s.superseded
	return fmt.Sprintf("superseded=%.0f calls=%.0f wal_bytes/row=%.1f wal_records/row=%.2f wal_fpi=%.0f lsn_bytes/row=%.1f hit/row=%.2f read/row=%.2f dirtied/row=%.3f written/row=%.3f temp_written=%.0f toast_inserts/row=%.3f heap_updates/row=%.3f heap_hot/row=%.3f exec_ms/row=%.4f",
		s.superseded, s.calls, perRow(s.walBytes, r), perRow(s.walRecords, r), s.walFPI, perRow(s.lsnBytes, r), perRow(s.hit, r),
		perRow(s.read, r), perRow(s.dirtied, r), perRow(s.written, r), s.tempWritten, perRow(s.toastInserts, r),
		perRow(s.heapUpdates, r), perRow(s.heapHotUpdates, r), perRow(s.execMillis, r))
}

func TestSupersedePriorFailureWriteCost(t *testing.T) {
	requireCostProof(t)
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	ctx := context.Background()
	const reps = 3
	scenarios := []struct {
		name        string
		rows, bytes int
	}{
		{"ops_qa_scale", costEnvInt("ESHU_7320_COST_ROWS", 3500), costEnvInt("ESHU_7320_COST_DETAIL_BYTES", 800)},
		{"stress_wide_details", costEnvInt("ESHU_7320_COST_STRESS_ROWS", 500), costEnvInt("ESHU_7320_COST_STRESS_BYTES", 65536)},
	}
	for _, wc := range walCases() {
		after := wc.text
		before := costBeforeTextWith(t, wc.name, after, wc.fold)
		for _, sc := range scenarios {
			rows := sc.rows
			if wc.capRows > 0 && rows > wc.capRows {
				rows = wc.capRows
			}
			for _, regime := range []string{"steady", "post_checkpoint"} {
				sums := map[string]*walSample{"before": {}, "after": {}}
				for rep := 0; rep < reps; rep++ {
					order := []string{"before", "after"}
					if rep%2 == 1 {
						order = []string{"after", "before"}
					}
					for _, variant := range order {
						wc.seed(t, database, rows, sc.bytes)
						text := after
						if variant == "before" {
							text = before
						}
						s := walMeasure(ctx, t, database, text, wc.calls(rows), regime == "post_checkpoint")
						wantPrior := 0.0
						if variant == "after" {
							wantPrior = float64(rows)
						}
						if s.superseded != float64(rows) || s.withPrior != wantPrior {
							t.Fatalf("%s %s %s: superseded %.0f rows (%.0f with prior_failure), want %d and %.0f: the run did not exercise the fold",
								wc.name, sc.name, variant, s.superseded, s.withPrior, rows, wantPrior)
						}
						t.Logf("WAL %s %s regime=%s variant=%s rep=%d rows=%d detail_bytes=%d %s", wc.name, sc.name, regime, variant, rep, rows, sc.bytes, walLine(s))
						acc := sums[variant]
						acc.calls += s.calls
						acc.superseded += s.superseded
						acc.walRecords += s.walRecords
						acc.walBytes += s.walBytes
						acc.walFPI += s.walFPI
						acc.lsnBytes += s.lsnBytes
						acc.hit += s.hit
						acc.read += s.read
						acc.dirtied += s.dirtied
						acc.written += s.written
						acc.tempWritten += s.tempWritten
						acc.toastInserts += s.toastInserts
						acc.heapUpdates += s.heapUpdates
						acc.heapHotUpdates += s.heapHotUpdates
						acc.execMillis += s.execMillis
					}
				}
				b, a := sums["before"], sums["after"]
				r := b.superseded
				t.Logf("DELTA %s %s regime=%s rows=%d detail_bytes=%d mean_of_%d: wal_bytes/row %.1f -> %.1f (+%.1f) wal_records/row %.2f -> %.2f wal_fpi %.0f -> %.0f dirtied/row %.3f -> %.3f (+%.3f) written/row %.3f -> %.3f hit/row %.2f -> %.2f (+%.2f) toast_inserts/row %.3f -> %.3f heap_hot/row %.3f -> %.3f",
					wc.name, sc.name, regime, rows, sc.bytes, reps,
					perRow(b.walBytes, r), perRow(a.walBytes, r), perRow(a.walBytes-b.walBytes, r),
					perRow(b.walRecords, r), perRow(a.walRecords, r), b.walFPI/reps, a.walFPI/reps,
					perRow(b.dirtied, r), perRow(a.dirtied, r), perRow(a.dirtied-b.dirtied, r),
					perRow(b.written, r), perRow(a.written, r),
					perRow(b.hit, r), perRow(a.hit, r), perRow(a.hit-b.hit, r),
					perRow(b.toastInserts, r), perRow(a.toastInserts, r), perRow(b.heapHotUpdates, r), perRow(a.heapHotUpdates, r))
			}
			if sc.name == "ops_qa_scale" && wc.calls(rows) != nil && len(wc.calls(rows)) == 1 {
				for _, variant := range []string{"before", "after"} {
					wc.seed(t, database, rows, sc.bytes)
					text := after
					if variant == "before" {
						text = before
					}
					t.Logf("EXPLAIN %s %s %s (ANALYZE, BUFFERS, WAL, VERBOSE) nodes that wrote WAL:%s", wc.name, sc.name, variant,
						walExplain(ctx, t, database, text, wc.calls(rows)[0].args))
				}
			}
		}
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Cost proof for #7320 (P5). The fold makes every supersede sweep read and
// re-write the old failure_details, so the claim statements' sweep rows get
// wider. This measures that on the production claim paths, not on a copy:
// ProjectorQueue.Claim, ReducerQueue.Claim and ReducerQueue.ClaimBatch each run
// the shipped statement ("after") against the same statement with the fold
// removed ("before", derived from the shipped text by cutting the fragment, so
// it cannot drift). Runs interleave before/after and alternate which goes
// first, over a freshly seeded worst case each time: many dead-lettered rows
// carrying large details, all superseded by one claim.
//
// Opt in with ESHU_7320_COST_PROOF=1 and the claim-proof DSN. Knobs:
// ESHU_7320_COST_PAIRS (default 9 pairs), ESHU_7320_COST_ROWS (3500) and
// ESHU_7320_COST_DETAIL_BYTES (800), ESHU_7320_COST_STRESS_ROWS (500) and
// ESHU_7320_COST_STRESS_BYTES (65536). Wall time on a shared host is not
// evidence; the test logs the host load so a reader can label the run.

func costEnvInt(name string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return v
	}
	return def
}

func hostLoad() string {
	out, err := exec.Command("uptime").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// costSeedProjector seeds n scopes, each with a dead_letter row of detailBytes
// details on a failed generation and a pending row on a newer generation.
func costSeedProjector(t *testing.T, database *sql.DB, n, detailBytes int) {
	t.Helper()
	costReset(t, database)
	costExec(t, database,
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'scope-'||i, 'repository','git','scope-'||i,'git','scope-'||i, now(), now(), 'failed' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g1','scope-'||i,'push', now()-interval '2 hours', now()-interval '2 hours','failed' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g2','scope-'||i,'push', now()-interval '1 hour', now()-interval '1 hour','pending' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, failure_class, failure_message, failure_details, payload, created_at, updated_at)
SELECT 'projector_scope-'||i||'_g1','scope-'||i,'scope-'||i||'-g1','projector','source_local','dead_letter',3, now()-interval '90 minutes','graph_write_timeout','timed out', repeat('x', %d),'{}'::jsonb, now()-interval '2 hours', now()-interval '90 minutes' FROM generate_series(1,%d) i`, detailBytes, n),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, visible_at, payload, created_at, updated_at)
SELECT 'projector_scope-'||i||'_g2','scope-'||i,'scope-'||i||'-g2','projector','source_local','pending',0, now()-interval '1 hour','{}'::jsonb, now()-interval '1 hour', now()-interval '1 hour' FROM generate_series(1,%d) i`, n),
		`VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`)
}

// costSeedReducer seeds n scopes with an active newer generation and a
// dead_letter reducer row of detailBytes details on the older one.
func costSeedReducer(t *testing.T, database *sql.DB, n, detailBytes int) {
	t.Helper()
	costReset(t, database)
	costExec(t, database,
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'scope-'||i, 'repository','git','scope-'||i,'git','scope-'||i, now(), now(), 'active' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g1','scope-'||i,'push', now()-interval '2 hours', now()-interval '2 hours','superseded' FROM generate_series(1,%d) i`, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g2','scope-'||i,'push', now()-interval '1 hour', now()-interval '1 hour','active' FROM generate_series(1,%d) i`, n),
		`UPDATE ingestion_scopes SET active_generation_id = scope_id || '-g2'`,
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, failure_class, failure_message, failure_details, payload, created_at, updated_at)
SELECT 'reducer_scope-'||i||'_g1','scope-'||i,'scope-'||i||'-g1','reducer','zz_probe_domain','dead_letter',3, now()-interval '90 minutes','graph_write_timeout','timed out', repeat('x', %d),'{}'::jsonb, now()-interval '2 hours', now()-interval '90 minutes' FROM generate_series(1,%d) i`, detailBytes, n),
		`VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`)
}

func costReset(t *testing.T, database *sql.DB) {
	t.Helper()
	costExec(t, database, `TRUNCATE fact_work_items, scope_generations, ingestion_scopes CASCADE`)
}

func costExec(t *testing.T, database *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("cost fixture: %v\n%s", err, s)
		}
	}
}

// costVariant runs one claim under a statement text. The claim vars are swapped
// for the run, so the measured path is the production queue method.
type costStatement struct {
	name  string
	text  *string
	alias string
	seed  func(*testing.T, *sql.DB, int, int)
	run   func(context.Context, *sql.DB) error
	args  func(now time.Time) []any
}

func TestSupersedePriorFailureClaimCost(t *testing.T) {
	if os.Getenv("ESHU_7320_COST_PROOF") != "1" {
		t.Skip("set ESHU_7320_COST_PROOF=1 to run the #7320 claim cost proof")
	}
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	ctx := context.Background()
	pairs := costEnvInt("ESHU_7320_COST_PAIRS", 9)
	statements := []costStatement{
		{
			name: "projector_claim", text: &claimProjectorWorkQuery, alias: "stale", seed: costSeedProjector,
			run: func(ctx context.Context, d *sql.DB) error {
				_, ok, err := NewProjectorQueue(SQLDB{DB: d}, "cost", time.Minute).Claim(ctx)
				if err == nil && !ok {
					err = fmt.Errorf("projector claim claimed nothing")
				}
				return err
			},
			args: func(now time.Time) []any { return []any{now, "cost", now.Add(time.Minute), ""} },
		},
		{
			name: "reducer_claim", text: &claimReducerWorkQuery, alias: "stale", seed: costSeedReducer,
			run: func(ctx context.Context, d *sql.DB) error {
				_, _, err := NewReducerQueue(SQLDB{DB: d}, "cost", time.Minute).Claim(ctx)
				return err
			},
			args: func(now time.Time) []any { return costReducerArgs(now, 0) },
		},
		{
			name: "reducer_claim_batch", text: &claimReducerWorkBatchQuery, alias: "stale", seed: costSeedReducer,
			run: func(ctx context.Context, d *sql.DB) error {
				_, err := NewReducerQueue(SQLDB{DB: d}, "cost", time.Minute).ClaimBatch(ctx, 4)
				return err
			},
			args: func(now time.Time) []any { return costReducerArgs(now, 4) },
		},
	}
	scenarios := []struct {
		name        string
		rows, bytes int
	}{
		{"ops_qa_scale", costEnvInt("ESHU_7320_COST_ROWS", 3500), costEnvInt("ESHU_7320_COST_DETAIL_BYTES", 800)},
		{"stress_wide_details", costEnvInt("ESHU_7320_COST_STRESS_ROWS", 500), costEnvInt("ESHU_7320_COST_STRESS_BYTES", 65536)},
	}
	t.Logf("host load: %s", hostLoad())
	for _, st := range statements {
		after := *st.text
		fragment := " || " + priorFailureDetailsSQL(st.alias)
		if !strings.Contains(after, fragment) {
			t.Fatalf("%s: shipped statement does not contain the fold, so there is no before variant to derive", st.name)
		}
		before := strings.Replace(after, fragment, "", 1)
		for _, sc := range scenarios {
			for _, variant := range []string{"before", "after"} {
				st.seed(t, database, sc.rows, sc.bytes)
				text := after
				if variant == "before" {
					text = before
				}
				t.Logf("PLAN %s %s %s: %s", st.name, sc.name, variant, costExplain(t, database, text, st.args(time.Now().UTC())))
			}
			var beforeSecs, afterSecs []float64
			for i := 0; i < pairs; i++ {
				order := []string{"before", "after"}
				if i%2 == 1 {
					order = []string{"after", "before"}
				}
				for _, variant := range order {
					st.seed(t, database, sc.rows, sc.bytes)
					if variant == "before" {
						*st.text = before
					} else {
						*st.text = after
					}
					start := time.Now()
					err := st.run(ctx, database)
					elapsed := time.Since(start).Seconds()
					*st.text = after
					if err != nil {
						t.Fatalf("%s %s %s: %v", st.name, sc.name, variant, err)
					}
					if variant == "before" {
						beforeSecs = append(beforeSecs, elapsed)
					} else {
						afterSecs = append(afterSecs, elapsed)
					}
				}
			}
			t.Logf("COST %s %s rows=%d detail_bytes=%d pairs=%d before_median=%.6fs after_median=%.6fs ratio=%.3f before=%s after=%s load_end=%q",
				st.name, sc.name, sc.rows, sc.bytes, pairs, median(beforeSecs), median(afterSecs),
				median(afterSecs)/median(beforeSecs), fmtSecs(beforeSecs), fmtSecs(afterSecs), hostLoad())
		}
	}
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s) == 0 {
		return 0
	}
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func fmtSecs(v []float64) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(x, 'f', 6, 64)
	}
	return strings.Join(parts, ",")
}

// costReducerArgs are the arguments ReducerQueue passes its claim statements;
// batch adds the row limit.
func costReducerArgs(now time.Time, batch int) []any {
	q := NewReducerQueue(nil, "cost", time.Minute)
	args := []any{
		now, q.claimDomainFilters(), q.LeaseOwner, now.Add(q.LeaseDuration),
		q.RequireProjectorDrainBeforeClaim, q.ExpectedSourceLocalProjectors, q.semanticEntityClaimLimit(),
	}
	if batch > 0 {
		args = append(args, batch)
	}
	return args
}

// costExplain runs EXPLAIN (ANALYZE, BUFFERS) of a claim statement inside a
// transaction it rolls back, and returns the numbers that matter: execution
// time, the top node's buffers, temp-file traffic and whether anything spilled.
func costExplain(t *testing.T, database *sql.DB, statement string, args []any) string {
	t.Helper()
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+statement, args...)
	if err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain: %v", err)
		}
		lines = append(lines, line)
	}
	var exec, buffers string
	spills := 0
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(trim, "Execution Time"):
			exec = trim
		case buffers == "" && strings.HasPrefix(trim, "Buffers:"):
			buffers = trim
		}
		if strings.Contains(trim, "Disk") || strings.Contains(trim, "temp read") || strings.Contains(trim, "Batches:") && !strings.Contains(trim, "Batches: 1 ") {
			spills++
		}
	}
	return fmt.Sprintf("%s | top %s | spill_lines=%d | plan_lines=%d", exec, buffers, spills, len(lines))
}

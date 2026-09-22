// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCloudResourceLivenessProbePlanStaysOnIndexAtChunkSizeLive pins #6946:
// at a lock-chunk-sized candidate array the live probe must plan on the
// migration-118 partial index, not walk ingestion_scopes. It seeds 2,000
// active scopes x 100 admitted uids (plus a superseded generation each, so
// the join is not trivially selective), VACUUM (ANALYZE)s, and explains the
// alive branch of the production probe for 500 candidates in BOTH plan
// modes: the custom plan (what the first executions of a prepared statement
// use) and the generic plan (what pgx's default statement cache settles on
// after five executions per connection, so the mode the retracter runs in
// steady state). Each arm must name the partial index, must not walk
// ingestion_scopes into fact_records_scope_generation_idx, and must stay
// under a buffer budget far below the scope-walk plan. Without migration
// 119's extended statistics the planner estimates ~2,000 rows per candidate
// and chooses the walk in both modes, which is the RED this test was written
// against (custom ~14,000 buffers; generic ~14,700 buffers / 800 ms).
//
// The explained statement is derived from the production fenced probe by
// cutting at the UNION ALL boundary (TestCloudRetractLivenessSQLGuards pins
// that boundary and the alive branch's predicates), never hand-copied.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN.
// Seeds under a unique prefix and deletes them at the end. VACUUM (ANALYZE)
// is table-wide, so run this suite alone (-p 1) against a private stack.
func TestCloudResourceLivenessProbePlanStaysOnIndexAtChunkSizeLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_LIVE=1 and ESHU_POSTGRES_DSN to run the cloud retract probe-plan proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	prefix := fmt.Sprintf("plan-%d", time.Now().UnixNano())
	const scopes, uidsPerScope, candidates = 2000, 100, 500
	seedCloudRetractPlanCorpus(ctx, t, database, prefix, scopes, uidsPerScope)
	defer func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id LIKE $1||'-s-%'`, prefix)
	}()
	// VACUUM (ANALYZE): the statistics migration 119 depends on are refreshed
	// and dead tuples from earlier seeds do not skew the planner's costs.
	if _, err := database.ExecContext(ctx, `VACUUM (ANALYZE) fact_records`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
	uids := make([]string, 0, candidates)
	for i := 1; i <= candidates; i++ {
		if i%2 == 0 {
			uids = append(uids, fmt.Sprintf("%s-uid-%d-%d", prefix, i%scopes+1, i%uidsPerScope+1))
		} else {
			uids = append(uids, fmt.Sprintf("%s-dead-%d", prefix, i))
		}
	}
	aliveSQL := cloudRetractAliveBranchSQL(t)
	// plan_cache_mode only governs plans that go through the plan cache, so
	// each arm PREPAREs the probe on a dedicated session and explains an
	// EXECUTE of it; an EXPLAIN of the bare parameterized text is planned
	// once with the values in hand and would show the custom plan in both
	// arms. The session is closed after the arm, which drops the prepared
	// statement.
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			conn, err := database.Conn(ctx)
			if err != nil {
				t.Fatalf("dedicated session: %v", err)
			}
			// Conn.Close returns the session to the pool, so the prepared
			// statement and the plan-cache setting are undone explicitly (only the
			// named statement: DEALLOCATE ALL would also drop pgx's own cache).
			defer func() {
				_, _ = conn.ExecContext(ctx, "DEALLOCATE cloud_retract_probe")
				_, _ = conn.ExecContext(ctx, "RESET plan_cache_mode")
				_ = conn.Close()
			}()
			if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
				t.Fatalf("set plan_cache_mode: %v", err)
			}
			if _, err := conn.ExecContext(ctx, "PREPARE cloud_retract_probe(text[]) AS "+aliveSQL); err != nil {
				t.Fatalf("prepare probe: %v", err)
			}
			text := explainPlanText(ctx, t, conn, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) EXECUTE cloud_retract_probe("+textArrayLiteral(uids)+")")
			if !strings.Contains(text, "fact_records_cloud_retract_admission_uid_idx") {
				t.Fatalf("500-candidate probe (%s) did not use the partial uid index:\n%s", mode, text)
			}
			if strings.Contains(text, "Index Scan using fact_records_scope_generation_idx") {
				t.Fatalf("500-candidate probe (%s) walked ingestion_scopes into fact_records_scope_generation_idx (the #6946 plan flip):\n%s", mode, text)
			}
			buffers := topLevelSharedBuffers(t, text)
			const budget = 5000
			if buffers > budget {
				t.Fatalf("500-candidate probe (%s) touched %d shared buffers, budget %d (scope walk is ~14,000):\n%s", mode, buffers, budget, text)
			}
			t.Logf("500-candidate probe (%s): %s, %d shared buffers", mode, joinNode(text), buffers)
		})
	}
}

// textArrayLiteral renders uids as a text[] literal for EXECUTE, which
// cannot take protocol-level parameters inside an EXPLAIN.
func textArrayLiteral(uids []string) string {
	quoted := make([]string, len(uids))
	for i, uid := range uids {
		quoted[i] = "'" + strings.ReplaceAll(uid, "'", "''") + "'"
	}
	return "ARRAY[" + strings.Join(quoted, ",") + "]::text[]"
}

// joinNode names the join operator an EXPLAIN text plan chose, for the log.
func joinNode(plan string) string {
	for _, node := range []string{"Nested Loop", "Hash Join", "Merge Join"} {
		if strings.Contains(plan, node) {
			return node
		}
	}
	return "no join node"
}

// cloudRetractAliveBranchSQL derives the alive branch of the production
// fenced probe: everything after the UNION ALL, with the wrapping
// parentheses removed, so the explained statement is the shipped text.
func cloudRetractAliveBranchSQL(t *testing.T) string {
	t.Helper()
	_, alive, found := strings.Cut(liveAdmissionCloudUIDsFencedSQL, "UNION ALL")
	if !found {
		t.Fatal("fenced probe has no UNION ALL boundary")
	}
	alive = strings.TrimSpace(alive)
	if !strings.HasPrefix(alive, "(") || !strings.HasSuffix(alive, ")") {
		t.Fatalf("alive branch is not parenthesized: %q", alive)
	}
	alive = alive[1 : len(alive)-1]
	if !strings.Contains(alive, "'alive' AS kind") || !strings.Contains(alive, "= ANY($1::text[])") {
		t.Fatalf("derived alive branch lost its shape: %q", alive)
	}
	return alive
}

// seedCloudRetractPlanCorpus inserts scopes x 2 generations (the second
// active) with uidsPerScope admission facts in each generation, all under
// prefix, so cleanup by scope prefix cascades through the FKs.
func seedCloudRetractPlanCorpus(ctx context.Context, t *testing.T, database *sql.DB, prefix string, scopes, uidsPerScope int) {
	t.Helper()
	seed := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		 SELECT $1||'-s-'||g, 'aws', 'aws', $1||'-s-'||g, 'aws', $1||'-s-'||g, now(), now(), 'active', NULL, '{}'::jsonb FROM generate_series(1, $2::int) g`, []any{prefix, scopes}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		 SELECT $1||'-g-'||s||'-'||n, $1||'-s-'||s, 'manual', now(), now(), CASE WHEN n = 2 THEN 'active' ELSE 'superseded' END, now()
		 FROM generate_series(1, $2::int) s, generate_series(1, 2) n`, []any{prefix, scopes}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1||'-g-'||substr(scope_id, length($1) + 4)||'-2' WHERE scope_id LIKE $1||'-s-%'`, []any{prefix}},
		{`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 SELECT $1||'-f-'||s||'-'||n||'-'||k, $1||'-s-'||s, $1||'-g-'||s||'-'||n, 'reducer_cloud_resource_identity', $1||'-k-'||s||'-'||k, 'aws', $1||'-k-'||s||'-'||k, now(), now(), FALSE,
		        jsonb_build_object('cloud_resource_uid', $1||'-uid-'||s||'-'||k)
		 FROM generate_series(1, $2::int) s, generate_series(1, 2) n, generate_series(1, $3::int) k`, []any{prefix, scopes, uidsPerScope}},
	}
	for i, step := range seed {
		if _, err := database.ExecContext(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
}

// explainPlanText runs an EXPLAIN statement on conn and returns its text plan.
func explainPlanText(ctx context.Context, t *testing.T, conn *sql.Conn, explain string, args ...any) string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, explain, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return plan.String()
}

// topLevelSharedBuffers returns the root node's shared-buffer total (hit +
// read + dirtied + written) from the FIRST "Buffers:" line of an EXPLAIN
// text plan. Only the first line is graded: a cold-cache root line that
// carries "shared read=" without "hit=" must not fall through to a child
// node's smaller figure and loosen the budget. A plan without a parseable
// root Buffers line fails, never grades.
func topLevelSharedBuffers(t *testing.T, plan string) int {
	t.Helper()
	for _, line := range strings.Split(plan, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Buffers:") {
			continue
		}
		shared, ok := strings.CutPrefix(trimmed, "Buffers: shared ")
		if !ok {
			t.Fatalf("root Buffers line has no shared component: %q", trimmed)
		}
		total := 0
		for _, field := range strings.Fields(shared) {
			key, value, found := strings.Cut(field, "=")
			if !found || (key != "hit" && key != "read" && key != "dirtied" && key != "written") {
				break
			}
			var n int
			if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
				t.Fatalf("unparseable root Buffers field %q in %q", field, trimmed)
			}
			total += n
		}
		if total <= 0 {
			t.Fatalf("root Buffers line has no shared hit/read figure: %q", trimmed)
		}
		return total
	}
	t.Fatalf("plan has no Buffers line:\n%s", plan)
	return 0
}

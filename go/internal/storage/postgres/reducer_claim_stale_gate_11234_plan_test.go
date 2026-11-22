// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// staleGate11234Plan is the part of one EXPLAIN (ANALYZE, BUFFERS, FORMAT
// JSON) document the #11234 pins read.
type staleGate11234Plan struct {
	execMS  float64
	buffers int64 // shared hit + read of the whole statement
	text    string
	// maxIndexLoops is the largest "Actual Loops" of any index scan on
	// fact_work_items or scope_generations: the per-scope probe count of
	// the #11234 loop.
	maxIndexLoops int64
	// maxRescanLoops is the largest "Actual Loops" of a CTE Scan or Seq
	// Scan on the inner side of a nested loop: the two-CTE nested loop
	// that reads nothing and burns CPU.
	maxRescanLoops int64
	// scopeLoops and scopeBuffers are the largest loop count and the summed
	// own buffers of scans on ingestion_scopes: logged, not pinned
	// (arbiter-11234-fix-e decision 4: QA's ingestion_scopes is bloated).
	scopeLoops   int64
	scopeBuffers int64
}

// staleGate11234ForcedNestedLoop disables hash and merge joins for one
// transaction: the worst join order the planner could pick from a wrong
// estimate. The #11234 fence must keep the gate bounded even then.
var staleGate11234ForcedNestedLoop = []string{
	"SET LOCAL enable_hashjoin = off",
	"SET LOCAL enable_mergejoin = off",
}

// explainStaleGate11234 runs EXPLAIN (ANALYZE, BUFFERS) of query inside a
// rolled-back transaction after the setup statements, and returns its
// execution time, buffers, the largest per-node index-scan loop count, and
// the text plan for the log.
func explainStaleGate11234(ctx context.Context, t testing.TB, conn *sql.Conn, setup []string, query string, args ...any) staleGate11234Plan {
	t.Helper()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range setup {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("explain setup %q: %v", stmt, err)
		}
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain json: %v", err)
	}
	var docs []struct {
		ExecutionTime float64        `json:"Execution Time"`
		Plan          map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &docs); err != nil {
		t.Fatalf("parse explain: %v", err)
	}
	out := staleGate11234Plan{execMS: docs[0].ExecutionTime}
	out.buffers = int64(staleGate11234Num(docs[0].Plan["Shared Hit Blocks"]) + staleGate11234Num(docs[0].Plan["Shared Read Blocks"]))
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		if strings.Contains(nodeType, "Index") && (relation == "fact_work_items" || relation == "scope_generations") {
			if loops := int64(staleGate11234Num(node["Actual Loops"])); loops > out.maxIndexLoops {
				out.maxIndexLoops = loops
			}
		}
		if relation == "ingestion_scopes" {
			if loops := int64(staleGate11234Num(node["Actual Loops"])); loops > out.scopeLoops {
				out.scopeLoops = loops
			}
			out.scopeBuffers += int64(staleGate11234Num(node["Shared Hit Blocks"]) + staleGate11234Num(node["Shared Read Blocks"]))
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			m, ok := child.(map[string]any)
			if !ok {
				continue
			}
			childType, _ := m["Node Type"].(string)
			relationship, _ := m["Parent Relationship"].(string)
			if nodeType == "Nested Loop" && relationship == "Inner" && (childType == "CTE Scan" || childType == "Seq Scan") {
				if loops := int64(staleGate11234Num(m["Actual Loops"])); loops > out.maxRescanLoops {
					out.maxRescanLoops = loops
				}
			}
			walk(m)
		}
	}
	walk(docs[0].Plan)
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
	if err != nil {
		t.Fatalf("explain text: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain text rows: %v", err)
	}
	out.text = b.String()
	return out
}

func staleGate11234Num(v any) float64 {
	f, _ := v.(float64)
	return f
}

// openStaleGate11234Live opens a dedicated connection on a fresh schema, or
// skips when no disposable DSN is set. It never touches QA. With full set
// the schema gets the full bootstrap (every migration, the production
// index set, arbiter-11234-accept decision 4); otherwise the lighter
// claim-benchmark schema, whose missing indexes hid the shipped text's
// no-statistics cliff. ESHU_STALE_GATE_11234_KEEP=1 keeps the schema for
// manual EXPLAIN work.
func openStaleGate11234Live(ctx context.Context, t *testing.T, label string, full bool) (*sql.Conn, reducerClaimBenchmarkConn) {
	t.Helper()
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN to a disposable Postgres to run the #11234 stale-gate plan proofs")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("stale_gate_11234_%s_%d", label, time.Now().UnixNano())
	t.Cleanup(func() {
		if os.Getenv("ESHU_STALE_GATE_11234_KEEP") == "1" {
			t.Logf("kept schema %s", schema)
			return
		}
		dropCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.ExecContext(dropCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	if !full {
		sqlConn, err := admin.Conn(ctx)
		if err != nil {
			t.Fatalf("open dedicated connection: %v", err)
		}
		t.Cleanup(func() { _ = sqlConn.Close() })
		conn := reducerClaimBenchmarkConn{conn: sqlConn}
		if err := createReducerClaimBenchmarkSchema(ctx, conn, schema); err != nil {
			t.Fatalf("create schema: %v", err)
		}
		for _, def := range []string{"fact_records", "cross_scope_completion_queue"} {
			if _, err := conn.ExecContext(ctx, MigrationSQL(def)); err != nil {
				t.Fatalf("create %s: %v", def, err)
			}
		}
		return sqlConn, conn
	}
	for _, stmt := range []string{"CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public", "CREATE SCHEMA " + schema} {
		if _, err := admin.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	database.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("bootstrap schema %s: %v", schema, err)
	}
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlConn.Close() })
	return sqlConn, reducerClaimBenchmarkConn{conn: sqlConn}
}

// staleGate11234RescanBound is the largest inner CTE or sequential scan
// loop count the gate chain may show (arbiter-11234-repro decision 2: no
// nested loop whose inner side rescans a CTE or table thousands of times).
const staleGate11234RescanBound = 1000

// staleGate11234LoopSlack is the constant the index-loop bound allows above
// the non-active row count: carrier and readiness subplans per stale row
// stay under it on the seeds.
const staleGate11234LoopSlack = 64

// staleGate11234NonActiveRows counts the pending/retrying reducer rows not
// on their scope's active generation id: the rows the gate may probe.
func staleGate11234NonActiveRows(ctx context.Context, t testing.TB, conn *sql.Conn) int64 {
	t.Helper()
	var n int64
	if err := conn.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items AS work
JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
WHERE work.stage = 'reducer' AND work.status IN ('pending', 'retrying')
  AND work.generation_id IS DISTINCT FROM scope.active_generation_id`).Scan(&n); err != nil {
		t.Fatalf("count non-active rows: %v", err)
	}
	return n
}

// staleGate11234PinViolations EXPLAINs cut with hash and merge joins on and
// off and the domain filter NULL and set, logs each plan's figures, and
// returns every breach of the #11234 pin: index loops on fact_work_items
// or scope_generations above bound, or a nested loop rescanning a CTE or
// table more than staleGate11234RescanBound times.
func staleGate11234PinViolations(ctx context.Context, t *testing.T, conn *sql.Conn, label, cut string, bound int64) []string {
	t.Helper()
	var violations []string
	for _, setup := range [][]string{nil, staleGate11234ForcedNestedLoop} {
		for _, domains := range []any{nil, staleGate11234Domains} {
			plan := explainStaleGate11234(ctx, t, conn, setup, cut, nil, domains)
			where := fmt.Sprintf("%s forced_nl=%v domains=%v", label, setup != nil, domains != nil)
			t.Logf("%s exec_ms=%.1f buffers=%d max_index_loops=%d max_rescan_loops=%d ingestion_scopes_loops=%d ingestion_scopes_buffers=%d",
				where, plan.execMS, plan.buffers, plan.maxIndexLoops, plan.maxRescanLoops, plan.scopeLoops, plan.scopeBuffers)
			if plan.maxIndexLoops > bound {
				violations = append(violations, fmt.Sprintf("%s: max index loops %d > %d\n%s", where, plan.maxIndexLoops, bound, plan.text))
			}
			if plan.maxRescanLoops > staleGate11234RescanBound {
				violations = append(violations, fmt.Sprintf("%s: %d CTE/table rescans > %d\n%s", where, plan.maxRescanLoops, staleGate11234RescanBound, plan.text))
			}
		}
	}
	return violations
}

// TestStaleGate11234NoPerScopeLoopLive is the #11234 plan-shape pin on the
// QA shape (12,236 active scopes, 13,366 older generations, 10,000 live
// reducer rows), with no stale row and with 200, on the claim-benchmark
// schema (fast guard) and on the full bootstrap schema (production index
// set): in the shipped gate chain, derived from the constants, index loops
// on fact_work_items and scope_generations stay within the non-active row
// count plus a constant, and no nested loop rescans a CTE or table
// thousands of times, with hash and merge joins on and off. Before #11234
// the planner's 1-row estimate for claim_active_generations drove a nested
// loop with 12,236 and 13,366 index loops (QA replica and primary,
// 2026-11-22); on the full schema the frozen pre-#11234 chain must still
// breach the pin in the same run (the RED half). Buffers, time and the
// ingestion_scopes probes are logged, not pinned.
func TestStaleGate11234NoPerScopeLoopLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	for _, full := range []bool{false, true} {
		for _, staleRows := range []int{0, 200} {
			t.Run(fmt.Sprintf("full=%v/stale=%d", full, staleRows), func(t *testing.T) {
				sqlConn, conn := openStaleGate11234Live(ctx, t, fmt.Sprintf("pin%d", staleRows), full)
				shape := staleGate11234QAShape()
				shape.staleRows = staleRows
				seedStaleGate11234(ctx, t, conn, shape)
				bound := staleGate11234NonActiveRows(ctx, t, sqlConn) + staleGate11234LoopSlack
				for _, v := range staleGate11234PinViolations(ctx, t, sqlConn, "shipped", staleGate11234ChainCut(), bound) {
					t.Error(v)
				}
				if !full {
					return
				}
				pre := staleGate11234SwapGate(t, staleGate11234ChainCut(), staleGate11234PreGate(t))
				if len(staleGate11234PinViolations(ctx, t, sqlConn, "pre-11234", pre, bound)) == 0 {
					t.Error("the frozen pre-#11234 chain no longer breaches the pin: the pin cannot tell the fix from the bug")
				}
			})
		}
	}
}

// TestStaleGate11234ReproLive logs the plan and buffers of the shipped gate
// chain on the QA shape, optionally with QA's physical layout
// (ESHU_STALE_GATE_11234_PHYSICAL=1). Operator-gated evidence, not a pin.
func TestStaleGate11234ReproLive(t *testing.T) {
	if os.Getenv("ESHU_STALE_GATE_11234_REPRO") != "1" {
		t.Skip("set ESHU_STALE_GATE_11234_REPRO=1 to log the #11234 reproduction")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	sqlConn, conn := openStaleGate11234Live(ctx, t, "repro", os.Getenv("ESHU_STALE_GATE_11234_FULL") == "1")
	shape := staleGate11234QAShape()
	shape.physicalQA = os.Getenv("ESHU_STALE_GATE_11234_PHYSICAL") == "1"
	if n := os.Getenv("ESHU_STALE_GATE_11234_TERMINAL"); n != "" {
		if _, err := fmt.Sscan(n, &shape.terminalPerGen); err != nil {
			t.Fatalf("parse ESHU_STALE_GATE_11234_TERMINAL: %v", err)
		}
	}
	seedStaleGate11234(ctx, t, conn, shape)
	if out := os.Getenv("ESHU_STALE_GATE_11234_DUMP"); out != "" {
		if err := os.WriteFile(out, []byte(staleGate11234ChainCut()), 0o600); err != nil {
			t.Fatalf("dump cut: %v", err)
		}
	}
	for _, domains := range []any{nil, staleGate11234Domains} {
		// The first run warms the cache; the second is reported.
		_ = explainStaleGate11234(ctx, t, sqlConn, nil, staleGate11234ChainCut(), nil, domains)
		plan := explainStaleGate11234(ctx, t, sqlConn, nil, staleGate11234ChainCut(), nil, domains)
		t.Logf("chain domains=%v exec_ms=%.1f buffers=%d max_index_loops=%d\n%s",
			domains != nil, plan.execMS, plan.buffers, plan.maxIndexLoops, plan.text)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// preLeaseRewriteBlockedCTE is the blocked CTE the status blockage section ran
// before #6794 replaced the live-lease join with a hashed filter. It is kept
// verbatim as the differential oracle for that rewrite.
const preLeaseRewriteBlockedCTE = `blocked AS (
    SELECT eligible.work_item_id,
           eligible.domain,
           eligible.conflict_domain,
           eligible.conflict_key,
           eligible.available_at
    FROM eligible
    JOIN fact_work_items AS inflight
      ON inflight.stage = 'reducer'
     AND inflight.conflict_domain = eligible.conflict_domain
     AND COALESCE(inflight.conflict_key, inflight.scope_id) = eligible.conflict_key
     AND inflight.work_item_id <> eligible.work_item_id
     AND inflight.status IN ('claimed', 'running')
     AND inflight.claim_until > $1
),
`

// preLeaseRewrite returns query with the live-lease CTEs swapped back to the
// pre-#6794 join. It fails the test when the swap does not happen, so an
// oracle can never silently equal the query it is checking.
func preLeaseRewrite(t *testing.T, query string) string {
	t.Helper()
	current := reducerConflictInflightLeasesCTE + reducerConflictBlockedCTE
	if strings.Count(query, current) != 1 {
		t.Fatalf("query does not contain the live-lease CTEs exactly once:\n%s", query)
	}
	return strings.Replace(query, current, preLeaseRewriteBlockedCTE, 1)
}

// TestReducerConflictBlockageLeaseMixMatchesPreChangeJoin is the #6794
// differential for the inflight_leases rewrite: over every lease state the
// blocked set must equal the pre-change join's, row for row.
//
// Fixture, by construction (scope s-extra, conflict domain "lm"):
//   - lm-running: a live running lease and a pending sibling -> sibling blocked;
//   - lm-claimed: a live claimed lease and a retrying sibling -> sibling blocked;
//   - lm-expired: a claimed lease whose claim_until has passed and a pending
//     sibling -> nothing blocked (the expired row is itself eligible);
//   - lm-null: a claimed lease with no claim_until and a pending sibling ->
//     nothing blocked;
//   - lm-self: a live claimed lease with no sibling -> nothing blocked (a row
//     never fences itself);
//   - lm-pending-future: a pending row whose claim_until is in the future and a
//     pending sibling -> nothing blocked (only claimed or running rows lease);
//   - conflict domain "lmn": a live claimed lease with no conflict_key, so it
//     keys on its scope_id, blocks a sibling with no conflict_key and a sibling
//     whose conflict_key is spelled as the scope_id (COALESCE on both sides).
func TestReducerConflictBlockageLeaseMixMatchesPreChangeJoin(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 blockage lease differential")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)
	seedActiveWorkDifferentialExtras(ctx, t, conn)
	seedBlockageLeaseMix(ctx, t, conn)

	blockedSet := func(ctes string) []string {
		t.Helper()
		rows, err := conn.QueryContext(ctx, `WITH `+activeFactWorkItemsCTE+`,
`+ctes+`
SELECT work_item_id || '|' || conflict_key FROM blocked ORDER BY 1`, statusSemanticsAsOf)
		if err != nil {
			t.Fatalf("blocked set: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatalf("scan blocked row: %v", err)
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate blocked rows: %v", err)
		}
		return out
	}
	got := blockedSet(reducerConflictBlockageCTEs)
	want := blockedSet(preLeaseRewrite(t, reducerConflictBlockageCTEs))
	if !slices.Equal(got, want) {
		t.Fatalf("blocked set differs from the pre-change join:\n got:  %v\n want: %v", got, want)
	}
	for _, row := range []string{
		"w-lm-running-sibling|lm-running", "w-lm-claimed-sibling|lm-claimed", "w-n2-pending|k",
		"w-lmn-null-sibling|s-extra", "w-lmn-explicit-sibling|s-extra",
	} {
		if !slices.Contains(got, row) {
			t.Fatalf("blocked set %v missing %q", got, row)
		}
	}
	for _, row := range got {
		for _, key := range []string{"lm-expired", "lm-null", "lm-self", "lm-pending-future"} {
			if strings.HasSuffix(row, "|"+key) {
				t.Fatalf("blocked set %v fences %q, which has no live lease on another row", got, key)
			}
		}
	}
	// 13 fenced keys from the differential extras, the two live-lease keys and
	// the two scope-keyed siblings here, and the semantics fixture's one
	// live-lease fence (w-n2-pending).
	if len(got) != 18 {
		t.Fatalf("blocked rows = %d, want 18: %v", len(got), got)
	}
}

// seedBlockageLeaseMix adds the lease states TestReducerConflictBlockageLeaseMixMatchesPreChangeJoin
// covers to the s-extra scope seedActiveWorkDifferentialExtras creates.
func seedBlockageLeaseMix(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	at := func(offset time.Duration) time.Time { return statusSemanticsAsOf.Add(offset) }
	insertIn := func(id, conflictDomain string, key any, status string, claimUntil any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  conflict_domain, conflict_key, status, attempt_count, claim_until, payload, created_at, updated_at)
VALUES ($1, 's-extra', 'g-extra', 'reducer', 'dx-lm', $2, $3, $4, 0, $5, '{}'::jsonb, $6, $6)`,
			id, conflictDomain, key, status, claimUntil, at(-30*time.Minute)); err != nil {
			t.Fatalf("seed lease mix %s: %v", id, err)
		}
	}
	insert := func(id, key, status string, claimUntil any) {
		t.Helper()
		insertIn(id, "lm", key, status, claimUntil)
	}
	insert("w-lm-running-lease", "lm-running", "running", at(10*time.Minute))
	insert("w-lm-running-sibling", "lm-running", "pending", nil)
	insert("w-lm-claimed-lease", "lm-claimed", "claimed", at(5*time.Minute))
	insert("w-lm-claimed-sibling", "lm-claimed", "retrying", nil)
	insert("w-lm-expired-lease", "lm-expired", "claimed", at(-5*time.Minute))
	insert("w-lm-expired-sibling", "lm-expired", "pending", nil)
	insert("w-lm-null-lease", "lm-null", "claimed", nil)
	insert("w-lm-null-sibling", "lm-null", "pending", nil)
	insert("w-lm-self-lease", "lm-self", "claimed", at(5*time.Minute))
	insert("w-lm-pending-future", "lm-pending-future", "pending", at(5*time.Minute))
	insert("w-lm-pending-future-sibling", "lm-pending-future", "pending", nil)
	insertIn("w-lmn-null-lease", "lmn", nil, "claimed", at(5*time.Minute))
	insertIn("w-lmn-null-sibling", "lmn", nil, "pending", nil)
	insertIn("w-lmn-explicit-sibling", "lmn", "s-extra", "pending", nil)
	if _, err := conn.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatalf("analyze lease mix: %v", err)
	}
}

// TestActiveWorkSummaryBlockageHashesLeasesOnce is the #6794 plan regression
// for the blockage filter. The old join, and a join to a materialized lease
// CTE, ran as nested loops whose cost grew with eligible rows times leases:
// without statistics the leases were rescanned per eligible row, and with stale
// statistics the planner flipped the join and rescanned eligible per lease,
// leaving every lease node at one loop. So in each statistics state (none, as
// after a bulk load; analyzed; and stale, analyzed before the leases went live)
// the plan must hash the lease set ("hashed SubPlan") and must run every
// eligible and lease-path node once.
func TestActiveWorkSummaryBlockageHashesLeasesOnce(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 blockage plan regression")
	}
	ctx := context.Background()
	for _, liveLeases := range []int{0, 50, 200} {
		t.Run(fmt.Sprintf("live_leases_%d", liveLeases), func(t *testing.T) {
			conn := openUnanalyzedBlockageSchema(ctx, t, dsn)
			seedUnanalyzedBlockageBacklog(ctx, t, conn, 200, 20, liveLeases)
			requireNoStatistics(ctx, t, conn)
			assertBlockageHashesLeasesOnce(ctx, t, conn, "no statistics")
			if _, err := conn.ExecContext(ctx, "ANALYZE ingestion_scopes, scope_generations, fact_work_items"); err != nil {
				t.Fatalf("analyze: %v", err)
			}
			assertBlockageHashesLeasesOnce(ctx, t, conn, "analyzed")
		})
	}
	t.Run("stale_statistics", func(t *testing.T) {
		conn := openUnanalyzedBlockageSchema(ctx, t, dsn)
		seedUnanalyzedBlockageBacklog(ctx, t, conn, 200, 20, 0)
		if _, err := conn.ExecContext(ctx, "ANALYZE ingestion_scopes, scope_generations, fact_work_items"); err != nil {
			t.Fatalf("analyze: %v", err)
		}
		// The statistics say no lease is live; now 200 are.
		if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET claim_until = $1::timestamptz + interval '1 hour'
WHERE work_item_id IN (SELECT 'nb-lease-' || i FROM generate_series(1, 200) AS i)`, statusSemanticsAsOf); err != nil {
			t.Fatalf("make leases live: %v", err)
		}
		assertBlockageHashesLeasesOnce(ctx, t, conn, "stale statistics")
	})
	t.Run("unhashed_subplan_is_detected", func(t *testing.T) {
		// The planner refuses to hash a SubPlan whose estimated size exceeds
		// work_mem * hash_mem_multiplier (subplan_is_hashable). Forcing that
		// shows what the regression looks like, so the marker check above is
		// proven able to fail. The estimate must be large, so the 20,000 live
		// leases are analyzed first: without statistics the planner estimates
		// about one live lease and hashes even in 64kB.
		conn := openUnanalyzedBlockageSchema(ctx, t, dsn)
		seedUnanalyzedBlockageBacklog(ctx, t, conn, 20000, 1, 20000)
		if _, err := conn.ExecContext(ctx, "ANALYZE ingestion_scopes, scope_generations, fact_work_items"); err != nil {
			t.Fatalf("analyze: %v", err)
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		for _, stmt := range []string{"SET LOCAL work_mem = '64kB'", "SET LOCAL hash_mem_multiplier = 1"} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
		var plan string
		if err := tx.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+activeWorkSummaryQuery, statusSemanticsAsOf).Scan(&plan); err != nil {
			t.Fatalf("explain: %v", err)
		}
		if strings.Contains(plan, "hashed SubPlan") || !strings.Contains(plan, "SubPlan") {
			t.Fatalf("with hash memory below the lease set, the plan must use an unhashed SubPlan; got:\n%s", plan)
		}
	})
}

// assertBlockageHashesLeasesOnce explains the summary query and requires the
// hashed lease filter and a single run of every eligible and lease-path node.
func assertBlockageHashesLeasesOnce(ctx context.Context, t *testing.T, conn *sql.Conn, state string) {
	t.Helper()
	plan := explainAnalyzeJSON(ctx, t, conn, activeWorkSummaryQuery)
	if _, missing := blockagePlanRoots(plan); len(missing) > 0 {
		t.Fatalf("%s: summary plan has no %v subtree; the blockage checks cannot read it", state, missing)
	}
	if !planFiltersByHashedSubPlan(plan) {
		t.Fatalf("%s: summary plan does not filter eligible by a hashed SubPlan", state)
	}
	if got := maxBlockagePathLoops(plan); got != 1 {
		t.Fatalf("%s: a blockage-path plan node ran %d times, want 1", state, got)
	}
	t.Logf("%s: pre-change query blockage-path max loops = %d (informational)", state,
		maxBlockagePathLoops(explainAnalyzeJSON(ctx, t, conn, preLeaseRewrite(t, activeWorkSummaryQuery))))
}

// openUnanalyzedBlockageSchema opens a fresh schema with autovacuum off for the
// tables the blockage section reads, so nothing analyzes them behind the test.
func openUnanalyzedBlockageSchema(ctx context.Context, t *testing.T, dsn string) *sql.Conn {
	t.Helper()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	for _, table := range []string{"ingestion_scopes", "scope_generations", "fact_work_items"} {
		if _, err := conn.ExecContext(ctx, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)"); err != nil {
			t.Fatalf("disable autovacuum on %s: %v", table, err)
		}
	}
	return conn
}

// requireNoStatistics fails unless the planner has no column statistics for
// the blockage tables. TRUNCATE does not clear pg_statistic, so a reused table
// could otherwise carry old statistics into a "no statistics" measurement.
func requireNoStatistics(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	var rows int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_stats
WHERE schemaname = current_schema()
  AND tablename IN ('ingestion_scopes', 'scope_generations', 'fact_work_items')`).Scan(&rows); err != nil {
		t.Fatalf("count statistics: %v", err)
	}
	if rows != 0 {
		t.Fatalf("blockage tables carry %d column statistics rows, want none", rows)
	}
}

// explainAnalyzeJSON returns the root plan node of EXPLAIN (ANALYZE, FORMAT JSON).
func explainAnalyzeJSON(ctx context.Context, t *testing.T, conn *sql.Conn, query string) map[string]any {
	t.Helper()
	var plan string
	if err := conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+query, statusSemanticsAsOf).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var parsed []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil || len(parsed) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	return parsed[0].Plan
}

// seedUnanalyzedBlockageBacklog seeds scopes reducer scopes, each with an
// active generation, one claimed lease, and eligible pending rows on the same
// conflict key, without analyzing anything. The first liveLeases leases are
// live (claim_until after the as-of time); the rest have expired.
func seedUnanalyzedBlockageBacklog(ctx context.Context, t *testing.T, conn *sql.Conn, scopes, eligiblePerScope, liveLeases int) {
	t.Helper()
	for _, stmt := range []string{
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'nb-' || i, 'repository', 'git', 'nb-' || i, 'git', 'nb-' || i, $1, $1, 'active', 'nb-g-' || i, '{}'::jsonb
FROM generate_series(1, %d) AS i`, scopes),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'nb-g-' || i, 'nb-' || i, 'snapshot', $1, $1, 'active', '{}'::jsonb
FROM generate_series(1, %d) AS i`, scopes),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  status, attempt_count, claim_until, payload, created_at, updated_at)
SELECT 'nb-lease-' || i, 'nb-' || i, 'nb-g-' || i, 'reducer', 'nb', 'claimed', 0,
       CASE WHEN i <= %d THEN $1::timestamptz + interval '1 hour' ELSE $1::timestamptz - interval '1 minute' END,
       '{}'::jsonb, $1, $1
FROM generate_series(1, %d) AS i`, liveLeases, scopes),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  status, attempt_count, payload, created_at, updated_at)
SELECT 'nb-w-' || i || '-' || j, 'nb-' || i, 'nb-g-' || i, 'reducer', 'nb', 'pending', 0, '{}'::jsonb, $1, $1
FROM generate_series(1, %d) AS i, generate_series(1, %d) AS j`, scopes, eligiblePerScope),
	} {
		if _, err := conn.ExecContext(ctx, stmt, statusSemanticsAsOf); err != nil {
			t.Fatalf("seed unanalyzed backlog: %v\n%s", err, stmt)
		}
	}
}

// blockagePlanCTEs are the blockage section's CTEs as EXPLAIN names their
// InitPlans. blocked itself is inlined into all_blocked. The plan checks read
// only these subtrees, so a plan change elsewhere in the summary query can
// neither fail them nor satisfy them.
var blockagePlanCTEs = []string{"CTE eligible", "CTE inflight_leases", "CTE all_blocked"}

// blockagePlanRoots returns the plan's blockage CTE subtrees keyed by their
// InitPlan name, and the names it did not find.
func blockagePlanRoots(plan map[string]any) (map[string]map[string]any, []string) {
	roots := map[string]map[string]any{}
	var collect func(node map[string]any)
	collect = func(node map[string]any) {
		if name, _ := node["Subplan Name"].(string); slices.Contains(blockagePlanCTEs, name) {
			roots[name] = node
		}
		for _, child := range planChildren(node) {
			collect(child)
		}
	}
	collect(plan)
	var missing []string
	for _, name := range blockagePlanCTEs {
		if roots[name] == nil {
			missing = append(missing, name)
		}
	}
	return roots, missing
}

// planFiltersByHashedSubPlan reports whether the blocked filter, inlined into
// all_blocked, probes a hashed SubPlan over inflight_leases: a scan of the
// eligible CTE whose Filter says "hashed SubPlan" and whose SubPlan child
// scans inflight_leases. A hashed SubPlan anywhere else does not count.
func planFiltersByHashedSubPlan(plan map[string]any) bool {
	roots, _ := blockagePlanRoots(plan)
	var found func(node map[string]any) bool
	found = func(node map[string]any) bool {
		if filter, _ := node["Filter"].(string); node["CTE Name"] == "eligible" && strings.Contains(filter, "hashed SubPlan") {
			for _, child := range planChildren(node) {
				if child["Parent Relationship"] == "SubPlan" && child["CTE Name"] == "inflight_leases" {
					return true
				}
			}
		}
		return slices.ContainsFunc(planChildren(node), found)
	}
	return roots["CTE all_blocked"] != nil && found(roots["CTE all_blocked"])
}

// maxBlockagePathLoops returns the largest Actual Loops of any node inside the
// blockage CTEs that reads fact_work_items or scans the eligible or
// inflight_leases CTE. Both sides are watched: a nested loop can rescan either
// one. Nodes outside the blockage CTEs are not counted.
func maxBlockagePathLoops(plan map[string]any) int {
	roots, _ := blockagePlanRoots(plan)
	var walk func(node map[string]any) int
	walk = func(node map[string]any) int {
		most := 0
		if node["Relation Name"] == "fact_work_items" || node["CTE Name"] == "eligible" || node["CTE Name"] == "inflight_leases" {
			if loops, ok := node["Actual Loops"].(float64); ok {
				most = int(loops)
			}
		}
		for _, child := range planChildren(node) {
			most = max(most, walk(child))
		}
		return most
	}
	most := 0
	for _, root := range roots {
		most = max(most, walk(root))
	}
	return most
}

// planChildren returns a plan node's child nodes.
func planChildren(node map[string]any) []map[string]any {
	raw, _ := node["Plans"].([]any)
	children := make([]map[string]any, 0, len(raw))
	for _, child := range raw {
		if childNode, ok := child.(map[string]any); ok {
			children = append(children, childNode)
		}
	}
	return children
}

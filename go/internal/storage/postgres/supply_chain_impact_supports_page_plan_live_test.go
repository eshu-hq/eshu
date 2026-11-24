// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11198 hot-key plan guard on real PostgreSQL: the supports page statement plans as per-key ordered index scans (the package, CVE, advisory and repository arms each on their own partial btree with the key and the cursor in the Index Cond) with no sequential scan of the supports, no scope-generation index probe, no temp, under both force_generic_plan and force_custom_plan, first page and next page; and seeded violations (the package or repository index dropped, a package or repository guard literal removed, an active join inside the lateral, the cursor written as an OR filter) each fail the guard
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// planGuardKeys is the all-arms filter the guard plans: one key per arm, each
// present in the seed. B2C_PLAN_GUARD_KEYS replaces it, with B2C_PLAN_GUARD_PACKAGE
// naming the package the cursor is taken from, when the guard runs against an
// existing corpus.
var planGuardKeys = envOr("B2C_PLAN_GUARD_KEYS", "'{repo:hot-r1}','{pkg:hot}','{CVE-hot-1}','{GHSA-hot-1}'")

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// planGuardSeed fills the supports table with 400,000 rows and one hot key per arm
// (a package of 267,000 rows, a CVE of 200,000, an advisory of 133,000, a
// repository of 100,000) and a spread of other keys, then vacuums and analyzes it,
// so the planner has real statistics: at 60,000 rows it prefers a bitmap scan and
// a sort for the CVE, advisory and repository arms, a plan the larger table does
// not take. With 300 active scopes the scope-driven plan is a live alternative the
// fence must keep the planner away from.
func planGuardSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload, active_generation_id)
SELECT 'pg-scope-'||g, 'repository', 'git', 'pg-scope-'||g, 'git', 'pg-scope-'||g, now(), now(), 'active', '{}'::jsonb, 'pg-gen-'||g FROM generate_series(1,300) g`); err != nil {
		t.Fatalf("seed scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'pg-gen-'||g, 'pg-scope-'||g, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb FROM generate_series(1,300) g`); err != nil {
		t.Fatalf("seed generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, repository_id, cve_id, advisory_id, package_id, payload)
SELECT md5(i::text) || md5((i*7)::text), 'k'||i, 'pg-scope-'||(1+i%300), 'pg-gen-'||(1+i%300), 'f'||i, 1, 50, 'exact', 'none',
       CASE WHEN i%4 = 0 THEN 'repo:hot-r1' ELSE 'repo:hot-r'||(2+i%50000) END,
       CASE WHEN i%2 = 0 THEN 'CVE-hot-1' ELSE 'CVE-hot-'||(2+i%40000) END,
       CASE WHEN i%3 = 0 THEN 'GHSA-hot-1' ELSE 'GHSA-hot-'||(2+i%50000) END,
       CASE WHEN i%3 < 2 THEN 'pkg:hot' ELSE 'pkg:other-'||(i%60000) END,
       jsonb_build_object('padding', repeat('x', 300))
FROM generate_series(1, 400000) i`); err != nil {
		t.Fatalf("seed supports: %v", err)
	}
	// The CVE and advisory arms are served by PR-B2b's migrations 339 and 340. The
	// guard proves the plans on those indexes, so it fails when a migration stops
	// building one instead of building a stand-in that would hide the loss.
	for _, col := range []string{"cve_id", "advisory_id"} {
		if planGuardIndexName(t, ctx, db, col) == "" {
			t.Fatalf("no partial (%s, fact_id) btree on the supports table: migrations 339 and 340 must build it", col)
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE) supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
}

var planGuardDefinition = regexp.MustCompile(`\(([a-z_]+), fact_id\) WHERE \(?[a-z_]+ <> ''`)

// planGuardIndexName returns the name of the partial (col, fact_id) btree on the
// supports table, or "" when none exists.
func planGuardIndexName(t *testing.T, ctx context.Context, db *sql.DB, col string) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE tablename = 'supply_chain_impact_finding_supports'`)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		if m := planGuardDefinition.FindStringSubmatch(def); m != nil && m[1] == col {
			return name
		}
	}
	return ""
}

// planGuardPlan returns the EXPLAIN (ANALYZE, BUFFERS) text of one reconciliation
// page statement (four key arrays) prepared and executed under the given
// plan_cache_mode. cursor is "" for the first-page text.
func planGuardPlan(t *testing.T, ctx context.Context, conn *sql.Conn, mode, query, cursor string) string {
	t.Helper()
	return planGuardPlanFor(t, ctx, conn, mode, query, planGuardKeys, 4, cursor)
}

// planGuardPlanFor is planGuardPlan for a statement with nkeys key arrays, whose
// bound values are the SQL literal list keys.
func planGuardPlanFor(t *testing.T, ctx context.Context, conn *sql.Conn, mode, query, keys string, nkeys int, cursor string) string {
	t.Helper()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
		t.Fatalf("set plan_cache_mode: %v", err)
	}
	types, args := strings.TrimSuffix(strings.Repeat("text[],", nkeys), ","), keys
	if cursor != "" {
		types, args = types+",text", args+",'"+cursor+"'"
	}
	// PREPARE takes $n parameters; the bound values arrive through EXECUTE. The
	// statement name is the guard's own, so the driver's statement cache is left
	// alone.
	_, _ = conn.ExecContext(ctx, "DEALLOCATE page_guard")
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PREPARE page_guard(%s) AS %s", types, query)); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) EXECUTE page_guard(%s)", args))
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	return strings.Join(plan, "\n")
}

// planGuardColumns maps an arm name to the supports column its key is matched on.
var planGuardColumns = map[string]string{
	"package": "package_id", "repository": "repository_id", "cve": "cve_id", "advisory": "advisory_id",
}

// planNodeIndexCond returns the Index Cond line of the first plan node that scans
// index, bounded to that node: its own attribute lines run until the next line
// that is not indented deeper than the node's own "->" line, so a later node's
// condition is never taken for it. found is false when no node scans the index.
func planNodeIndexCond(plan, index string) (cond string, found bool) {
	scan := regexp.MustCompile(`Index (?:Only )?Scan using ` + regexp.QuoteMeta(index) + ` on `)
	lines := strings.Split(plan, "\n")
	for i, line := range lines {
		if !scan.MatchString(line) {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		for _, attr := range lines[i+1:] {
			attrIndent := len(attr) - len(strings.TrimLeft(attr, " "))
			if attrIndent <= indent {
				break
			}
			if strings.HasPrefix(strings.TrimSpace(attr), "Index Cond:") {
				return strings.TrimSpace(attr), true
			}
		}
		return "", true
	}
	return "", false
}

// planGuardViolation returns "" when plan meets the guard and the reason it does
// not otherwise. The guard: every arm runs as an index scan on its own index
// under a Limit; the partial-index arms carry the key and, on a next page, the
// cursor in the Index Cond; the supports are never sequentially scanned or
// probed per scope; nothing spills to temp.
func planGuardViolation(plan string, indexes map[string]string, nextPage bool) string {
	switch {
	case strings.Contains(plan, "Seq Scan on supply_chain_impact_finding_supports"):
		return "sequential scan of the supports"
	case strings.Contains(plan, "supply_chain_impact_finding_supports_scope_generation_idx"):
		return "one probe per active scope (scope_generation index)"
	case strings.Contains(plan, "temp read=") || strings.Contains(plan, "temp written="):
		return "temp blocks written"
	case strings.Contains(plan, "CTE Scan") || strings.Contains(plan, "Storage: Disk"):
		return "the match set is materialized or spilled"
	}
	for arm, index := range indexes {
		cond, found := planNodeIndexCond(plan, index)
		if !found {
			return "the " + arm + " arm does not use its index " + index
		}
		key := regexp.MustCompile(`\b` + planGuardColumns[arm] + ` = k(?:_\d+)?\.v\b`)
		if !key.MatchString(cond) {
			return "the key is not in the Index Cond of the " + arm + " arm: " + cond
		}
		if nextPage && !strings.Contains(cond, "fact_id > ") {
			return "the cursor is not in the Index Cond of the " + arm + " arm: " + cond
		}
	}
	return ""
}

// planGuardCases is the matrix the guard runs.
func planGuardCases() (modes []string, pages []string) {
	return []string{"force_generic_plan", "force_custom_plan"}, []string{"first", "next"}
}

// TestSupplyChainImpactSupportsPagePlanGuardLive pins the plan of the ruled page
// statement under both plan cache modes and both page texts. B2C_PLAN_GUARD_DSN,
// when set, runs the same guard against an existing corpus (the hot-key and the
// 8,000,000-support databases of the evidence) instead of a fresh seed.
func TestSupplyChainImpactSupportsPagePlanGuardLive(t *testing.T) {
	var db *sql.DB
	var ctx context.Context
	if dsn := os.Getenv("B2C_PLAN_GUARD_DSN"); dsn != "" {
		var err error
		if db, err = sql.Open("pgx", dsn); err != nil {
			t.Fatalf("open guard database: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		ctx = context.Background()
	} else {
		db, ctx = openIsolatedLiveDB(t, "b2c_plan_guard",
			workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
		planGuardSeed(t, ctx, db)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	indexes := map[string]string{
		"package":    planGuardIndexName(t, ctx, db, "package_id"),
		"repository": planGuardIndexName(t, ctx, db, "repository_id"),
		"cve":        planGuardIndexName(t, ctx, db, "cve_id"),
		"advisory":   planGuardIndexName(t, ctx, db, "advisory_id"),
	}
	for col, name := range indexes {
		if name == "" {
			t.Fatalf("no partial (%s_id, fact_id) btree on the supports table: the guard has nothing to prove", col)
		}
	}
	// The guard proves the arms on these indexes by name; the -v output records which.
	// On a fresh database each is a migration's index; on the B2C_PLAN_GUARD_DSN path
	// they are whatever the lab corpus holds.
	t.Logf("plan guard indexes: package=%s repository=%s cve=%s advisory=%s",
		indexes["package"], indexes["repository"], indexes["cve"], indexes["advisory"])
	var cursor string
	guardPackage := envOr("B2C_PLAN_GUARD_PACKAGE", "pkg:hot")
	if err := db.QueryRowContext(ctx, `
SELECT fact_id FROM supply_chain_impact_finding_supports WHERE package_id = $1 ORDER BY fact_id
OFFSET (SELECT count(*) / 2 FROM supply_chain_impact_finding_supports WHERE package_id = $1) LIMIT 1`, guardPackage).Scan(&cursor); err != nil {
		t.Fatalf("pick a midpoint cursor: %v", err)
	}
	first := supplyChainImpactSupportsPageQuery(securityAlertReconciliationSupportsArms, 0)
	next := supplyChainImpactSupportsPageQuery(securityAlertReconciliationSupportsArms, 5)

	modes, pages := planGuardCases()
	for _, mode := range modes {
		for _, page := range pages {
			t.Run(mode+"/"+page+" page is green", func(t *testing.T) {
				query, cur := first, ""
				if page == "next" {
					query, cur = next, cursor
				}
				plan := planGuardPlan(t, ctx, conn, mode, query, cur)
				if why := planGuardViolation(plan, indexes, page == "next"); why != "" {
					t.Fatalf("plan violates the guard: %s\n%s", why, plan)
				}
			})
		}
	}

	// The service-catalog loader's one-arm statement: the same guard on the
	// repository index alone, both modes, both pages.
	repoKey := "'{" + envOr("B2C_PLAN_GUARD_REPOSITORY", "repo:hot-r1") + "}'"
	scFirst := supplyChainImpactSupportsPageQuery(supplyChainAdvisoriesForReposArms, 0)
	scNext := supplyChainImpactSupportsPageQuery(supplyChainAdvisoriesForReposArms, 2)
	for _, mode := range modes {
		for _, page := range pages {
			t.Run("service catalog/"+mode+"/"+page+" page is green", func(t *testing.T) {
				query, cur := scFirst, ""
				if page == "next" {
					query, cur = scNext, cursor
				}
				plan := planGuardPlanFor(t, ctx, conn, mode, query, repoKey, 1, cur)
				if why := planGuardViolation(plan, map[string]string{"repository": indexes["repository"]}, page == "next"); why != "" {
					t.Fatalf("service-catalog plan violates the guard: %s\n%s", why, plan)
				}
			})
		}
	}

	// Seeded violations. Each must make the guard fail, or the guard proves
	// nothing. They run on the next page, generic plan: the shape that carries the
	// cursor.
	for _, arm := range []string{"package", "repository"} {
		t.Run("violation: the "+arm+" index dropped", func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, "DROP INDEX "+indexes[arm]); err != nil {
				t.Fatalf("drop index: %v", err)
			}
			var plan strings.Builder
			if _, err := tx.ExecContext(ctx, "SET LOCAL plan_cache_mode = force_generic_plan"); err != nil {
				t.Fatalf("set plan_cache_mode: %v", err)
			}
			if _, err := tx.ExecContext(ctx, "PREPARE page_guard_"+arm+"(text[],text[],text[],text[],text) AS "+next); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			rows, err := tx.QueryContext(ctx, fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) EXECUTE page_guard_%s(%s,'%s')", arm, planGuardKeys, cursor))
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			for rows.Next() {
				var line string
				_ = rows.Scan(&line)
				plan.WriteString(line + "\n")
			}
			_ = rows.Close()
			if why := planGuardViolation(plan.String(), indexes, true); why == "" {
				t.Fatalf("the guard passed a plan with the %s index dropped:\n%s", arm, plan.String())
			}
		})
	}
	for _, arm := range []struct{ name, column string }{{"package", "package_id"}, {"repository", "repository_id"}} {
		t.Run("violation: the "+arm.name+" guard literal removed", func(t *testing.T) {
			bare := strings.ReplaceAll(next, " AND s."+arm.column+" <> ''", "")
			if bare == next {
				t.Fatal("the seeded violation did not change the statement")
			}
			plan := planGuardPlan(t, ctx, conn, "force_generic_plan", bare, cursor)
			if why := planGuardViolation(plan, indexes, true); why == "" {
				t.Fatalf("the guard passed a plan with the %s guard literal removed:\n%s", arm.name, plan)
			}
		})
	}
	t.Run("violation: an active join inside the lateral", func(t *testing.T) {
		joined := strings.ReplaceAll(next, "SELECT s.fact_id FROM supply_chain_impact_finding_supports AS s\n",
			"SELECT s.fact_id FROM supply_chain_impact_finding_supports AS s\n"+
				"        JOIN ingestion_scopes AS sc0 ON sc0.scope_id = s.scope_id AND sc0.active_generation_id = s.generation_id\n"+
				"        JOIN scope_generations AS g0 ON g0.scope_id = s.scope_id AND g0.generation_id = s.generation_id AND g0.status = 'active'\n")
		if joined == next {
			t.Fatal("the seeded violation did not change the statement")
		}
		plan := planGuardPlan(t, ctx, conn, "force_generic_plan", joined, cursor)
		if why := planGuardViolation(plan, indexes, true); why == "" {
			// On a large existing corpus the planner may keep the ordered scans even
			// with the join inside; the statement-text test
			// (TestSupplyChainImpactSupportsPageQueryFencesEveryArm) is what forbids
			// the join there. The fresh seed, with 300 active scopes, must flip.
			if os.Getenv("B2C_PLAN_GUARD_DSN") != "" {
				t.Logf("the planner kept ordered scans with the join inside the lateral on this corpus; the text guard forbids it")
				return
			}
			t.Fatalf("the guard passed a plan with the active join inside the lateral:\n%s", plan)
		}
	})
	t.Run("violation: the cursor written as an OR filter", func(t *testing.T) {
		orForm := strings.ReplaceAll(next, " AND s.fact_id > $5", " AND ($5 = '' OR s.fact_id > $5)")
		if orForm == next {
			t.Fatal("the seeded violation did not change the statement")
		}
		plan := planGuardPlan(t, ctx, conn, "force_generic_plan", orForm, cursor)
		if why := planGuardViolation(plan, indexes, true); why == "" {
			t.Fatalf("the guard passed a plan with the cursor as an OR filter:\n%s", plan)
		}
	})
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11198 and #11199 repository-arm bounds on real PostgreSQL, on the index migration 346 builds: for a repository of 160,000 supports the shipped supports page statement (the reconciliation repository arm and the service-catalog loader's one arm) reads page 1, a midpoint page and the last page in at most 6,000 buffers with no temp under force_generic_plan and force_custom_plan, the midpoint costs at most 1.5 times page 1, and the same pages stay within 7,000 buffers after every twentieth row of the repository was rewritten and nothing was vacuumed
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const repositoryArmRows = 160000

// repositoryArmRepo is the repository the bounds are taken on. B2C_REPO_ARM_DSN,
// when set, runs the same test against an existing corpus (the lab database of the
// evidence) with B2C_REPO_ARM_REPOSITORY naming a repository of at least 160,000
// supports there, instead of seeding a fresh one.
var repositoryArmRepo = envOr("B2C_REPO_ARM_REPOSITORY", "repo:big")

var (
	planBuffersLine = regexp.MustCompile(`(?m)^\s+Buffers: shared hit=(\d+)(?: read=(\d+))?`)
	planExecTime    = regexp.MustCompile(`Execution Time: ([0-9.]+) ms`)
)

// planTopBuffers returns the buffers of the plan's top node (the first Buffers
// line of an EXPLAIN (ANALYZE, BUFFERS) text) and its execution time in ms.
func planTopBuffers(t *testing.T, plan string) (buffers int, ms float64) {
	t.Helper()
	m := planBuffersLine.FindStringSubmatch(plan)
	if m == nil {
		t.Fatalf("no Buffers line in plan:\n%s", plan)
	}
	hit, _ := strconv.Atoi(m[1])
	read := 0
	if m[2] != "" {
		read, _ = strconv.Atoi(m[2])
	}
	e := planExecTime.FindStringSubmatch(plan)
	if e == nil {
		t.Fatalf("no Execution Time in plan:\n%s", plan)
	}
	ms, _ = strconv.ParseFloat(e[1], 64)
	return hit + read, ms
}

// repositoryArmSeed gives the supports table one repository of 160,000 supports
// among 100,000 rows of other repositories, all in one active generation, so the
// repository composite is the only index that can order the repository's rows.
func repositoryArmSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	b2cSeedScopes(t, ctx, db)
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, repository_id, package_id, payload)
SELECT md5(i::text) || md5((i*7)::text), 'k'||i, $1, $2, 'f'||i, 1, 50, 'exact', 'none',
       CASE WHEN i <= $3 THEN 'repo:big' ELSE 'repo:other-'||(i % 20000) END, 'pkg:'||(i % 5000),
       jsonb_build_object('padding', repeat('x', 300))
FROM generate_series(1, $4::int) AS i`, b2cScopeA, b2cGenA, repositoryArmRows, repositoryArmRows+100000); err != nil {
		t.Fatalf("seed supports: %v", err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE) supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
}

// repositoryArmPages measures page 1, the midpoint page and the last page of the
// repository's drain under one plan cache mode for the arms of one loader, and
// returns each page's top-node buffers.
func repositoryArmPages(
	t *testing.T, ctx context.Context, db *sql.DB, conn *sql.Conn, mode string, arms []supplyChainImpactSupportsArm, repos []string,
) (page1, mid, last int) {
	t.Helper()
	var midID, lastID string
	if err := db.QueryRowContext(ctx, `
SELECT fact_id FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($1::text[]) ORDER BY fact_id
OFFSET (SELECT count(*) / 2 FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($1::text[])) LIMIT 1`, pqArray(repos)).Scan(&midID); err != nil {
		t.Fatalf("pick midpoint: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT fact_id FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($1::text[]) ORDER BY fact_id
OFFSET (SELECT count(*) - 250 FROM supply_chain_impact_finding_supports WHERE repository_id = ANY($1::text[])) LIMIT 1`, pqArray(repos)).Scan(&lastID); err != nil {
		t.Fatalf("pick last page: %v", err)
	}
	first := supplyChainImpactSupportsPageQuery(arms, 0)
	next := supplyChainImpactSupportsPageQuery(arms, len(arms)+1)
	// The arms bind one array per parameter: the repository array first, the
	// rest empty, so the same keys string serves both loaders' arms.
	keys := "'{" + strings.Join(repos, ",") + "}'" + strings.Repeat(",'{}'", len(arms)-1)
	run := func(query, cursor string) (int, float64) {
		types := strings.TrimSuffix(strings.Repeat("text[],", len(arms)), ",")
		args := keys
		if cursor != "" {
			types, args = types+",text", args+",'"+cursor+"'"
		}
		if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
			t.Fatalf("set plan_cache_mode: %v", err)
		}
		_, _ = conn.ExecContext(ctx, "DEALLOCATE repo_arm_guard")
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PREPARE repo_arm_guard(%s) AS %s", types, query)); err != nil {
			t.Fatalf("prepare: %v", err)
		}
		var buffers int
		var ms float64
		for pass := 0; pass < 2; pass++ { // the second run is the warm one
			rows, err := conn.QueryContext(ctx, fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, COSTS OFF) EXECUTE repo_arm_guard(%s)", args))
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			var lines []string
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatalf("scan: %v", err)
				}
				lines = append(lines, line)
			}
			_ = rows.Close()
			plan := strings.Join(lines, "\n")
			if strings.Contains(plan, "temp read=") || strings.Contains(plan, "temp written=") {
				t.Fatalf("a page wrote temp:\n%s", plan)
			}
			// An arm bound to an empty array is "never executed"; a sequential scan
			// there is only a missing index (PR-B2b's CVE and advisory btrees) on an
			// arm no key reaches.
			for _, line := range strings.Split(plan, "\n") {
				if strings.Contains(line, "Seq Scan on supply_chain_impact_finding_supports") && !strings.Contains(line, "(never executed)") {
					t.Fatalf("a page scanned the supports sequentially:\n%s", plan)
				}
			}
			buffers, ms = planTopBuffers(t, plan)
		}
		return buffers, ms
	}
	var ms [3]float64
	page1, ms[0] = run(first, "")
	mid, ms[1] = run(next, midID)
	last, ms[2] = run(next, lastID)
	for i, name := range []string{"page 1", "midpoint", "last page"} {
		if ms[i] > 50 {
			t.Errorf("%s under %s took %.1f ms warm, want at most 50", name, mode, ms[i])
		}
	}
	return page1, mid, last
}

// pqArray renders ids as the text[] literal a query parameter takes.
func pqArray(ids []string) string { return "{" + strings.Join(ids, ",") + "}" }

// TestServiceCatalogSupportsShapesLive is proof 4 of the
// repository-arm ruling: the service-catalog loader's statement on three call
// shapes over an existing lab corpus (B2C_REPO_ARM_DSN) holding repo:hotrepo,
// repo:hotrepo2 and repo:hotrepo3 of 160,000 supports each and repo:mid-1 to
// repo:mid-200 of at least 1,000 each: one repository; three repositories; 200
// repositories plus one of 160,000. The loader drains one repository at a time, so
// every repository's pages are measured: no temp, at most 50 ms warm, at most 15,000
// buffers.
func TestServiceCatalogSupportsShapesLive(t *testing.T) {
	dsn := os.Getenv("B2C_REPO_ARM_DSN")
	if dsn == "" {
		t.Skip("B2C_REPO_ARM_DSN is not set; the three call shapes need the lab corpus")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open lab database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mid := make([]string, 0, 201)
	for i := 1; i <= 200; i++ {
		mid = append(mid, fmt.Sprintf("repo:mid-%d", i))
	}
	for name, repos := range map[string][]string{
		"one repository of 160,000":            {"repo:hotrepo"},
		"three repositories of 160,000":        {"repo:hotrepo", "repo:hotrepo2", "repo:hotrepo3"},
		"200 repositories plus one of 160,000": append(append([]string{}, mid...), "repo:hotrepo"),
	} {
		t.Run(name, func(t *testing.T) {
			// The loader drains one repository id at a time (GetSupplyChainAdvisoriesForRepos):
			// a single 200-key drain read 92,259 buffers a page, past the bound. The bound
			// holds for every repository's pages, so the largest page is what is reported.
			worst := 0
			for _, repo := range repos {
				for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
					page1, midPage, last := repositoryArmPages(t, ctx, db, conn, mode, supplyChainAdvisoriesForReposArms, []string{repo})
					for page, got := range map[string]int{"page 1": page1, "midpoint": midPage, "last page": last} {
						if got > worst {
							worst = got
						}
						if got > 15000 {
							t.Errorf("%s: %s %s %s read %d buffers, want at most 15,000", name, repo, mode, page, got)
						}
					}
				}
			}
			t.Logf("%s: %d drains, largest page %d buffers", name, len(repos), worst)
		})
	}
}

// TestRepositoryArmBoundsLive is proof 1 of the
// repository-arm ruling (arbiter-b2c-wal-repo-arm.md), on the index migration 346
// builds, for the arms of both loaders.
//
// The name deliberately does not start with TestSupplyChainImpactSupports: that prefix is
// selected by the reducer contention gate's supply-chain -run filter, and this proof seeds
// 160,000 supports (about three minutes on the lab without -race). It belongs to the live
// Postgres readiness shards, which the readiness header above registers it in, not to the
// contention gate. TestServiceCatalogSupportsShapesLive is named the same way and skips
// without the lab corpus.
func TestRepositoryArmBoundsLive(t *testing.T) {
	var db *sql.DB
	var ctx context.Context
	if dsn := os.Getenv("B2C_REPO_ARM_DSN"); dsn != "" {
		var err error
		if db, err = sql.Open("pgx", dsn); err != nil {
			t.Fatalf("open lab database: %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		ctx = context.Background()
	} else {
		db, ctx = openIsolatedLiveDB(t, "b2c_repo_arm",
			workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
		repositoryArmSeed(t, ctx, db)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	check := func(t *testing.T, limit int) {
		for loader, arms := range map[string][]supplyChainImpactSupportsArm{
			"reconciliation":  securityAlertReconciliationSupportsArms,
			"service catalog": supplyChainAdvisoriesForReposArms,
		} {
			for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
				page1, mid, last := repositoryArmPages(t, ctx, db, conn, mode, arms, []string{repositoryArmRepo})
				t.Logf("%s %s: page 1 %d, midpoint %d, last page %d buffers", loader, mode, page1, mid, last)
				for name, got := range map[string]int{"page 1": page1, "midpoint": mid, "last page": last} {
					if got > limit {
						t.Errorf("%s %s %s read %d buffers, want at most %d", loader, mode, name, got, limit)
					}
				}
				if float64(mid) > 1.5*float64(page1) {
					t.Errorf("%s %s midpoint %d buffers exceeds 1.5 x page 1 (%d)", loader, mode, mid, page1)
				}
			}
		}
	}
	t.Run("after vacuum", func(t *testing.T) { check(t, 6000) })

	// Every twentieth row of the repository is rewritten and nothing is
	// vacuumed: the index-only scan now fetches heap pages for the rewritten rows.
	// On an existing lab corpus (B2C_REPO_ARM_DSN) this rewrite is permanent: it sets
	// priority_score = 51 on the same rows every run and leaves dead tuples behind,
	// so vacuum the lab database after a run (a fresh seed is dropped with its
	// database). The test cannot roll it back: the page reads run on other
	// connections and must see the rewritten rows.
	if _, err := db.ExecContext(ctx, `
UPDATE supply_chain_impact_finding_supports SET priority_score = 51
WHERE fact_id IN (SELECT fact_id FROM (SELECT fact_id, row_number() OVER (ORDER BY fact_id) AS n
                  FROM supply_chain_impact_finding_supports WHERE repository_id = $1) r WHERE n % 20 = 0)`, repositoryArmRepo); err != nil {
		t.Fatalf("rewrite every 20th row: %v", err)
	}
	t.Run("every 20th row rewritten, no vacuum", func(t *testing.T) { check(t, 7000) })
}

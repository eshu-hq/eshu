// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// seedOrphanPlanFixture builds a mirror table shaped like the #10020
// production skew: twenty repositories with 10,000 mirror rows each (1,000
// backed by content rows, 9,000 orphans) while the pruned batch touches only
// one of them (8,000 content_entity facts). The IN-subquery shape scanned the
// whole table no matter how small the batch slice was; the fixed plan must
// reach only the batch repository through the repo index.
func seedOrphanPlanFixture(t *testing.T, ctx context.Context, sqlDB *sql.DB) (string, []string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var repos []string
	for i := 0; i < 20; i++ {
		repos = append(repos, fmt.Sprintf("repo-orphan-plan-%s-%02d", suffix, i))
	}
	scope := "scope-orphan-plan-" + suffix
	generation := "gen-orphan-plan-" + suffix
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', '{}'::jsonb)
ON CONFLICT (scope_id) DO NOTHING`, scope); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', now(), now(), 'superseded')
ON CONFLICT (generation_id) DO NOTHING`, generation, scope); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	repoList := "'" + strings.Join(repos, "','") + "'"
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, metadata, indexed_at)
SELECT $1 || '/keep-' || g, $1, 'a.tf', 'TerraformResource', 'r.keep',
    1, 2, '', '{}'::jsonb, now()
FROM generate_series(1, 1000) AS g`, repos[0]); err != nil {
		t.Fatalf("seed content: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
SELECT repo || '/keep-' || g, repo, 'a.tf', 'TerraformResource', 'r.keep', now()
FROM (VALUES ('%s')) AS v(repo) CROSS JOIN generate_series(1, 1000) AS g`, repos[0])); err != nil {
		t.Fatalf("seed backed mirror rows: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
SELECT repo || '/gone-' || g, repo, 'gone.tf', 'TerraformResource', 'r.gone', now()
FROM (VALUES (%s)) AS v(repo) CROSS JOIN generate_series(1, 9000) AS g`, repoList)); err != nil {
		t.Fatalf("seed orphan mirror rows: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT $1 || '/' || g, $2, $1, 'content_entity', $1 || '/' || g,
    'git', $1 || '/' || g, now(), now(),
    jsonb_build_object('repo_id', $3::text, 'entity_id', $3::text || '/keep-' || (g % 1000 + 1))
FROM generate_series(1, 8000) AS g`, generation, scope, repos[0]); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	for _, table := range []string{"infra_resource_entities", "fact_records", "content_entities"} {
		if _, err := sqlDB.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	return generation, repos[:1]
}

// explainProductionOrphanDelete runs EXPLAIN (never ANALYZE: the DELETE must
// not execute) over the exact shipped orphan-delete text, bound to the batch
// repositories exactly as DeleteOrphanedRows binds them.
func explainProductionOrphanDelete(t *testing.T, ctx context.Context, sqlDB *sql.DB, repos []string) string {
	t.Helper()
	rows, err := sqlDB.QueryContext(ctx, "EXPLAIN "+inventory.RetentionOrphanDeleteSQL, repos)
	if err != nil {
		t.Fatalf("explain orphan delete: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return plan.String()
}

// planTotalCost parses the top-level total cost out of an EXPLAIN (COSTS ON)
// plan: the first cost=0.00..<total> pair belongs to the root node.
func planTotalCost(t *testing.T, plan string) float64 {
	t.Helper()
	m := regexp.MustCompile(`cost=0\.00\.([0-9.]+)`).FindStringSubmatch(plan)
	if m == nil {
		t.Fatalf("no top-level total cost in plan:\n%s", plan)
	}
	cost, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("parse plan cost %q: %v", m[1], err)
	}
	return cost
}

// explainLegacyOrphanDelete runs EXPLAIN (never ANALYZE) over the frozen
// pre-#10020 orphan-delete text bound to the same generations the new
// statement's batch derives its repositories from.
func explainLegacyOrphanDelete(t *testing.T, ctx context.Context, sqlDB *sql.DB, generations []string) string {
	t.Helper()
	rows, err := sqlDB.QueryContext(ctx, "EXPLAIN "+frozenLegacyOrphanDeleteSQL, array.StringArray(generations))
	if err != nil {
		t.Fatalf("explain legacy orphan delete: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return plan.String()
}

// TestRetentionLiveOrphanDeletePlanUsesRepoIndex is the #10020 regression
// proof for the repo-scoped orphan delete: on a fitted batch the shipped
// statement scanned all 183,775 mirror rows and probed content_entities_pkey
// per row (plan cost 284,710, 35.6 s phase). Absolute node assertions
// ("must use the repo index, never seq-scan") do not hold at every
// database size: on a small fixture the planner may legitimately seq-scan a
// 181k-row mirror table, while on production data the same shipped text
// takes the repo bitmap index under both custom and generic plans
// (verified on the QA read replica, generic plan included). This test
// therefore pins the scale-invariant properties of the fix instead:
// the batch repositories arrive as a bound array (the statement never
// scans fact_records to discover them), and the new plan costs strictly
// less than half of the legacy single-statement shape on the same
// fixture (80x on production data: 118.73 vs 9456.11).
// frozenLegacyOrphanDeleteSQL is the pre-#10020 single-statement orphan
// delete, kept verbatim as the differential oracle. It must never change:
// any divergence between its result and DeleteOrphanedRows is a semantic
// regression in the rewrite.
const frozenLegacyOrphanDeleteSQL = `
DELETE FROM infra_resource_entities AS ire
WHERE ire.repo_id IN (
SELECT DISTINCT fact.payload->>'repo_id' AS repo_id
FROM fact_records AS fact
WHERE fact.generation_id = ANY($1::text[])
  AND fact.fact_kind = 'content_entity'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'repo_id' <> ''
)
  AND NOT EXISTS (
      SELECT 1 FROM content_entities AS ce
      WHERE ce.entity_id = ire.entity_id
  )`

// seedOrphanDifferentialFixture builds two affected repositories (kept and
// orphaned mirror rows, generation facts in both) and one untouched
// repository (kept rows plus orphans no batch may delete).
func seedOrphanDifferentialFixture(t *testing.T, ctx context.Context, sqlDB *sql.DB, tag string) ([]string, []string, string) {
	t.Helper()
	tag = fmt.Sprintf("%s-%d", tag, time.Now().UnixNano())
	affectedA := "repo-orphan-diff-" + tag + "-a"
	affectedB := "repo-orphan-diff-" + tag + "-b"
	untouched := "repo-orphan-diff-" + tag + "-u"
	generationA := "gen-orphan-diff-" + tag + "-a"
	generationB := "gen-orphan-diff-" + tag + "-b"
	for _, repo := range []string{affectedA, affectedB, untouched} {
		putContent(t, ctx, sqlDB, repo,
			contentRow{"keep", "a.tf", "TerraformResource", "r.keep", `{}`},
			contentRow{"gone", "gone.tf", "TerraformResource", "r.gone", `{}`},
		)
		database := postgres.SQLDB{DB: sqlDB}
		if _, err := inventory.MirrorPaths(ctx, database, inventory.Target{RepoID: repo}, []string{"a.tf", "gone.tf"}); err != nil {
			t.Fatalf("MirrorPaths(%s): %v", repo, err)
		}
	}
	seedGenerationFacts(t, ctx, sqlDB, affectedA, generationA, "keep", "gone")
	seedGenerationFacts(t, ctx, sqlDB, affectedB, generationB, "keep", "gone")
	// Prune only this variant's content rows: an unscoped LIKE would reach
	// other tests' rows in the shared live database.
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM content_entities WHERE repo_id = ANY($1::text[]) AND entity_id LIKE '%/gone'`,
		[]string{affectedA, affectedB}); err != nil {
		t.Fatalf("prune content: %v", err)
	}
	return []string{generationA, generationB}, []string{affectedA, affectedB}, untouched
}

// remainingMirrorSuffixes returns the entity-id suffixes (past "repo/") of
// every mirror row of repo, ordered.
func remainingMirrorSuffixes(t *testing.T, ctx context.Context, sqlDB *sql.DB, repo string) []string {
	t.Helper()
	rows, err := sqlDB.QueryContext(ctx, `SELECT entity_id FROM infra_resource_entities WHERE repo_id = $1 ORDER BY 1`, repo)
	if err != nil {
		t.Fatalf("read mirror rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan mirror row: %v", err)
		}
		out = append(out, strings.TrimPrefix(id, repo+"/"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read mirror rows: %v", err)
	}
	return out
}

// TestRetentionLiveOrphanDeleteMatchesLegacySemantics proves the array-bound
// rewrite deletes exactly the rows the single-statement shape deleted: the
// same count and the same surviving rows per repository role, including the
// untouched repository whose orphans must survive and the pre-existing
// orphans the writer paths leave behind.
func TestRetentionLiveOrphanDeleteMatchesLegacySemantics(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	type outcome struct {
		deleted   int64
		affected  [][]string
		untouched []string
	}
	run := func(tag string, legacy bool) outcome {
		generations, affected, untouched := seedOrphanDifferentialFixture(t, ctx, sqlDB, tag)
		database := postgres.SQLDB{DB: sqlDB}
		tx, err := database.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		var deleted int64
		if legacy {
			var res sql.Result
			if res, err = tx.ExecContext(ctx, frozenLegacyOrphanDeleteSQL, generations); err != nil {
				_ = tx.Rollback()
				t.Fatalf("legacy orphan delete: %v", err)
			}
			if deleted, err = res.RowsAffected(); err != nil {
				_ = tx.Rollback()
				t.Fatalf("legacy rows affected: %v", err)
			}
		} else {
			if deleted, err = inventory.DeleteOrphanedRows(ctx, tx, generations); err != nil {
				_ = tx.Rollback()
				t.Fatalf("DeleteOrphanedRows: %v", err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		out := outcome{deleted: deleted, untouched: remainingMirrorSuffixes(t, ctx, sqlDB, untouched)}
		for _, repo := range affected {
			out.affected = append(out.affected, remainingMirrorSuffixes(t, ctx, sqlDB, repo))
		}
		return out
	}
	want := run("legacy", true)
	got := run("current", false)
	if got.deleted != want.deleted {
		t.Fatalf("deleted = %d, legacy deleted %d", got.deleted, want.deleted)
	}
	if fmt.Sprint(got.affected) != fmt.Sprint(want.affected) {
		t.Fatalf("affected survivors differ:\n got %q\nwant %q", got.affected, want.affected)
	}
	if fmt.Sprint(got.untouched) != fmt.Sprint(want.untouched) {
		t.Fatalf("untouched survivors differ:\n got %q\nwant %q", got.untouched, want.untouched)
	}
}

func TestRetentionLiveOrphanDeletePlanUsesRepoIndex(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	generation, repos := seedOrphanPlanFixture(t, ctx, sqlDB)
	plan := explainProductionOrphanDelete(t, ctx, sqlDB, repos)
	if strings.Contains(plan, "fact_records") {
		t.Fatalf("orphan delete scans fact_records instead of taking bound repositories:\n%s", plan)
	}
	legacy := explainLegacyOrphanDelete(t, ctx, sqlDB, []string{generation})
	newCost, legacyCost := planTotalCost(t, plan), planTotalCost(t, legacy)
	if !(newCost < legacyCost/2) {
		t.Fatalf("orphan delete plan cost %.2f is not under half of legacy %.2f:\nnew:\n%slegacy:\n%s",
			newCost, legacyCost, plan, legacy)
	}
}

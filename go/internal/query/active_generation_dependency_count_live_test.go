// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10824 disposable PostgreSQL proof that the repository dependency count joins the active relationship generation: rows in a superseded, pending, or deleted generation do not count, a non-dependency relationship type does not count, the count is the distinct union of active generations across scopes and a per-scope mutant is caught, and EXPLAIN (ANALYZE, BUFFERS) over 210,000 populated resolved rows must stay index-led on resolved_relationships; disposable bootstrapped database per test
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const dependencyCountLiveSkip = "set ESHU_POSTGRES_DSN to run the #10824 repository dependency count live proofs"

// dependencyCountLiveFixture seeds relationship tables in a disposable
// bootstrapped database.
type dependencyCountLiveFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
}

func newDependencyCountLiveFixture(t *testing.T) *dependencyCountLiveFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip(dependencyCountLiveSkip)
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	return &dependencyCountLiveFixture{t: t, ctx: ctx, database: database}
}

func (f *dependencyCountLiveFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *dependencyCountLiveFixture) generation(id, scope, status string) {
	f.t.Helper()
	f.exec(`INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
VALUES ($1, $2, $3, now(), now())`, id, scope, status)
}

func (f *dependencyCountLiveFixture) resolved(id, generationID, source, target, relType string) {
	f.t.Helper()
	f.exec(`INSERT INTO resolved_relationships (
	resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
	confidence, evidence_count, rationale, resolution_source, details
) VALUES ($1, $2, $3, $4, $5, 0.9, 1, 'test', 'test', '{}'::jsonb)`, id, generationID, source, target, relType)
}

func (f *dependencyCountLiveFixture) count(repoID string) int {
	f.t.Helper()
	got, err := NewContentReader(f.database).repositoryDependencyCount(f.ctx, repoID)
	if err != nil {
		f.t.Fatalf("repositoryDependencyCount(%q): %v", repoID, err)
	}
	return got
}

// TestRepositoryDependencyCountActiveGenerationLive proves #10824 against the
// real schema. resolved_relationships is append-only, so rows of a superseded
// generation, a not-yet-active generation, and a generation whose
// relationship_generations row was deleted (the #10820 purge) all stay on disk
// and must not contribute to the count.
func TestRepositoryDependencyCountActiveGenerationLive(t *testing.T) {
	f := newDependencyCountLiveFixture(t)

	// repo-a: the superseded generation points at dep-1..dep-5, the active
	// generation at dep-1, dep-2, dep-6, so only the active three count.
	f.generation("gen-a-old", "scope-a", "superseded")
	f.generation("gen-a-new", "scope-a", "active")
	for i, target := range []string{"dep-1", "dep-2", "dep-3", "dep-4", "dep-5"} {
		f.resolved("a-old-"+string(rune('0'+i)), "gen-a-old", "repo-a", target, "DEPENDS_ON")
	}
	for i, target := range []string{"dep-1", "dep-2", "dep-6"} {
		f.resolved("a-new-"+string(rune('0'+i)), "gen-a-new", "repo-a", target, "DEPENDS_ON")
	}
	// A non-dependency type in the active generation stays out of the count.
	f.resolved("a-new-ctx", "gen-a-new", "repo-a", "dep-7", "CORRELATES_DEPLOYABLE_UNIT")
	if got, want := f.count("repo-a"), 3; got != want {
		t.Fatalf("repo-a dependency count = %d, want %d (only the active generation, dependency types)", got, want)
	}

	// repo-b: rows only in a generation that is pending, not active.
	f.generation("gen-b-pending", "scope-b", "pending")
	f.resolved("b-pending-0", "gen-b-pending", "repo-b", "dep-1", "USES_MODULE")
	if got, want := f.count("repo-b"), 0; got != want {
		t.Fatalf("repo-b dependency count = %d, want %d (pending generation is not active)", got, want)
	}

	// repo-c: the generation row was deleted (retired scope purge); the
	// append-only resolved rows remain and must not count.
	f.resolved("c-orphan-0", "gen-c-deleted", "repo-c", "dep-1", "DEPLOYS_FROM")
	f.resolved("c-orphan-1", "gen-c-deleted", "repo-c", "dep-2", "RUNS_ON")
	if got, want := f.count("repo-c"), 0; got != want {
		t.Fatalf("repo-c dependency count = %d, want %d (deleted generation)", got, want)
	}

	// repo-d: the happy path, a single active generation, is unchanged and
	// another repository's active rows never leak in.
	f.generation("gen-d", "scope-d", "active")
	f.resolved("d-0", "gen-d", "repo-d", "dep-1", "DEPENDS_ON")
	f.resolved("d-1", "gen-d", "repo-d", "dep-2", "READS_CONFIG_FROM")
	f.resolved("d-dup", "gen-d", "repo-d", "dep-2", "DEPENDS_ON")
	if got, want := f.count("repo-d"), 2; got != want {
		t.Fatalf("repo-d dependency count = %d, want %d (distinct targets)", got, want)
	}
}

// TestRepositoryDependencyCountPlanLive records EXPLAIN (ANALYZE, BUFFERS) for
// the shipped query over a populated table of 210,000 resolved rows (4,000
// source repositories x 5 generations x 10 rows, plus a hub repository with a
// 2,000-row active generation, 2,000 superseded rows, and 6,000 more superseded
// rows). The
// plan is logged for the PR proof section; the only gate is that the plan never
// sequentially scans resolved_relationships (the issue's index-led criterion).
func TestRepositoryDependencyCountPlanLive(t *testing.T) {
	f := newDependencyCountLiveFixture(t)
	f.exec(`INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
SELECT 'gen-' || r || '-' || g, 'scope-' || r, CASE WHEN g = 5 THEN 'active' ELSE 'superseded' END, now(), now()
FROM generate_series(1, 4000) AS r, generate_series(1, 5) AS g`)
	f.exec(`INSERT INTO resolved_relationships (
	resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
	confidence, evidence_count, rationale, resolution_source, details)
SELECT 'rr-' || r || '-' || g || '-' || t, 'gen-' || r || '-' || g, 'repo-' || r,
       'target-' || ((r * 7 + g * 3 + t) % 5000), 'DEPENDS_ON', 0.9, 1, 'test', 'test', '{}'::jsonb
FROM generate_series(1, 4000) AS r, generate_series(1, 5) AS g, generate_series(1, 10) AS t`)
	f.generation("gen-hub-old", "scope-hub", "superseded")
	f.generation("gen-hub-new", "scope-hub", "active")
	f.exec(`INSERT INTO resolved_relationships (
	resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
	confidence, evidence_count, rationale, resolution_source, details)
SELECT 'hub-' || gen || '-' || t, 'gen-hub-' || gen, 'repo-hub', 'hub-target-' || t, 'DEPENDS_ON', 0.9, 1, 'test', 'test', '{}'::jsonb
FROM unnest(ARRAY['old', 'new']) AS gen,
     generate_series(1, 2000) AS t`)
	f.exec(`INSERT INTO resolved_relationships (
	resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
	confidence, evidence_count, rationale, resolution_source, details)
SELECT 'hub-extra-' || t, 'gen-hub-old', 'repo-hub', 'hub-extra-' || t, 'DEPENDS_ON', 0.9, 1, 'test', 'test', '{}'::jsonb
FROM generate_series(1, 6000) AS t`)
	f.exec(`VACUUM ANALYZE resolved_relationships`)
	f.exec(`VACUUM ANALYZE relationship_generations`)

	if got, want := f.count("repo-hub"), 2000; got != want {
		t.Fatalf("repo-hub dependency count = %d, want %d (active generation only)", got, want)
	}
	if got, want := f.count("repo-1"), 10; got != want {
		t.Fatalf("repo-1 dependency count = %d, want %d", got, want)
	}

	for _, repoID := range []string{"repo-1", "repo-hub"} {
		rows, err := f.database.QueryContext(f.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+repositoryDependencyCountSQL, repoID)
		if err != nil {
			t.Fatalf("explain %s: %v", repoID, err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("scan plan %s: %v", repoID, err)
			}
			lines = append(lines, line)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate plan %s: %v", repoID, err)
		}
		plan := strings.Join(lines, "\n")
		t.Logf("repository %s dependency count plan:\n%s", repoID, plan)
		if strings.Contains(plan, "Seq Scan on resolved_relationships") {
			t.Fatalf("plan for %s sequentially scans resolved_relationships:\n%s", repoID, plan)
		}
	}
}

// TestRepositoryDependencyCountAcrossScopesLive proves the count is a
// cross-scope union of ACTIVE generations. One source repository has outgoing
// rows under two scopes: each scope has an active and a superseded generation,
// and the two active generations share targets, so count(DISTINCT
// target_repo_id) must dedup across scopes. A per-scope-only variant of the
// predicate must be caught by this case, so the test runs one mutant of the
// shipped SQL and requires it to disagree.
//
// Known limit: a retired repository whose own generation was purged still
// counts rows it holds under another scope's active generation, because the
// join is on generation status, not on the repository's scope. That is left to
// the planned source_repo_id cleanup (#10822).
func TestRepositoryDependencyCountAcrossScopesLive(t *testing.T) {
	f := newDependencyCountLiveFixture(t)
	f.generation("gen-x1-old", "scope-x1", "superseded")
	f.generation("gen-x1", "scope-x1", "active")
	f.generation("gen-x2-old", "scope-x2", "superseded")
	f.generation("gen-x2", "scope-x2", "active")
	rows := []struct{ id, gen, target string }{
		{"x1-a", "gen-x1", "t1"},
		{"x1-b", "gen-x1", "t2"},
		{"x1-c", "gen-x1", "t3"},
		{"x2-a", "gen-x2", "t2"},
		{"x2-b", "gen-x2", "t3"},
		{"x2-c", "gen-x2", "t4"},
		{"x1-old", "gen-x1-old", "t5"},
		{"x2-old", "gen-x2-old", "t6"},
	}
	for _, row := range rows {
		f.resolved(row.id, row.gen, "repo-x", row.target, "DEPENDS_ON")
	}
	// Active targets: {t1,t2,t3} union {t2,t3,t4} = 4; t5 and t6 are superseded.
	const want = 4
	if got := f.count("repo-x"); got != want {
		t.Fatalf("repo-x dependency count = %d, want %d (distinct union of both scopes' active generations)", got, want)
	}

	// Mutant: restrict the join to one scope. It must read 3, not 4, so a
	// per-scope-only implementation fails the assertion above.
	mutated := strings.Replace(repositoryDependencyCountSQL,
		"WHERE g.status = 'active'", "WHERE g.status = 'active' AND g.scope = 'scope-x1'", 1)
	if mutated == repositoryDependencyCountSQL {
		t.Fatal("mutant did not change the shipped SQL; the predicate text moved")
	}
	var mutantCount int
	if err := f.database.QueryRowContext(f.ctx, mutated, "repo-x").Scan(&mutantCount); err != nil {
		t.Fatalf("run per-scope mutant: %v", err)
	}
	if mutantCount == want {
		t.Fatalf("per-scope mutant counted %d, the same as the shipped query; the case does not discriminate", mutantCount)
	}
}

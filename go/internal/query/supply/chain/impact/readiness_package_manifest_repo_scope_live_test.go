// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive is the
// #7007 regression proof for pushing the repository anchor into
// package_manifest_active (readiness_postgres_query.go). Before #7007 that
// CTE read every dependency-variable content_entity fact repo-wide with no
// repository_id predicate at all; on ops-qa (content_entity is the largest
// fact_kind in the corpus) that unfiltered, unindexed read was the dominant
// cost of the impact/findings endpoint's reported 13-25s latency (EXPLAIN
// (ANALYZE, BUFFERS) against ops-qa, read-only, recorded in the #7007 PR
// evidence — not reproducible from this file, which proves the fix's shape
// and correctness on a disposable local corpus instead).
//
// It is gated behind ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN
// because it needs a real Postgres instance; see the scan-tier and
// entity-name live tests in this package for the same pattern.
func TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	targetRepoID, targetCount, noiseCount := seedPackageManifestRepoScopeCorpus(t, ctx, db)

	args := readinessArgsForRepository(targetRepoID)

	var raw []byte
	if err := db.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+ListReadinessQuery,
		args...,
	).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN production readiness query: %v", err)
	}
	var plan []struct {
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
		t.Fatalf("decode readiness plan: count=%d err=%v raw=%s", len(plan), err, raw)
	}
	t.Logf("package-manifest repo-scope readiness plan: execution_ms=%.3f noise_repos=%d target_repo_manifest_facts=%d",
		plan[0].ExecutionTime, noiseCount, targetCount)
	if plan[0].ExecutionTime > 500 {
		t.Fatalf("execution time = %.3fms, want <=500ms (local-performance-envelope budget)", plan[0].ExecutionTime)
	}
	// The #7007 fix's whole point is that package_manifest_active resolves
	// through the new repo_id-leading partial index instead of a per-scope
	// scan-and-filter over every noise repository's dependency-variable
	// facts. A regression that drops the repo_id predicate or the supporting
	// migration still passes the row-count assertions below (the downstream
	// filters already exist) but silently reverts to scanning every noise
	// repository, which this plan-shape assertion catches directly.
	if !strings.Contains(string(raw), "fact_records_content_entity_dependency_variable_repo_idx") {
		t.Fatalf("expected package_manifest_active to plan through fact_records_content_entity_dependency_variable_repo_idx, plan=%s", raw)
	}

	// #7007: package_dependency_gap_active has no repo_id-leading index (the
	// gap kinds are rare, so none was added), so it must be bounded to the
	// requested repository's own scope through ingestion_scopes.source_key,
	// which the git collector mirrors to the repository id
	// (TestBuildScopeRepositorySourceKeyMatchesMetadataRepoID). Unbounded it
	// probes fact_records once per active scope (~810 on ops-qa, 8s of the
	// 8.1s readiness read). The local corpus is too small for the planner to
	// pick the per-scope nested loop, so the proof is the plan-shape anchor,
	// not a loop count: a regression that drops the predicate removes every
	// source_key reference from the plan.
	if !strings.Contains(string(raw), "source_key = '"+targetRepoID+"'") {
		t.Fatalf("expected the dependency-gap read to anchor ingestion_scopes.source_key to %q, plan=%s", targetRepoID, raw)
	}

	// Correctness: the readiness snapshot's package.consumption family must
	// count only the target repository's manifest-dependency facts, never
	// leaking noise from the other seeded repositories.
	rows, err := db.QueryContext(ctx, ListReadinessQuery, args...)
	if err != nil {
		t.Fatalf("query production shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	families := map[string]int{}
	var unsupportedTargetsJSON sql.NullString
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reasons array.StringArray
		var a, b, c sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &a, &b, &c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		families[family] = factCount
		if family == unsupportedTargetFamilyMarker {
			unsupportedTargetsJSON = c
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if got := families["package.consumption"]; got != targetCount {
		t.Fatalf("families[package.consumption] = %d, want %d (target repository's manifest-dependency facts only, no noise leakage)", got, targetCount)
	}
	// #7007: the dependency-gap read (package_dependency_gap_active) must
	// report only the target repository's provenance-only dependency rows.
	targets, err := decodeUnsupportedTargets(unsupportedTargetsJSON)
	if err != nil {
		t.Fatalf("decode unsupported targets: %v", err)
	}
	gapReasons := map[string]int{}
	for _, target := range targets {
		if target.TargetKind == "dependency_source" {
			gapReasons[target.Reason] += target.Count
		}
	}
	wantGaps := map[string]int{"vcs_dependency_unsupported": 1, "path_dependency_unsupported": 1}
	if !reflect.DeepEqual(gapReasons, wantGaps) {
		t.Fatalf("dependency_source unsupported targets = %v, want %v (target repository only, no noise leakage)", gapReasons, wantGaps)
	}
}

// seedPackageManifestRepoScopeCorpus seeds noiseRepos repositories, each
// carrying a handful of dependency-variable content_entity facts under a
// DISTINCT repo_id, plus one target repository with a known fact count, and
// returns the target repository id, its manifest-dependency fact count, and
// the number of noise repositories seeded.
func seedPackageManifestRepoScopeCorpus(t *testing.T, ctx context.Context, db *sql.DB) (targetRepoID string, targetCount int, noiseRepos int) {
	t.Helper()
	const (
		seededNoiseRepos          = 600
		factsPerNoiseRepo         = 5
		targetManifestFactCount   = 4
		targetRepositoryIDLiteral = "repository:repo-manifest-scope-target"
	)

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'manifest-noise-scope-' || n, 'repository', 'git', 'repository:repo-manifest-scope-noise-' || n, 'git', 'repository:repo-manifest-scope-noise-' || n, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, `+strconv.Itoa(seededNoiseRepos)+`) AS n;
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'manifest-noise-scope-' || n, 'manifest-noise-gen-' || n, 'sync', clock_timestamp(), clock_timestamp(), 'active'
FROM generate_series(1, `+strconv.Itoa(seededNoiseRepos)+`) AS n;
UPDATE ingestion_scopes SET active_generation_id = 'manifest-noise-gen-' || substring(scope_id from 21)
WHERE scope_id LIKE 'manifest-noise-scope-%';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'manifest-noise-var-' || n || '-' || p,
       'manifest-noise-scope-' || n, 'manifest-noise-gen-' || n,
       'content_entity', 'manifest-noise-var-key-' || n || '-' || p, 'git', 'manifest-noise-var-' || n || '-' || p,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repo_id', 'repository:repo-manifest-scope-noise-' || n,
         'entity_type', 'Variable',
         'entity_name', 'DEP_' || p,
         'entity_metadata', jsonb_build_object('config_kind', 'dependency')
       )
FROM generate_series(1, `+strconv.Itoa(seededNoiseRepos)+`) AS n, generate_series(1, `+strconv.Itoa(factsPerNoiseRepo)+`) AS p;
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'manifest-noise-gap-' || n, 'manifest-noise-scope-' || n, 'manifest-noise-gen-' || n,
       'content_entity', 'manifest-noise-gap-key-' || n, 'git', 'manifest-noise-gap-' || n,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repo_id', 'repository:repo-manifest-scope-noise-' || n,
         'entity_type', 'Variable',
         'entity_name', 'GAP_NOISE',
         'entity_metadata', jsonb_build_object('config_kind', 'vcs_dependency', 'package_manager', 'npm')
       )
FROM generate_series(1, `+strconv.Itoa(seededNoiseRepos)+`) AS n;
`); err != nil {
		t.Fatalf("seed noise manifest repositories: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ('manifest-target-scope', 'repository', 'git', '`+targetRepositoryIDLiteral+`', 'git', '`+targetRepositoryIDLiteral+`', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb);
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('manifest-target-scope', 'manifest-target-gen', 'sync', clock_timestamp(), clock_timestamp(), 'active');
UPDATE ingestion_scopes SET active_generation_id = 'manifest-target-gen' WHERE scope_id = 'manifest-target-scope';
`); err != nil {
		t.Fatalf("seed target manifest scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'manifest-target-var-' || p, 'manifest-target-scope', 'manifest-target-gen',
       'content_entity', 'manifest-target-var-key-' || p, 'git', 'manifest-target-var-' || p,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repo_id', $1::text,
         'entity_type', 'Variable',
         'entity_name', 'DEP_TARGET_' || p,
         'entity_metadata', jsonb_build_object('config_kind', 'dependency')
       )
FROM generate_series(1, `+strconv.Itoa(targetManifestFactCount)+`) AS p;
`, targetRepositoryIDLiteral); err != nil {
		t.Fatalf("seed target manifest facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'manifest-target-gap-' || kind, 'manifest-target-scope', 'manifest-target-gen',
       'content_entity', 'manifest-target-gap-key-' || kind, 'git', 'manifest-target-gap-' || kind,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repo_id', $1::text,
         'entity_type', 'Variable',
         'entity_name', 'GAP_TARGET_' || kind,
         'entity_metadata', jsonb_build_object('config_kind', kind, 'package_manager', 'npm')
       )
FROM unnest(ARRAY['vcs_dependency', 'path_dependency']) AS kind;
`, targetRepositoryIDLiteral); err != nil {
		t.Fatalf("seed target dependency-gap facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;
`); err != nil {
		t.Fatalf("analyze seeded corpus: %v", err)
	}

	return targetRepositoryIDLiteral, targetManifestFactCount, seededNoiseRepos
}

// TestSupplyChainImpactReadinessRepoArmScopeLive is the #7088 proof for a
// repository-only anchor ($11 = repository id, $20 = false). Before #7088
// package_manifest_active's legacy-shape arm (top-level config_kind) had no
// repository-leading index and carried a "$11 = ” OR" escape, so it probed
// fact_records once per active scope (819 loops, 9.4 s warm / >30 s cold on
// ops-qa); arm 1 heap-fetched every generation's rows before the
// active-generation join; and package_dependency_gap_active scanned the whole
// active scope. The proof asserts the plan shape from EXPLAIN (ANALYZE,
// BUFFERS, FORMAT JSON) under both a custom and a forced generic plan, and
// that the rewritten statement returns exactly the rows the previous one
// did for the target repository (repository plus repository_ref scope), a
// repository that owns only a repository_ref scope, and an unknown one.
func TestSupplyChainImpactReadinessRepoArmScopeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_MANIFEST_REPO_SCOPE_EXPLAIN_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedRepoArmScopeCorpus(t, ctx, db)

	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+ListReadinessQuery,
		readinessArgsForRepository(repoArmTargetRepoID)...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN readiness (custom plan): %v", err)
	}
	assertRepoArmPlanBounded(t, "custom plan", raw)

	previous := previousRepoArmReadinessQuery(t)
	current := map[string][]string{}
	for _, repositoryID := range []string{repoArmTargetRepoID, repoArmRefOnlyRepoID, repoArmUnknownRepoID} {
		args := readinessArgsForRepository(repositoryID)
		got := readinessResultRows(t, ctx, db, ListReadinessQuery, args...)
		want := readinessResultRows(t, ctx, db, previous, args...)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: rewritten readiness rows differ from the previous statement\n got=%v\nwant=%v", repositoryID, got, want)
		}
		current[repositoryID] = got
		assertRepoArmCounts(t, ctx, db, repositoryID)
	}

	// Generic-plan safety: the rewrite must not depend on the planner
	// folding $11, which a cached generic plan cannot do.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn(): %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PREPARE repo_arm_readiness("+
		"text[], text[], text[], text[], text[], text[], text[], text[], "+
		"text, text, text, text, text, text, text[], text[], text[], text[], text[], boolean) AS "+ListReadinessQuery); err != nil {
		t.Fatalf("PREPARE readiness: %v", err)
	}
	for i := 0; i < 7; i++ {
		for _, repositoryID := range []string{repoArmTargetRepoID, repoArmRefOnlyRepoID, repoArmUnknownRepoID} {
			execute := repoArmExecuteStatement(t, "repo_arm_readiness", readinessArgsForRepository(repositoryID))
			got := readinessResultRows(t, ctx, conn, execute)
			if !reflect.DeepEqual(got, current[repositoryID]) {
				t.Fatalf("generic EXECUTE %d for %s differs from the custom plan\n got=%v\nwant=%v", i+1, repositoryID, got, current[repositoryID])
			}
		}
	}
	raw = nil
	if err := conn.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+
		repoArmExecuteStatement(t, "repo_arm_readiness", readinessArgsForRepository(repoArmTargetRepoID))).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN readiness (generic plan): %v", err)
	}
	if !strings.Contains(string(raw), "$11") {
		t.Fatalf("expected a generic plan (parameter $11 left unbound in the plan), plan=%s", raw)
	}
	assertRepoArmPlanBounded(t, "generic plan", raw)
}

// assertRepoArmCounts pins the absolute counts, so the differential above
// cannot pass on two equally wrong statements.
func assertRepoArmCounts(t *testing.T, ctx context.Context, db *sql.DB, repositoryID string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, ListReadinessQuery, readinessArgsForRepository(repositoryID)...)
	if err != nil {
		t.Fatalf("query readiness: %v", err)
	}
	defer func() { _ = rows.Close() }()
	manifest := 0
	gaps := 0
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incompleteFlag sql.NullBool
		var reasons array.StringArray
		var a, b, c sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incompleteFlag, &reasons, &a, &b, &c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		switch family {
		case "package.consumption":
			manifest = factCount
		case unsupportedTargetFamilyMarker:
			targets, err := decodeUnsupportedTargets(c)
			if err != nil {
				t.Fatalf("decode unsupported targets: %v", err)
			}
			for _, target := range targets {
				if target.TargetKind == "dependency_source" {
					gaps += target.Count
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if want := repoArmExpectedManifestFacts[repositoryID]; manifest != want {
		t.Errorf("%s: package.consumption = %d, want %d (active generation, both payload shapes, repository and repository_ref scopes, no tombstones)", repositoryID, manifest, want)
	}
	if want := repoArmExpectedGapFacts[repositoryID]; gaps != want {
		t.Errorf("%s: dependency_source unsupported targets = %d, want %d", repositoryID, gaps, want)
	}
}

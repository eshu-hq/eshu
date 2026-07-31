// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

// Regression coverage for the P0 review finding on issue #5594's local-backend
// default-path fix: listTerraformBackendCanonicalRowsQuery's repository LEFT
// JOIN must key on (scope_id, generation_id, repo_id) with exactly one row
// selected per key (latest observed_at wins), not an unfiltered,
// unordered LEFT JOIN. Without the repo_id predicate, a scope/generation
// carrying more than one distinct repo_id -- the exact shape
// backend_generations' own DISTINCT exists to handle -- can attach a
// DIFFERENT repo's checkout root onto this one's backend rows. Without
// DISTINCT ON + ORDER BY, more than one repository fact per
// (scope_id, generation_id, repo_id) -- a real shape after re-collection or
// retries -- fans the outer JOIN out, duplicating backend rows via
// mergeTerraformBackendFactContext's append semantics (corrupting even the
// pre-existing s3 candidate path, not just the new local one).
//
// This test seeds both hazards in one fact_records corpus and DSN, following
// the same ESHU_POSTGRES_DSN-gated, prefix-scoped-cleanup convention as
// TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath in
// tfstate_drift_jsonb_null_integration_test.go.

const repoLocalPathDSNEnv = "ESHU_POSTGRES_DSN"

func openRepoLocalPathDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(repoLocalPathDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping repo_local_path join regression test", repoLocalPathDSNEnv)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("PingContext() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// repoLocalPathScopePrefix is the unique scope/generation/fact-id prefix this
// test writes under; teardown deletes by prefix only, same safety contract as
// driftJSONBNullScopePrefix.
const repoLocalPathScopePrefix = "repo-local-path-repro"

// repoLocalPathSeedScope installs one active scope/generation under the
// prefix and returns its IDs plus a cleanup-registered teardown.
func repoLocalPathSeedScope(t *testing.T, db *sql.DB, observedAt time.Time) (string, string) {
	t.Helper()
	ctx := context.Background()

	scopeID := "repo_snapshot:" + repoLocalPathScopePrefix
	generationID := "gen:" + repoLocalPathScopePrefix

	if _, err := db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("delete prior scope error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repo_snapshot', 'git', $4, 'git', $4,
        $2, $2, 'active', $3)
ON CONFLICT (scope_id) DO NOTHING
`, scopeID, observedAt, generationID, repoLocalPathScopePrefix); err != nil {
		t.Fatalf("insert ingestion_scopes error = %v, want nil", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'commit', $3, $3, 'active', $3)
ON CONFLICT (generation_id) DO NOTHING
`, generationID, scopeID, observedAt); err != nil {
		t.Fatalf("insert scope_generations error = %v, want nil", err)
	}

	return scopeID, generationID
}

func repoLocalPathInsertFact(
	t *testing.T,
	db *sql.DB,
	factID, scopeID, generationID, factKind string,
	observedAt time.Time,
	payloadJSON string,
) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, $5, $6::jsonb)
ON CONFLICT (fact_id) DO NOTHING
`, factID, scopeID, generationID, factKind, observedAt, payloadJSON); err != nil {
		t.Fatalf("insert fact_records (%s) error = %v, want nil", factID, err)
	}
}

// TestPostgresTerraformBackendQueryRepoLocalPathJoinIsRepoScopedAndLatest
// seeds one scope/generation carrying:
//   - two distinct repos (repo-alpha, repo-beta), each with its own bare
//     `backend "local" {}` file fact, sharing the (scope_id, generation_id)
//     backend_generations groups by -- the exact shape that would let an
//     unfiltered repository JOIN cross-contaminate local_path across repos;
//   - two repository facts for repo-alpha with different local_path and
//     observed_at, so a naive unordered LEFT JOIN would either duplicate
//     repo-alpha's backend row or pick an arbitrary (non-latest) local_path.
//
// It asserts: exactly one row per repo, repo-alpha's row carries repo-alpha's
// LATEST local_path (never repo-beta's, never the stale one), and
// repo-beta's row carries repo-beta's own local_path.
func TestPostgresTerraformBackendQueryRepoLocalPathJoinIsRepoScopedAndLatest(t *testing.T) {
	db := openRepoLocalPathDB(t)
	base := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	scopeID, generationID := repoLocalPathSeedScope(t, db, base)

	// repo-alpha: bare local backend at env/prod/main.tf.
	alphaBackendFile := "/repos/alpha/env/prod/main.tf"
	repoLocalPathInsertFact(t, db, "fact:repo-alpha-file", scopeID, generationID, "file", base, `{
        "repo_id":"repo-alpha",
        "relative_path":"env/prod/main.tf",
        "parsed_file_data":{"terraform_backends":[{
            "backend_kind":"local",
            "path":"`+alphaBackendFile+`"
        }]}
    }`)

	// repo-beta: bare local backend at env/prod/main.tf too (same relative
	// layout, different repo -- this is the shape that would collide if the
	// repository JOIN ever cross-contaminated local_path across repos).
	betaBackendFile := "/repos/beta/env/prod/main.tf"
	repoLocalPathInsertFact(t, db, "fact:repo-beta-file", scopeID, generationID, "file", base, `{
        "repo_id":"repo-beta",
        "relative_path":"env/prod/main.tf",
        "parsed_file_data":{"terraform_backends":[{
            "backend_kind":"local",
            "path":"`+betaBackendFile+`"
        }]}
    }`)

	// repo-alpha: two repository facts, an older one with the WRONG
	// local_path and a newer one with the correct, expected local_path. The
	// query must pick the latest by observed_at.
	repoLocalPathInsertFact(t, db, "fact:repo-alpha-repository-stale", scopeID, generationID, "repository",
		base.Add(-time.Hour), `{"repo_id":"repo-alpha","local_path":"/repos/alpha-STALE"}`)
	repoLocalPathInsertFact(t, db, "fact:repo-alpha-repository-latest", scopeID, generationID, "repository",
		base, `{"repo_id":"repo-alpha","local_path":"/repos/alpha"}`)

	// repo-beta: exactly one repository fact.
	repoLocalPathInsertFact(t, db, "fact:repo-beta-repository", scopeID, generationID, "repository",
		base, `{"repo_id":"repo-beta","local_path":"/repos/beta"}`)

	query := PostgresTerraformBackendQuery{DB: SQLDB{DB: db}}

	alphaLocator := "/repos/alpha/env/prod/terraform.tfstate"
	alphaHash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, alphaLocator)
	alphaRows, err := query.ListTerraformBackendsByLocator(context.Background(), "local", alphaHash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator(alpha) error = %v, want nil", err)
	}
	if got, want := len(alphaRows), 1; got != want {
		t.Fatalf("len(alphaRows) = %d, want %d (repo_id predicate + DISTINCT ON must yield exactly one "+
			"row, not a fan-out duplicate); got=%#v", got, want, alphaRows)
	}
	if got, want := alphaRows[0].RepoID, "repo-alpha"; got != want {
		t.Fatalf("alphaRows[0].RepoID = %q, want %q (must not cross-contaminate with repo-beta)", got, want)
	}

	betaLocator := "/repos/beta/env/prod/terraform.tfstate"
	betaHash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, betaLocator)
	betaRows, err := query.ListTerraformBackendsByLocator(context.Background(), "local", betaHash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator(beta) error = %v, want nil", err)
	}
	if got, want := len(betaRows), 1; got != want {
		t.Fatalf("len(betaRows) = %d, want %d; got=%#v", got, want, betaRows)
	}
	if got, want := betaRows[0].RepoID, "repo-beta"; got != want {
		t.Fatalf("betaRows[0].RepoID = %q, want %q", got, want)
	}

	// The negative control: repo-alpha's STALE local_path must never produce
	// a match. If the join picked the stale row (or fanned out to both), this
	// hash would also resolve, which the assertions above already rule out --
	// but assert it directly too so a future regression that duplicates rows
	// without breaking the two assertions above still gets caught.
	staleLocator := "/repos/alpha-STALE/env/prod/terraform.tfstate"
	staleHash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, staleLocator)
	staleRows, err := query.ListTerraformBackendsByLocator(context.Background(), "local", staleHash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator(stale) error = %v, want nil", err)
	}
	if len(staleRows) != 0 {
		t.Fatalf("staleRows = %#v, want none (the older repository fact's local_path must not win)", staleRows)
	}
}

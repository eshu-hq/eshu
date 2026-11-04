// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8875 row-differential proof for the canonical terraform-backends reader on disposable PostgreSQL: the derived COUNT probe built from the shipped constant stays exact across the MATERIALIZED repo_local_paths change and the predicate-matched partial index, the adapter returns both owners of a shared locator hash, and the plan seeks the new index
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Lane-1 (issue #8875) row differential for the canonical terraform-backends
// reader. Live read-only EXPLAIN (ANALYZE, BUFFERS) against the
// production-scale primary (99M-row
// fact_records, 2,254 active generations) measured the shipped
// listTerraformBackendCanonicalRowsQuery at 15.3 s warm / ~3.0M shared-buffer
// hits returning 9,188 rows the adapter then filters in Go to ~1 matching
// repo (scanned:matched ≈ 200,000:1), with two removable defects:
//
//  1. The repo_local_paths CTE is inlined and re-executed once per outer row
//     (9,188 loops) with 8.6M rows removed by the join filter (~1.8 s).
//     AS MATERIALIZED evaluates it once; the result set is unchanged.
//  2. The backend_generations CTE probes every active generation's file facts
//     and evaluates the JSONB array-length CASE per heap row (1.1M buffers,
//     5.9 s) with no usable index. The predicate-matched partial index
//     fact_records_terraform_backends_active_idx serves that arm.
//
// This test pins the row-set contract across both changes: the derived
// COUNT(*) (built from the shipped constant, never hand-copied) must stay
// exact, the adapter must keep returning every matching owner (ambiguity
// contract), and null/scalar terraform_backends rows must keep surviving.

const tf8875ScopePrefix = "tf8875"

func tf8875Seed(t *testing.T, db *sql.DB) time.Time {
	t.Helper()
	ctx := context.Background()
	observedAt := time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC)

	for _, scopeSuffix := range []string{"a", "b"} {
		scopeID := "repo_snapshot:" + tf8875ScopePrefix + "-" + scopeSuffix
		if _, err := db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID); err != nil {
			t.Fatalf("delete prior scope error = %v, want nil", err)
		}
	}
	t.Cleanup(func() {
		for _, scopeSuffix := range []string{"a", "b"} {
			_, _ = db.ExecContext(context.Background(),
				`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
				"repo_snapshot:"+tf8875ScopePrefix+"-"+scopeSuffix)
		}
	})

	for _, scopeSuffix := range []string{"a", "b"} {
		scopeID := "repo_snapshot:" + tf8875ScopePrefix + "-" + scopeSuffix
		generationID := "gen:" + tf8875ScopePrefix + "-" + scopeSuffix
		repoID := tf8875ScopePrefix + "-" + scopeSuffix
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repo_snapshot', 'git', $4, 'git', $4, $2, $2, 'active', $3)
ON CONFLICT (scope_id) DO NOTHING
`, scopeID, observedAt, generationID, repoID); err != nil {
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
	}
	return observedAt
}

func tf8875InsertFact(t *testing.T, db *sql.DB, factID, scopeSuffix, payloadJSON string) {
	t.Helper()
	scopeID := "repo_snapshot:" + tf8875ScopePrefix + "-" + scopeSuffix
	generationID := "gen:" + tf8875ScopePrefix + "-" + scopeSuffix
	observedAt := time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC)
	kind := "file"
	if strings.Contains(factID, ":repo:") {
		kind = "repository"
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, $5, $6::jsonb)
ON CONFLICT (fact_id) DO NOTHING
`, factID, scopeID, generationID, kind, observedAt, payloadJSON); err != nil {
		t.Fatalf("insert fact_records (%s) error = %v, want nil", factID, err)
	}
}

// tf8875DerivedCountQuery derives a read-only row-count probe from the
// shipped canonical constant. The probe MUST stay a byte-exact derivation:
// the guard below fails if the constant drifts, so the probe can never
// silently diverge from production. The outer filter pins the probe to this
// test's own scopes: the shipped query reads the whole table, so unrelated
// pre-existing rows in a shared database must not move the pinned count.
func tf8875DerivedCountQuery() string {
	return `SELECT count(*) AS n FROM (` + listTerraformBackendCanonicalRowsQuery +
		`) AS canonical_rows WHERE scope_id LIKE 'repo_snapshot:tf8875-%'`
}

func TestTF8875CanonicalQueryDerivesFromShippedConstant(t *testing.T) {
	t.Parallel()

	const stableHead = "\nWITH backend_generations AS ("
	if !strings.HasPrefix(listTerraformBackendCanonicalRowsQuery, stableHead) {
		t.Fatalf("canonical query head drifted; re-derive the differential probe, do not hand-copy it")
	}
	probe := tf8875DerivedCountQuery()
	inner := strings.TrimPrefix(strings.TrimSuffix(probe,
		`) AS canonical_rows WHERE scope_id LIKE 'repo_snapshot:tf8875-%'`), `SELECT count(*) AS n FROM (`)
	if inner != listTerraformBackendCanonicalRowsQuery {
		t.Fatalf("differential probe is not a byte-exact derivation of the shipped constant")
	}
}

// TestTF8875CanonicalRepoLocalPathsMaterialized pins the query-shape half of
// the lane-1 fix: repo_local_paths MUST be fenced AS MATERIALIZED. Live
// evidence (production EXPLAIN ANALYZE): the inlined CTE re-executed 9,188
// times with 8,657,033 rows removed by the outer join filter. Fails until
// the marker lands; the row differential below proves the marker changes
// no row.
func TestTF8875CanonicalRepoLocalPathsMaterialized(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listTerraformBackendCanonicalRowsQuery, "repo_local_paths AS MATERIALIZED") &&
		!strings.Contains(listTerraformBackendCanonicalRowsQuery, "repo_local_paths AS MATERIALIZED (") {
		t.Fatalf("repo_local_paths is not AS MATERIALIZED; per-row re-execution (9,188 loops live) is the defect")
	}
}

func TestTF8875CanonicalBackendsIndexExists(t *testing.T) {
	t.Parallel()

	db := openDriftJSONBNullDB(t)
	var name string
	err := db.QueryRow(
		`SELECT indexname FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'fact_records' AND indexname = 'fact_records_terraform_backends_active_idx'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("partial backends index missing: %v (add migration + Go DDL entry)", err)
	}
}

// TestTF8875CanonicalRowDifferentialLive is the row-set contract across the
// lane-1 change: same 404 output rows before and after (4 shaped rows + 400
// variables-only decoys), same two ambiguous owners from the adapter, null
// and scalar terraform_backends rows surviving, latest-wins repository
// local_path not fanning rows out.
// tf8875SeedBackendsFixture seeds the full tf8875 shape (scopes,
// generations, shaped rows, decoy bulks) and ANALYZEs fact_records so the
// planner faces a real selectivity choice. Shared by the row differential
// and the outer-leg plan assertion so both prove against the same shape.
func tf8875SeedBackendsFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	tf8875Seed(t, db)

	bucket, key := "tf8875-bucket", "envs/prod.tfstate"
	backendJSON := string(fixtureBackendFact("s3", bucket, key, "us-east-1"))

	// Scope A: one backends row carrying variables+locals (merge shape), one
	// variables-only row, one locals-only row, hostile non-array rows, and a
	// duplicated older repository fact (latest-wins must not fan out).
	tf8875InsertFact(t, db, "fact:tf8875-a:repo:old", "a", `{
        "repo_id":"tf8875-a", "local_path":"/repos/old",
        "relative_path":"repo.json"}`)
	tf8875InsertFact(t, db, "fact:tf8875-a:repo:new", "a", `{
        "repo_id":"tf8875-a", "local_path":"/repos/new",
        "relative_path":"repo.json"}`)
	tf8875InsertFact(t, db, "fact:tf8875-a:main", "a", fmt.Sprintf(`{
        "repo_id":"tf8875-a", "relative_path":"main.tf",
        "parsed_file_data":{"terraform_backends":%s,
            "terraform_variables":[{"name":"env"}],
            "terraform_locals":[{"name":"prefix"}]}}`, backendJSON))
	tf8875InsertFact(t, db, "fact:tf8875-a:vars", "a", `{
        "repo_id":"tf8875-a", "relative_path":"vars.tf",
        "parsed_file_data":{"terraform_backends":[],
            "terraform_variables":[{"name":"solo"}],
            "terraform_locals":[]}}`)
	tf8875InsertFact(t, db, "fact:tf8875-a:locals", "a", `{
        "repo_id":"tf8875-a", "relative_path":"locals.tf",
        "parsed_file_data":{"terraform_backends":[],
            "terraform_variables":[],
            "terraform_locals":[{"name":"solo"}]}}`)
	tf8875InsertFact(t, db, "fact:tf8875-a:null", "a", `{
        "repo_id":"tf8875-a", "relative_path":"empty.tf",
        "parsed_file_data":{"terraform_backends":null}}`)
	tf8875InsertFact(t, db, "fact:tf8875-a:scalar", "a", `{
        "repo_id":"tf8875-a", "relative_path":"odd.tf",
        "parsed_file_data":{"terraform_backends":"s3"}}`)
	tf8875InsertFact(t, db, "fact:tf8875-a:bare", "a", `{
        "repo_id":"tf8875-a", "relative_path":"bare.tf",
        "parsed_file_data":{}}`)

	// Scope B: a second owner of the identical (backend_kind, locator_hash)
	// composite key. The adapter must return both rows (ambiguity contract).
	tf8875InsertFact(t, db, "fact:tf8875-b:repo", "b", `{
        "repo_id":"tf8875-b", "local_path":"/repos/b",
        "relative_path":"repo.json"}`)
	tf8875InsertFact(t, db, "fact:tf8875-b:main", "b", fmt.Sprintf(`{
        "repo_id":"tf8875-b", "relative_path":"main.tf",
        "parsed_file_data":{"terraform_backends":%s,
            "terraform_variables":[], "terraform_locals":[]}}`, backendJSON))

	// Decoy bulk so the fixture planner faces a real selectivity choice:
	// 3,000 array-less files plus 400 variables-only files in scope A.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, payload)
SELECT 'fact:tf8875-a:decoy:' || g,
       'repo_snapshot:tf8875-a', 'gen:tf8875-a', 'file',
       'fact:tf8875-a:decoy:' || g, 'git',
       'fact:tf8875-a:decoy:' || g,
       TIMESTAMPTZ '2026-10-30 12:00:00+00', TIMESTAMPTZ '2026-10-30 12:00:00+00',
       '{"repo_id":"tf8875-a","relative_path":"decoy.tf","parsed_file_data":{"terraform_backends":[]}}'::jsonb
FROM generate_series(1, 3000) AS g
ON CONFLICT (fact_id) DO NOTHING
`); err != nil {
		t.Fatalf("insert decoy facts error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, payload)
SELECT 'fact:tf8875-a:vardecoy:' || g,
       'repo_snapshot:tf8875-a', 'gen:tf8875-a', 'file',
       'fact:tf8875-a:vardecoy:' || g, 'git',
       'fact:tf8875-a:vardecoy:' || g,
       TIMESTAMPTZ '2026-10-30 12:00:00+00', TIMESTAMPTZ '2026-10-30 12:00:00+00',
       '{"repo_id":"tf8875-a","relative_path":"vardecoy.tf","parsed_file_data":{"terraform_backends":[],"terraform_variables":[{"name":"bulk"}]}}'::jsonb
FROM generate_series(1, 400) AS g
ON CONFLICT (fact_id) DO NOTHING
`); err != nil {
		t.Fatalf("insert vars decoy facts error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze error = %v, want nil", err)
	}
}

func TestTF8875CanonicalRowDifferentialLive(t *testing.T) {
	db := openDriftJSONBNullDB(t)
	ctx := context.Background()

	bucket, key := "tf8875-bucket", "envs/prod.tfstate"
	hash := expectedLocatorHash(bucket, key)
	tf8875SeedBackendsFixture(t, db)

	// Row differential: 3 shaped rows (main/vars/locals) + 1 scope-B main +
	// 400 variables-only decoys. The duplicated older repository fact must
	// not fan rows out (latest-wins); null/scalar/bare/decoy rows stay out.
	var count int
	if err := db.QueryRowContext(ctx, tf8875DerivedCountQuery()).Scan(&count); err != nil {
		t.Fatalf("derived count query error = %v, want nil", err)
	}
	if got, want := count, 404; got != want {
		t.Fatalf("derived row count = %d, want %d", got, want)
	}

	query := PostgresTerraformBackendQuery{DB: tfstateSQLDB{db: db}}
	rows, err := query.ListTerraformBackendsByLocator(ctx, "s3", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d (both owners must surface)", got, want)
	}
	observedAt := time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC)
	for i, wantRepo := range []string{"tf8875-a", "tf8875-b"} {
		got := rows[i]
		if got.RepoID != wantRepo {
			t.Fatalf("rows[%d].RepoID = %q, want %q (query ORDER BY contract)", i, got.RepoID, wantRepo)
		}
		if got.BackendKind != "s3" || got.LocatorHash != hash {
			t.Fatalf("rows[%d] = %#v, want kind s3 with the requested hash", i, got)
		}
		if !got.CommitObservedAt.Equal(observedAt) {
			t.Fatalf("rows[%d].CommitObservedAt = %v, want %v", i, got.CommitObservedAt, observedAt)
		}
		if got.LocatorDefaulted {
			t.Fatalf("rows[%d].LocatorDefaulted = true, want false (literal s3 block)", i)
		}
	}

	empty, err := query.ListTerraformBackendsByLocator(ctx, "s3", strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("non-matching lookup error = %v, want nil", err)
	}
	if len(empty) != 0 {
		t.Fatalf("len(empty) = %d, want 0", len(empty))
	}

	// Plan proof: the backend_generations arm must seek the partial index,
	// never walk every active generation's file heap.
	planRows, err := db.QueryContext(ctx, `EXPLAIN `+listTerraformBackendCanonicalRowsQuery)
	if err != nil {
		t.Fatalf("EXPLAIN error = %v, want nil", err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan plan error = %v, want nil", err)
		}
		plan.WriteString(line + "\n")
	}
	_ = planRows.Close()
	if err := planRows.Err(); err != nil {
		t.Fatalf("plan rows error = %v, want nil", err)
	}
	if !strings.Contains(plan.String(), "fact_records_terraform_backends_active_idx") {
		t.Fatalf("plan does not use the partial backends index:\n%s", plan.String())
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const runtimeEnvironmentEvidenceArtifactIndex = "fact_records_ci_cd_run_correlations_artifact_lookup_idx"

// TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive proves the
// production SQL stays on the artifact lookup index, with one index probe per
// candidate digest, when one visible digest has many current deployment facts.
//
// It pins that cost shape at the seeded active scope/generation pair count (the
// test requires at least two, because ApplyBootstrap seeds the eshu:global pair)
// and with every table the query joins analyzed. It does not check how the plan
// scales as the active pair count grows: a scope-first plan multiplies the probe
// count by the pair count, and that exposure is tracked in #7552. It is opt-in
// because it writes about one million disposable rows into a production-schema
// PostgreSQL database in a database created and dropped by the helper.
func TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DSN"),
		os.Getenv("ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply production Postgres schema: %v", err)
	}
	seedRuntimeEnvironmentEvidenceHotDigest(t, ctx, db)
	requireRuntimeEnvironmentEvidenceActivePairs(t, ctx, db, 2)

	candidates, digests, environments := runtimeEnvironmentEvidenceLiveCandidates()
	assertRuntimeEnvironmentEvidenceIndexPlan(
		t,
		ctx,
		db,
		selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery,
		digests,
		environments,
	)

	started := time.Now()
	got, err := (PostgresFindingStore{DB: db}).
		ListSupplyChainImpactRuntimeEnvironmentEvidence(ctx, candidates, nil, nil)
	if err != nil {
		t.Fatalf("ListSupplyChainImpactRuntimeEnvironmentEvidence(): %v", err)
	}
	elapsed := time.Since(started)
	if len(got) != len(candidates) {
		t.Fatalf("confirmed digests = %d, want %d", len(got), len(candidates))
	}
	if got[digests[0]]["prod"] != RuntimeEnvironmentEvidenceDeployEvent {
		t.Fatalf("hot digest evidence = %#v, want deploy_event", got[digests[0]])
	}
	// Wall time is reported, not gated: a clock bound on a shared runner has no
	// derived spread (docs/internal/timing-proof-rules.md rule 2), and the plan
	// assertion above pins the cost shape.
	t.Logf("RUNTIME_ENVIRONMENT_EVIDENCE candidates=%d hot_rows=100000 total_rows=1000199 elapsed=%s", len(candidates), elapsed)
}

// TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive proves the
// production store admits only current, authorized, accepted facts before it
// folds deploy_event over declared.
func TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DSN"),
		os.Getenv("ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply production Postgres schema: %v", err)
	}
	seedRuntimeEnvironmentEvidenceTruthMatrix(t, ctx, db)

	digests := map[string]string{
		"scope-authorized": fmt.Sprintf("sha256:%064x", 1001),
		"repo-authorized":  fmt.Sprintf("sha256:%064x", 1002),
		"denied":           fmt.Sprintf("sha256:%064x", 1003),
		"stale":            fmt.Sprintf("sha256:%064x", 1004),
		"tombstone":        fmt.Sprintf("sha256:%064x", 1005),
		"rejected":         fmt.Sprintf("sha256:%064x", 1006),
		"provenance":       fmt.Sprintf("sha256:%064x", 1007),
	}
	candidates := make([]RuntimeEnvironmentCandidate, 0, len(digests))
	for _, name := range []string{"scope-authorized", "repo-authorized", "denied", "stale", "tombstone", "rejected", "provenance"} {
		candidates = append(candidates, RuntimeEnvironmentCandidate{
			SubjectDigest: digests[name],
			Environment:   "prod",
		})
	}
	got, err := (PostgresFindingStore{DB: db}).
		ListSupplyChainImpactRuntimeEnvironmentEvidence(
			ctx,
			candidates,
			[]string{"repository:r_allowed"},
			[]string{"scope:allowed"},
		)
	if err != nil {
		t.Fatalf("ListSupplyChainImpactRuntimeEnvironmentEvidence(): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("confirmed digest count = %d, want 2; got %#v", len(got), got)
	}
	if evidence := got[digests["scope-authorized"]]["prod"]; evidence != RuntimeEnvironmentEvidenceDeployEvent {
		t.Fatalf("scope-authorized evidence = %q, want deploy_event", evidence)
	}
	if evidence := got[digests["repo-authorized"]]["prod"]; evidence != RuntimeEnvironmentEvidenceDeclared {
		t.Fatalf("repo-authorized evidence = %q, want declared", evidence)
	}
	for _, name := range []string{"denied", "stale", "tombstone", "rejected", "provenance"} {
		if _, exists := got[digests[name]]; exists {
			t.Fatalf("%s digest unexpectedly confirmed: %#v", name, got[digests[name]])
		}
	}
}

func seedRuntimeEnvironmentEvidenceTruthMatrix(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
  ('scope:allowed', 'repository', 'proof', 'allowed', 'proof', 'allowed', clock_timestamp(), clock_timestamp(), 'active', 'generation:allowed'),
  ('scope:repo', 'repository', 'proof', 'repo', 'proof', 'repo', clock_timestamp(), clock_timestamp(), 'active', 'generation:repo'),
  ('scope:denied', 'repository', 'proof', 'denied', 'proof', 'denied', clock_timestamp(), clock_timestamp(), 'active', 'generation:denied'),
  ('scope:stale', 'repository', 'proof', 'stale', 'proof', 'stale', clock_timestamp(), clock_timestamp(), 'active', 'generation:stale-current')`,
		`INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES
  ('generation:allowed', 'scope:allowed', 'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()),
  ('generation:repo', 'scope:repo', 'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()),
  ('generation:denied', 'scope:denied', 'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()),
  ('generation:stale-old', 'scope:stale', 'proof', clock_timestamp(), clock_timestamp(), 'superseded', NULL),
  ('generation:stale-current', 'scope:stale', 'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp())`,
		`INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
) VALUES
  ('truth:scope-declared', 'scope:allowed', 'generation:allowed', 'reducer_ci_cd_run_correlation', 'truth:scope-declared', 'proof', 'proof', 'truth:scope-declared', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_denied', 'artifact_digest', 'sha256:' || lpad(to_hex(1001), 64, '0'), 'environment', 'prod', 'environment_evidence', 'declared', 'outcome', 'exact')),
  ('truth:scope-deploy', 'scope:allowed', 'generation:allowed', 'reducer_ci_cd_run_correlation', 'truth:scope-deploy', 'proof', 'proof', 'truth:scope-deploy', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_denied', 'artifact_digest', 'sha256:' || lpad(to_hex(1001), 64, '0'), 'environment', 'prod', 'environment_evidence', 'deploy_event', 'outcome', 'derived')),
  ('truth:repo', 'scope:repo', 'generation:repo', 'reducer_ci_cd_run_correlation', 'truth:repo', 'proof', 'proof', 'truth:repo', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_allowed', 'artifact_digest', 'sha256:' || lpad(to_hex(1002), 64, '0'), 'environment', 'prod', 'environment_evidence', 'declared', 'outcome', 'exact')),
  ('truth:denied', 'scope:denied', 'generation:denied', 'reducer_ci_cd_run_correlation', 'truth:denied', 'proof', 'proof', 'truth:denied', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_denied', 'artifact_digest', 'sha256:' || lpad(to_hex(1003), 64, '0'), 'environment', 'prod', 'environment_evidence', 'deploy_event', 'outcome', 'exact')),
  ('truth:stale', 'scope:stale', 'generation:stale-old', 'reducer_ci_cd_run_correlation', 'truth:stale', 'proof', 'proof', 'truth:stale', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_allowed', 'artifact_digest', 'sha256:' || lpad(to_hex(1004), 64, '0'), 'environment', 'prod', 'environment_evidence', 'deploy_event', 'outcome', 'exact')),
  ('truth:tombstone', 'scope:allowed', 'generation:allowed', 'reducer_ci_cd_run_correlation', 'truth:tombstone', 'proof', 'proof', 'truth:tombstone', clock_timestamp(), clock_timestamp(), TRUE,
   jsonb_build_object('repository_id', 'repository:r_allowed', 'artifact_digest', 'sha256:' || lpad(to_hex(1005), 64, '0'), 'environment', 'prod', 'environment_evidence', 'deploy_event', 'outcome', 'exact')),
  ('truth:rejected', 'scope:allowed', 'generation:allowed', 'reducer_ci_cd_run_correlation', 'truth:rejected', 'proof', 'proof', 'truth:rejected', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_allowed', 'artifact_digest', 'sha256:' || lpad(to_hex(1006), 64, '0'), 'environment', 'prod', 'environment_evidence', 'deploy_event', 'outcome', 'rejected')),
  ('truth:provenance', 'scope:allowed', 'generation:allowed', 'reducer_ci_cd_run_correlation', 'truth:provenance', 'proof', 'proof', 'truth:provenance', clock_timestamp(), clock_timestamp(), FALSE,
   jsonb_build_object('repository_id', 'repository:r_allowed', 'artifact_digest', 'sha256:' || lpad(to_hex(1007), 64, '0'), 'environment', 'prod', 'environment_evidence', 'deploy_event', 'outcome', 'exact', 'provenance_only', true))`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed runtime environment truth matrix: %v", err)
		}
	}
}

func seedRuntimeEnvironmentEvidenceHotDigest(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
  'scope:runtime-environment-live', 'repository', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active', 'generation:runtime-environment-live'
)`,
		`INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES (
  'generation:runtime-environment-live', 'scope:runtime-environment-live',
  'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()
)`,
		`INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
)
SELECT 'runtime-environment-hot:' || n,
       'scope:runtime-environment-live', 'generation:runtime-environment-live',
       'reducer_ci_cd_run_correlation', 'runtime-environment-hot:' || n,
       'proof', 'proof', 'runtime-environment-hot:' || n,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repository_id', 'repository:r_runtime_environment_live',
         'artifact_digest', 'sha256:' || lpad(to_hex(1), 64, '0'),
         'environment', 'prod',
         'environment_evidence', CASE WHEN n = 100000 THEN 'deploy_event' ELSE 'declared' END,
         'outcome', 'exact'
       )
FROM generate_series(1, 100000) AS n`,
		`INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
)
SELECT 'runtime-environment-cold:' || n,
       'scope:runtime-environment-live', 'generation:runtime-environment-live',
       'reducer_ci_cd_run_correlation', 'runtime-environment-cold:' || n,
       'proof', 'proof', 'runtime-environment-cold:' || n,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repository_id', 'repository:r_runtime_environment_live',
         'artifact_digest', 'sha256:' || lpad(to_hex(n), 64, '0'),
         'environment', 'prod', 'environment_evidence', 'declared', 'outcome', 'exact'
       )
FROM generate_series(2, 200) AS n`,
		`INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
)
SELECT 'runtime-environment-background:' || n,
       'scope:runtime-environment-live', 'generation:runtime-environment-live',
       'reducer_ci_cd_run_correlation', 'runtime-environment-background:' || n,
       'proof', 'proof', 'runtime-environment-background:' || n,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repository_id', 'repository:r_runtime_environment_live',
         'artifact_digest', 'sha256:background:' || n,
         'environment', 'prod', 'environment_evidence', 'declared', 'outcome', 'exact'
       )
FROM generate_series(1, 900000) AS n`,
		// The query joins fact_records to the active scope/generation pairs, so
		// the planner needs statistics for all three tables, not only the large one.
		`ANALYZE fact_records`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE scope_generations`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed runtime environment evidence proof: %v", err)
		}
	}
}

// requireRuntimeEnvironmentEvidenceActivePairs fails the proof when fewer than
// minimum active scope/generation pairs exist. With a single pair a scope-first
// plan and a fact-first plan both probe the artifact index once per candidate,
// so the loop-count assertion could not tell them apart.
func requireRuntimeEnvironmentEvidenceActivePairs(t *testing.T, ctx context.Context, db *sql.DB, minimum int) {
	t.Helper()
	var pairs int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM ingestion_scopes AS scopes
JOIN scope_generations AS generations
  ON generations.generation_id = scopes.active_generation_id
WHERE scopes.status = 'active'
  AND generations.status = 'active'`).Scan(&pairs); err != nil {
		t.Fatalf("count active scope/generation pairs: %v", err)
	}
	if pairs < minimum {
		t.Fatalf("active scope/generation pairs = %d, want at least %d so a scope-first plan is distinguishable", pairs, minimum)
	}
	t.Logf("RUNTIME_ENVIRONMENT_EVIDENCE active_pairs=%d", pairs)
}

func runtimeEnvironmentEvidenceLiveCandidates() ([]RuntimeEnvironmentCandidate, []string, []string) {
	candidates := make([]RuntimeEnvironmentCandidate, 0, 200)
	digests := make([]string, 0, 200)
	environments := make([]string, 0, 200)
	for i := 1; i <= 200; i++ {
		digest := fmt.Sprintf("sha256:%064x", i)
		candidates = append(candidates, RuntimeEnvironmentCandidate{
			SubjectDigest: digest,
			Environment:   "prod",
		})
		digests = append(digests, digest)
		environments = append(environments, "prod")
	}
	return candidates, digests, environments
}

func assertRuntimeEnvironmentEvidenceIndexPlan(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	digests []string,
	environments []string,
) {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		array.Of(digests),
		array.Of(environments),
		array.Of([]string{}),
		array.Of([]string{}),
	).Scan(&raw); err != nil {
		t.Fatalf("explain runtime environment evidence query: %v", err)
	}
	var plans []runtimeEnvironmentEvidencePlanResult
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode runtime environment evidence plan: err=%v raw=%s", err, raw)
	}
	indexPlan, ok := runtimeEnvironmentEvidencePlanIndex(plans[0].Plan, runtimeEnvironmentEvidenceArtifactIndex)
	if !ok {
		t.Fatalf("plan did not use %s: %s", runtimeEnvironmentEvidenceArtifactIndex, raw)
	}
	if got, want := int(indexPlan.ActualLoops), len(digests); got != want {
		t.Fatalf("artifact index loops = %d, want one per candidate (%d): %s", got, want, raw)
	}
	t.Logf(
		"RUNTIME_ENVIRONMENT_EVIDENCE_PLAN planning_ms=%.3f execution_ms=%.3f",
		plans[0].PlanningTime,
		plans[0].ExecutionTime,
	)
}

type runtimeEnvironmentEvidencePlanResult struct {
	Plan          runtimeEnvironmentEvidencePlanNode `json:"Plan"`
	PlanningTime  float64                            `json:"Planning Time"`
	ExecutionTime float64                            `json:"Execution Time"`
}

type runtimeEnvironmentEvidencePlanNode struct {
	IndexName   string                               `json:"Index Name"`
	ActualLoops float64                              `json:"Actual Loops"`
	Plans       []runtimeEnvironmentEvidencePlanNode `json:"Plans"`
}

func runtimeEnvironmentEvidencePlanIndex(
	node runtimeEnvironmentEvidencePlanNode,
	indexName string,
) (runtimeEnvironmentEvidencePlanNode, bool) {
	if node.IndexName == indexName {
		return node, true
	}
	for _, child := range node.Plans {
		if found, ok := runtimeEnvironmentEvidencePlanIndex(child, indexName); ok {
			return found, true
		}
	}
	return runtimeEnvironmentEvidencePlanNode{}, false
}

// TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive proves the production
// SQL keeps one artifact-index probe per candidate when the active
// scope/generation pair count reaches ops-qa scale (#7552: 819 pairs there).
// Without the fenced fact probe the planner multiplies the transitively implied
// scope join clauses (rows=1 estimated for 901 actual pairs) and plans
// scope-first: 200 candidates x 901 pairs = 180,200 probes. It is opt-in
// because it writes about 202,000 disposable rows into a production-schema
// PostgreSQL database in a database created and dropped by the helper.
func TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DSN"),
		os.Getenv("ESHU_RUNTIME_ENVIRONMENT_EVIDENCE_POSTGRES_DISPOSABLE"),
		15*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply production Postgres schema: %v", err)
	}
	seedRuntimeEnvironmentEvidenceManyPairs(t, ctx, db)
	requireRuntimeEnvironmentEvidenceActivePairs(t, ctx, db, 819)

	candidates, digests, environments := runtimeEnvironmentEvidenceLiveCandidates()
	assertRuntimeEnvironmentEvidenceIndexPlan(
		t,
		ctx,
		db,
		selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery,
		digests,
		environments,
	)

	got, err := (PostgresFindingStore{DB: db}).
		ListSupplyChainImpactRuntimeEnvironmentEvidence(ctx, candidates, nil, nil)
	if err != nil {
		t.Fatalf("ListSupplyChainImpactRuntimeEnvironmentEvidence(): %v", err)
	}
	if len(got) != len(candidates) {
		t.Fatalf("confirmed digests = %d, want %d", len(got), len(candidates))
	}
	if got[digests[0]]["prod"] != RuntimeEnvironmentEvidenceDeployEvent {
		t.Fatalf("digest 0 evidence = %#v, want deploy_event fold", got[digests[0]])
	}
}

// seedRuntimeEnvironmentEvidenceManyPairs seeds 900 active scope/generation
// pairs (901 with the bootstrap global pair), 200 candidate digests each with
// 10 matching facts, and 200,000 background facts, then analyzes every table
// the production query joins. Digest 1 matches deploy_event facts so the fold
// is exercised; all other matches are declared.
func seedRuntimeEnvironmentEvidenceManyPairs(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id
)
SELECT 'scope:runtime-environment-many-' || n, 'repository', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active', 'generation:runtime-environment-many-' || n
FROM generate_series(1, 900) AS n`,
		`INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
)
SELECT 'generation:runtime-environment-many-' || n, 'scope:runtime-environment-many-' || n,
  'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()
FROM generate_series(1, 900) AS n`,
		`INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
)
SELECT 'runtime-environment-many:' || d || ':' || s,
       'scope:runtime-environment-many-' || s, 'generation:runtime-environment-many-' || s,
       'reducer_ci_cd_run_correlation', 'runtime-environment-many:' || d || ':' || s,
       'proof', 'proof', 'runtime-environment-many:' || d || ':' || s,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repository_id', 'repository:r_runtime_environment_many',
         'artifact_digest', 'sha256:' || lpad(to_hex(d), 64, '0'),
         'environment', 'prod',
         'environment_evidence', CASE WHEN d = 1 THEN 'deploy_event' ELSE 'declared' END,
         'outcome', 'exact'
       )
FROM generate_series(1, 200) AS d CROSS JOIN generate_series(1, 10) AS s`,
		`INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
)
SELECT 'runtime-environment-many-background:' || n,
       'scope:runtime-environment-many-' || ((n - 1) % 900 + 1),
       'generation:runtime-environment-many-' || ((n - 1) % 900 + 1),
       'reducer_ci_cd_run_correlation', 'runtime-environment-many-background:' || n,
       'proof', 'proof', 'runtime-environment-many-background:' || n,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
         'repository_id', 'repository:r_runtime_environment_many',
         'artifact_digest', 'sha256:background:' || n,
         'environment', 'prod', 'environment_evidence', 'declared', 'outcome', 'exact'
       )
FROM generate_series(1, 200000) AS n`,
		`ANALYZE fact_records`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE scope_generations`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed runtime environment many-pairs proof: %v", err)
		}
	}
}

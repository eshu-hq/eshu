// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const runtimeEnvironmentEvidenceArtifactIndex = "fact_records_ci_cd_run_correlations_artifact_lookup_idx"

// TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive proves the
// production SQL remains indexed when one visible digest has many current
// deployment facts. It is opt-in because it writes one million disposable
// rows into a production-schema PostgreSQL database.
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
	got, err := (PostgresSupplyChainImpactFindingStore{DB: db}).
		ListSupplyChainImpactRuntimeEnvironmentEvidence(ctx, candidates, nil, nil)
	if err != nil {
		t.Fatalf("ListSupplyChainImpactRuntimeEnvironmentEvidence(): %v", err)
	}
	elapsed := time.Since(started)
	if len(got) != len(candidates) {
		t.Fatalf("confirmed digests = %d, want %d", len(got), len(candidates))
	}
	if got[digests[0]]["prod"] != SupplyChainRuntimeEnvironmentEvidenceDeployEvent {
		t.Fatalf("hot digest evidence = %#v, want deploy_event", got[digests[0]])
	}
	if elapsed > 2*time.Second {
		t.Fatalf("production store call = %s, want <= 2s", elapsed)
	}
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
	candidates := make([]SupplyChainRuntimeEnvironmentCandidate, 0, len(digests))
	for _, name := range []string{"scope-authorized", "repo-authorized", "denied", "stale", "tombstone", "rejected", "provenance"} {
		candidates = append(candidates, SupplyChainRuntimeEnvironmentCandidate{
			SubjectDigest: digests[name],
			Environment:   "prod",
		})
	}
	got, err := (PostgresSupplyChainImpactFindingStore{DB: db}).
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
	if evidence := got[digests["scope-authorized"]]["prod"]; evidence != SupplyChainRuntimeEnvironmentEvidenceDeployEvent {
		t.Fatalf("scope-authorized evidence = %q, want deploy_event", evidence)
	}
	if evidence := got[digests["repo-authorized"]]["prod"]; evidence != SupplyChainRuntimeEnvironmentEvidenceDeclared {
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
		`ANALYZE fact_records`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed runtime environment evidence proof: %v", err)
		}
	}
}

func runtimeEnvironmentEvidenceLiveCandidates() ([]SupplyChainRuntimeEnvironmentCandidate, []string, []string) {
	candidates := make([]SupplyChainRuntimeEnvironmentCandidate, 0, 200)
	digests := make([]string, 0, 200)
	environments := make([]string, 0, 200)
	for i := 1; i <= 200; i++ {
		digest := fmt.Sprintf("sha256:%064x", i)
		candidates = append(candidates, SupplyChainRuntimeEnvironmentCandidate{
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
		pgarray.Array(digests),
		pgarray.Array(environments),
		pgarray.Array([]string{}),
		pgarray.Array([]string{}),
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

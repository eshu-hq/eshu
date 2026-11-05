// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	metricdata "go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tfstate"
)

// TestTFStateOverlayTextsDerive is the hermetic shape guard for the
// #10006 overlay texts: every active text the Terraform drift overlay
// runs must derive a base text through the shared helper, binding the
// candidate base generation ids at a placeholder the active text leaves
// unused. A future edit that breaks the shape (renamed alias, dropped
// active predicate, reused placeholder) fails here instead of failing
// closed at runtime.
func TestTFStateOverlayTextsDerive(t *testing.T) {
	t.Parallel()

	active := []struct {
		name       string
		text       string
		scopeParam int
	}{
		{"canonical", tfstate.ActiveTerraformBackendCanonicalQuery, 1},
		{"backend_facts", tfstate.ActiveTerraformBackendFactsQuery, 2},
		{"config", tfstate.ActiveConfigResourcesForOverlayQuery, 2},
		{"modules", tfstate.ActiveModuleCallsForOverlayQuery, 2},
	}
	for _, tc := range active {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, err := deriveDeltaOverlayBaseQuery(tc.text, tc.scopeParam)
			if err != nil {
				t.Fatalf("deriveDeltaOverlayBaseQuery(%s) error = %v", tc.name, err)
			}
			for _, fragment := range []string{
				"active_delta",
				"delta_union_base_commit_sha",
				fmt.Sprintf("fact.generation_id = ANY($%d::text[])", tc.scopeParam),
			} {
				if !strings.Contains(base, fragment) {
					t.Errorf("derived %s base query missing %q", tc.name, fragment)
				}
			}
			if strings.Contains(base, "scope.active_generation_id = fact.generation_id") {
				t.Errorf("derived %s base query still carries the active scope join", tc.name)
			}
		})
	}
}

// describeMetricData renders sum datapoints for failure output.
func describeMetricData(data metricdata.Aggregation) string {
	sum, ok := data.(metricdata.Sum[int64])
	if !ok {
		return "(non-sum aggregation)"
	}
	var lines []string
	for _, point := range sum.DataPoints {
		var attrs []string
		for _, attr := range point.Attributes.ToSlice() {
			attrs = append(attrs, string(attr.Key)+"="+attr.Value.String())
		}
		lines = append(lines, fmt.Sprintf("%s => %v", strings.Join(attrs, ","), point.Value))
	}
	return strings.Join(lines, "\n")
}

// driftOverlayFile is one file fact to seed into a generation.
type driftOverlayFile struct {
	factID       string
	stableKey    string
	relativePath string
	tombstone    bool
	payload      string
}

// seedDriftOverlayLiveFixture seeds the #10006 repro on the production
// schema: config scope repository:repo-drift whose full generation
// (gen-full, commit-full) holds backend.tf plus main.tf with two
// resources, a pending delta (gen-delta) touching only outputs.tf, and
// an aligned state scope. Deltas activate between phases.
func seedDriftOverlayLiveFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	exec := func(what, statement string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	stateScope := "state_snapshot:s3:" + terraformstate.ScopeLocatorHash(terraformstate.BackendS3, "s3://drift-bucket/envs/prod.tfstate")

	exec("seed config scope", `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('repository:repo-drift', 'repository', 'git', 'repo-drift', 'git', 'repo-drift', $1, $1, 'active', 'gen-full', '{}'::jsonb)`, now)
	exec("seed state scope", `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'state_snapshot', 'terraform-state', $1, 'terraform-state', $1, $2, $2, 'active', 'st-gen-1', '{}'::jsonb)`,
		stateScope, now)

	exec("seed gen-full", `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, source_commit_sha, observed_at, ingested_at, activated_at, status, payload)
VALUES ('gen-full', 'repository:repo-drift', 'sync', 'commit-full', $1, $1, $1, 'active', '{}'::jsonb)`, now)
	exec("seed st-gen-1", `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ('st-gen-1', $1, 'sync', $2, $2, 'active', '{}'::jsonb)`, stateScope, now)

	insertFileFact := func(factID, generationID string, file driftOverlayFile) {
		t.Helper()
		exec("seed fact "+factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'repository:repo-drift', $2, 'file', $3, 'git', $3, $4, $4, $5, $6::jsonb)`,
			factID, generationID, file.stableKey, now, file.tombstone, file.payload)
	}
	backendEntry := `{"backend_kind":"s3","bucket":"drift-bucket","bucket_is_literal":true,` +
		`"key":"envs/prod.tfstate","key_is_literal":true,"region":"us-east-1","region_is_literal":true}`
	insertFileFact("fact-full-repo", "gen-full", driftOverlayFile{
		stableKey: "repository:repo-drift",
		payload:   `{"repo_id":"repo-drift","local_path":"/repo"}`,
	})
	insertFileFact("fact-full-backend", "gen-full", driftOverlayFile{
		stableKey: "file:repo-drift:backend.tf", relativePath: "backend.tf",
		payload: `{"repo_id":"repo-drift","relative_path":"backend.tf","parsed_file_data":` +
			`{"terraform_backends":[` + backendEntry + `],"terraform_variables":[],"terraform_locals":[]}}`,
	})
	insertFileFact("fact-full-main", "gen-full", driftOverlayFile{
		stableKey: "file:repo-drift:main.tf", relativePath: "main.tf",
		payload: `{"repo_id":"repo-drift","relative_path":"main.tf","parsed_file_data":` +
			`{"terraform_resources":[{"resource_type":"aws_instance","resource_name":"a","path":"main.tf"},` +
			`{"resource_type":"aws_instance","resource_name":"b","path":"main.tf"}]}}`,
	})

	seedDriftOverlayDelta(t, ctx, database, "gen-delta", "commit-delta",
		`{"repo_id":"repo-drift","local_path":"/repo","delta_generation":true,`+
			`"delta_relative_paths":["outputs.tf"],"delta_deleted_relative_paths":[]}`,
		[]driftOverlayFile{{
			factID: "fact-delta-outputs", stableKey: "file:repo-drift:outputs.tf", relativePath: "outputs.tf",
			payload: `{"repo_id":"repo-drift","relative_path":"outputs.tf","parsed_file_data":{"terraform_resources":[],"terraform_modules":[]}}`,
		}})

	exec("seed state snapshot", `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ('fact-state-snapshot', $1, 'st-gen-1', 'terraform_state_snapshot', 'snapshot:st-gen-1',
        'terraform-state', 'snapshot:st-gen-1', $2, $2, '{"lineage":"drift-lineage-1","serial":0}'::jsonb)`, stateScope, now)
	for _, name := range []string{"a", "b"} {
		exec("seed state resource "+name, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, 'st-gen-1', 'terraform_state_resource', $3, 'terraform-state', $3, $4, $4, $5::jsonb)`,
			"fact-state-"+name, stateScope, "state:aws_instance."+name, now,
			`{"address":"aws_instance.`+name+`","type":"aws_instance","attributes":{}}`)
	}
}

// seedDriftOverlayDelta seeds one pending delta generation with its
// repository fact and file facts.
func seedDriftOverlayDelta(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	generationID string,
	commit string,
	repositoryPayload string,
	files []driftOverlayFile,
) {
	t.Helper()
	now := time.Now().UTC()
	exec := func(what, statement string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	exec("seed "+generationID, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, is_delta, source_commit_sha,
     delta_baseline_commit_sha, delta_union_base_commit_sha,
     observed_at, ingested_at, status, payload)
VALUES ($1, 'repository:repo-drift', 'sync', true, $2,
        'commit-full', 'commit-full', $3, $3, 'pending', '{}'::jsonb)`,
		generationID, commit, now)
	exec("seed "+generationID+" repository fact", `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'repository:repo-drift', $2, 'repository', 'repository:repo-drift', 'git', 'repository:repo-drift', $3, $3, $4::jsonb)`,
		"fact-"+generationID+"-repo", generationID, now, repositoryPayload)
	for _, file := range files {
		exec("seed fact "+file.factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'repository:repo-drift', $2, 'file', $3, 'git', $3, $4, $4, $5, $6::jsonb)`,
			file.factID, generationID, file.stableKey, now, file.tombstone, file.payload)
	}
}

// activateDriftOverlayGeneration supersedes one generation and activates
// the next the way the ingestion Ack does.
func activateDriftOverlayGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, fromID, toID string) {
	t.Helper()
	exec := func(what, statement string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	exec("supersede "+fromID, `UPDATE scope_generations SET status = 'superseded' WHERE scope_id = $1 AND generation_id = $2`, scopeID, fromID)
	exec("activate "+toID, `UPDATE scope_generations SET status = 'active', activated_at = clock_timestamp() WHERE generation_id = $1`, toID)
	exec("point scope "+scopeID, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, toID)
}

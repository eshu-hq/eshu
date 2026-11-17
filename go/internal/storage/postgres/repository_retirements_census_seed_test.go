// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// censusSeedStatements writes the #10820 rows for one repository partition and
// its scopes. A per-scope statement takes $1 = one scope id and $2 = that
// scope's generation id and runs once per scope; a repoLevel statement takes
// $1 = the partition (collector repo id) and runs once per partition.
type censusSeedStatement struct {
	name      string
	repoLevel bool
	sql       string
}

var censusSeedStatements = []censusSeedStatement{
	{name: "reducer_readiness_waits", sql: `INSERT INTO reducer_readiness_waits
		(scope_id, domain, first_deferred_at, missing_fingerprint, updated_at)
		VALUES ($1, 'd1', NOW(), 'fp', NOW())`},
	{name: "crossplane_satisfied_by_redrive_state", sql: `INSERT INTO crossplane_satisfied_by_redrive_state
		(xrd_scope_id, xrd_generation_id, status, updated_at) VALUES ($1, $2, 'queued', NOW())`},
	{name: "crossplane_satisfied_by_redrive_target_ledger", sql: `INSERT INTO crossplane_satisfied_by_redrive_target_ledger
		(target_scope_id, xrd_group, xrd_claim_kind, redriven_at) VALUES ($1, 'g.example', 'Claim', NOW())`},
	{name: "service_materialization_generations", sql: `INSERT INTO service_materialization_generations
		(generation_id, service_id, trigger_kind, observed_at, ingested_at, status, scope_id)
		VALUES ($1 || '-svc', 'svc-' || $1, 'push', NOW(), NOW(), 'superseded', $1)`},
	{name: "service_evidence_snapshots", sql: `INSERT INTO service_evidence_snapshots
		(generation_id, service_id, evidence_family, service_evidence_key, payload_hash, observed_at)
		VALUES ($1 || '-svc', 'svc-' || $1, 'fam', 'k1', 'h', NOW()), ($1 || '-svc', 'svc-' || $1, 'fam', 'k2', 'h', NOW())`},
	{name: "relationship_generations", sql: `INSERT INTO relationship_generations
		(generation_id, scope, status, created_at) VALUES ($1 || '-relgen', $1, 'active', NOW())`},
	// Evidence under the relationship generation id and under the scope's own
	// generation id: the purge predicate reaches both maps.
	{name: "relationship_evidence_facts", sql: `INSERT INTO relationship_evidence_facts
		(evidence_id, generation_id, evidence_kind, relationship_type, confidence, rationale, details, observed_at)
		VALUES ($1 || '-ev1', $1 || '-relgen', 'k', 'DEPLOYS', 1, 'r', '{}', NOW()),
		       ($1 || '-ev2', $2, 'k', 'DEPLOYS', 1, 'r', '{}', NOW())`},
	{name: "relationship_candidates", sql: `INSERT INTO relationship_candidates
		(candidate_id, generation_id, relationship_type, confidence, evidence_count, rationale, details)
		VALUES ($1 || '-cand', $1 || '-relgen', 'DEPLOYS', 1, 1, 'r', '{}')`},
	{name: "projection_decisions", repoLevel: true, sql: `INSERT INTO projection_decisions
		(decision_id, decision_type, repository_id, source_run_id, work_item_id, subject,
		 confidence_score, confidence_reason, provenance_summary, created_at)
		VALUES ($1 || '-d1', 't', $1, 'run', 'w', 's', 1, 'r', '{}', NOW()),
		       ($1 || '-d2', 't', $1, 'run', 'w', 's', 1, 'r', '{}', NOW())`},
	{name: "projection_decision_evidence", repoLevel: true, sql: `INSERT INTO projection_decision_evidence
		(evidence_id, decision_id, evidence_kind, detail, created_at)
		VALUES ($1 || '-pe1', $1 || '-d1', 'k', '{}', NOW()), ($1 || '-pe2', $1 || '-d2', 'k', '{}', NOW())`},
	{name: "config_artifact_paths", repoLevel: true, sql: `INSERT INTO config_artifact_paths
		(repo_id, raw_path, relative_path, evidence_kind, name_scoped)
		VALUES ($1, 'a.yaml', 'a.yaml', 'k', false)`},
	{name: "content_file_ref_deltas", repoLevel: true, sql: `INSERT INTO content_file_ref_deltas
		(repo_id, ref, relative_path, content, content_hash, is_deleted, indexed_at)
		VALUES ($1, 'feature', 'a.go', 'x', 'h', false, NOW())`},
	{name: "content_file_ref_delta_bases", repoLevel: true, sql: `INSERT INTO content_file_ref_delta_bases
		(repo_id, ref, base_default_sha, updated_at) VALUES ($1, 'feature', 'sha', NOW())`},
	{name: "code_function_fingerprint", repoLevel: true, sql: `INSERT INTO code_function_fingerprint
		(entity_id, repo_id, fp_exact, token_count, indexed_at) VALUES ($1 || '-fn', $1, 'fp', 3, NOW())`},
	{name: "code_fingerprint_band", repoLevel: true, sql: `INSERT INTO code_fingerprint_band
		(repo_id, band_no, band_hash, entity_id) VALUES ($1, 1, 'bh', $1 || '-fn')`},
	{name: "function_summaries", repoLevel: true, sql: `INSERT INTO function_summaries
		(function_id, effects, version, structural_hash, repo, updated_at)
		VALUES ($1 || '-f', '{}', 'v', 'sh', $1, NOW())`},
	{name: "function_sources", repoLevel: true, sql: `INSERT INTO function_sources
		(function_id, param_index, kind, repo, updated_at) VALUES ($1 || '-f', 0, 'k', $1, NOW())`},
	{name: "function_graph_ids", repoLevel: true, sql: `INSERT INTO function_graph_ids
		(function_id, uid, repo, updated_at) VALUES ($1 || '-f', 'uid', $1, NOW())`},
	{name: "infra_resource_entities", repoLevel: true, sql: `INSERT INTO infra_resource_entities
		(entity_id, repo_id, scope_id, relative_path, label, entity_name, updated_at)
		VALUES ($1 || '-infra', $1, '', 'main.tf', 'TerraformResource', 'r', NOW())`},
	// An infra-typed content entity: its delete fires the migration 109 row
	// trigger that marks the repository dirty again unless the session is a
	// derive session.
	{name: "content_entities", repoLevel: true, sql: `INSERT INTO content_entities
		(entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, source_cache, indexed_at)
		VALUES ($1 || '-tf', $1, 'main.tf', 'TerraformResource', 'r', 1, 1, '', NOW())`},
	{name: "infra_resource_entity_dirty_repos", repoLevel: true, sql: `INSERT INTO infra_resource_entity_dirty_repos
		(repo_id, marked_at) VALUES ($1, NOW()) ON CONFLICT DO NOTHING`},
}

// seedCensus2eRows writes the #10820 rows for a partition. scopeIDs must
// already exist with a "<scope>-gen-old" generation.
func seedCensus2eRows(t *testing.T, ctx context.Context, database *sql.DB, partition string, scopeIDs []string) {
	t.Helper()
	for _, stmt := range censusSeedStatements {
		if stmt.repoLevel {
			if _, err := database.ExecContext(ctx, stmt.sql, partition); err != nil {
				t.Fatalf("seed %s: %v", stmt.name, err)
			}
			continue
		}
		for _, scopeID := range scopeIDs {
			args := []any{scopeID, scopeID + "-gen-old"}
			if !strings.Contains(stmt.sql, "$2") {
				args = args[:1]
			}
			if _, err := database.ExecContext(ctx, stmt.sql, args...); err != nil {
				t.Fatalf("seed %s for %s: %v", stmt.name, scopeID, err)
			}
		}
	}
	// A dependency Variable fact: deleting it with the scope cascade fires
	// the migration 139 trigger that marks the scope dirty.
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
		(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
		 observed_at, ingested_at, payload)
		VALUES ($1, $2, $3, 'content_entity', $1, 'git', $1, NOW(), NOW(),
		 '{"entity_type":"Variable","config_kind":"dependency"}'::jsonb)`,
		scopeIDs[0]+"-dep-fact", scopeIDs[0], scopeIDs[0]+"-gen-old"); err != nil {
		t.Fatalf("seed dependency fact: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_dirty_scopes
		(scope_id, marked_at) VALUES ($1, NOW()) ON CONFLICT DO NOTHING`, scopeIDs[0]); err != nil {
		t.Fatalf("seed consumption dirty scope: %v", err)
	}
}

// seedCensusSentinel writes a second partition with one scope and the same
// #10820 rows; the purge of the first partition must leave every row alone.
func seedCensusSentinel(t *testing.T, ctx context.Context, database *sql.DB) (string, []string) {
	t.Helper()
	partition, scopeID := "PARTITION:sentinel", "sentinel-main"
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1, 'repository', 'git', 'key-sentinel', 'git_collector', $2, NOW(), NOW(), 'active', NULL, '{}'::jsonb)`,
		scopeID, partition); err != nil {
		t.Fatalf("seed sentinel scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
		VALUES ($1, $2, 'push', NOW(), NOW(), 'active', '{}'::jsonb)`, scopeID+"-gen-old", scopeID); err != nil {
		t.Fatalf("seed sentinel generation: %v", err)
	}
	scopes := []string{scopeID}
	seedCensus2eRows(t, ctx, database, partition, scopes)
	return partition, scopes
}

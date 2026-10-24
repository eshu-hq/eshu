// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
	"testing"
)

// goldenBootstrapDefinitionsDigest pins BootstrapDefinitions(). It is sha256
// over "Name|Path|sha256(SQL)\n" per definition, in BootstrapDefinitions()
// order. The migration tracker keys applied migrations by path + variant +
// checksum_sha256 (schema_bootstrap_lock.go), so any drift here means an
// existing deployment's bootstrap would try to re-apply or diverge on
// already-applied migrations. A change to this constant must be justified by
// an intentional migration edit, never by a refactor alone.
//
// #7002: this value was last updated to 093_cross_scope_completion_queue.sql
// being restored to its originally shipped bytes (checksum
// c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42). #6785
// and #6923 had edited that file in place instead of shipping a new guarded
// migration, which is exactly the mistake this golden digest exists to catch
// -- see README.md and checksum_alias.go. #7007 then added
// 121_fact_records_content_entity_dependency_variable_repo_idx.sql; #7126 added
// 122_fact_records_documentation_source_only_idx.sql and
// 123_fact_records_story_support_kinds_idx.sql and
// 124_fact_records_documentation_semantic_target_refs_idx.sql, #6679 added
// 125_shared_projection_acceptance_generation_key.sql, and #6475 added
// 126-129 (the service materialization lineage scope column, its backfill,
// and the rescoped active-service index); #7115 added
// 130_projector_scope_claim_fences.sql; #7237 added
// 131_fact_records_semantic_code_hint_order_idx.sql and
// 132_content_entities_repo_path_start_idx.sql; #7242 added
// 133_repository_entry_points_index.sql; #7033 added the path GIN and
// lifecycle validator as migrations 134-135; #7127 added
// 136_changed_since_link_ledger.sql; #7248 added
// 137_content_files_repo_path_pattern_idx.sql; #7125 adds
// 138_content_file_secret_lines.sql; #7088 adds package-consumption identity
// and readiness indexes in migrations 139-144; #7242 adds
// 145_fact_records_workload_names_scope_idx.sql; #7279 adds the retention key
// indexes as 146_fact_records_content_entity_key_idx.sql and
// 147_fact_records_file_key_idx.sql; #7319 adds
// 148_scope_generations_delta_baseline_commit_sha.sql; #7389 adds
// 149_scope_generations_projection_write_started_at.sql and
// 150_scope_generations_fillfactor.sql; #7254 adds
// 151_code_fingerprint_band_entity_idx.sql; #7138 adds
// 152_fact_records_story_support_link_repo_idx.sql; #7154 adds
// 153_fact_records_os_package_purl_prefix_idx.sql; #7142 adds
// 154_supply_chain_impact_write_admission.sql; #7464 adds
// 155_fact_records_story_support_issue_idx.sql; #7419 adds
// 156_graph_projection_phase_state_generation_idx.sql; #7088 adds
// 159_fact_records_content_entity_dependency_legacy_gap_repo_idx.sql; #7584 adds
// 160_activation_obligations.sql; #7009 adds 161_status_summary_snapshots.sql;
// #7620 adds 162_repository_reindex_requests.sql; #7635 adds
// 163_producer_activation_obligations.sql; #7729 adds
// 164_content_entities_repo_language_type_idx.sql; #7166 adds
// 165_phase_state_quiescence_gate_stats.sql; #7625 adds
// 166_repository_selection_observations.sql; #7799 adds
// 167_unroutable_intents_generation_idx.sql; #7777 adds
// 168_webhook_refresh_triggers_claim_fencing.sql; #7760 adds
// 169_content_files_generation_id.sql; #7126 adds
// 170_config_artifact_read_model.sql; #7728 adds
// 171_drop_content_files_language_repo_idx.sql,
// 172_content_files_language_covering_idx.sql, and
// 173_content_files_language_covering_idx_vacuum.sql; #7735 adds
// 174_content_entities_repo_language_type_path_idx.sql; #7727 adds
// 175_eshu_search_index_documents_bm25_posting_idx.sql; #7975 adds
// 176_content_files_content_lower.sql and
// 177_content_files_content_lower_trgm_index.sql; #7765 adds
// 178_repository_selection_observations_gitlab_group.sql; #7929 adds
// 179_shared_projection_intents_acceptance_completed_idx.sql; #7666 adds
// 180_fact_records_terraform_state_resource_arn_idx.sql; #7663 adds
// 181_fact_records_package_manifest_consumption_backfill_idx.sql; #7420 adds
// 182_package_manifest_consumption_keys_partition_idx.sql and
// 183_package_registry_identity_keys_partition_idx.sql; #7926 adds
// 184_workflow_claims_failure_classification.sql and
// 185_workflow_work_items_failure_classification.sql; #8155 adds
// 186_content_files_version_fence.sql and
// 187_config_artifact_content_versions_seed.sql; #7420 adds
// 188_relationship_reference_candidate_keys_drop_fact_fk.sql,
// 189_package_manifest_consumption_keys_drop_fact_fk.sql and
// 190_package_registry_identity_keys_drop_fact_fk.sql; #7427 adds
// 191_eshu_search_index_terms_drop_generation_fk.sql; #8283 adds
// 193_deployable_unit_reopen_fingerprints.sql (numbered 193 while open PRs held
// 191-192; sequence gaps are tolerated); #7098 slice D-read adds
// 194_fact_records_changed_since_live_keys_idx.sql and
// 195_fact_records_changed_since_tombstone_keys_idx.sql, and slice B adds
// 196_fact_records_documentation_collected_target_refs_idx.sql (numbered 196
// while the slice-D PR holds 194-195; gaps are tolerated); #8316 adds
// 198_deployable_unit_gap_admitted_anchor_idx.sql (numbered 198: main landed
// its own 194/195/196/197 while this branch held 194, then 196, then 197;
// sequence gaps are tolerated); #8454 adds
// 199_repository_selection_observation_classification.sql; #7840 adds
// 200_repository_selectors.sql; #8469 adds
// 201_workflow_work_items_claim_class_columns.sql (DDL only),
// 202_workflow_work_items_fair_claim_idx.sql,
// 203_workflow_work_items_claim_order_idx_drop.sql and
// 204_workflow_work_items_claim_class_backfill.sql (the split 201's DML
// half: no file holds a table lock through bulk DML, #7961; the lane's own
// step-1 200_workflow_work_items_claim_order_idx.sql create is gone under
// the 101/102 shape, so the lane holds no 200 of its own; main's 200 is
// #7840's repository_selectors above; gaps are tolerated); #8469 revises
// unshipped 204 in-lane with the empty-class sink pass (owner P2: blank
// class segments sink to 4 instead of floating at the 201 default 0);
// #8470 adds 205_workflow_claim_pacing.sql (claim-pacing latch table, DDL
// only: latch rows appear through runtime upserts, no backfill).
// #8471 adds 206_component_quota_windows.sql (renumbered from 205: base took
// 205 for claim pacing; DDL only, usage rows created lazily by the grant
// upsert, so no backfill half exists).
// #7766 adds 207_repository_retirements.sql (retirement tombstone marker
// table plus its open-row, runnable, scope, and idempotency indexes).
// #8659 adds 208_repository_retirement_graph_capture.sql (graph_repo_root
// plus graph_scope_ids capture columns for the 2c graph retraction) and
// 209_partition_lease_claimed_at.sql (claim epoch for the 2b' barrier).
// #8780 adds 210_scope_generations_delta_union_base_commit_sha.sql (the
// union-base commit a cumulative delta was diffed from; catalog-only column).
// #8803 adds 211_workflow_work_items_failed_terminal_instance_idx.sql (the
// partial failed_terminal index for the collector terminal-failure read).
// #8880 adds 212-225, the supply-chain impact active-evidence anchors
// (one CREATE INDEX CONCURRENTLY per file).
// #8880 adds 212-224, the supply-chain impact active-evidence anchors
// (one CREATE INDEX CONCURRENTLY per file).
const goldenBootstrapDefinitionsDigest = "4bdc926d68ad3e48513bcad8dfb9d9300ac708832ba7e910291e0b89529f3889"

// goldenBootstrapDefinitionsCount pins the definition count alongside the
// digest so a truncated embed pattern (e.g. matching embed.go itself, or
// silently dropping files) fails loudly even in the unlikely case of a hash
// collision.
const goldenBootstrapDefinitionsCount = 242

func TestBootstrapDefinitionsMatchesPreRefactorGolden(t *testing.T) {
	defs := BootstrapDefinitions()
	if len(defs) != goldenBootstrapDefinitionsCount {
		t.Fatalf("BootstrapDefinitions() count = %d, want %d", len(defs), goldenBootstrapDefinitionsCount)
	}
	h := sha256.New()
	for _, def := range defs {
		sqlSum := sha256.Sum256([]byte(def.SQL))
		fmt.Fprintf(h, "%s|%s|%s\n", def.Name, def.Path, hex.EncodeToString(sqlSum[:]))
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != goldenBootstrapDefinitionsDigest {
		t.Fatalf("BootstrapDefinitions() digest = %s, want %s (Name/Path/SQL/order must stay byte-identical)",
			got, goldenBootstrapDefinitionsDigest)
	}
}

// TestBootstrapDefinitionsHaveUniqueSequenceNumbers keeps recent migrations
// from claiming the same ordering slot after concurrent PRs merge. Older
// migrations intentionally reuse several slots, so the guard starts at 100.
func TestBootstrapDefinitionsHaveUniqueSequenceNumbers(t *testing.T) {
	seen := make(map[int]string)
	for _, def := range BootstrapDefinitions() {
		filename := path.Base(def.Path)
		parts := strings.SplitN(filename, "_", 2)
		if len(parts) != 2 {
			t.Fatalf("migration %q has no sequence prefix", filename)
		}
		sequence, err := strconv.Atoi(parts[0])
		if err != nil || sequence < 100 {
			continue
		}
		if previous, exists := seen[sequence]; exists {
			t.Fatalf("migration sequence %d is shared by %q and %q", sequence, previous, filename)
		}
		seen[sequence] = filename
	}
}

// TestBootstrapDefinitionsExcludesNonSQLFiles guards the embed pattern: only
// *.sql files may become definitions. embed.go itself, doc.go, README.md and
// AGENTS.md must never appear.
func TestBootstrapDefinitionsExcludesNonSQLFiles(t *testing.T) {
	for _, def := range BootstrapDefinitions() {
		if def.Name == "" {
			t.Fatalf("definition with empty name for path %q", def.Path)
		}
	}
	for _, name := range []string{"embed", "doc", "README", "AGENTS"} {
		for _, def := range BootstrapDefinitions() {
			if def.Name == name {
				t.Fatalf("non-SQL file %q leaked into BootstrapDefinitions()", name)
			}
		}
	}
}

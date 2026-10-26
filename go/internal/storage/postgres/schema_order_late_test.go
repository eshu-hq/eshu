// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

var orderedBootstrapDefinitionNamesLate = []string{
	// migration 120 (#6923) admits code_function_summary as the fifth
	// value-flow refresh producer domain: converges the
	// cross_scope_completion_events producer_domain CHECK and the
	// fact_work_items_cross_scope_completion trigger's domain list on
	// installs that already applied 112 but predate this migration.
	// Numbered 120 because main merged the #6946 liveness-stats
	// migration as 119 first.
	"value_flow_refresh_code_function_summary_producer",
	// migration 121 (#7007) gives the supply-chain impact readiness query's
	// package_manifest_active CTE a repo_id-leading partial index on
	// content_entity dependency-variable facts, alongside pushing the
	// repository_id predicate into that CTE (readiness_postgres_query.go).
	// Without it, package_manifest_active had no repository_id predicate at
	// all and no supporting index, forcing a per-scope scan-and-filter over
	// content_entity (the largest fact_kind in the corpus) on every
	// impact/findings readiness read.
	"fact_records_content_entity_dependency_variable_repo_idx",
	// migration 122 (#7126) gives the documentation story's source-only
	// count a partial index over the four counted documentation kinds with no
	// structured refs.
	"fact_records_documentation_source_only_idx",
	// migration 123 (#7126) gives the story target-support reads a partial
	// index over the twelve work_item.* and incident_routing.* support kinds.
	"fact_records_story_support_kinds_idx",
	// migration 124 (#7126) gives the documentation target-facts read's
	// semantic branch a partial GIN over semantic.documentation_observation,
	// mirroring fact_records_documentation_target_refs_idx.
	"fact_records_documentation_semantic_target_refs_idx",
	// migration 125 (#6679) adds shared_projection_acceptance.
	// generation_ingested_at, the stored generation's ordering key the
	// advance-only upsert compares row-locally. There is no backfill: the
	// guard resolves a NULL key from scope_generations at write time.
	"shared_projection_acceptance_generation_key",
	// migrations 126-129 (#6475) scope the service materialization lineage by
	// ingestion scope: the scope_id column, its witness backfill (a file of its
	// own so it does not hold the ALTER's ACCESS EXCLUSIVE lock), the guarded
	// drop of 025's single-active-per-service definition, and its
	// (scope_id, service_id) rebuild under the same name.
	"service_materialization_generations_scope_column",
	"service_materialization_generations_scope_backfill",
	"service_materialization_generations_active_service_idx_rescope",
	"service_materialization_generations_active_service_idx_v2",
	// migration 130 (#7115) adds projector_scope_claim_fences, the per-scope
	// claim fence the projector claim locks and bumps (and, since #7819, the
	// write-start marker), with an AFTER INSERT trigger on ingestion_scopes
	// and a backfill. Numbered 130, after #6475's 126-129.
	"projector_scope_claim_fences",
	// #7237 adds ordered partial indexes for semantic code hints and repo paths.
	"fact_records_semantic_code_hint_order_idx",
	"content_entities_repo_path_start_idx",
	// Migration 133 (#7242) narrows repository-context entry-point reads.
	"repository_entry_points_index",
	// Migrations 134-135 (#7033) add the all-repository relative_path GIN and
	// extend the deferred content-index lifecycle to build and validate it only
	// after the write-heavy content projection drain.
	"content_files_relative_path_trgm_index",
	"content_files_relative_path_trgm_index_lifecycle",
	// Migration 136 (#7127) adds the changed-since link ledger tables, without foreign keys.
	"changed_since_link_ledger",
	// Migration 137 (#7248) bounds repository tree reads by a literal path prefix under ICU.
	"content_files_repo_path_pattern_idx",
	// Migration 138 (#7125) creates the hardcoded-secret side table after its content_files dependencies.
	"content_file_secret_lines",
	// Migrations 139-144 (#7088) add the package-consumption read model and target indexes.
	"package_manifest_consumption_keys",
	"supply_chain_readiness_target_indexes",
	"supply_chain_readiness_sbom_component_index",
	"supply_chain_readiness_package_registry_index",
	"package_registry_identity_keys",
	"supply_chain_readiness_sbom_warning_document_index",
	// Migration 145 (#7242) seeks workload-name facts by repository scope.
	"fact_records_workload_names_scope_idx",
	// Migrations 146-147 (#7279) add the retention key indexes the per-candidate
	// retained-holder probe reads, one concurrent build per file. Renumbered
	// from 138-139 to 139-140 after #7206, to 145-146 after #7301, then to
	// 146-147 after #7352.
	"fact_records_content_entity_key_idx",
	"fact_records_file_key_idx",
	// Migration 148 (#7319) records the commit a delta generation diffed from.
	"scope_generations_delta_baseline_commit_sha",
	// Migrations 149-150 (#7389) add the projector write-start marker and give
	// scope_generations the free space that keeps the marker update HOT.
	"scope_generations_projection_write_started_at",
	"scope_generations_fillfactor",
	// Migration 151 (#7254) gives code_fingerprint_band the (repo_id, entity_id)
	// seek its delete statements filter on.
	"code_fingerprint_band_entity_idx",
	"fact_records_story_support_link_repo_idx",
	"fact_records_os_package_purl_prefix_idx",
	"supply_chain_impact_write_admission",
	// Migration 155 (#7464) keys the issue-id probe of a linked Jira issue's
	// records and transitions.
	"fact_records_story_support_issue_idx",
	// Migration 156 (#7419) gives graph_projection_phase_state the
	// generation_id seek that the foreign-key cascade of every generation prune
	// fires once per deleted generation.
	"graph_projection_phase_state_generation_idx",
	// Migration 159 (#7088) keys the readiness query's legacy-shape and
	// dependency-gap reads by repository, scope and generation.
	"fact_records_content_entity_dependency_legacy_gap_repo_idx",
	// Migration 160 (#7584) adds the durable exact-generation activation
	// obligations that ProjectorQueue.Ack writes and the resolution engine's
	// leased consumer settles.
	"activation_obligations",
	// Migration 161 (#7009) adds status_summary_snapshots, the one-row-per-model
	// read model behind the status routes' active-work summary.
	"status_summary_snapshots",
	// Migration 162 (#7620) adds repository_reindex_requests, the per-repository
	// reindex watermarks the git ingesters read once per cycle.
	"repository_reindex_requests",
	// Migration 163 (#7635) adds producer_activation_obligations, the lease-
	// fenced obligations a quiet Ack owes for producer generations.
	"producer_activation_obligations",
	// Migration 164 (#7729) bounds the #6540 language-query existence gate by
	// repository: a (repo_id, language, entity_type) btree under a NEW name, so
	// the gate no longer walks the corpus-wide (language, entity_type) entries
	// that sort before a late repository's first row.
	"content_entities_repo_language_type_idx",
	// Migration 165 (#7166) adds a functional-dependency statistics object on
	// phase-state (scope_id, generation_id), restoring the pkey pushdown for
	// the canonical-code quiescence gate probe.
	"phase_state_quiescence_gate_stats",
	// Migration 166 (#7625) adds repository_selection_observations, the
	// per-scope, per-selector githubOrg listing evidence shard 0 writes.
	"repository_selection_observations",
	// Migration 167 (#7799) adds a (generation_id) btree on
	// shared_projection_unroutable_intents, so the generation-only
	// retention count leg and the generation-only reap DELETE both probe
	// instead of skip-scanning the scope-leading index.
	"unroutable_intents_generation_idx",
	// Migration 168 (#7777) adds the webhook trigger claim fencing token and
	// stale-claim reap index that only the store's EnsureSchema created.
	"webhook_refresh_triggers_claim_fencing",
	// Migration 169 (#7760) adds the content_files generation tag and
	// backfills legacy rows to the scope's active generation where the
	// fail-safe guard clauses hold.
	"content_files_generation_id",
	// Migration 170 (#7126) creates the empty config-artifact read model
	// tables: no trigger, no seed.
	"config_artifact_read_model",
	// Migration 171 (#7728) drops content_files_language_repo_idx so 172 can
	// rebuild it as a covering index (CONCURRENTLY cannot run in a
	// transaction, so the drop is a separate migration).
	"drop_content_files_language_repo_idx",
	// Migration 172 (#7728) rebuilds the language index as
	// (language, repo_id) INCLUDE (indexed_at) for index-only
	// per-language COUNT(*)/MAX(indexed_at) reads.
	"content_files_language_covering_idx",
	// Migration 173 (#7728) runs VACUUM (ANALYZE) on content_files so the
	// covering index pays off immediately instead of waiting on autovacuum.
	"content_files_language_covering_idx_vacuum",
	// Migration 174 (#7735) carries the language-query equality columns AND
	// the page ORDER BY on content_entities, so a repository whose matching
	// rows sort late in the corpus-wide (language, entity_type) index is a
	// btree descent that stops at LIMIT instead of an ordered walk that
	// heap-filters every earlier repository's rows. Migration 164's
	// (repo_id, language, entity_type) key is a strict PREFIX of this one
	// and still serves the existence gate on its smaller key, which is why
	// both indexes exist rather than one superseding the other.
	"content_entities_repo_language_type_path_idx",
	// Migration 175 (#7727) covers the BM25 per-posting document lateral
	// with repo_id and document_length on the documents pkey.
	"eshu_search_index_documents_bm25_posting_idx",
	// Migration 176 (#7975) adds the stored lowercase copy of
	// content_files.content for the unscoped search recheck.
	"content_files_content_lower",
	// Migration 177 (#7975) indexes the stored lowercase copy with a
	// trigram index so the recheck uses the index.
	"content_files_content_lower_trgm_index",
	// Migration 178 (#7765) admits the gitlab_group selector kind to
	// repository_selection_observations on installs that already applied
	// migration 166.
	"repository_selection_observations_gitlab_group",
	// Migration 179 (#7929) covers the reachability loader's per-run
	// max(completed_at) probe with the probe's equality columns plus the
	// domain IN list and trailing completed_at, partial to completed rows,
	// so each probe is an index-only scan instead of a heap walk.
	"shared_projection_intents_acceptance_completed_idx",
	// Migration 180 (#7666) is the partial ARN expression index the
	// aws_cloud_runtime_drift state join probes once per AWS ARN.
	"fact_records_terraform_state_resource_arn_idx",
	// Migration 181 (#7663) gives the package-consumption backfill's page
	// query a partial (scope_id, fact_id) index on its own fact predicate.
	"fact_records_package_manifest_consumption_backfill_idx",
	// Migrations 182 and 183 (#7420) add the (scope_id, generation_id,
	// fact_id) indexes the explicit retention key sidecar deletes probe.
	"package_manifest_consumption_keys_partition_idx",
	"package_registry_identity_keys_partition_idx",
	// Migrations 184-185 (#7926) persist the bounded structured failure
	// classification (operation, status code, terminal flag) on
	// workflow_claims and workflow_work_items alongside the message.
	"workflow_claims_failure_classification",
	"workflow_work_items_failure_classification",
	// Migration 186 (#8155, #7126 PR3b-1b) adds the four content_files
	// version-fence triggers of the config-artifact read model.
	"content_files_version_fence",
	// Migration 187 seeds one version-0 row per repository with content,
	// in its own file so no table lock is held through the scan (#7961).
	"config_artifact_content_versions_seed",
	// Migrations 188 to 190 (#7420) drop the per-fact foreign keys of the
	// three key sidecars once retention deletes their rows explicitly.
	"relationship_reference_candidate_keys_drop_fact_fk",
	"package_manifest_consumption_keys_drop_fact_fk",
	"package_registry_identity_keys_drop_fact_fk",
	// Migration 191 (#7427) drops the eshu_search_index_terms generation
	// foreign key once retention deletes a generation's terms explicitly.
	"eshu_search_index_terms_drop_generation_fk",
	// Migration 193 (#8283) records the per-item input fingerprint the
	// deployable_unit_correlation reopen compares to skip unchanged items.
	"deployable_unit_reopen_fingerprints",
	// Migrations 194 and 195 (#7098 slice D-read) trim the changed-since
	// diff: a partial live-keys index excluding reducer fact_kinds and a
	// tombstone-only index. Read path unchanged.
	"fact_records_changed_since_live_keys_idx",
	"fact_records_changed_since_tombstone_keys_idx",
	// Migration 196 (#7098 slice B) gives the documentation-facts read a
	// partial GIN over documentation_document, documentation_section, and
	// documentation_link, mirroring
	// fact_records_documentation_target_refs_idx.
	"fact_records_documentation_collected_target_refs_idx",
	// Migration 197 (#5393 Phase 1) adds the branch-overlay deltas table
	// plus its bases ledger; canonical content_files stays default-only.
	"content_file_ref_deltas",
	// Migration 198 (#8316) serves the per-pass latest-decision-per-anchor
	// listing with a partial anchor-leading index.
	"deployable_unit_gap_admitted_anchor_idx",
	// Migration 199 (#8454) stores the upstream classification probe
	// verdict on repository_selection_observations. No read path change.
	"repository_selection_observation_classification",
	// Migration 200 (#7840) adds the per-selector liveness marker the
	// expired-observation sweep joins to reclaim abandoned not_listed
	// rows. No read path change.
	"repository_selectors",
	// Migration 201 (#8469 step 2) adds the fair-claim priority and
	// ecosystem columns to workflow_work_items with a backfill from the
	// old four-segment fairness key. There is no migration 200 on this
	// lane: the lane's own step-1 200 created the FIFO claim-order index
	// that step 3 drops in 203, and a create the drop file removes would
	// rebuild that index on every untracked replay (the 101/102 shape);
	// the create is gone and 203's drop alone converges the installs
	// that already built it. (Main's 200 is #7840's repository_selectors,
	// kept above; sequence gaps are tolerated.)
	"workflow_work_items_claim_class_columns",
	// Migration 202 (#8469 step 3) builds the priority-then-rotation claim
	// index under a new name, served by the rewritten candidate.
	"workflow_work_items_fair_claim_idx",
	// Migration 203 (#8469 step 3) drops the step-1 FIFO order index the
	// rewritten candidate cannot serve. Nothing creates that name any
	// more, so the drop converges once and is a no-op on every replay.
	"workflow_work_items_claim_order_idx_drop",
	// Migration 204 (#8469 step 2) carries the claim-class backfill 201's
	// lock shape forbids (#7961): 201 holds only the DDL, and this file
	// holds only the two idempotent UPDATEs, so no file holds a table lock
	// through bulk DML.
	"workflow_work_items_claim_class_backfill",
	// Migration 205 (#8470) creates the claim-pacing latch table. It holds
	// only DDL; latch rows appear through runtime upserts, so no backfill
	// can stall writers.
	"workflow_claim_pacing",
	// Migration 206 (#8471, renumbered from 205: base took 205) creates the
	// per-component generation-quota usage windows the quota-gated claim
	// statement reserves in the same statement that grants the claim. DDL
	// only, no backfill: usage rows are created lazily by the grant upsert
	// itself.
	"component_quota_windows",
	// Migration 207 (#7766) creates the repository_retirements tombstone
	// marker table plus its open-row, runnable, scope, and idempotency
	// indexes. Tombstone rows are opened by the operator-driven retirement
	// mark; no backfill exists.
	"repository_retirements",
	// Migration 208 (#8659, Deliverable 2 step 2c) adds the graph-retraction
	// capture columns to the tombstone: the stable copy of the repo root
	// and scope ids every 2c resume and the 2z census read instead of the
	// live rows the purge may already have deleted. DDL only, no backfill:
	// captures are written per retirement by the 2c entry write.
	"repository_retirement_graph_capture",
	// Migration 209 (#8659, Deliverable 2 steps 2b/2b') adds the lease
	// claim-epoch column the shared-projection barrier predicate reads
	// (claimed_at IS NULL OR claimed_at <= intent_barrier_at). DDL only,
	// no backfill: claimed_at is stamped when a new owner takes the lease.
	"partition_lease_claimed_at",
	// Migration 210 (#8780) records the union-base commit a cumulative delta
	// was diffed from, next to its #7319 fence commit. DDL only, no backfill.
	"scope_generations_delta_union_base_commit_sha",
	// Migration 211 (#8803) adds the partial failed_terminal index the
	// collector terminal-failure status read seeks per instance, newest
	// first. Migration-only and built CONCURRENTLY, like migration 156.
	"workflow_work_items_failed_terminal_instance_idx",
	// Migrations 212-216 (#8835) add the supply-chain runtime fallback indexes.
	"supply_chain_identity_workload_trim_index",
	"supply_chain_identity_entity_trim_index",
	"supply_chain_catalog_workload_trim_index",
	"supply_chain_catalog_service_trim_index",
	"supply_chain_cicd_environment_trim_index",
	// Migrations 217-229 (#8880) add the supply-chain impact active-evidence
	// anchors, one sole CREATE INDEX CONCURRENTLY per file, generated from
	// storage/postgres/facts/impact (TestNewIndexStatementsMatchMigrations).
	"fact_records_vulnerability_affected_product_criteria_idx",
	"fact_records_supply_chain_impact_package_id_idx",
	"fact_records_package_version_purl_idx",
	"fact_records_security_alert_cve_id_idx",
	"fact_records_vulnerability_hint_advisory_id_idx",
	"fact_records_oci_image_digest_idx",
	"fact_records_oci_tag_resolved_digest_idx",
	"fact_records_oci_referrer_digest_idx",
	"fact_records_supply_chain_impact_payload_scope_id_idx",
	"fact_records_vulnerability_suppression_active_scan_idx",
	"fact_records_platform_materialization_active_scan_idx",
	"fact_records_jsts_reachability_repo_file_idx",
	"fact_records_supply_chain_impact_foreign_key_guard_idx",
	// Migration 230 (#8943) creates the collector-owned security-alert org read
	// state and its unread-repository child rows; DDL only, no backfill.
	"security_alert_target_read_states",
	// Migration 231 (#8830) creates the collector terminal-failure
	// acknowledgement table the status read anti-joins. DDL only; rows are
	// written by the admin acknowledge route, so no backfill exists.
	"collector_terminal_failure_acknowledgements",
	// Migration 232 (#8946) adds the org fan-out start columns to that read-state
	// row (fanout_start_repository, fanout_next_repository,
	// fanout_lap_repository); an ALTER only, no backfill. 231 belongs to #8830.
	"security_alert_fanout_cursor",
	// Migration 233 (#8840) adds the partial latest-work-item-per-scope index the
	// security-alert collection-state read probes per target. Migration-only
	// and built CONCURRENTLY, like migrations 156 and 211.
	"workflow_work_items_security_alert_scope_updated_idx",
	// Migration 234 (#8833) adds policy_version to the secret-lines state row;
	// 235 replaces eshu_secret_line_findings with the marker arm and the
	// detector extras and moves the state to not_built in one transaction.
	"content_file_secret_lines_policy_version",
	"content_file_secret_lines_marker_policy",
	// Migration 236 (#9146) stores fenced GitHub Actions webhook wake-ups.
	"cicd_actions_deliveries",
	// Migration 237 (#8950) sets content_files fillfactor = 50 (catalog-only
	// ALTER) so the metadata-only rewrite stays HOT on new pages.
	"content_files_fillfactor",
}

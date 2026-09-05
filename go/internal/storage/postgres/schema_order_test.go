// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

var orderedBootstrapDefinitionNames = []string{
	"ingestion_scopes",
	"scope_generations",
	"fact_records",
	"service_catalog_fact_record_indexes",
	"fact_record_sbom_attestation_indexes",
	"eshu_search_index",
	"eshu_search_vector_metadata",
	"eshu_search_vector_values",
	"content_store",
	"fact_work_items",
	"fact_work_item_audit",
	"semantic_extraction_jobs",
	"collector_generation_dead_letters",
	"governance_audit_events",
	"tenant_workspace_grants",
	"generation_retention_events",
	"scoped_api_tokens",
	"identity_subjects",
	"browser_sessions",
	"identity_oidc_login",
	"projection_decisions",
	"admission_decisions",
	"shared_projection_intents",
	"runtime_ingester_control",
	"relationship_tables",
	"shared_projection_acceptance",
	"graph_projection_phase_state",
	"graph_projection_phase_repair_queue",
	"workflow_control_plane",
	"workflow_coordinator_state",
	"deferred_maintenance_barriers",
	"iac_reachability",
	"webhook_refresh_triggers",
	"aws_pagination_checkpoints",
	"aws_scan_status",
	"aws_freshness_triggers",
	"graph_schema_applications",
	"vulnerability_source_states",
	"incident_freshness_triggers",
	"graph_endpoint_presence",
	"service_materialization_generations",
	"service_evidence_snapshots",
	"code_reachability",
	"function_summaries",
	"function_sources",
	"function_graph_ids",
	"admin_replay_requests",
	"value_flow_fixpoint_components",
	"supply_chain_impact_canonical_winners",
	"supply_chain_impact_winners_materialization",
	"content_entities_repo_entity_idx",
	"collector_evidence_summary",
	"eshu_search_index_terms_doc_idx",
	"drop_eshu_search_index_terms_lookup_idx",
	"drop_eshu_search_index_terms_doc_idx",
	"gcp_freshness_triggers",
	"partition_eshu_search_index_terms",
	"search_vector_build_materialization",
	"aws_gcp_freshness_claim_lease",
	"deferred_backfill_partition_memo",
	"dead_letter_poison_idx",
	"code_interproc_projected_edge",
	"code_taint_evidence_projected_node",
	"code_value_flow_backfill_state",
	"projected_source_edge",
	"drop_graph_projection_phase_state_lookup_idx",
	"drop_fact_records_stable_key_idx",
	"provider_config_sealed_secret",
	"identity_bootstrap_credential",
	"identity_sign_in_policy",
	"identity_local_credentials_must_change_password",
	"eshu_search_document_projection_state",
	"eshu_search_vector_scope_state",
	"graph_node_owner",
	"content_substring_index_state",
	"relationship_reference_candidate_keys",
	"relationship_family_candidate_index",
	"reducer_input_invalid_facts",
	"identity_token_metadata_display_label",
	"content_entity_name_trgm_index",
	"identity_github_login",
	"code_root_verdicts",
	"create_documentation_findings_read_idx",
	"create_documentation_findings_filter_idx",
	"iac_active_inventory_index",
	"drop_relationship_family_candidate_index_legacy",
	"fact_records_identity_epoch_idx",
	"cloud_resource_owner_page_index",
	"cloud_resource_owner_provider_page_index",
	"cloud_resource_owner_region_page_index",
	"cloud_resource_owner_account_page_index",
	"graph_node_owner_backfill_state",
	// Both indexes ship under migration prefix 075 (a duplicate-number merge
	// artifact from two independently-landed PRs). BootstrapDefinitions() sorts
	// by migration filename, so "075_fact_records_active_container_image_slsa_idx"
	// (f) deterministically precedes "075_kubernetes_live_pod_template_object_index"
	// (k). This mirror list must follow that same filename order.
	"fact_records_active_container_image_slsa_idx",
	"kubernetes_live_pod_template_object_index",
	// migration 076 (#5476 crossplane SATISFIED_BY cross-scope redrive state);
	// sorts after the 075_* pair by filename.
	"crossplane_satisfied_by_redrive_state",
	// migration 076 (#5460 base-image lineage) DROPs the identity epoch partial
	// index so 077 can recreate it wider (admitting Dockerfile base-image
	// evidence). The DROP ships under 076 and the CREATE under 077 so the DROP
	// deterministically precedes the CREATE by filename; "076_fact_records...drop"
	// is a duplicate-number merge artifact like the 075_* pair above, sorting
	// after "076_crossplane" ("c" < "f").
	"fact_records_identity_epoch_idx_drop",
	// migration 077 (#5490 K8sResource impact-trace candidate scan partial
	// covering index); "077_content..." precedes "077_fact_records..." ("c" < "f").
	"content_entities_k8s_select_partial_index",
	// migration 077 (#5460 base-image lineage) recreates the identity epoch
	// partial index with the wider Dockerfile predicate, after the 076 DROP.
	"fact_records_identity_epoch_idx_dockerfile",
	// migration 078 (#5429 CI/CD run cross-cycle watermark gap detection).
	"cicd_run_watermarks",
	// migrations 079/080 (#5747 current-runtime workload filtering).
	"supply_chain_runtime_filter_workload_index",
	"supply_chain_runtime_filter_entity_keys_index",
	// migration 081 (#5810 cross-scope CI bridge for DERIVED_FROM base-image
	// lineage): a dedicated partial index mirroring 075's SLSA precedent
	// rather than widening the drift-locked identity-epoch index.
	"fact_records_active_container_image_ci_idx",
	// migration 082 (#5810 P1 follow-up): the owner-scoped repository_id
	// lookup index that lets ListActiveContainerImageCIFacts push the
	// calling intent's owning repository into Postgres instead of loading
	// every active CI fact platform-wide and filtering in Go.
	"fact_records_active_container_image_ci_run_repository_idx",
	// migration 083 (#5465 operator suppression expiry read parity).
	"supply_chain_suppression_expiry",
	// migration 084 (#5465 bounded public finding-ID explanation lookup).
	"supply_chain_impact_finding_id_index",
	// migration 085 (#5465 failed suppression lineage retry lookup).
	"vulnerability_suppression_lineage_index",
	// migration 086 (#5469 indexed current runtime-image evidence lookup).
	"cloud_resource_owner_runtime_digest_index",
	// migration 087 (#5848 begin-before-mutate insert-admission watermark for
	// the aws_cloud_runtime_drift reducer writer). Numbered 087, not 086: #5469
	// landed on origin/main after this branch forked and claimed 086 for
	// cloud_resource_owner_runtime_digest_index above. This branch is rebased
	// onto that commit, so both numbers are live in the same tree.
	"aws_cloud_runtime_drift_write_admission",
	// migration 087 (#5854 ordered cross-scope OCI warning safety load).
	"fact_records_active_oci_warning_idx",
	// migration 088 (#5854 old-binary compatibility fence after identity-key cutover).
	"container_image_identity_cutover_guard",
	// migration 088 (#5848/#5837 round-3 review): fact_work_items.reopened_at,
	// a per-cycle readiness-defer anchor that ReopenSucceeded/ReplayDomain reset
	// alongside attempt_count, so a maintenance-triggered reopen gets a fresh
	// grace window instead of inheriting an already-elapsed one anchored on the
	// immutable created_at.
	"reducer_work_item_reopened_at",
	// migration 089 (#5875 P1, round-5 review): a Postgres sequence replacing
	// the host-wall-clock-derived aws_cloud_runtime_drift fencing token, so
	// cross-reducer-replica clock skew can no longer invert the admission
	// CAS's evidence-recency ordering.
	"aws_cloud_runtime_drift_fencing_token_sequence",
	// migration 090 (#5875 P2, round-6 review): the partial index migration
	// 089's own seed query needs, split into its own file because
	// CREATE INDEX CONCURRENTLY cannot share a multi-statement bootstrap file
	// with 089's CREATE SEQUENCE / seeding DO block (ApplyBootstrap sends each
	// file as one statement string; Postgres implicitly transaction-wraps a
	// multi-statement string, and CONCURRENTLY cannot run inside one).
	"fact_records_aws_cloud_runtime_drift_fencing_token_idx",
	// migration 091 (#5593 bounded index for the reducer's config-state-drift
	// catch-up sweep's recurring active state_snapshot scope scan). It was
	// numbered 091 rather than the then-next-available 087 because this branch
	// had already claimed 087-090; with both merged, the range is contiguous.
	"ingestion_scopes_active_state_snapshot_index",
	// migration 092 (#5740 anchor-free digest-v3 identity support store).
	"container_image_identity_support_store",
	"container_image_identity_support_current_view",
	"container_image_identity_current_facts_function",
	"container_image_identity_current_support_facts_function",
	// migration 093 (#5740 durable producer-completion fanout queue).
	"cross_scope_completion_queue",
	// migration 094 (#5740 non-blocking source-intent lookup index).
	"fact_work_items_cross_scope_source_idx",
	// migration 095 (#5740 quiet-upgrade producer replay seed).
	"cross_scope_completion_upgrade_seed",
	// migration 096 (#5827 one-time provenance edge identity rebuild seed).
	"provenance_edge_identity_upgrade_seed",
	// migration 097 (#5704 deterministic digest-v3 identity-strength fold).
	"container_image_identity_strength_precedence",
	// migration 098 (#5984 durable record of intents no edge write could route).
	"shared_projection_unroutable_intents",
	// migration 099 (#6154 keyset index so paging a generation seeks, not rescans).
	"fact_records_keyset_index",
	// migration 101 (#5167 four-column key so a walk step seeks the ACTIVE
	// consumer row instead of scanning every retained generation of a group).
	// There is no migration 100: it created the two-column index 101
	// supersedes, and every file in this directory replays on every bootstrap,
	// so a create the next file drops would rebuild that index on every
	// startup. The create is gone; 102's drop alone converges the installs
	// that already built it.
	"code_reachability_entity_repository_scope_generation_idx",
	// migration 102 (#5167) drops that two-column index: its key is a strict
	// prefix of 101's, so keeping it only costs reducer write amplification.
	"drop_code_reachability_entity_repository_idx",
	// migration 103 (#6527) carries the cross-repo consumer-evidence page's
	// ORDER BY, so that read stops at its own LIMIT instead of ranking a
	// producer entity's whole consumer fan-in first.
	"code_reachability_entity_confidence_rank_idx",
}

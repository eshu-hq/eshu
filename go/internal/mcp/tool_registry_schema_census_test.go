// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"slices"
	"testing"
)

// Extraction input-schema census for eshu-hq/eshu#8873 (eshu-mcp cutover,
// part of #6707).
//
// The name/order census in tool_registry_census_test.go pins which tools exist
// and in what order; this file pins each tool's advertised input surface: the
// sorted InputSchema property names and the sorted required names. A
// schema-only change (added, renamed, or removed argument, flipped required)
// is a contract change for eshu-mcp: it fails the build until the snapshot
// and the destination contract move together.
//
// This closes the gap the ordered-name hash explicitly leaves open ("It is
// not a metadata or input-schema compatibility guard"): the search_file_content
// budget_ms argument shipped without touching the name census, and no gate
// noticed. Property values, descriptions, and nested shapes stay unpinned on
// purpose: names and required-ness are the advertised caller contract, the
// rest is handler detail covered by the per-tool dispatch tests.

// censusToolSchema is one tool's pinned input surface: property and required
// names sorted so map iteration order can never flake the comparison.
type censusToolSchema struct {
	Name       string
	Properties []string
	Required   []string
}

// censusToolSchemaOf extracts the pinned surface from a live ToolDefinition,
// failing closed on any schema shape the pin does not understand.
func censusToolSchemaOf(t *testing.T, tool ToolDefinition) censusToolSchema {
	t.Helper()
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("tool %q InputSchema is %T, want map[string]any", tool.Name, tool.InputSchema)
	}
	props := []string{}
	if raw, exists := schema["properties"]; exists {
		m, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool %q properties is %T, want map[string]any", tool.Name, raw)
		}
		for name := range m {
			props = append(props, name)
		}
	}
	slices.Sort(props)
	required := []string{}
	if raw, exists := schema["required"]; exists {
		switch list := raw.(type) {
		case []any:
			for _, item := range list {
				name, ok := item.(string)
				if !ok {
					t.Fatalf("tool %q required entry is %T, want string", tool.Name, item)
				}
				required = append(required, name)
			}
		case []string:
			required = append(required, list...)
		default:
			t.Fatalf("tool %q required is %T, want []any or []string", tool.Name, raw)
		}
	}
	slices.Sort(required)
	return censusToolSchema{Name: tool.Name, Properties: props, Required: required}
}

// TestToolRegistrySchemaCensusSurface pins the per-tool input-schema surface
// in registration order. Order and count stay owned by the name census; this
// test compares each position's schema so a schema-only drift names the tool
// and the exact property or required change.
func TestToolRegistrySchemaCensusSurface(t *testing.T) {
	t.Parallel()

	tools := ReadOnlyTools()
	if len(tools) != len(censusToolSchemaSnapshot) {
		t.Fatalf("ReadOnlyTools() count = %d, want schema snapshot %d (update the snapshot and the #8873 destination contract together)", len(tools), len(censusToolSchemaSnapshot))
	}
	for i, tool := range tools {
		want := censusToolSchemaSnapshot[i]
		if tool.Name != want.Name {
			t.Fatalf("ReadOnlyTools()[%d] = %q, want snapshot %q (registration order moved: update both censuses together)", i, tool.Name, want.Name)
		}
		got := censusToolSchemaOf(t, tool)
		if !slices.Equal(got.Properties, want.Properties) {
			t.Errorf("tool %q properties = %q, want %q (a schema argument was added, renamed, or removed: update the snapshot and the #8873 destination contract together)", tool.Name, got.Properties, want.Properties)
		}
		if !slices.Equal(got.Required, want.Required) {
			t.Errorf("tool %q required = %q, want %q (required-ness changed: update the snapshot and the #8873 destination contract together)", tool.Name, got.Required, want.Required)
		}
	}
}

// censusToolSchemaSnapshot is the ordered extraction baseline: ReadOnlyTools()
// input surfaces in registration order, transcribed from the live registry.
// Any add/remove/rename of a tool or of one of its schema properties, or any
// required-ness flip, fails TestToolRegistrySchemaCensusSurface until this
// snapshot and the eshu-mcp destination contract move together.
var censusToolSchemaSnapshot = []censusToolSchema{
	{Name: "find_code", Properties: []string{"edit_distance", "exact", "language", "limit", "offset", "query", "rank_cursor", "repo_id", "scope"}, Required: []string{"query"}},
	{Name: "find_symbol", Properties: []string{"entity_type", "entity_types", "language", "limit", "match_mode", "offset", "repo_id", "symbol"}, Required: []string{"symbol"}},
	{Name: "inspect_code_inventory", Properties: []string{"class_name", "decorator", "entity_kind", "file_path", "inventory_kind", "language", "limit", "method_name", "offset", "repo_id", "symbol"}, Required: []string{}},
	{Name: "investigate_import_dependencies", Properties: []string{"language", "limit", "max_cycle_length", "offset", "query_type", "repo_id", "source_file", "source_module", "target_file", "target_module"}, Required: []string{}},
	{Name: "inspect_call_graph_metrics", Properties: []string{"language", "limit", "metric_type", "offset", "repo_id"}, Required: []string{"repo_id"}},
	{Name: "trace_route_callers", Properties: []string{"limit", "max_depth", "method", "path", "repo_id", "service_id", "service_name"}, Required: []string{"path"}},
	{Name: "investigate_code_topic", Properties: []string{"intent", "language", "limit", "offset", "repo_id", "topic"}, Required: []string{"topic"}},
	{Name: "investigate_hardcoded_secrets", Properties: []string{"finding_kinds", "include_suppressed", "language", "limit", "offset", "repo_id"}, Required: []string{}},
	{Name: "get_code_relationship_story", Properties: []string{"direction", "entity_id", "include_transitive", "language", "limit", "max_depth", "min_confidence", "offset", "relationship_type", "relationship_types", "repo_id", "target", "token_budget"}, Required: []string{}},
	{Name: "analyze_code_relationships", Properties: []string{"context", "cross_repo", "end_entity_id", "end_repo_id", "entity_id", "limit", "max_depth", "min_confidence", "offset", "query_type", "relationship_types", "repo_id", "scope", "start_entity_id", "start_repo_id", "target", "token_budget"}, Required: []string{"query_type"}},
	{Name: "find_dead_code", Properties: []string{"exclude_decorated_with", "limit", "repo_id", "scope"}, Required: []string{}},
	{Name: "investigate_dead_code", Properties: []string{"exclude_decorated_with", "language", "limit", "offset", "repo_id"}, Required: []string{}},
	{Name: "find_cross_repo_dead_code", Properties: []string{"consumer_repo_ids", "evidence_detail", "exclude_decorated_with", "language", "limit", "repo_id"}, Required: []string{"repo_id"}},
	{Name: "find_dead_iac", Properties: []string{"families", "include_ambiguous", "limit", "offset", "repo_id", "repo_ids"}, Required: []string{}},
	{Name: "find_unmanaged_resources", Properties: []string{"account_id", "finding_kinds", "limit", "offset", "region", "scope_id"}, Required: []string{}},
	{Name: "get_iac_management_status", Properties: []string{"account_id", "arn", "finding_kinds", "region", "resource_id", "scope_id"}, Required: []string{}},
	{Name: "explain_iac_management_status", Properties: []string{"account_id", "arn", "finding_kinds", "region", "resource_id", "scope_id"}, Required: []string{}},
	{Name: "propose_terraform_import_plan", Properties: []string{"account_id", "arn", "finding_kinds", "limit", "offset", "region", "resource_id", "scope_id"}, Required: []string{}},
	{Name: "compose_replatforming_plan", Properties: []string{"account_id", "arn", "environment", "finding_kinds", "limit", "offset", "region", "repo_id", "resource_id", "scope_id", "scope_kind", "service_name", "workload_id"}, Required: []string{}},
	{Name: "list_aws_runtime_drift_findings", Properties: []string{"account_id", "arn", "finding_kinds", "limit", "offset", "region", "scope_id"}, Required: []string{}},
	{Name: "list_terraform_config_state_drift_findings", Properties: []string{"address", "drift_kinds", "limit", "offset", "outcome", "scope_id"}, Required: []string{"scope_id"}},
	{Name: "get_replatforming_rollups", Properties: []string{"account_id", "finding_kinds", "limit", "offset", "region", "scope_id"}, Required: []string{}},
	{Name: "find_unmanaged_resource_owners", Properties: []string{"account_id", "finding_kinds", "limit", "offset", "region", "scope_id"}, Required: []string{}},
	{Name: "calculate_cyclomatic_complexity", Properties: []string{"entity_id", "function_name", "path", "repo_id", "scope"}, Required: []string{}},
	{Name: "find_most_complex_functions", Properties: []string{"limit", "repo_id"}, Required: []string{}},
	{Name: "inspect_code_quality", Properties: []string{"check", "entity_id", "function_name", "language", "limit", "min_arguments", "min_complexity", "min_lines", "offset", "repo_id"}, Required: []string{}},
	{Name: "find_code_divergence", Properties: []string{"include_tests", "kind", "limit", "offset", "repo_id"}, Required: []string{"repo_id"}},
	{Name: "investigate_code_divergence", Properties: []string{"fingerprint", "include_tests", "kind", "repo_id"}, Required: []string{"fingerprint", "kind", "repo_id"}},
	{Name: "report_code_divergence", Properties: []string{"include_tests", "repo_id", "top_per_kind"}, Required: []string{"repo_id"}},
	{Name: "execute_cypher_query", Properties: []string{"cypher_query", "limit"}, Required: []string{"cypher_query"}},
	{Name: "visualize_graph_query", Properties: []string{"cypher_query", "limit"}, Required: []string{"cypher_query"}},
	{Name: "search_registry_bundles", Properties: []string{"ecosystem", "limit", "query", "unique_only"}, Required: []string{}},
	{Name: "list_indexed_repositories", Properties: []string{"limit", "offset"}, Required: []string{}},
	{Name: "get_repository_stats", Properties: []string{"repo_id"}, Required: []string{}},
	{Name: "execute_language_query", Properties: []string{"entity_type", "language", "limit", "query", "repo_id"}, Required: []string{"entity_type", "language"}},
	{Name: "find_function_call_chain", Properties: []string{"cross_repo", "end", "end_entity_id", "end_repo_id", "max_depth", "repo_id", "start", "start_entity_id", "start_repo_id"}, Required: []string{}},
	{Name: "compare_code_paths", Properties: []string{"end", "end_entity_id", "max_depth", "max_paths", "repo_id", "start", "start_entity_id"}, Required: []string{"repo_id"}},
	{Name: "dispatch_taint_path", Properties: []string{"file_path", "language", "limit", "line", "repo_id", "symbol"}, Required: []string{"repo_id"}},
	{Name: "dispatch_reaching_def", Properties: []string{"file_path", "language", "limit", "line", "repo_id", "symbol"}, Required: []string{"repo_id"}},
	{Name: "dispatch_cfg_summary", Properties: []string{"file_path", "language", "limit", "line", "repo_id", "symbol"}, Required: []string{"repo_id"}},
	{Name: "dispatch_pdg_summary", Properties: []string{"file_path", "language", "limit", "line", "repo_id", "symbol"}, Required: []string{"repo_id"}},
	{Name: "count_repositories_by_language", Properties: []string{"language"}, Required: []string{"language"}},
	{Name: "list_repositories_by_language", Properties: []string{"language", "limit", "offset"}, Required: []string{"language"}},
	{Name: "get_repository_language_inventory", Properties: []string{"limit", "offset"}, Required: []string{}},
	{Name: "get_ecosystem_overview", Properties: []string{}, Required: []string{}},
	{Name: "get_graph_summary_packet", Properties: []string{"limit", "repo_id"}, Required: []string{}},
	{Name: "investigate_contract_impact", Properties: []string{"consumer_repo_id", "family", "limit", "method", "provider_repo_id", "repo_id", "route", "service_name", "topic"}, Required: []string{}},
	{Name: "trace_deployment_chain", Properties: []string{"direct_only", "evidence_detail", "include_related_module_usage", "max_depth", "sections", "service_name"}, Required: []string{"service_name"}},
	{Name: "investigate_deployment_config", Properties: []string{"environment", "limit", "service_name", "workload_id"}, Required: []string{}},
	{Name: "find_blast_radius", Properties: []string{"limit", "target", "target_type"}, Required: []string{"target"}},
	{Name: "find_infra_resources", Properties: []string{"category", "environment", "kind", "limit", "provider", "query", "resource_category", "resource_service"}, Required: []string{}},
	{Name: "investigate_resource", Properties: []string{"environment", "limit", "max_depth", "query", "resource_id", "resource_type"}, Required: []string{}},
	{Name: "analyze_infra_relationships", Properties: []string{"query_type", "target"}, Required: []string{"query_type", "target"}},
	{Name: "get_repo_summary", Properties: []string{"repo_id", "repo_name"}, Required: []string{}},
	{Name: "get_repo_context", Properties: []string{"repo_id"}, Required: []string{"repo_id"}},
	{Name: "get_relationship_evidence", Properties: []string{"resolved_id"}, Required: []string{"resolved_id"}},
	{Name: "analyze_pre_change_impact", Properties: []string{"base_ref", "changed_paths", "changes", "environment", "head_ref", "limit", "max_depth", "module_id", "offset", "repo_id", "resource_id", "service_name", "target", "target_type", "topic", "workload_id"}, Required: []string{}},
	{Name: "plan_developer_change", Properties: []string{"base_ref", "changed_paths", "changes", "developer_intent", "environment", "head_ref", "limit", "max_depth", "module_id", "offset", "repo_id", "resource_id", "service_name", "target", "target_type", "topic", "workload_id"}, Required: []string{}},
	{Name: "list_package_registry_packages", Properties: []string{"ecosystem", "limit", "name", "package_id"}, Required: []string{"limit"}},
	{Name: "list_package_registry_versions", Properties: []string{"limit", "package_id"}, Required: []string{"limit", "package_id"}},
	{Name: "get_repo_story", Properties: []string{"repo_id"}, Required: []string{"repo_id"}},
	{Name: "get_repository_coverage", Properties: []string{"repo_id"}, Required: []string{"repo_id"}},
	{Name: "trace_resource_to_code", Properties: []string{"environment", "limit", "max_depth", "start"}, Required: []string{"start"}},
	{Name: "explain_dependency_path", Properties: []string{"environment", "source", "target"}, Required: []string{"source", "target"}},
	{Name: "find_change_surface", Properties: []string{"environment", "limit", "target"}, Required: []string{"target"}},
	{Name: "investigate_change_surface", Properties: []string{"changed_paths", "environment", "limit", "max_depth", "module_id", "offset", "repo_id", "resource_id", "service_name", "target", "target_type", "topic", "workload_id"}, Required: []string{}},
	{Name: "compare_environments", Properties: []string{"left", "limit", "right", "workload_id"}, Required: []string{"left", "right", "workload_id"}},
	{Name: "count_infra_resources", Properties: []string{"category", "environment", "kind", "provider", "resource_category", "resource_service", "resource_type"}, Required: []string{}},
	{Name: "get_infra_resource_inventory", Properties: []string{"category", "environment", "group_by", "kind", "limit", "offset", "provider", "resource_category", "resource_service", "resource_type"}, Required: []string{}},
	{Name: "list_cloud_resource_inventory", Properties: []string{"account_id", "cursor", "limit", "management_origin", "project_id", "provider", "scope_id", "subscription_id"}, Required: []string{}},
	{Name: "list_cloud_runtime_drift_findings", Properties: []string{"account_id", "cloud_resource_uid", "finding_kinds", "limit", "offset", "project_id", "provider", "scope_id", "subscription_id"}, Required: []string{}},
	{Name: "list_package_registry_dependencies", Properties: []string{"after_dependency_id", "after_version_id", "limit", "package_id", "version_id"}, Required: []string{}},
	{Name: "list_package_registry_correlations", Properties: []string{"after_correlation_id", "limit", "package_id", "relationship_kind", "repository_id"}, Required: []string{}},
	{Name: "list_admission_decisions", Properties: []string{"anchor_id", "anchor_kind", "domain", "generation_id", "include_evidence", "limit", "scope_id", "state"}, Required: []string{"domain", "generation_id", "scope_id"}},
	{Name: "count_package_registry_packages", Properties: []string{"ecosystem", "namespace", "package_manager", "registry", "visibility"}, Required: []string{}},
	{Name: "get_package_registry_package_inventory", Properties: []string{"ecosystem", "group_by", "limit", "namespace", "offset", "package_manager", "registry", "visibility"}, Required: []string{}},
	{Name: "list_ci_cd_run_correlations", Properties: []string{"after_correlation_id", "artifact_digest", "commit_sha", "environment", "image_ref", "limit", "outcome", "provider", "provider_run_id", "repository_id", "scope_id"}, Required: []string{}},
	{Name: "count_ci_cd_run_correlations", Properties: []string{"artifact_digest", "commit_sha", "environment", "image_ref", "outcome", "provider", "repository_id", "scope_id"}, Required: []string{}},
	{Name: "get_ci_cd_run_correlation_inventory", Properties: []string{"artifact_digest", "commit_sha", "environment", "group_by", "image_ref", "limit", "offset", "outcome", "provider", "repository_id", "scope_id"}, Required: []string{}},
	{Name: "list_service_catalog_correlations", Properties: []string{"after_correlation_id", "drift_status", "entity_ref", "limit", "outcome", "owner_ref", "provider", "repository_id", "scope_id", "service_id", "workload_id"}, Required: []string{}},
	{Name: "list_codeowners_ownership", Properties: []string{"after_order_index", "after_pattern", "after_ref", "limit", "repository_id"}, Required: []string{"repository_id"}},
	{Name: "list_kubernetes_correlations", Properties: []string{"after_correlation_id", "cluster_id", "drift_kind", "image_ref", "limit", "namespace", "outcome", "scope_id", "source_digest", "workload_object_id"}, Required: []string{}},
	{Name: "list_secrets_iam_identity_trust_chains", Properties: []string{"after_chain_id", "chain_id", "iam_role_fingerprint", "limit", "scope_id", "service_account_join_key", "state", "workload_object_id"}, Required: []string{}},
	{Name: "list_secrets_iam_privilege_posture_observations", Properties: []string{"after_observation_id", "limit", "observation_id", "risk_type", "scope_id", "severity", "state"}, Required: []string{}},
	{Name: "list_secrets_iam_secret_access_paths", Properties: []string{"after_path_id", "chain_id", "limit", "path_id", "scope_id", "state", "vault_mount_join_key"}, Required: []string{}},
	{Name: "list_secrets_iam_posture_gaps", Properties: []string{"after_gap_id", "gap_id", "gap_type", "limit", "scope_id", "service_account_join_key", "state"}, Required: []string{}},
	{Name: "count_secrets_iam_posture", Properties: []string{"scope_id"}, Required: []string{"scope_id"}},
	{Name: "list_observability_coverage_correlations", Properties: []string{"after_correlation_id", "coverage_signal", "coverage_status", "limit", "observability_object_ref", "outcome", "provider", "resource_class", "scope_id", "source_class", "target_service_ref", "target_uid"}, Required: []string{}},
	{Name: "get_vulnerability_scanner_read_contract", Properties: []string{"route"}, Required: []string{}},
	{Name: "list_container_image_identities", Properties: []string{"after_identity_id", "digest", "image_ref", "limit", "outcome", "repository_id", "source_repository_id"}, Required: []string{}},
	{Name: "list_container_image_tag_history", Properties: []string{"cursor", "limit", "offset", "repository_id", "tag"}, Required: []string{"repository_id", "tag"}},
	{Name: "list_supply_chain_impact_findings", Properties: []string{"advisory_id", "after_finding_id", "cve_id", "ecosystem", "environment", "ghsa_id", "image_ref", "impact_status", "include_suppressed", "limit", "min_priority_score", "osv_id", "package_id", "priority_bucket", "profile", "repository_id", "service_id", "severity", "sort", "subject_digest", "suppression_state", "workload_id"}, Required: []string{}},
	{Name: "list_advisory_evidence", Properties: []string{"advisory_id", "after_advisory_key", "cve_id", "limit", "package_id", "repository_id", "service_id", "source", "workload_id"}, Required: []string{}},
	{Name: "explain_supply_chain_impact", Properties: []string{"advisory_id", "cve_id", "finding_id", "image_ref", "package_id", "repository_id", "service_id", "subject_digest", "workload_id"}, Required: []string{}},
	{Name: "list_security_alert_reconciliations", Properties: []string{"after_reconciliation_id", "cve_id", "ghsa_id", "limit", "package_id", "provider", "provider_state", "reconciliation_status", "repository_id"}, Required: []string{}},
	{Name: "list_sbom_attestation_attachments", Properties: []string{"after_attachment_id", "artifact_kind", "attachment_status", "digest", "document_digest", "document_id", "limit", "repository_id", "service_id", "subject_digest", "workload_id"}, Required: []string{}},
	{Name: "count_supply_chain_impact_findings", Properties: []string{"advisory_id", "cve_id", "ecosystem", "environment", "ghsa_id", "image_ref", "impact_status", "include_suppressed", "min_priority_score", "osv_id", "package_id", "priority_bucket", "profile", "repository_id", "service_id", "severity", "subject_digest", "suppression_state", "workload_id"}, Required: []string{}},
	{Name: "get_supply_chain_impact_inventory", Properties: []string{"advisory_id", "cve_id", "ecosystem", "environment", "ghsa_id", "group_by", "image_ref", "impact_status", "include_suppressed", "limit", "min_priority_score", "offset", "osv_id", "package_id", "priority_bucket", "profile", "repository_id", "service_id", "severity", "subject_digest", "suppression_state", "workload_id"}, Required: []string{}},
	{Name: "count_security_alert_reconciliations", Properties: []string{"cve_id", "ghsa_id", "package_id", "provider", "provider_state", "reconciliation_status", "repository_id"}, Required: []string{}},
	{Name: "get_security_alert_reconciliation_inventory", Properties: []string{"cve_id", "ghsa_id", "group_by", "limit", "offset", "package_id", "provider", "provider_state", "reconciliation_status", "repository_id"}, Required: []string{}},
	{Name: "count_container_image_identities", Properties: []string{"digest", "image_ref", "outcome", "repository_id", "source_repository_id"}, Required: []string{}},
	{Name: "get_container_image_identity_inventory", Properties: []string{"digest", "group_by", "image_ref", "limit", "offset", "outcome", "repository_id", "source_repository_id"}, Required: []string{}},
	{Name: "count_sbom_attestation_attachments", Properties: []string{"artifact_kind", "attachment_status", "document_digest", "document_id", "repository_id", "service_id", "subject_digest", "workload_id"}, Required: []string{}},
	{Name: "get_sbom_attestation_attachment_inventory", Properties: []string{"artifact_kind", "attachment_status", "document_digest", "document_id", "group_by", "limit", "offset", "repository_id", "service_id", "subject_digest", "workload_id"}, Required: []string{}},
	{Name: "get_incident_context", Properties: []string{"limit", "provider", "provider_incident_id", "scope_id", "service_id", "since", "until"}, Required: []string{"provider_incident_id"}},
	{Name: "list_work_item_evidence", Properties: []string{"after_fact_id", "external_url", "limit", "observed_after", "project_key", "provider_work_item_id", "scope_id", "url_fingerprint", "work_item_key"}, Required: []string{}},
	{Name: "derive_visualization_packet", Properties: []string{"source_response", "source_truth", "view"}, Required: []string{"source_response", "view"}},
	{Name: "get_generation_lifecycle", Properties: []string{"collector_kind", "generation_id", "limit", "repository", "scope_id", "source_system", "status"}, Required: []string{}},
	{Name: "get_changed_since", Properties: []string{"repository", "sample_limit", "scope_id", "since_generation_id", "since_observed_at"}, Required: []string{}},
	{Name: "get_repository_freshness", Properties: []string{"expected_commit", "repo_id"}, Required: []string{"repo_id"}},
	{Name: "get_service_changed_since", Properties: []string{"sample_limit", "scope_id", "service_id", "since_generation_id"}, Required: []string{"service_id", "since_generation_id"}},
	{Name: "resolve_entity", Properties: []string{"limit", "query", "repo_id", "type", "types"}, Required: []string{"query"}},
	{Name: "get_entity_context", Properties: []string{"entity_id", "environment"}, Required: []string{"entity_id"}},
	{Name: "get_workload_context", Properties: []string{"environment", "evidence_detail", "workload_id"}, Required: []string{"workload_id"}},
	{Name: "get_workload_story", Properties: []string{"environment", "workload_id"}, Required: []string{"workload_id"}},
	{Name: "get_service_context", Properties: []string{"environment", "evidence_detail", "workload_id"}, Required: []string{"workload_id"}},
	{Name: "get_service_story", Properties: []string{"environment", "evidence_detail", "repo", "repo_id", "repository_id", "service_id", "service_name", "workload_id"}, Required: []string{}},
	{Name: "investigate_service", Properties: []string{"environment", "intent", "question", "repo", "repo_id", "repository_id", "service_id", "service_name"}, Required: []string{}},
	{Name: "get_service_intelligence_report", Properties: []string{"environment", "repo", "repo_id", "repository_id", "service_id", "service_name", "workload_id"}, Required: []string{}},
	{Name: "get_file_content", Properties: []string{"relative_path", "repo_id"}, Required: []string{"relative_path", "repo_id"}},
	{Name: "get_file_lines", Properties: []string{"end_line", "relative_path", "repo_id", "start_line"}, Required: []string{"end_line", "relative_path", "repo_id", "start_line"}},
	{Name: "get_entity_content", Properties: []string{"entity_id"}, Required: []string{"entity_id"}},
	{Name: "build_evidence_citation_packet", Properties: []string{"handles", "limit", "question", "subject"}, Required: []string{"handles"}},
	{Name: "search_file_content", Properties: []string{"budget_ms", "cursor", "limit", "offset", "pattern", "repo_ids"}, Required: []string{"pattern"}},
	{Name: "search_entity_content", Properties: []string{"cursor", "limit", "offset", "pattern", "rank_cursor", "repo_ids"}, Required: []string{"pattern"}},
	{Name: "list_documentation_findings", Properties: []string{"cursor", "document_id", "finding_type", "freshness_state", "generation_id", "limit", "repo", "scope_id", "service_id", "source_id", "status", "target_id", "target_kind", "truth_level", "updated_since"}, Required: []string{}},
	{Name: "list_documentation_facts", Properties: []string{"cursor", "document_id", "fact_kind", "generation_id", "limit", "q", "repo", "scope_id", "section_id", "service_id", "source_id", "target_id", "target_kind", "updated_since"}, Required: []string{}},
	{Name: "get_documentation_evidence_packet", Properties: []string{"finding_id"}, Required: []string{"finding_id"}},
	{Name: "check_documentation_evidence_packet_freshness", Properties: []string{"packet_id", "packet_version"}, Required: []string{"packet_id"}},
	{Name: "list_query_playbooks", Properties: []string{"limit", "offset", "view"}, Required: []string{}},
	{Name: "resolve_query_playbook", Properties: []string{"inputs", "playbook_id"}, Required: []string{"playbook_id"}},
	{Name: "list_investigation_workflows", Properties: []string{"limit", "offset", "view"}, Required: []string{}},
	{Name: "resolve_investigation_workflow", Properties: []string{"inputs", "missing_evidence", "workflow_id"}, Required: []string{"workflow_id"}},
	{Name: "export_supply_chain_impact_packet", Properties: []string{"advisory_id", "cve_id", "finding_id", "image_ref", "max_source_facts", "package_id", "repository_id", "service_id", "subject_digest", "workload_id"}, Required: []string{}},
	{Name: "export_deployable_unit_packet", Properties: []string{"generation_id", "max_source_facts", "repo_id", "repository_id", "scope_id"}, Required: []string{"generation_id", "scope_id"}},
	{Name: "export_cloud_runtime_drift_packet", Properties: []string{"account_id", "cloud_resource_uid", "max_source_facts", "project_id", "provider", "scope_id", "subscription_id"}, Required: []string{}},
	{Name: "list_semantic_documentation_observations", Properties: []string{"admission_state", "cursor", "document_id", "extraction_mode", "fact_id", "freshness_state", "generation_id", "limit", "observation_type", "policy_state", "prompt_version", "provider_kind", "provider_profile_id", "q", "redaction_state", "redaction_version", "repo", "scope_id", "section_id", "service_id", "source_class", "source_id", "target_id", "target_kind", "updated_since"}, Required: []string{}},
	{Name: "list_semantic_code_hints", Properties: []string{"corroboration_state", "cursor", "entity_id", "extraction_mode", "fact_id", "freshness_state", "generation_id", "hint_type", "limit", "policy_state", "prompt_version", "provider_kind", "provider_profile_id", "q", "redaction_state", "redaction_version", "relationship_kind", "relative_path", "repo", "scope_id", "service_id", "source_class", "source_id", "target_id", "target_kind", "updated_since"}, Required: []string{}},
	{Name: "search_semantic_context", Properties: []string{"environment", "languages", "limit", "mode", "query", "repo_id", "rerank", "service_id", "source_kinds", "timeout_ms", "workload_id"}, Required: []string{"limit", "mode", "query", "repo_id", "timeout_ms"}},
	{Name: "count_documentation_findings", Properties: []string{"document_id", "finding_type", "freshness_state", "scope_id", "source_id", "status", "truth_level"}, Required: []string{}},
	{Name: "get_documentation_finding_inventory", Properties: []string{"document_id", "finding_type", "freshness_state", "group_by", "limit", "offset", "scope_id", "source_id", "status", "truth_level"}, Required: []string{}},
	{Name: "list_component_extensions", Properties: []string{"limit"}, Required: []string{}},
	{Name: "get_component_extension_diagnostics", Properties: []string{"component_id"}, Required: []string{"component_id"}},
	{Name: "list_collector_extraction_readiness", Properties: []string{"limit"}, Required: []string{}},
	{Name: "get_collector_extraction_readiness", Properties: []string{"family"}, Required: []string{"family"}},
	{Name: "list_fact_schema_versions", Properties: []string{"limit"}, Required: []string{}},
	{Name: "get_fact_schema_version", Properties: []string{"candidate", "fact_kind"}, Required: []string{"fact_kind"}},
	{Name: "list_collectors", Properties: []string{}, Required: []string{}},
	{Name: "list_ingesters", Properties: []string{}, Required: []string{}},
	{Name: "get_ingester_status", Properties: []string{"ingester"}, Required: []string{}},
	{Name: "get_index_status", Properties: []string{}, Required: []string{}},
	{Name: "get_hosted_readiness", Properties: []string{}, Required: []string{}},
	{Name: "get_collector_readiness", Properties: []string{}, Required: []string{}},
	{Name: "get_operator_control_plane", Properties: []string{}, Required: []string{}},
	{Name: "list_dead_letter_work_items", Properties: []string{"collector_kind", "domain", "failure_class", "limit", "scope_id", "timeout_ms", "updated_after", "updated_before"}, Required: []string{"limit", "timeout_ms"}},
	{Name: "list_reducer_input_invalid_facts", Properties: []string{"domain", "fact_kind", "generation_id", "limit", "scope_id", "timeout_ms"}, Required: []string{"generation_id", "limit", "scope_id", "timeout_ms"}},
	{Name: "list_changed_since_poisoned_links", Properties: []string{"cursor", "limit", "scope_id", "status", "timeout_ms"}, Required: []string{"limit", "timeout_ms"}},
	{Name: "get_freshness_causality", Properties: []string{}, Required: []string{}},
	{Name: "get_hosted_governance_status", Properties: []string{}, Required: []string{}},
	{Name: "get_semantic_capability_status", Properties: []string{}, Required: []string{}},
	{Name: "get_answer_narration_status", Properties: []string{}, Required: []string{}},
	{Name: "get_capability_catalog", Properties: []string{"include_authorization", "limit", "maturity", "offset", "owner", "view"}, Required: []string{}},
	{Name: "get_surface_inventory", Properties: []string{"category", "limit", "offset", "readiness"}, Required: []string{}},
	{Name: "trace_exposure_path", Properties: []string{"max_depth", "repo_id", "source", "source_entity_id"}, Required: []string{}},
	{Name: "ask", Properties: []string{"format", "question"}, Required: []string{"question"}},
	{Name: "list_relationship_edges", Properties: []string{"limit", "source_tool", "verb"}, Required: []string{"verb"}},
	{Name: "list_repository_files", Properties: []string{"language", "path", "recursive", "ref", "repo_id"}, Required: []string{"repo_id"}},
	{Name: "list_runtime_version_declarations", Properties: []string{"cursor", "limit", "major_below", "repo_ids", "runtime", "source_kind", "version_below"}, Required: []string{}},
	{Name: "search_dockerfile_installs", Properties: []string{"cursor", "limit", "package", "repo_id", "repo_ids"}, Required: []string{"package"}},
}

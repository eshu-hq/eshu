// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "slices"

// toolFamily is a group of overlapping MCP tools: tools that answer near
// versions of the same question, so a router needs a tie-break among them.
type toolFamily struct {
	// Name is the stable family identifier shown in the README table.
	Name string
	// Members are the MCP tool names in the family. A tool belongs to at most
	// one family.
	Members []string
	// Reason is the one-line reason the canonical member is tried first.
	Reason string
}

// toolFamilies is the single list of overlapping-tool families. The tests that
// enforce "exactly one canonical tool per family" and "no shared intent inside a
// family" read it, and the README family table must match it row for row. The
// canonical member is the one whose intents use canonicalIntents. The table is
// a package variable so the function stays short; callers get a copy and may
// not change the shared rows.
func toolFamilies() []toolFamily {
	return slices.Clone(toolFamilyTable)
}

var toolFamilyTable = []toolFamily{
	{
		"code-lookup",
		[]string{"find_symbol", "find_code", "resolve_entity", "execute_language_query", "search_entity_content", "search_file_content", "inspect_code_inventory", "investigate_code_topic"},
		"find_symbol is symbol-aware (type, language, exact or fuzzy match), low cost, and returns source handles",
	},
	{
		"source-read",
		[]string{"get_file_content", "get_file_lines", "get_entity_content", "list_repository_files"},
		"a repository plus path needs no earlier lookup; the others need a line range, an entity id, or a listing first",
	},
	{
		"code-relationships",
		[]string{"get_code_relationship_story", "analyze_code_relationships", "find_function_call_chain", "compare_code_paths", "investigate_import_dependencies", "inspect_call_graph_metrics", "trace_route_callers"},
		"resolves ambiguity first, then returns callers, callees, and imports with provenance in one bounded call",
	},
	{
		"dead-code",
		[]string{"investigate_dead_code", "find_dead_code", "find_cross_repo_dead_code", "find_dead_iac"},
		"adds coverage, language maturity, and exactness blockers, so a candidate is never reported as certain without them",
	},
	{
		"code-quality",
		[]string{"inspect_code_quality", "find_most_complex_functions", "calculate_cyclomatic_complexity"},
		"one bounded tool covers complexity, length, argument count, and combined refactoring candidates",
	},
	{
		"code-divergence",
		[]string{"report_code_divergence", "find_code_divergence", "investigate_code_divergence"},
		"the one-call rollup gives counts and top findings per kind; the other two drill into it",
	},
	{
		"repository-overview",
		[]string{"get_repo_story", "get_repo_summary", "get_repo_context", "get_repository_stats", "get_repository_coverage"},
		"answers \"what does this repo do\" in one call; summary, context, stats, and coverage are narrower slices",
	},
	{
		"ecosystem-overview",
		[]string{"get_ecosystem_overview", "get_graph_summary_packet"},
		"covers repos, tiers, infrastructure counts, and cross-repo links; the packet is a graph-shaped summary",
	},
	{
		"repository-language",
		[]string{"list_repositories_by_language", "count_repositories_by_language", "get_repository_language_inventory"},
		"rows answer which repos; the count and inventory tools carry how-many and breakdown phrases; the MCP guide's question table lists the count first for a how-many question, which its own phrases already win, but rows are the broader first try",
	},
	{
		"service-workload",
		[]string{"get_service_story", "investigate_service", "get_service_intelligence_report", "get_service_context", "get_workload_context", "get_workload_story"},
		"the one-call service dossier covers identity, API surface, deployment lanes, dependencies, and consumers",
	},
	{
		"change-impact",
		[]string{"investigate_change_surface", "find_blast_radius", "find_change_surface", "analyze_pre_change_impact", "plan_developer_change", "investigate_contract_impact", "explain_dependency_path"},
		"one bounded call spans code, repositories, workloads, infrastructure, and transitive impact for a service, module, resource, topic, or path set",
	},
	{
		"changed-since",
		[]string{"get_changed_since", "get_service_changed_since"},
		"repository and scope diffs are the general case; the service form needs a service materialization generation",
	},
	{
		"deployment",
		[]string{"trace_deployment_chain", "investigate_deployment_config", "compare_environments"},
		"answers \"how is this deployed\" end to end; config and environment comparison are follow-ups",
	},
	{
		"infra-resource-search",
		[]string{"find_infra_resources", "count_infra_resources", "get_infra_resource_inventory"},
		"rows answer which resources; the count and inventory tools carry how-many and grouping phrases",
	},
	{
		"infra-resource-trace",
		[]string{"investigate_resource", "analyze_infra_relationships", "trace_resource_to_code"},
		"resolves a queue, database, cloud, or Terraform resource into users, provisioning repositories, and next calls",
	},
	{
		"vulnerability-impact",
		[]string{"list_supply_chain_impact_findings", "count_supply_chain_impact_findings", "get_supply_chain_impact_inventory", "explain_supply_chain_impact", "list_advisory_evidence", "export_supply_chain_impact_packet"},
		"returns the reducer-owned impact findings with CVE, package, repository, and image anchors; the others count, group, explain, or export them",
	},
	{
		"security-alert-reconciliation",
		[]string{"list_security_alert_reconciliations", "count_security_alert_reconciliations", "get_security_alert_reconciliation_inventory"},
		"rows answer which alerts; the count and inventory tools carry how-many and grouping phrases",
	},
	{
		"sbom-attestation",
		[]string{"list_sbom_attestation_attachments", "count_sbom_attestation_attachments", "get_sbom_attestation_attachment_inventory"},
		"rows answer which images have an SBOM or attestation; the count and inventory tools carry how-many and grouping phrases",
	},
	{
		"container-image-identity",
		[]string{"list_container_image_identities", "count_container_image_identities", "get_container_image_identity_inventory"},
		"rows answer which image a reference or digest is; the count and inventory tools carry how-many and grouping phrases",
	},
	{
		"package-registry-packages",
		[]string{"list_package_registry_packages", "count_package_registry_packages", "get_package_registry_package_inventory", "search_registry_bundles"},
		"rows answer which packages exist; the count and inventory tools carry how-many and grouping phrases, and bundle search is the PURL path",
	},
	{
		"ci-cd",
		[]string{"list_ci_cd_run_correlations", "count_ci_cd_run_correlations", "get_ci_cd_run_correlation_inventory"},
		"rows answer which runs built or deployed an artifact; the count and inventory tools carry how-many and grouping phrases",
	},
	{
		"documentation-findings",
		[]string{"list_documentation_findings", "count_documentation_findings", "get_documentation_finding_inventory"},
		"rows answer which findings exist; the count and inventory tools carry how-many and grouping phrases",
	},
	{
		"secrets-iam-posture",
		[]string{"count_secrets_iam_posture", "list_secrets_iam_identity_trust_chains", "list_secrets_iam_privilege_posture_observations", "list_secrets_iam_secret_access_paths", "list_secrets_iam_posture_gaps"},
		"one call summarizes all four record kinds and shows which list to open",
	},
	{
		"runtime-drift",
		[]string{"list_cloud_runtime_drift_findings", "list_aws_runtime_drift_findings", "export_cloud_runtime_drift_packet"},
		"aggregates AWS, GCP, and Azure drift in one query; the AWS list is its single-provider subset",
	},
	{
		"iac-management",
		[]string{"find_unmanaged_resources", "find_unmanaged_resource_owners", "get_iac_management_status", "explain_iac_management_status"},
		"scope-level entry point; the other three drill into owners or one resource",
	},
	{
		"replatforming",
		[]string{"compose_replatforming_plan", "get_replatforming_rollups", "propose_terraform_import_plan"},
		"composes the whole plan with waves, safety gates, and owner candidates; rollups and import candidates are slices of it",
	},
	{
		"runtime-health",
		[]string{"get_operator_control_plane", "get_hosted_readiness", "get_index_status", "get_collector_readiness"},
		"one call returns queue depth, reducer backlogs, collector verdicts, and dead-letter state",
	},
	{
		"ingester-status",
		[]string{"list_ingesters", "get_ingester_status"},
		"the list covers every configured ingester and the one-ingester call needs a name first",
	},
	{
		"extraction-readiness",
		[]string{"list_collector_extraction_readiness", "get_collector_extraction_readiness"},
		"the catalog covers every collector family and the drilldown needs a family name first",
	},
	{
		"fact-schema",
		[]string{"list_fact_schema_versions", "get_fact_schema_version"},
		"the registry covers every core fact kind; the single-kind call is for a candidate version check",
	},
	{
		"capability-catalog",
		[]string{"get_capability_catalog", "get_surface_inventory"},
		"carries maturity, owner, gaps, and authorization per capability; the inventory is the raw surface list",
	},
	{
		"call-plans",
		[]string{"resolve_query_playbook", "resolve_investigation_workflow", "list_query_playbooks", "list_investigation_workflows"},
		"turns a question and its inputs into an ordered, bounded call sequence without running it",
	},
	{
		"failed-work",
		[]string{"list_dead_letter_work_items", "list_reducer_input_invalid_facts", "list_changed_since_poisoned_links"},
		"dead letters are the durable record of failed queue work; the other two cover narrower failure kinds",
	},
	{
		"freshness",
		[]string{"get_freshness_causality", "get_repository_freshness", "get_generation_lifecycle"},
		"explains why answers are stale by closed cause; the others report one repository or one scope",
	},
}

// sharedIntentAllowed lists intent phrases that two tools in one family may
// share, keyed by phrase with the reason. The README must explain each entry.
// It is empty: every phrase is unique inside its family.
var sharedIntentAllowed = map[string]string{}

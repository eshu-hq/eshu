// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// The tables below enumerate every validator divergence the runtime capture
// found (go/internal/mcp/router/README.md, "Known divergence from a direct
// call"). Each row was checked by replaying the route dispatch builds against
// the real API router: a required-field row records the handler's own 400
// text for the tool's catalog-sweep arguments with only those fields removed.

// requiredFieldRow is one tool and the exact set of schema-required fields a
// rejected map lacks.
type requiredFieldRow struct {
	tool     string
	missing  []string
	evidence string
}

// handlerRejectedRequired (class C): the handler refuses the call too, or the
// route 404s on an empty path segment, which direct
// dispatch reports as an error as well. Rows whose evidence starts with
// "direct dispatch:" are path ids: resolveRoute refuses the empty segment
// before any request is built (#8772), so no handler runs and the call never
// reaches the validator comparison;
// TestDirectCallRefusesEmptyPathSegmentRows holds them to that.
var handlerRejectedRequired = []requiredFieldRow{
	{"analyze_infra_relationships", []string{"query_type", "target"}, "POST /api/v0/infra/relationships: 400 entity_id is required"},
	{"ask", []string{"question"}, "POST /api/v0/ask: query/ask Handler answers 400 question is required"},
	{"compare_code_paths", []string{"repo_id"}, "POST /api/v0/code/call-chain/compare: 400 repo_id is required"},
	{"compare_environments", []string{"left", "right", "workload_id"}, "POST /api/v0/compare/environments: 400 workload_id is required"},
	{"count_repositories_by_language", []string{"language"}, "GET /api/v0/repositories/by-language: 400 language is required"},
	{"count_secrets_iam_posture", []string{"scope_id"}, "GET /api/v0/secrets-iam/posture-summary: 400 scope_id is required"},
	{"derive_visualization_packet", []string{"source_response", "view"}, "POST /api/v0/visualizations/derive: 400 view is required"},
	{"dispatch_cfg_summary", []string{"repo_id"}, "POST /api/v0/code/flow/cfg-summary: 400 repo_id is required"},
	{"dispatch_pdg_summary", []string{"repo_id"}, "POST /api/v0/code/flow/pdg-summary: 400 repo_id is required"},
	{"dispatch_reaching_def", []string{"repo_id"}, "POST /api/v0/code/flow/reaching-def: 400 repo_id is required"},
	{"dispatch_taint_path", []string{"repo_id"}, "POST /api/v0/code/flow/taint-path: 400 repo_id is required"},
	{"execute_cypher_query", []string{"cypher_query"}, "POST /api/v0/code/cypher: 400 cypher_query is required"},
	{"execute_language_query", []string{"entity_type", "language"}, "POST /api/v0/code/language-query: 400 language is required"},
	{"explain_dependency_path", []string{"source", "target"}, "POST /api/v0/impact/explain-dependency-path: 400 source is required"},
	{"export_deployable_unit_packet", []string{"generation_id", "scope_id"}, "GET /api/v0/investigations/deployable-unit/packet: 400 scope_id and generation_id are required"},
	{"find_blast_radius", []string{"target"}, "POST /api/v0/impact/blast-radius: 400 target is required"},
	{"find_change_surface", []string{"target"}, "POST /api/v0/impact/change-surface: 400 target is required"},
	{"find_code", []string{"query"}, "POST /api/v0/code/search: 400 query is required"},
	{"find_code_divergence", []string{"repo_id"}, "POST /api/v0/code/divergence/findings: 400 repo_id is required"},
	{"find_cross_repo_dead_code", []string{"repo_id"}, "POST /api/v0/code/dead-code/cross-repo: 400 repo_id is required"},
	{"find_symbol", []string{"symbol"}, "POST /api/v0/code/symbols/search: 400 symbol is required"},
	{"get_entity_content", []string{"entity_id"}, "POST /api/v0/content/entities/read: 400 entity_id is required"},
	{"get_entity_context", []string{"entity_id"}, "direct dispatch: entity_id is required, no handler runs (GET /api/v0/entities//context)"},
	{"get_file_content", []string{"relative_path", "repo_id"}, "POST /api/v0/content/files/read: 400 repo_id is required"},
	{"get_file_lines", []string{"end_line", "relative_path", "repo_id", "start_line"}, "POST /api/v0/content/files/lines: 400 repo_id is required"},
	{"get_incident_context", []string{"provider_incident_id"}, "direct dispatch: provider_incident_id is required, no handler runs (GET /api/v0/incidents//context)"},
	{"get_repository_coverage", []string{"repo_id"}, "direct dispatch: repo_id is required, no handler runs (GET /api/v0/repositories//coverage)"},
	{"get_service_changed_since", []string{"service_id", "since_generation_id"}, "GET /api/v0/freshness/services/changed-since: 400 service_id is required"},
	{"get_workload_context", []string{"workload_id"}, "direct dispatch: workload_id is required, no handler runs (GET /api/v0/workloads//context)"},
	{"get_workload_story", []string{"workload_id"}, "direct dispatch: workload_id is required, no handler runs (GET /api/v0/workloads//story)"},
	{"inspect_call_graph_metrics", []string{"repo_id"}, "POST /api/v0/code/call-graph/metrics: 400 repo_id is required"},
	{"investigate_code_divergence", []string{"fingerprint", "kind"}, "POST /api/v0/code/divergence/investigate: 400 kind must be one of: "},
	{"investigate_code_divergence", []string{"fingerprint", "kind", "repo_id"}, "POST /api/v0/code/divergence/investigate: 400 repo_id is required"},
	{"investigate_code_topic", []string{"topic"}, "POST /api/v0/code/topics/investigate: 400 topic is required"},
	{"list_admission_decisions", []string{"domain", "generation_id", "scope_id"}, "GET /api/v0/evidence/admission-decisions: 400 domain, scope_id, and generation_id are required"},
	{"list_codeowners_ownership", []string{"repository_id"}, "GET /api/v0/codeowners/ownership: 400 repository_id is required"},
	{"list_container_image_tag_history", []string{"repository_id", "tag"}, "GET /api/v0/images/tag-history: 400 repository_id and tag are required and repository_id must be an oci-registry:// id"},
	{"list_container_image_tag_history", []string{"tag"}, "GET /api/v0/images/tag-history: 400 repository_id and tag are required and repository_id must be an oci-registry:// id"},
	{"list_package_registry_versions", []string{"limit", "package_id"}, "GET /api/v0/package-registry/versions: 400 package_id is required"},
	{"list_relationship_edges", []string{"verb"}, "POST /api/v0/relationships/edges: 400 verb is required"},
	{"list_repositories_by_language", []string{"language"}, "GET /api/v0/repositories/by-language: 400 language is required"},
	{"list_terraform_config_state_drift_findings", []string{"scope_id"}, "POST /api/v0/terraform/config-state-drift/findings: 400 scope_id is required"},
	{"report_code_divergence", []string{"repo_id"}, "POST /api/v0/code/divergence/report: 400 repo_id is required"},
	{"resolve_entity", []string{"query"}, "POST /api/v0/entities/resolve: 400 name is required"},
	{"resolve_investigation_workflow", []string{"workflow_id"}, "POST /api/v0/investigation-workflows/resolve: 400 workflow_id is required"},
	{"resolve_query_playbook", []string{"playbook_id"}, "POST /api/v0/query-playbooks/resolve: 400 playbook_id is required"},
	{"search_entity_content", []string{"pattern"}, "POST /api/v0/content/entities/search: 400 query is required"},
	{"search_file_content", []string{"pattern"}, "POST /api/v0/content/files/search: 400 query is required"},
	{"search_semantic_context", []string{"limit", "mode", "query", "repo_id", "timeout_ms"}, "POST /api/v0/search/semantic: 400 repo_id is required"},
	{"trace_deployment_chain", []string{"service_name"}, "POST /api/v0/impact/trace-deployment-chain: 400 service_name is required"},
	{"trace_resource_to_code", []string{"start"}, "POST /api/v0/impact/trace-resource-to-code: 400 start is required"},
	{"trace_route_callers", []string{"path"}, "POST /api/v0/code/routes/callers: 400 path is required"},
	{"visualize_graph_query", []string{"cypher_query"}, "POST /api/v0/code/visualize: 400 cypher_query is required"},
}

// handlerOptionalRequired (class F, a divergence): the schema marks the field
// required, but dispatch defaults it or the handler serves the call without
// it.
var handlerOptionalRequired = []requiredFieldRow{
	{"analyze_code_relationships", []string{"query_type"}, "mcp/relationships resolveAnalyzeCodeRelationshipsRequest sends POST /api/v0/code/relationships with query_type empty; the handler runs the lookup"},
	{"analyze_infra_relationships", []string{"query_type"}, "the route sends relationship_type empty; query infra_relationship_filter treats relationship_type as optional and returns every relationship"},
	{"build_evidence_citation_packet", []string{"handles"}, "query/evidence normalizeEvidenceCitationRequest accepts no handles and the handler returns an empty packet"},
	{"list_package_registry_packages", []string{"limit"}, "the route defaults limit to 50, so requiredPackageRegistryLimit always sees one"},
}

// enumFieldRow is one off-enum field a handler rejects.
type enumFieldRow struct {
	field    string
	evidence string
}

// handlerRejectedEnums (class C): off-enum values the handler answers with 400.
var handlerRejectedEnums = map[string]enumFieldRow{
	"count_ci_cd_run_correlations":              {"outcome", "GET /api/v0/ci-cd/run-correlations/count: 400 outcome must be one of exact, derived, ambiguous, unresolved, rejected"},
	"count_sbom_attestation_attachments":        {"attachment_status", "GET /api/v0/supply-chain/sbom-attestations/attachments/count: 400 attachment_status must be one of ..."},
	"find_blast_radius":                         {"target_type", "POST /api/v0/impact/blast-radius: 400 unsupported target_type"},
	"get_ci_cd_run_correlation_inventory":       {"outcome", "GET /api/v0/ci-cd/run-correlations/inventory: 400 outcome must be one of ..."},
	"get_sbom_attestation_attachment_inventory": {"attachment_status", "GET /api/v0/supply-chain/sbom-attestations/attachments/inventory: 400 attachment_status must be one of ..."},
	"get_workload_context":                      {"evidence_detail", "GET /api/v0/workloads/{id}/context: 400 invalid context evidence_detail"},
}

// defaultedTypeFields (class E, a divergence): fields where a wrong JSON type
// reaches dispatch, whose argument helpers (routecontract Arguments String,
// IntOr, BoolOr, StringSlice; mcp str, intOr, boolOr) drop it or use the
// default, so a direct call can run with the default. The capture replayed
// list_indexed_repositories {"limit":"25"} past validation.
var defaultedTypeFields = map[string][]string{
	"calculate_cyclomatic_complexity":                 {"entity_id", "repo_id"},
	"compose_replatforming_plan":                      {"finding_kinds", "limit", "offset", "scope_kind"},
	"count_ci_cd_run_correlations":                    {"environment"},
	"count_container_image_identities":                {"digest", "repository_id"},
	"count_package_registry_packages":                 {"ecosystem"},
	"count_secrets_iam_posture":                       {"scope_id"},
	"count_security_alert_reconciliations":            {"repository_id"},
	"count_supply_chain_impact_findings":              {"include_suppressed", "repository_id"},
	"dispatch_cfg_summary":                            {"file_path", "limit", "line", "repo_id", "symbol"},
	"dispatch_pdg_summary":                            {"file_path", "limit", "line", "repo_id", "symbol"},
	"dispatch_reaching_def":                           {"file_path", "limit", "line", "repo_id", "symbol"},
	"dispatch_taint_path":                             {"file_path", "limit", "line", "repo_id", "symbol"},
	"explain_dependency_path":                         {"environment"},
	"explain_supply_chain_impact":                     {"finding_id", "repository_id"},
	"find_blast_radius":                               {"limit"},
	"find_change_surface":                             {"environment", "limit"},
	"find_code_divergence":                            {"include_tests", "kind", "limit", "offset", "repo_id"},
	"find_cross_repo_dead_code":                       {"consumer_repo_ids", "exclude_decorated_with", "limit", "repo_id"},
	"find_dead_code":                                  {"exclude_decorated_with", "limit", "repo_id"},
	"find_infra_resources":                            {"environment", "kind", "limit", "provider", "query"},
	"find_most_complex_functions":                     {"limit", "repo_id"},
	"get_ci_cd_run_correlation_inventory":             {"environment", "group_by", "limit", "offset"},
	"get_container_image_identity_inventory":          {"digest", "group_by", "limit", "offset", "repository_id"},
	"get_entity_content":                              {"entity_id"},
	"get_entity_context":                              {"environment"},
	"get_package_registry_package_inventory":          {"ecosystem", "group_by", "limit", "offset"},
	"get_replatforming_rollups":                       {"finding_kinds", "limit", "offset"},
	"get_sbom_attestation_attachment_inventory":       {"group_by", "limit"},
	"get_security_alert_reconciliation_inventory":     {"group_by", "limit", "offset", "repository_id"},
	"get_supply_chain_impact_inventory":               {"group_by", "include_suppressed", "limit", "offset", "repository_id"},
	"inspect_code_quality":                            {"check", "entity_id", "limit", "min_complexity", "offset", "repo_id"},
	"investigate_change_surface":                      {"environment", "limit", "max_depth", "service_name"},
	"investigate_code_divergence":                     {"include_tests", "kind", "repo_id"},
	"investigate_contract_impact":                     {"limit", "service_name"},
	"investigate_dead_code":                           {"exclude_decorated_with", "limit", "offset", "repo_id"},
	"investigate_deployment_config":                   {"environment", "limit", "service_name"},
	"list_admission_decisions":                        {"anchor_kind", "include_evidence", "limit", "scope_id", "state"},
	"list_advisory_evidence":                          {"advisory_id", "limit", "source"},
	"list_ci_cd_run_correlations":                     {"environment", "limit", "provider_run_id"},
	"list_codeowners_ownership":                       {"after_order_index", "after_pattern", "after_ref", "limit", "repository_id"},
	"list_container_image_identities":                 {"digest", "limit", "repository_id"},
	"list_container_image_tag_history":                {"limit", "offset", "repository_id", "tag"},
	"list_indexed_repositories":                       {"limit"},
	"list_kubernetes_correlations":                    {"cluster_id", "limit", "outcome", "scope_id", "workload_object_id"},
	"list_observability_coverage_correlations":        {"coverage_signal", "limit", "provider", "scope_id", "target_uid"},
	"list_package_registry_correlations":              {"limit", "package_id"},
	"list_package_registry_dependencies":              {"limit", "package_id"},
	"list_package_registry_packages":                  {"ecosystem", "limit", "package_id"},
	"list_package_registry_versions":                  {"limit", "package_id"},
	"list_sbom_attestation_attachments":               {"limit"},
	"list_secrets_iam_identity_trust_chains":          {"chain_id", "limit", "scope_id"},
	"list_secrets_iam_posture_gaps":                   {"limit", "scope_id"},
	"list_secrets_iam_privilege_posture_observations": {"limit", "scope_id", "severity"},
	"list_secrets_iam_secret_access_paths":            {"chain_id", "limit", "scope_id"},
	"list_security_alert_reconciliations":             {"limit", "repository_id"},
	"list_supply_chain_impact_findings":               {"include_suppressed", "limit", "repository_id"},
	"report_code_divergence":                          {"include_tests", "repo_id"},
	"resolve_entity":                                  {"limit", "query", "repo_id", "type", "types"},
	"trace_deployment_chain":                          {"direct_only", "max_depth", "service_name"},
	"trace_exposure_path":                             {"max_depth"},
	"trace_resource_to_code":                          {"environment", "limit", "max_depth"},
}

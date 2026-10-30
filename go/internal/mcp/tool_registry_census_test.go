// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// Extraction census for eshu-hq/eshu#8873 (eshu-mcp cutover, part of #6707).
//
// This file pins the assembled tool registry of the mcp-server binary as the
// migration baseline: tool names, registration order, schemas, per-call
// limits, dispatch timeout/budget, and the data envelope. The destination
// eshu-mcp repository must reproduce this surface exactly; any drift here
// (added, removed, renamed, or reordered tool) fails the build so the
// extractor updates the census and the destination content plan together.
//
// What this pins per binary surface:
//   - ReadOnlyTools: the full tools/list registry the server advertises on
//     the default surface (router find_tool/call_tool are separate).
//   - router.Tools: find_tool + call_tool with DefaultLimit/MaxLimit.
//   - dispatchTool defaults: timeout, response budget, and the
//     application/eshu.envelope+json Accept envelope on every dispatch.

// censusToolCount is the number of tools ReadOnlyTools assembles. A new tool
// registration must update this census and the #8873 destination content plan
// in the same change.
const censusToolCount = 171

// TestToolRegistryCensusCount pins the registry size first (TDD red gate:
// this fails while the census is being established).
func TestToolRegistryCensusCount(t *testing.T) {
	t.Parallel()

	if got := len(ReadOnlyTools()); got != censusToolCount {
		t.Fatalf("ReadOnlyTools() count = %d, want census %d (update the census snapshot and the #8873 destination plan together)", got, censusToolCount)
	}
}

// TestToolRegistryCensusOrder pins the exact registration order. Order is
// part of the contract: the core_router position pins and the advertised
// tools/list payload must stay byte-stable for the extraction.
func TestToolRegistryCensusOrder(t *testing.T) {
	t.Parallel()

	tools := ReadOnlyTools()
	if len(tools) != len(censusToolNames) {
		t.Fatalf("ReadOnlyTools() count = %d, want census snapshot %d", len(tools), len(censusToolNames))
	}
	for i, tool := range tools {
		if tool.Name != censusToolNames[i] {
			t.Fatalf("ReadOnlyTools()[%d] = %q, want census %q", i, tool.Name, censusToolNames[i])
		}
	}
}

// TestToolRegistryCensusDefinitionsWellFormed pins the per-tool contract
// every extraction consumer relies on: unique names, a description, and an
// object input schema.
func TestToolRegistryCensusDefinitionsWellFormed(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int, len(censusToolNames))
	for i, tool := range ReadOnlyTools() {
		if tool.Name == "" {
			t.Errorf("ReadOnlyTools()[%d] has an empty name", i)
		}
		if first, dup := seen[tool.Name]; dup {
			t.Errorf("tool %q registered twice (positions %d and %d)", tool.Name, first, i)
		}
		seen[tool.Name] = i
		if tool.Description == "" {
			t.Errorf("tool %q has an empty description", tool.Name)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("tool %q schema is %T, want map[string]any", tool.Name, tool.InputSchema)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q schema type = %v, want object", tool.Name, schema["type"])
		}
	}
}

// TestToolRegistryCensusDispatchBounds pins the per-call limits every tool
// dispatch runs under: the timeout and the response byte budget.
func TestToolRegistryCensusDispatchBounds(t *testing.T) {
	t.Parallel()

	if defaultToolDispatchTimeout != 30*time.Second {
		t.Errorf("defaultToolDispatchTimeout = %v, want 30s", defaultToolDispatchTimeout)
	}
	if defaultToolResponseByteBudget != 256*1024 {
		t.Errorf("defaultToolResponseByteBudget = %d, want %d", defaultToolResponseByteBudget, 256*1024)
	}
}

// TestToolRegistryCensusRouterSurface pins the router tools the server
// intercepts before dispatchTool, with their candidate-count limits.
func TestToolRegistryCensusRouterSurface(t *testing.T) {
	t.Parallel()

	tools := router.Tools()
	if len(tools) != 2 {
		t.Fatalf("router.Tools() count = %d, want 2 (find_tool, call_tool)", len(tools))
	}
	if tools[0].Name != router.FindToolName || tools[1].Name != router.CallToolName {
		t.Fatalf("router.Tools() order = [%q %q], want [find_tool call_tool]", tools[0].Name, tools[1].Name)
	}
	if router.DefaultLimit != 5 {
		t.Errorf("router.DefaultLimit = %d, want 5", router.DefaultLimit)
	}
	if router.MaxLimit != 10 {
		t.Errorf("router.MaxLimit = %d, want 10", router.MaxLimit)
	}
	for _, name := range []string{router.FindToolName, router.CallToolName} {
		if !router.Intercepts(name) {
			t.Errorf("router.Intercepts(%q) = false, want true", name)
		}
	}
}

// TestToolRegistryCensusDispatchEnvelope pins the data envelope behaviorally:
// every dispatch asks the internal handler for the canonical eshu envelope.
func TestToolRegistryCensusDispatchEnvelope(t *testing.T) {
	t.Parallel()

	var gotAccept string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// list_component_extensions resolves with empty args (see
	// component_extensions_test.go), so the stub handler observes a real
	// dispatch path without needing backend stores.
	if _, err := dispatchTool(context.Background(), handler, "list_component_extensions", map[string]any{}, "", logger); err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if gotAccept != "application/eshu.envelope+json" {
		t.Errorf("dispatch Accept header = %q, want application/eshu.envelope+json", gotAccept)
	}
}

// censusToolNames is the ordered extraction baseline: ReadOnlyTools() names
// in registration order, dumped from the live registry. The order test fails
// on any add/remove/rename/reorder so the destination eshu-mcp plan stays in
// lockstep with this registry.
var censusToolNames = []string{ //nolint:gochecknoglobals // pinned extraction baseline
	"find_code",
	"find_symbol",
	"inspect_code_inventory",
	"investigate_import_dependencies",
	"inspect_call_graph_metrics",
	"trace_route_callers",
	"investigate_code_topic",
	"investigate_hardcoded_secrets",
	"get_code_relationship_story",
	"analyze_code_relationships",
	"find_dead_code",
	"investigate_dead_code",
	"find_cross_repo_dead_code",
	"find_dead_iac",
	"find_unmanaged_resources",
	"get_iac_management_status",
	"explain_iac_management_status",
	"propose_terraform_import_plan",
	"compose_replatforming_plan",
	"list_aws_runtime_drift_findings",
	"list_terraform_config_state_drift_findings",
	"get_replatforming_rollups",
	"find_unmanaged_resource_owners",
	"calculate_cyclomatic_complexity",
	"find_most_complex_functions",
	"inspect_code_quality",
	"find_code_divergence",
	"investigate_code_divergence",
	"report_code_divergence",
	"execute_cypher_query",
	"visualize_graph_query",
	"search_registry_bundles",
	"list_indexed_repositories",
	"get_repository_stats",
	"execute_language_query",
	"find_function_call_chain",
	"compare_code_paths",
	"dispatch_taint_path",
	"dispatch_reaching_def",
	"dispatch_cfg_summary",
	"dispatch_pdg_summary",
	"count_repositories_by_language",
	"list_repositories_by_language",
	"get_repository_language_inventory",
	"get_ecosystem_overview",
	"get_graph_summary_packet",
	"investigate_contract_impact",
	"trace_deployment_chain",
	"investigate_deployment_config",
	"find_blast_radius",
	"find_infra_resources",
	"investigate_resource",
	"analyze_infra_relationships",
	"get_repo_summary",
	"get_repo_context",
	"get_relationship_evidence",
	"analyze_pre_change_impact",
	"plan_developer_change",
	"list_package_registry_packages",
	"list_package_registry_versions",
	"get_repo_story",
	"get_repository_coverage",
	"trace_resource_to_code",
	"explain_dependency_path",
	"find_change_surface",
	"investigate_change_surface",
	"compare_environments",
	"count_infra_resources",
	"get_infra_resource_inventory",
	"list_cloud_resource_inventory",
	"list_cloud_runtime_drift_findings",
	"list_package_registry_dependencies",
	"list_package_registry_correlations",
	"list_admission_decisions",
	"count_package_registry_packages",
	"get_package_registry_package_inventory",
	"list_ci_cd_run_correlations",
	"count_ci_cd_run_correlations",
	"get_ci_cd_run_correlation_inventory",
	"list_service_catalog_correlations",
	"list_codeowners_ownership",
	"list_kubernetes_correlations",
	"list_secrets_iam_identity_trust_chains",
	"list_secrets_iam_privilege_posture_observations",
	"list_secrets_iam_secret_access_paths",
	"list_secrets_iam_posture_gaps",
	"count_secrets_iam_posture",
	"list_observability_coverage_correlations",
	"get_vulnerability_scanner_read_contract",
	"list_container_image_identities",
	"list_container_image_tag_history",
	"list_supply_chain_impact_findings",
	"list_advisory_evidence",
	"explain_supply_chain_impact",
	"list_security_alert_reconciliations",
	"list_sbom_attestation_attachments",
	"count_supply_chain_impact_findings",
	"get_supply_chain_impact_inventory",
	"count_security_alert_reconciliations",
	"get_security_alert_reconciliation_inventory",
	"count_container_image_identities",
	"get_container_image_identity_inventory",
	"count_sbom_attestation_attachments",
	"get_sbom_attestation_attachment_inventory",
	"get_incident_context",
	"list_work_item_evidence",
	"derive_visualization_packet",
	"get_generation_lifecycle",
	"get_changed_since",
	"get_repository_freshness",
	"get_service_changed_since",
	"resolve_entity",
	"get_entity_context",
	"get_workload_context",
	"get_workload_story",
	"get_service_context",
	"get_service_story",
	"investigate_service",
	"get_service_intelligence_report",
	"get_file_content",
	"get_file_lines",
	"get_entity_content",
	"build_evidence_citation_packet",
	"search_file_content",
	"search_entity_content",
	"list_documentation_findings",
	"list_documentation_facts",
	"get_documentation_evidence_packet",
	"check_documentation_evidence_packet_freshness",
	"list_query_playbooks",
	"resolve_query_playbook",
	"list_investigation_workflows",
	"resolve_investigation_workflow",
	"export_supply_chain_impact_packet",
	"export_deployable_unit_packet",
	"export_cloud_runtime_drift_packet",
	"list_semantic_documentation_observations",
	"list_semantic_code_hints",
	"search_semantic_context",
	"count_documentation_findings",
	"get_documentation_finding_inventory",
	"list_component_extensions",
	"get_component_extension_diagnostics",
	"list_collector_extraction_readiness",
	"get_collector_extraction_readiness",
	"list_fact_schema_versions",
	"get_fact_schema_version",
	"list_collectors",
	"list_ingesters",
	"get_ingester_status",
	"get_index_status",
	"get_hosted_readiness",
	"get_collector_readiness",
	"get_operator_control_plane",
	"list_dead_letter_work_items",
	"list_reducer_input_invalid_facts",
	"list_changed_since_poisoned_links",
	"get_freshness_causality",
	"get_hosted_governance_status",
	"get_semantic_capability_status",
	"get_answer_narration_status",
	"get_capability_catalog",
	"get_surface_inventory",
	"trace_exposure_path",
	"ask",
	"list_relationship_edges",
	"list_repository_files",
	"list_runtime_version_declarations",
	"search_dockerfile_installs",
	"search_php_configuration",
	"list_ssm_host_inventory",
}

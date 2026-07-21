// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestReadOnlyTools(t *testing.T) {
	tools := ReadOnlyTools()

	expectedCount := 161
	if len(tools) != expectedCount {
		t.Errorf("Expected %d tools, got %d", expectedCount, len(tools))
	}

	// Verify all tools have required fields
	for i, tool := range tools {
		if tool.Name == "" {
			t.Errorf("Tool %d has empty name", i)
		}
		if tool.Description == "" {
			t.Errorf("Tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("Tool %s has nil InputSchema", tool.Name)
		}
	}

	// Verify some expected tool names
	expectedTools := []string{
		"find_code",
		"find_symbol",
		"inspect_code_inventory",
		"investigate_import_dependencies",
		"inspect_call_graph_metrics",
		"dispatch_taint_path",
		"dispatch_reaching_def",
		"dispatch_cfg_summary",
		"dispatch_pdg_summary",
		"trace_route_callers",
		"investigate_code_topic",
		"investigate_hardcoded_secrets",
		"get_code_relationship_story",
		"analyze_code_relationships",
		"investigate_dead_code",
		"find_cross_repo_dead_code",
		"inspect_code_quality",
		"find_dead_iac",
		"find_unmanaged_resources",
		"get_iac_management_status",
		"explain_iac_management_status",
		"propose_terraform_import_plan",
		"compose_replatforming_plan",
		"list_aws_runtime_drift_findings",
		"get_replatforming_rollups",
		"find_unmanaged_resource_owners",
		"get_ecosystem_overview",
		"get_graph_summary_packet",
		"investigate_contract_impact",
		"get_relationship_evidence",
		"list_admission_decisions",
		"build_evidence_citation_packet",
		"analyze_pre_change_impact",
		"plan_developer_change",
		"investigate_change_surface",
		"investigate_deployment_config",
		"investigate_resource",
		"investigate_service",
		"get_incident_context",
		"list_work_item_evidence",
		"derive_visualization_packet",
		"list_package_registry_packages",
		"list_package_registry_versions",
		"list_package_registry_dependencies",
		"list_package_registry_correlations",
		"list_ci_cd_run_correlations",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_observability_coverage_correlations",
		"list_container_image_identities",
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_supply_chain_impact_findings",
		"explain_supply_chain_impact",
		"list_security_alert_reconciliations",
		"list_sbom_attestation_attachments",
		"resolve_entity",
		"get_file_content",
		"list_documentation_findings",
		"list_documentation_facts",
		"list_semantic_documentation_observations",
		"list_semantic_code_hints",
		"search_semantic_context",
		"get_documentation_evidence_packet",
		"check_documentation_evidence_packet_freshness",
		"list_query_playbooks",
		"resolve_query_playbook",
		"list_investigation_workflows",
		"resolve_investigation_workflow",
		"export_supply_chain_impact_packet",
		"export_deployable_unit_packet",
		"export_cloud_runtime_drift_packet",
		"get_hosted_readiness",
		"get_operator_control_plane",
		"list_dead_letter_work_items",
		"get_freshness_causality",
		"get_hosted_governance_status",
		"get_semantic_capability_status",
		"get_answer_narration_status",
		"get_capability_catalog",
		"list_component_extensions",
		"get_component_extension_diagnostics",
		"list_collector_extraction_readiness",
		"get_collector_extraction_readiness",
		"list_collectors",
		"list_ingesters",
		"count_repositories_by_language",
		"list_repositories_by_language",
		"get_repository_language_inventory",
		"ask",
		"list_relationship_edges",
		"list_repository_files",
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("Expected tool %s not found", expected)
		}
	}
}

func TestRelationshipToolsAdvertiseMinConfidence(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"get_code_relationship_story", "analyze_code_relationships"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		raw, ok := properties["min_confidence"]
		if !ok {
			t.Fatalf("tool %s missing min_confidence schema", name)
		}
		field, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool %s min_confidence schema type = %T, want map[string]any", name, raw)
		}
		if got, want := field["type"], "number"; got != want {
			t.Fatalf("tool %s min_confidence type = %#v, want %#v", name, got, want)
		}
		if got, want := field["minimum"], float64(0); got != want {
			t.Fatalf("tool %s min_confidence minimum = %#v, want %#v", name, got, want)
		}
		if got, want := field["maximum"], float64(1); got != want {
			t.Fatalf("tool %s min_confidence maximum = %#v, want %#v", name, got, want)
		}
	}
}

func TestRelationshipStoryToolsAdvertiseProvenanceOutput(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"get_code_relationship_story", "analyze_code_relationships"} {
		tool := requireToolDefinition(t, name)
		description := strings.ToLower(tool.Description)
		for _, want := range []string{"relationship", "provenance"} {
			if !strings.Contains(description, want) {
				t.Fatalf("tool %s description missing %q: %s", name, want, tool.Description)
			}
		}
	}
}

func requireToolDefinition(t *testing.T, name string) ToolDefinition {
	t.Helper()

	for _, tool := range ReadOnlyTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return ToolDefinition{}
}

func TestPackageRegistryDependencyToolLimitDefaultIsOptional(t *testing.T) {
	t.Parallel()

	tools := packageRegistryTools()
	if got, want := len(tools), 2; got != want {
		t.Fatalf("len(packageRegistryTools()) = %d, want %d", got, want)
	}
	schema, ok := tools[0].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tools[0].InputSchema)
	}
	required, _ := schema["required"].([]string)
	for _, field := range required {
		if field == "limit" {
			t.Fatalf("required = %#v, want limit omitted because schema default is informational", required)
		}
	}
}

func TestPackageRegistryPackageToolNamesHexEcosystemScope(t *testing.T) {
	t.Parallel()

	tools := ecosystemTools()
	var packageTool ToolDefinition
	for _, tool := range tools {
		if tool.Name == "list_package_registry_packages" {
			packageTool = tool
			break
		}
	}
	schema, ok := packageTool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", packageTool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	ecosystem, ok := properties["ecosystem"].(map[string]any)
	if !ok {
		t.Fatalf("ecosystem schema type = %T, want map[string]any", properties["ecosystem"])
	}
	description, _ := ecosystem["description"].(string)
	if !strings.Contains(description, "hex") {
		t.Fatalf("ecosystem description = %q, want Hex named among package-registry scopes", description)
	}
	if !strings.Contains(packageTool.Description, "identity_issues") ||
		!strings.Contains(packageTool.Description, "malformed") {
		t.Fatalf("description = %q, want malformed identity_issues contract", packageTool.Description)
	}
}

func TestPackageRegistryCorrelationToolAllowsPublicationRelationship(t *testing.T) {
	t.Parallel()

	tools := packageRegistryTools()
	schema, ok := tools[1].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tools[1].InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	relationshipKind, ok := properties["relationship_kind"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_kind schema type = %T, want map[string]any", properties["relationship_kind"])
	}
	enum, ok := relationshipKind["enum"].([]string)
	if !ok {
		t.Fatalf("relationship_kind enum type = %T, want []string", relationshipKind["enum"])
	}
	if !stringSliceContains(enum, "publication") {
		t.Fatalf("relationship_kind enum = %#v, want publication", enum)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCodebaseTools(t *testing.T) {
	tools := codebaseTools()
	if len(tools) != 33 {
		t.Errorf("Expected 33 codebase tools, got %d", len(tools))
	}
}

func TestEcosystemTools(t *testing.T) {
	tools := ecosystemTools()
	if len(tools) != 23 {
		t.Errorf("Expected 23 ecosystem tools, got %d", len(tools))
	}
}

func TestRepositoryLanguageTools(t *testing.T) {
	tools := repositoryLanguageTools()
	if len(tools) != 3 {
		t.Errorf("Expected 3 repository language tools, got %d", len(tools))
	}
}

func TestContextTools(t *testing.T) {
	tools := contextTools()
	if len(tools) != 7 {
		t.Errorf("Expected 7 context tools, got %d", len(tools))
	}
}

func TestContentTools(t *testing.T) {
	tools := contentTools()
	if len(tools) != 6 {
		t.Errorf("Expected 6 content tools, got %d", len(tools))
	}
}

func TestRuntimeTools(t *testing.T) {
	tools := runtimeTools()
	if len(tools) != 15 {
		t.Errorf("Expected 15 runtime tools, got %d", len(tools))
	}
}

func TestDocumentationTools(t *testing.T) {
	tools := documentationTools()
	if len(tools) != 4 {
		t.Errorf("Expected 4 documentation tools, got %d", len(tools))
	}
}

func TestQueryPlaybookTools(t *testing.T) {
	tools := queryPlaybookTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 query playbook tools, got %d", len(tools))
	}
}

func TestInvestigationWorkflowTools(t *testing.T) {
	tools := investigationWorkflowTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 investigation workflow tools, got %d", len(tools))
	}
}

func TestSemanticEvidenceTools(t *testing.T) {
	tools := semanticEvidenceTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 semantic evidence tools, got %d", len(tools))
	}
}

func TestComponentExtensionTools(t *testing.T) {
	tools := componentExtensionTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 component extension tools, got %d", len(tools))
	}
}

func TestEveryRegisteredToolHasDispatchRoute(t *testing.T) {
	tools := ReadOnlyTools()
	for _, tool := range tools {
		// Provide minimal args so resolveRoute can build a route.
		args := minimalDispatchRouteArgs(tool.Name)
		_, err := resolveRoute(tool.Name, args)
		if err != nil {
			t.Errorf("tool %q is registered but has no dispatch route: %v", tool.Name, err)
		}
	}
}

func minimalDispatchRouteArgs(toolName string) map[string]any {
	switch toolName {
	case "get_service_context", "get_service_story", "get_service_intelligence_report":
		return map[string]any{"workload_id": "sample-service-api"}
	case "get_component_extension_diagnostics":
		return map[string]any{"component_id": "dev.eshu.collector.aws"}
	case "get_collector_extraction_readiness":
		return map[string]any{"family": "pagerduty"}
	case "list_dead_letter_work_items":
		return map[string]any{"limit": 10, "timeout_ms": 5000}
	case "list_reducer_input_invalid_facts":
		return map[string]any{"scope_id": "sample-scope", "generation_id": "sample-generation", "limit": 10, "timeout_ms": 5000}
	case "get_fact_schema_version":
		return map[string]any{"fact_kind": "terraform_state_resource"}
	case "get_repo_summary":
		// get_repo_summary requires at least one of repo_id or repo_name;
		// supply the canonical selector so a route can be built.
		return map[string]any{"repo_id": "sample-repo"}
	case "list_relationship_edges":
		return map[string]any{"verb": "DEPENDS_ON"}
	case "list_repository_files", "get_repo_context", "get_repo_story",
		"get_repository_coverage", "get_repository_freshness":
		return map[string]any{"repo_id": "sample-repo"}
	case "get_entity_context":
		return map[string]any{"entity_id": "sample-entity"}
	case "get_workload_context", "get_workload_story":
		return map[string]any{"workload_id": "sample-workload"}
	case "get_incident_context":
		return map[string]any{"provider_incident_id": "sample-incident"}
	case "investigate_service":
		return map[string]any{"service_name": "sample-service-api"}
	case "get_relationship_evidence":
		return map[string]any{"resolved_id": "sample-resolved-id"}
	case "get_documentation_evidence_packet":
		return map[string]any{"finding_id": "sample-finding-id"}
	case "check_documentation_evidence_packet_freshness":
		return map[string]any{"packet_id": "sample-packet-id"}
	default:
		return map[string]any{}
	}
}

func TestReadOnlyToolsAdvertiseRepositoryLanguageInventoryWithoutCoverageFanout(t *testing.T) {
	tools := ReadOnlyTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.Name == "list_repository_coverage" {
			t.Fatal("unexpected list_repository_coverage tool in read-only tool set")
		}
	}
	for _, want := range []string{
		"count_repositories_by_language",
		"list_repositories_by_language",
		"get_repository_language_inventory",
	} {
		if !names[want] {
			t.Fatalf("missing repository language tool %q", want)
		}
	}
}

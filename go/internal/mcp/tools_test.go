package mcp

import (
	"testing"
)

func TestReadOnlyTools(t *testing.T) {
	tools := ReadOnlyTools()

	expectedCount := 66
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
		"investigate_code_topic",
		"investigate_hardcoded_secrets",
		"get_code_relationship_story",
		"analyze_code_relationships",
		"investigate_dead_code",
		"inspect_code_quality",
		"find_dead_iac",
		"find_unmanaged_resources",
		"get_iac_management_status",
		"explain_iac_management_status",
		"propose_terraform_import_plan",
		"list_aws_runtime_drift_findings",
		"get_ecosystem_overview",
		"get_relationship_evidence",
		"build_evidence_citation_packet",
		"investigate_change_surface",
		"investigate_deployment_config",
		"investigate_resource",
		"investigate_service",
		"list_package_registry_packages",
		"list_package_registry_versions",
		"list_package_registry_dependencies",
		"list_package_registry_correlations",
		"list_ci_cd_run_correlations",
		"list_sbom_attestation_attachments",
		"resolve_entity",
		"get_file_content",
		"list_ingesters",
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
	if len(tools) != 27 {
		t.Errorf("Expected 27 codebase tools, got %d", len(tools))
	}
}

func TestEcosystemTools(t *testing.T) {
	tools := ecosystemTools()
	if len(tools) != 19 {
		t.Errorf("Expected 19 ecosystem tools, got %d", len(tools))
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
	if len(tools) != 3 {
		t.Errorf("Expected 3 runtime tools, got %d", len(tools))
	}
}

func TestEveryRegisteredToolHasDispatchRoute(t *testing.T) {
	tools := ReadOnlyTools()
	for _, tool := range tools {
		// Provide minimal args so resolveRoute can build a route.
		args := map[string]any{}
		_, err := resolveRoute(tool.Name, args)
		if err != nil {
			t.Errorf("tool %q is registered but has no dispatch route: %v", tool.Name, err)
		}
	}
}

func TestReadOnlyToolsDoNotAdvertiseUnsupportedCoverageListing(t *testing.T) {
	tools := ReadOnlyTools()
	for _, tool := range tools {
		if tool.Name == "list_repository_coverage" {
			t.Fatal("unexpected list_repository_coverage tool in read-only tool set")
		}
	}
}

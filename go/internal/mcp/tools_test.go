package mcp

import (
	"testing"
)

func TestReadOnlyTools(t *testing.T) {
	tools := ReadOnlyTools()

	expectedCount := 41
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
		"analyze_code_relationships",
		"find_dead_iac",
		"get_ecosystem_overview",
		"get_relationship_evidence",
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

func TestCodebaseTools(t *testing.T) {
	tools := codebaseTools()
	if len(tools) != 13 {
		t.Errorf("Expected 13 codebase tools, got %d", len(tools))
	}
}

func TestEcosystemTools(t *testing.T) {
	tools := ecosystemTools()
	if len(tools) != 14 {
		t.Errorf("Expected 14 ecosystem tools, got %d", len(tools))
	}
}

func TestContextTools(t *testing.T) {
	tools := contextTools()
	if len(tools) != 6 {
		t.Errorf("Expected 6 context tools, got %d", len(tools))
	}
}

func TestContentTools(t *testing.T) {
	tools := contentTools()
	if len(tools) != 5 {
		t.Errorf("Expected 5 content tools, got %d", len(tools))
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

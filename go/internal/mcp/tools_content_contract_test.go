package mcp

import "testing"

func TestSearchContentToolsAdvertiseMaxOffset(t *testing.T) {
	t.Parallel()

	for _, tool := range contentTools() {
		if tool.Name != "search_file_content" && tool.Name != "search_entity_content" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s InputSchema type = %T, want map", tool.Name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties type = %T, want map", tool.Name, schema["properties"])
		}
		offset, ok := properties["offset"].(map[string]any)
		if !ok {
			t.Fatalf("%s offset schema type = %T, want map", tool.Name, properties["offset"])
		}
		if got, want := offset["maximum"], 10000; got != want {
			t.Fatalf("%s offset maximum = %#v, want %#v", tool.Name, got, want)
		}
	}
}

func TestSearchContentToolsDoNotAdvertiseUnsupportedFilters(t *testing.T) {
	t.Parallel()

	unsupported := []string{"languages", "artifact_types", "template_dialects", "iac_relevant", "entity_types"}
	for _, tool := range contentTools() {
		if tool.Name != "search_file_content" && tool.Name != "search_entity_content" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s InputSchema type = %T, want map", tool.Name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties type = %T, want map", tool.Name, schema["properties"])
		}
		for _, field := range unsupported {
			if _, ok := properties[field]; ok {
				t.Fatalf("%s advertises unsupported filter %s", tool.Name, field)
			}
		}
	}
}

func TestEvidenceCitationToolAdvertisesInputHandleCap(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range contentTools() {
		if candidate.Name == "build_evidence_citation_packet" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("build_evidence_citation_packet tool is not registered")
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map", schema["properties"])
	}
	handles, ok := properties["handles"].(map[string]any)
	if !ok {
		t.Fatalf("handles schema type = %T, want map", properties["handles"])
	}
	if got, want := handles["maxItems"], 500; got != want {
		t.Fatalf("handles.maxItems = %#v, want %#v", got, want)
	}
}

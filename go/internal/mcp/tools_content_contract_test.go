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

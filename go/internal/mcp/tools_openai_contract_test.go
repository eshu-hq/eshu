package mcp

import "testing"

func TestReadOnlyToolInputSchemasAvoidTopLevelOpenAIRestrictedKeywords(t *testing.T) {
	t.Parallel()

	for _, tool := range ReadOnlyTools() {
		tool := tool
		t.Run(tool.Name, func(t *testing.T) {
			t.Parallel()

			schema, ok := tool.InputSchema.(map[string]any)
			if !ok {
				t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
			}
			if got, want := schema["type"], "object"; got != want {
				t.Fatalf("schema type = %#v, want %q", got, want)
			}
			for _, key := range []string{"oneOf", "anyOf", "allOf", "enum", "not"} {
				if _, ok := schema[key]; ok {
					t.Fatalf("schema has top-level %q; exported MCP tool schemas must keep alternate anchors and restricted top-level keywords in descriptions or handler validation", key)
				}
			}
		})
	}
}

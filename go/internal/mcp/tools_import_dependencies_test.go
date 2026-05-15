package mcp

import "testing"

func TestResolveRouteMapsImportDependencyToolToBoundedEndpoint(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_import_dependencies", map[string]any{
		"query_type":    "imports_by_file",
		"repo_id":       "repo-1",
		"source_file":   "src/module_a.py",
		"target_module": "requests",
		"language":      "python",
		"limit":         float64(10),
		"offset":        float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/imports/investigate"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["query_type"], "imports_by_file"; got != want {
		t.Fatalf("body[query_type] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 10; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 5; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
}

func TestImportDependencyToolSchemaRequiresScopeAndBounds(t *testing.T) {
	t.Parallel()

	tool := importDependencyTool()
	if got, want := tool.Name, "investigate_import_dependencies"; got != want {
		t.Fatalf("tool.Name = %q, want %q", got, want)
	}
	schema := tool.InputSchema.(map[string]any)
	anyOf, ok := schema["anyOf"].([]map[string]any)
	if !ok || len(anyOf) == 0 {
		t.Fatalf("schema anyOf = %#v, want scope requirements", schema["anyOf"])
	}
	properties := schema["properties"].(map[string]any)
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 200; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	if got, want := limit["minimum"], 0; got != want {
		t.Fatalf("limit minimum = %#v, want %#v", got, want)
	}
	offset := properties["offset"].(map[string]any)
	if got, want := offset["maximum"], 10000; got != want {
		t.Fatalf("offset maximum = %#v, want %#v", got, want)
	}
	if got, want := offset["minimum"], 0; got != want {
		t.Fatalf("offset minimum = %#v, want %#v", got, want)
	}
}

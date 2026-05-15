package mcp

import "testing"

func TestInspectCodeInventoryToolContract(t *testing.T) {
	t.Parallel()

	var tool ToolDefinition
	for _, candidate := range ReadOnlyTools() {
		if candidate.Name == "inspect_code_inventory" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("inspect_code_inventory tool is not registered")
	}

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map", schema["properties"])
	}
	for _, field := range []string{
		"repo_id",
		"language",
		"inventory_kind",
		"entity_kind",
		"file_path",
		"symbol",
		"decorator",
		"method_name",
		"class_name",
		"limit",
		"offset",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("inspect_code_inventory schema missing %q", field)
		}
	}
}

func TestInspectCodeInventoryDispatchRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("inspect_code_inventory", map[string]any{
		"repo_id":        "repo-1",
		"language":       "python",
		"inventory_kind": "decorated",
		"entity_kind":    "function",
		"file_path":      "src/app.py",
		"decorator":      "route",
		"method_name":    "handler",
		"class_name":     "App",
		"limit":          12,
		"offset":         24,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/structure/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map", route.body)
	}
	for key, want := range map[string]any{
		"repo_id":        "repo-1",
		"language":       "python",
		"inventory_kind": "decorated",
		"entity_kind":    "function",
		"file_path":      "src/app.py",
		"decorator":      "route",
		"method_name":    "handler",
		"class_name":     "App",
		"limit":          12,
		"offset":         24,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

package mcp

import "testing"

func TestComponentExtensionToolsResolveToQueryRoutes(t *testing.T) {
	t.Parallel()

	inventory, err := resolveRoute("list_component_extensions", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(list_component_extensions) error = %v, want nil", err)
	}
	if got, want := inventory.method, "GET"; got != want {
		t.Fatalf("inventory method = %q, want %q", got, want)
	}
	if got, want := inventory.path, "/api/v0/component-extensions"; got != want {
		t.Fatalf("inventory path = %q, want %q", got, want)
	}

	boundedInventory, err := resolveRoute("list_component_extensions", map[string]any{
		"limit": float64(1),
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_component_extensions limit) error = %v, want nil", err)
	}
	if got, want := boundedInventory.path, "/api/v0/component-extensions"; got != want {
		t.Fatalf("bounded inventory path = %q, want %q", got, want)
	}
	if got, want := boundedInventory.query["limit"], "1"; got != want {
		t.Fatalf("bounded inventory limit query = %q, want %q", got, want)
	}

	diagnostics, err := resolveRoute("get_component_extension_diagnostics", map[string]any{
		"component_id": "dev.eshu.collector.aws",
	})
	if err != nil {
		t.Fatalf("resolveRoute(get_component_extension_diagnostics) error = %v, want nil", err)
	}
	if got, want := diagnostics.method, "GET"; got != want {
		t.Fatalf("diagnostics method = %q, want %q", got, want)
	}
	if got, want := diagnostics.path, "/api/v0/component-extensions/dev.eshu.collector.aws/diagnostics"; got != want {
		t.Fatalf("diagnostics path = %q, want %q", got, want)
	}
}

func TestReadOnlyToolsIncludesComponentExtensionDiagnostics(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_component_extensions", "get_component_extension_diagnostics"} {
		if !names[want] {
			t.Fatalf("ReadOnlyTools() missing %q", want)
		}
	}
}

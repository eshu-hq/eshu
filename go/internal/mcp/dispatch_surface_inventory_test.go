package mcp

import "testing"

// TestSurfaceInventoryToolResolvesToAPIRoute is the MCP side of the #3148 parity
// proof: get_surface_inventory must resolve to the same GET
// /api/v0/surface-inventory route the API serves and the console reads, with the
// category/readiness filters and bounded paging passed through. This keeps the
// three surfaces backed by one source of truth.
func TestSurfaceInventoryToolResolvesToAPIRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_surface_inventory", map[string]any{
		"category":  "collector",
		"readiness": "gated",
		"limit":     float64(50),
		"offset":    float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/surface-inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"category":  "collector",
		"readiness": "gated",
		"limit":     "50",
		"offset":    "10",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %q, want %q", key, got, want)
		}
	}
}

// TestSurfaceInventoryToolIsRegistered confirms the tool is in the read-only
// registry so it is discoverable by clients.
func TestSurfaceInventoryToolIsRegistered(t *testing.T) {
	t.Parallel()
	for _, tool := range ReadOnlyTools() {
		if tool.Name == "get_surface_inventory" {
			return
		}
	}
	t.Fatal("get_surface_inventory not found in ReadOnlyTools")
}

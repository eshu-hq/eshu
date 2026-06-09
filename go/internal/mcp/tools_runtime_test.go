package mcp

import "testing"

func TestListCollectorsRuntimeToolRoutesToStatusCollectors(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_collectors", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/collectors"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestSemanticCapabilityRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_semantic_capability_status", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/semantic-extraction"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

package mcp

import "testing"

func TestResolveRouteMapsGenerationLifecycleToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_generation_lifecycle", map[string]any{
		"scope_id":       "git-repository-scope:acme/app",
		"repository":     "acme/app",
		"collector_kind": "git",
		"source_system":  "github",
		"generation_id":  "gen-1",
		"status":         "active",
		"limit":          float64(75),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/freshness/generations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"scope_id":       "git-repository-scope:acme/app",
		"repository":     "acme/app",
		"collector_kind": "git",
		"source_system":  "github",
		"generation_id":  "gen-1",
		"status":         "active",
		"limit":          "75",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestResolveRouteGenerationLifecycleDefaultLimit(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_generation_lifecycle", map[string]any{
		"repository": "acme/app",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.query["limit"], "50"; got != want {
		t.Fatalf("default limit = %q, want %q", got, want)
	}
}

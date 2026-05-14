package mcp

import "testing"

func TestChangeSurfaceInvestigationToolContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range ecosystemTools() {
		if candidate.Name == "investigate_change_surface" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("investigate_change_surface tool is not registered")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, key := range []string{"target", "target_type", "service_name", "topic", "changed_paths", "repo_id", "limit", "max_depth"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("tool schema missing %q", key)
		}
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 100; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsChangeSurfaceInvestigationToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_change_surface", map[string]any{
		"service_name":  "orders-api",
		"topic":         "repo sync auth",
		"repo_id":       "repo-1",
		"changed_paths": []any{"go/internal/collector/reposync/auth.go"},
		"environment":   "prod",
		"max_depth":     float64(3),
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/change-surface/investigate"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"service_name": "orders-api",
		"topic":        "repo sync auth",
		"repo_id":      "repo-1",
		"environment":  "prod",
		"max_depth":    3,
		"limit":        25,
		"offset":       50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
	paths := body["changed_paths"].([]any)
	if got, want := paths[0], "go/internal/collector/reposync/auth.go"; got != want {
		t.Fatalf("changed path = %#v, want %#v", got, want)
	}
}

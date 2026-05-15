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

func TestDeploymentConfigInfluenceToolContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range ecosystemTools() {
		if candidate.Name == "investigate_deployment_config" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("investigate_deployment_config tool is not registered")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, key := range []string{"service_name", "workload_id", "environment", "limit"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("tool schema missing %q", key)
		}
	}
	anyOf, ok := schema["anyOf"].([]map[string]any)
	if !ok {
		t.Fatalf("tool schema anyOf type = %T, want []map[string]any", schema["anyOf"])
	}
	if len(anyOf) != 2 {
		t.Fatalf("tool schema anyOf length = %d, want 2", len(anyOf))
	}
	firstRequired := anyOf[0]["required"].([]string)
	secondRequired := anyOf[1]["required"].([]string)
	if firstRequired[0] != "service_name" || secondRequired[0] != "workload_id" {
		t.Fatalf("tool schema anyOf required = %#v / %#v, want service_name / workload_id", firstRequired, secondRequired)
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 100; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsDeploymentConfigInfluenceToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_deployment_config", map[string]any{
		"service_name": "eshu-hqgraph-resolution-engine",
		"workload_id":  "workload:eshu-hqgraph-resolution-engine",
		"environment":  "ops-qa",
		"limit":        float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/deployment-config-influence"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"service_name": "eshu-hqgraph-resolution-engine",
		"workload_id":  "workload:eshu-hqgraph-resolution-engine",
		"environment":  "ops-qa",
		"limit":        25,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

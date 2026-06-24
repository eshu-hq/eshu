package mcp

import (
	"strings"
	"testing"
)

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

func TestPreChangeImpactToolContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range ecosystemTools() {
		if candidate.Name == "analyze_pre_change_impact" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("analyze_pre_change_impact tool is not registered")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, key := range []string{"repo_id", "base_ref", "head_ref", "changed_paths", "changes", "target", "topic", "limit", "max_depth"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("tool schema missing %q", key)
		}
	}
	changes := properties["changes"].(map[string]any)
	items := changes["items"].(map[string]any)
	changeProperties := items["properties"].(map[string]any)
	for _, key := range []string{"path", "old_path", "status"} {
		if _, ok := changeProperties[key]; !ok {
			t.Fatalf("changes item schema missing %q", key)
		}
	}
	if !strings.Contains(tool.Description, "base/head") || !strings.Contains(tool.Description, "changed files") {
		t.Fatalf("tool description = %q, want pre-change diff guidance", tool.Description)
	}
}

func TestResolveRouteMapsPreChangeImpactToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_pre_change_impact", map[string]any{
		"repo_id":       "repo-1",
		"base_ref":      "main",
		"head_ref":      "feature/pre-change",
		"changed_paths": []any{"go/internal/query/prechange_impact.go"},
		"changes": []any{map[string]any{
			"path":     "go/internal/query/prechange_impact.go",
			"old_path": "go/internal/query/change_impact.go",
			"status":   "renamed",
		}},
		"topic":       "pre-change workflow",
		"environment": "prod",
		"max_depth":   float64(3),
		"limit":       float64(25),
		"offset":      float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/pre-change"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"repo_id":     "repo-1",
		"base_ref":    "main",
		"head_ref":    "feature/pre-change",
		"topic":       "pre-change workflow",
		"environment": "prod",
		"max_depth":   3,
		"limit":       25,
		"offset":      50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
	changes := body["changes"].([]any)
	first := changes[0].(map[string]any)
	if got, want := first["old_path"], "go/internal/query/change_impact.go"; got != want {
		t.Fatalf("changes[0].old_path = %#v, want %#v", got, want)
	}
}

func TestDeveloperChangePlanToolContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range ecosystemTools() {
		if candidate.Name == "plan_developer_change" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("plan_developer_change tool is not registered")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, key := range []string{"developer_intent", "repo_id", "changes", "changed_paths", "limit", "max_depth"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("tool schema missing %q", key)
		}
	}
	if !strings.Contains(tool.Description, "developer_change_plan.v1") || !strings.Contains(tool.Description, "read-only") {
		t.Fatalf("tool description = %q, want developer plan guidance", tool.Description)
	}
}

func TestResolveRouteMapsDeveloperChangePlanToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("plan_developer_change", map[string]any{
		"developer_intent": "rename helper safely",
		"repo_id":          "repo-1",
		"changes": []any{map[string]any{
			"path":     "go/internal/query/developer_change_plan.go",
			"old_path": "go/internal/query/prechange_impact.go",
			"status":   "renamed",
		}},
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/developer-change-plan"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := route.body.(map[string]any)
	if got, want := body["developer_intent"], "rename helper safely"; got != want {
		t.Fatalf("body.developer_intent = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 10; got != want {
		t.Fatalf("body.limit = %#v, want %#v", got, want)
	}
	changes := body["changes"].([]any)
	first := changes[0].(map[string]any)
	if got, want := first["status"], "renamed"; got != want {
		t.Fatalf("changes[0].status = %#v, want %#v", got, want)
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
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("tool schema must not advertise top-level anyOf")
	}
	if !strings.Contains(tool.Description, "Provide service_name or workload_id") {
		t.Fatalf("tool description = %q, want service_name/workload_id guidance", tool.Description)
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 100; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
}

func TestResourceInvestigationToolContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range ecosystemTools() {
		if candidate.Name == "investigate_resource" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("investigate_resource tool is not registered")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, key := range []string{"query", "resource_id", "resource_type", "environment", "limit", "max_depth"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("tool schema missing %q", key)
		}
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 100; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	maxDepth := properties["max_depth"].(map[string]any)
	if got, want := maxDepth["maximum"], 8; got != want {
		t.Fatalf("max_depth maximum = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsResourceInvestigationToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_resource", map[string]any{
		"query":         "orders-db",
		"resource_id":   "cloud:rds:orders-db",
		"resource_type": "database",
		"environment":   "prod",
		"max_depth":     float64(3),
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/impact/resource-investigation"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"query":         "orders-db",
		"resource_id":   "cloud:rds:orders-db",
		"resource_type": "database",
		"environment":   "prod",
		"max_depth":     3,
		"limit":         25,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestResolveRouteMapsDeploymentConfigInfluenceToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_deployment_config", map[string]any{
		"service_name": "eshu-hqgraph-resolution-engine",
		"workload_id":  "workload:eshu-hqgraph-resolution-engine",
		"environment":  "platform-qa",
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
		"environment":  "platform-qa",
		"limit":        25,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

package mcp

import "testing"

func TestResolveRouteMapsFindDeadCodeExclusions(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_dead_code", map[string]any{
		"repo_id":                "repo-1",
		"limit":                  float64(25),
		"exclude_decorated_with": []any{"@route", "@app.route"},
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/dead-code" {
		t.Fatalf("route.path = %q, want /api/v0/code/dead-code", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	exclusions, ok := body["exclude_decorated_with"].([]any)
	if !ok {
		t.Fatalf("body[exclude_decorated_with] type = %T, want []any", body["exclude_decorated_with"])
	}
	if len(exclusions) != 2 {
		t.Fatalf("len(body[exclude_decorated_with]) = %d, want 2", len(exclusions))
	}
}

func TestResolveRouteMapsFindDeadIaC(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_dead_iac", map[string]any{
		"repo_ids":          []any{"terraform-stack", "terraform-modules"},
		"families":          []any{"terraform"},
		"include_ambiguous": true,
		"limit":             float64(25),
		"offset":            float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/iac/dead" {
		t.Fatalf("route.path = %q, want /api/v0/iac/dead", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	if got, want := body["include_ambiguous"], true; got != want {
		t.Fatalf("body[include_ambiguous] = %#v, want %#v", got, want)
	}
	repoIDs := body["repo_ids"].([]any)
	if len(repoIDs) != 2 {
		t.Fatalf("len(repo_ids) = %d, want 2", len(repoIDs))
	}
	families := body["families"].([]any)
	if len(families) != 1 || families[0] != "terraform" {
		t.Fatalf("families = %#v, want terraform", families)
	}
}

func TestResolveRouteMapsFindUnmanagedResources(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_unmanaged_resources", map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"finding_kinds": []any{"unmanaged_cloud_resource"},
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/iac/unmanaged-resources" {
		t.Fatalf("route.path = %q, want /api/v0/iac/unmanaged-resources", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["account_id"], "123456789012"; got != want {
		t.Fatalf("body[account_id] = %#v, want %#v", got, want)
	}
	if got, want := body["region"], "us-east-1"; got != want {
		t.Fatalf("body[region] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	kinds := body["finding_kinds"].([]any)
	if len(kinds) != 1 || kinds[0] != "unmanaged_cloud_resource" {
		t.Fatalf("finding_kinds = %#v, want unmanaged_cloud_resource", kinds)
	}
}

func TestResolveRouteMapsAnalyzeDeadCodeLimit(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "dead_code",
		"repo_id":    "repo-1",
		"limit":      12,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/dead-code" {
		t.Fatalf("route.path = %q, want /api/v0/code/dead-code", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["limit"], 12; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsTraceDeploymentChain(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("trace_deployment_chain", map[string]any{
		"service_name":                 "payments-api",
		"direct_only":                  true,
		"max_depth":                    float64(6),
		"include_related_module_usage": true,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/impact/trace-deployment-chain" {
		t.Fatalf("route.path = %q, want /api/v0/impact/trace-deployment-chain", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["service_name"], "payments-api"; got != want {
		t.Fatalf("body[service_name] = %#v, want %#v", got, want)
	}
	if got, want := body["direct_only"], true; got != want {
		t.Fatalf("body[direct_only] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 6; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
	if got, want := body["include_related_module_usage"], true; got != want {
		t.Fatalf("body[include_related_module_usage] = %#v, want %#v", got, want)
	}
}

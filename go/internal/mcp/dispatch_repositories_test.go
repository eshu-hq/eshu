package mcp

import (
	"testing"
)

// TestResolveRouteGetRepoSummaryRoutesToStats asserts that get_repo_summary
// routes to the /stats endpoint, not /context. The two tools must have
// distinct HTTP targets so agents can choose a lightweight summary versus the
// full context payload without hitting the same handler twice.
func TestResolveRouteGetRepoSummaryRoutesToStats(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repo_summary", map[string]any{
		"repo_id": "repo-abc",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, wantMethod := route.method, "GET"; got != wantMethod {
		t.Fatalf("route.method = %q, want %q", got, wantMethod)
	}
	if got, wantPath := route.path, "/api/v0/repositories/repo-abc/stats"; got != wantPath {
		t.Fatalf("route.path = %q, want %q (get_repo_summary must route to /stats, not /context)", got, wantPath)
	}
}

// TestResolveRouteGetRepoContextRoutesToContext asserts that get_repo_context
// continues to route to the /context endpoint unchanged.
func TestResolveRouteGetRepoContextRoutesToContext(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repo_context", map[string]any{
		"repo_id": "repo-abc",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, wantMethod := route.method, "GET"; got != wantMethod {
		t.Fatalf("route.method = %q, want %q", got, wantMethod)
	}
	if got, wantPath := route.path, "/api/v0/repositories/repo-abc/context"; got != wantPath {
		t.Fatalf("route.path = %q, want %q", got, wantPath)
	}
}

// TestGetRepoSummaryToolAcceptsRepoID asserts that the get_repo_summary tool
// definition uses repo_id (not repo_name) as its required parameter, matching
// the canonical repository selector convention used by all other repo tools.
func TestGetRepoSummaryToolAcceptsRepoID(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_repo_summary")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("get_repo_summary InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_repo_summary properties type = %T, want map[string]any", schema["properties"])
	}
	if _, hasRepoID := props["repo_id"]; !hasRepoID {
		t.Fatal("get_repo_summary schema missing repo_id property; must use canonical repository selector")
	}
	if _, hasRepoName := props["repo_name"]; hasRepoName {
		t.Fatal("get_repo_summary schema must not use repo_name; use repo_id (canonical repository selector)")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("get_repo_summary required type = %T, want []string", schema["required"])
	}
	for _, r := range required {
		if r == "repo_id" {
			return
		}
	}
	t.Fatal("get_repo_summary: repo_id must be in required")
}

// TestGetRepoSummaryDescriptionDistinctFromGetRepoContext asserts that
// get_repo_summary has a description that clearly distinguishes it from
// get_repo_context. Undocumented aliases confuse agents and cause unnecessary
// duplicate calls.
func TestGetRepoSummaryDescriptionDistinctFromGetRepoContext(t *testing.T) {
	t.Parallel()

	summary := requireToolDefinition(t, "get_repo_summary")
	ctx := requireToolDefinition(t, "get_repo_context")

	if summary.Description == "" {
		t.Fatal("get_repo_summary description is empty")
	}
	if ctx.Description == "" {
		t.Fatal("get_repo_context description is empty")
	}
	if summary.Description == ctx.Description {
		t.Fatal("get_repo_summary and get_repo_context have identical descriptions; they must be distinct tools")
	}
}

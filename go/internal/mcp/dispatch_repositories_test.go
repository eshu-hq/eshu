package mcp

import (
	"strings"
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

// TestResolveRouteGetRepoSummaryFallsBackToRepoName asserts that an MCP client
// that still sends the legacy repo_name argument (instead of repo_id) resolves
// to a valid /stats path with a non-empty selector. tools/call forwards the
// caller's arguments straight into dispatch, so dropping the repo_name fallback
// produced "/api/v0/repositories//stats" (empty selector) for those clients,
// which fails. repo_id, when present, takes priority.
func TestResolveRouteGetRepoSummaryFallsBackToRepoName(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repo_summary", map[string]any{
		"repo_name": "legacy-service",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, wantPath := route.path, "/api/v0/repositories/legacy-service/stats"; got != wantPath {
		t.Fatalf("route.path = %q, want %q (repo_name must resolve, not build an empty selector)", got, wantPath)
	}
}

// TestResolveRouteGetRepoSummaryPrefersRepoIDOverRepoName asserts that when a
// caller supplies both selectors, repo_id wins. repo_name is only a
// backward-compatibility fallback for clients that predate the repo_id field.
func TestResolveRouteGetRepoSummaryPrefersRepoIDOverRepoName(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repo_summary", map[string]any{
		"repo_id":   "repo-canonical",
		"repo_name": "legacy-service",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, wantPath := route.path, "/api/v0/repositories/repo-canonical/stats"; got != wantPath {
		t.Fatalf("route.path = %q, want %q (repo_id must take priority over repo_name)", got, wantPath)
	}
}

// TestGetRepoSummaryDescriptionMatchesStatsPayload asserts that the
// get_repo_summary tool description only promises fields the /stats route
// actually returns (file_count, languages, entity_count, entity_types,
// coverage) and does not claim workload, platform, or dependency counts, which
// /stats does not return. Promising absent fields misleads agents.
func TestGetRepoSummaryDescriptionMatchesStatsPayload(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "get_repo_summary")
	description := strings.ToLower(tool.Description)

	for _, forbidden := range []string{"workload count", "platform count", "dependency count"} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("get_repo_summary description promises %q, but /stats does not return it: %s", forbidden, tool.Description)
		}
	}
	for _, want := range []string{"file", "languages", "entit", "coverage"} {
		if !strings.Contains(description, want) {
			t.Fatalf("get_repo_summary description missing real /stats field %q: %s", want, tool.Description)
		}
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
// definition advertises repo_id as its canonical required selector. repo_name
// is also advertised as a documented deprecated alias so existing clients keep
// working; see TestGetRepoSummaryToolAdvertisesRepoNameCompat.
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

// TestGetRepoSummaryToolAdvertisesRepoNameCompat asserts that the schema still
// advertises the legacy repo_name field so clients that predate repo_id can
// discover it remains accepted. The dispatch path falls back to repo_name when
// repo_id is absent.
func TestGetRepoSummaryToolAdvertisesRepoNameCompat(t *testing.T) {
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
	if _, hasRepoName := props["repo_name"]; !hasRepoName {
		t.Fatal("get_repo_summary schema must advertise repo_name as a backward-compatibility alias")
	}
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

package mcp

import "testing"

func TestResolveRouteMapsCICDRunCorrelationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_ci_cd_run_correlations", map[string]any{
		"after_correlation_id": "correlation-1",
		"repository_id":        "repo-api",
		"commit_sha":           "abc123",
		"provider":             "github_actions",
		"artifact_digest":      "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"environment":          "prod",
		"outcome":              "exact",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ci-cd/run-correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"after_correlation_id": "correlation-1",
		"repository_id":        "repo-api",
		"commit_sha":           "abc123",
		"provider":             "github_actions",
		"artifact_digest":      "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"environment":          "prod",
		"outcome":              "exact",
		"limit":                "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}

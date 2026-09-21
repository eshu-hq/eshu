// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsPackageRegistryCorrelationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_package_registry_correlations", map[string]any{
		"after_correlation_id": "correlation-1",
		"package_id":           "pkg:npm://registry.example/team-api",
		"relationship_kind":    "publication",
		"repository_id":        "repo-web",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/package-registry/correlations"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"after_correlation_id": "correlation-1",
		"package_id":           "pkg:npm://registry.example/team-api",
		"relationship_kind":    "publication",
		"repository_id":        "repo-web",
		"limit":                "25",
	} {
		if got := route.Query[key]; got != want {
			t.Fatalf("route.Query[%s] = %#v, want %#v", key, got, want)
		}
	}
}

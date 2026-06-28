// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsTraceRouteCallers(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("trace_route_callers", map[string]any{
		"repo_id":   "repo-payments",
		"method":    "get",
		"path":      "/payments/{id}",
		"max_depth": 3,
		"limit":     25,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/routes/callers"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	for key, want := range map[string]any{
		"repo_id":   "repo-payments",
		"method":    "get",
		"path":      "/payments/{id}",
		"max_depth": 3,
		"limit":     25,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

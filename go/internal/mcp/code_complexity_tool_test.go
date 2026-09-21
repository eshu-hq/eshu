// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsCalculateCyclomaticComplexityEntityID(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("calculate_cyclomatic_complexity", map[string]any{
		"entity_id":     "function:processPayment",
		"function_name": "processPayment",
		"repo_id":       "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Path, "/api/v0/code/complexity"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	body, ok := route.Body.(map[string]any)
	if !ok {
		t.Fatalf("route.Body type = %T, want map[string]any", route.Body)
	}
	if got, want := body["entity_id"], "function:processPayment"; got != want {
		t.Fatalf("route.Body[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["function_name"], "processPayment"; got != want {
		t.Fatalf("route.Body[function_name] = %#v, want %#v", got, want)
	}
}

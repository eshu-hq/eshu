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
	if got, want := route.path, "/api/v0/code/complexity"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["entity_id"], "function:processPayment"; got != want {
		t.Fatalf("route.body[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["function_name"], "processPayment"; got != want {
		t.Fatalf("route.body[function_name] = %#v, want %#v", got, want)
	}
}

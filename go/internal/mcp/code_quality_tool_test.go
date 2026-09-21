// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsInspectCodeQualityToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("inspect_code_quality", map[string]any{
		"check":         "argument_count",
		"repo_id":       "repo-payments",
		"language":      "go",
		"min_arguments": float64(5),
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Method, "POST"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/code/quality/inspect"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	body, ok := route.Body.(map[string]any)
	if !ok {
		t.Fatalf("route.Body type = %T, want map[string]any", route.Body)
	}
	for key, want := range map[string]any{
		"check":         "argument_count",
		"repo_id":       "repo-payments",
		"language":      "go",
		"min_arguments": 5,
		"limit":         25,
		"offset":        50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

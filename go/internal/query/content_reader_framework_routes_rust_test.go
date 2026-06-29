// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestParseFrameworkSemanticsExtractsRustFrameworkRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["axum"],
		"axum": {
			"route_methods": ["GET", "POST"],
			"route_paths": ["/axum/:id", "/axum"],
			"route_entries": [
				{"method": "GET", "path": "/axum/:id", "handler": "axum_show"},
				{"method": "POST", "path": "/axum", "handler": "axum_create"}
			]
		}
	}`)

	results := parseFrameworkSemantics("src/lib.rs", raw)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	axum := results[0]
	if got, want := axum.Framework, "axum"; got != want {
		t.Fatalf("Framework = %q, want %q", got, want)
	}
	if got, want := axum.RelativePath, "src/lib.rs"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if len(axum.RouteEntries) != 2 {
		t.Fatalf("len(RouteEntries) = %d, want 2", len(axum.RouteEntries))
	}
	if got, want := axum.RouteEntries[0].Handler, "axum_show"; got != want {
		t.Fatalf("RouteEntries[0].Handler = %q, want %q", got, want)
	}
	if got, want := axum.RouteEntries[0].Path, "/axum/:id"; got != want {
		t.Fatalf("RouteEntries[0].Path = %q, want %q", got, want)
	}
	if got, want := axum.RouteEntries[1].Method, "POST"; got != want {
		t.Fatalf("RouteEntries[1].Method = %q, want %q", got, want)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestParseFrameworkSemanticsExtractsScalaRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["play", "http4s"],
		"play": {
			"route_methods": ["GET"],
			"route_paths": ["/reports/:id"],
			"route_entries": [
				{"method": "GET", "path": "/reports/:id", "handler": "ReportsController.show"}
			]
		},
		"http4s": {
			"route_methods": ["POST"],
			"route_paths": ["/reports"],
			"route_entries": [
				{"method": "POST", "path": "/reports", "handler": "ReportRoutes.createReport"}
			]
		}
	}`)

	results := parseFrameworkSemantics("conf/routes", raw)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if got, want := results[0].Framework, "play"; got != want {
		t.Fatalf("results[0].Framework = %q, want %q", got, want)
	}
	if got, want := results[0].RouteEntries[0].Handler, "ReportsController.show"; got != want {
		t.Fatalf("results[0].RouteEntries[0].Handler = %q, want %q", got, want)
	}
	if got, want := results[1].Framework, "http4s"; got != want {
		t.Fatalf("results[1].Framework = %q, want %q", got, want)
	}
	if got, want := results[1].RouteEntries[0].Path, "/reports"; got != want {
		t.Fatalf("results[1].RouteEntries[0].Path = %q, want %q", got, want)
	}
}

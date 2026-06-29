// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestParseFrameworkSemanticsExtractsRubyRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["rails", "sinatra"],
		"rails": {
			"route_methods": ["GET"],
			"route_paths": ["/reports/:id"],
			"route_entries": [
				{"method": "GET", "path": "/reports/:id", "handler": "ReportsController.show"}
			]
		},
		"sinatra": {
			"route_methods": ["POST"],
			"route_paths": ["/reports"],
			"route_entries": [
				{"method": "POST", "path": "/reports", "handler": "ReportsApp.create_report"}
			]
		}
	}`)

	results := parseFrameworkSemantics("app/routes.rb", raw)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if got, want := results[0].Framework, "rails"; got != want {
		t.Fatalf("results[0].Framework = %q, want %q", got, want)
	}
	if got, want := results[0].RouteEntries[0].Handler, "ReportsController.show"; got != want {
		t.Fatalf("results[0].RouteEntries[0].Handler = %q, want %q", got, want)
	}
	if got, want := results[1].Framework, "sinatra"; got != want {
		t.Fatalf("results[1].Framework = %q, want %q", got, want)
	}
	if got, want := results[1].RouteEntries[0].Path, "/reports"; got != want {
		t.Fatalf("results[1].RouteEntries[0].Path = %q, want %q", got, want)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestParseFrameworkSemanticsExtractsPHPSymfonyRoutes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"frameworks": ["symfony"],
		"symfony": {
			"route_methods": ["GET", "POST"],
			"route_paths": ["/reports/{id}", "/reports"],
			"route_entries": [
				{"method": "GET", "path": "/reports/{id}", "handler": "ReportController.show"},
				{"method": "POST", "path": "/reports", "handler": "ReportController.create"}
			]
		}
	}`)

	results := parseFrameworkSemantics("src/Controller/ReportController.php", raw)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	symfony := results[0]
	if got, want := symfony.Framework, "symfony"; got != want {
		t.Fatalf("Framework = %q, want %q", got, want)
	}
	if got, want := symfony.RelativePath, "src/Controller/ReportController.php"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if len(symfony.RouteEntries) != 2 {
		t.Fatalf("len(RouteEntries) = %d, want 2", len(symfony.RouteEntries))
	}
	if got, want := symfony.RouteEntries[0].Handler, "ReportController.show"; got != want {
		t.Fatalf("RouteEntries[0].Handler = %q, want %q", got, want)
	}
	if got, want := symfony.RouteEntries[0].Path, "/reports/{id}"; got != want {
		t.Fatalf("RouteEntries[0].Path = %q, want %q", got, want)
	}
	if got, want := symfony.RouteEntries[1].Method, "POST"; got != want {
		t.Fatalf("RouteEntries[1].Method = %q, want %q", got, want)
	}
}

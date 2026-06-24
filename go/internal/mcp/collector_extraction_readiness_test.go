// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCollectorExtractionReadinessToolsResolveToQueryRoutes(t *testing.T) {
	t.Parallel()

	list, err := resolveRoute("list_collector_extraction_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(list_collector_extraction_readiness) error = %v, want nil", err)
	}
	if got, want := list.method, "GET"; got != want {
		t.Fatalf("list method = %q, want %q", got, want)
	}
	if got, want := list.path, "/api/v0/collector-extraction-readiness"; got != want {
		t.Fatalf("list path = %q, want %q", got, want)
	}
	if got, want := list.query["limit"], "100"; got != want {
		t.Fatalf("list default limit = %q, want %q", got, want)
	}

	bounded, err := resolveRoute("list_collector_extraction_readiness", map[string]any{"limit": float64(2)})
	if err != nil {
		t.Fatalf("resolveRoute(list bounded) error = %v, want nil", err)
	}
	if got, want := bounded.query["limit"], "2"; got != want {
		t.Fatalf("bounded limit = %q, want %q", got, want)
	}

	family, err := resolveRoute("get_collector_extraction_readiness", map[string]any{"family": "pagerduty"})
	if err != nil {
		t.Fatalf("resolveRoute(get_collector_extraction_readiness) error = %v, want nil", err)
	}
	if got, want := family.path, "/api/v0/collector-extraction-readiness/pagerduty"; got != want {
		t.Fatalf("family path = %q, want %q", got, want)
	}

	if _, err := resolveRoute("get_collector_extraction_readiness", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(get_collector_extraction_readiness) without family = nil error, want error")
	}
}

func TestReadOnlyToolsIncludesCollectorExtractionReadiness(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_collector_extraction_readiness", "get_collector_extraction_readiness"} {
		if !names[want] {
			t.Fatalf("ReadOnlyTools() missing %q", want)
		}
	}
}

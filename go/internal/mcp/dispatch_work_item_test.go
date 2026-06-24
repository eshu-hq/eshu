// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsWorkItemEvidenceToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_work_item_evidence", map[string]any{
		"scope_id":              "jira:site:example",
		"work_item_key":         "OPS-123",
		"provider_work_item_id": "10001",
		"external_url":          "https://github.com/example/app/pull/42?token=secret",
		"observed_after":        "2026-06-01T12:00:00Z",
		"after_fact_id":         "fact-1",
		"limit":                 float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/work-items/evidence"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"scope_id":              "jira:site:example",
		"work_item_key":         "OPS-123",
		"provider_work_item_id": "10001",
		"external_url":          "https://github.com/example/app/pull/42?token=secret",
		"observed_after":        "2026-06-01T12:00:00Z",
		"after_fact_id":         "fact-1",
		"limit":                 "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %q, want %q", key, got, want)
		}
	}
}

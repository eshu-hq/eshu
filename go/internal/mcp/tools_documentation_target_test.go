// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestListDocumentationFindingsRouteIncludesTargetFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_documentation_findings", map[string]any{
		"repo":        "repo:platform-api",
		"target_kind": "service",
		"target_id":   "service:payment-api",
		"service_id":  "service:payment-api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	for _, key := range []string{"repo", "target_kind", "target_id", "service_id"} {
		if got := route.query[key]; got == "" {
			t.Fatalf("route.query[%q] = empty, want routed filter", key)
		}
	}
}

func TestListDocumentationFactsRouteIncludesTargetFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_documentation_facts", map[string]any{
		"fact_kind":  "entity_mention",
		"repo":       "repo:platform-api",
		"service_id": "service:payment-api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	for _, key := range []string{"fact_kind", "repo", "service_id"} {
		if got := route.query[key]; got == "" {
			t.Fatalf("route.query[%q] = empty, want routed filter", key)
		}
	}
}

func TestDocumentationToolSchemasIncludeTargetFilters(t *testing.T) {
	t.Parallel()

	tools := documentationTools()
	for _, index := range []int{0, 1} {
		schema := tools[index].InputSchema.(map[string]any)
		properties := schema["properties"].(map[string]any)
		for _, name := range []string{"repo", "target_kind", "target_id", "service_id"} {
			if _, ok := properties[name]; !ok {
				t.Fatalf("%s InputSchema missing target filter %q", tools[index].Name, name)
			}
		}
	}
}

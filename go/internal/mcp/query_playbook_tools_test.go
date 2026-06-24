// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestQueryPlaybookToolsAdvertised(t *testing.T) {
	t.Parallel()

	tools := ReadOnlyTools()
	seen := map[string]ToolDefinition{}
	for _, tool := range tools {
		seen[tool.Name] = tool
	}

	for _, name := range []string{"list_query_playbooks", "resolve_query_playbook"} {
		tool, ok := seen[name]
		if !ok {
			t.Fatalf("missing query playbook tool %q", name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %q InputSchema is nil", name)
		}
	}
}

func TestResolveRouteMapsQueryPlaybookToolsToCatalogRoutes(t *testing.T) {
	t.Parallel()

	listRoute, err := resolveRoute("list_query_playbooks", map[string]any{})
	if err != nil {
		t.Fatalf("resolve list route: %v", err)
	}
	if got, want := listRoute.method, "GET"; got != want {
		t.Fatalf("list method = %q, want %q", got, want)
	}
	if got, want := listRoute.path, "/api/v0/query-playbooks"; got != want {
		t.Fatalf("list path = %q, want %q", got, want)
	}

	resolveRoute, err := resolveRoute("resolve_query_playbook", map[string]any{
		"playbook_id": "service_story_citation",
		"inputs": map[string]any{
			"service_name": "payments-api",
			"environment":  "prod",
		},
	})
	if err != nil {
		t.Fatalf("resolve resolver route: %v", err)
	}
	if got, want := resolveRoute.method, "POST"; got != want {
		t.Fatalf("resolve method = %q, want %q", got, want)
	}
	if got, want := resolveRoute.path, "/api/v0/query-playbooks/resolve"; got != want {
		t.Fatalf("resolve path = %q, want %q", got, want)
	}
	body, ok := resolveRoute.body.(map[string]any)
	if !ok {
		t.Fatalf("resolve body type = %T, want map", resolveRoute.body)
	}
	if got, want := body["playbook_id"], "service_story_citation"; got != want {
		t.Fatalf("playbook_id = %#v, want %#v", got, want)
	}
	inputs, ok := body["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("inputs type = %T, want map", body["inputs"])
	}
	if got, want := inputs["service_name"], "payments-api"; got != want {
		t.Fatalf("inputs.service_name = %#v, want %#v", got, want)
	}
}

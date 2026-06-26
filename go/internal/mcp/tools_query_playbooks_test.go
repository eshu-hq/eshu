// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestQueryPlaybookToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_query_playbooks", "resolve_query_playbook"} {
		tool := requireToolDefinition(t, name)
		_, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
	}
}

func TestResolveRouteMapsListQueryPlaybooks(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_query_playbooks", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/query-playbooks"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsResolveQueryPlaybook(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("resolve_query_playbook", map[string]any{
		"playbook_id": "pb-1",
		"inputs":      map[string]any{"repo": "my-repo"},
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/query-playbooks/resolve"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestInvestigationWorkflowToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_investigation_workflows", "resolve_investigation_workflow"} {
		tool := requireToolDefinition(t, name)
		_, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
	}
}

func TestResolveRouteMapsListInvestigationWorkflows(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_investigation_workflows", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigation-workflows"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsResolveInvestigationWorkflow(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("resolve_investigation_workflow", map[string]any{
		"workflow_id": "wf-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigation-workflows/resolve"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

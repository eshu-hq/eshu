package mcp

import "testing"

func TestInvestigationWorkflowToolsAdvertised(t *testing.T) {
	t.Parallel()

	tools := ReadOnlyTools()
	seen := map[string]ToolDefinition{}
	for _, tool := range tools {
		seen[tool.Name] = tool
	}

	for _, name := range []string{"list_investigation_workflows", "resolve_investigation_workflow"} {
		tool, ok := seen[name]
		if !ok {
			t.Fatalf("missing investigation workflow tool %q", name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %q InputSchema is nil", name)
		}
	}
}

func TestResolveRouteMapsInvestigationWorkflowTools(t *testing.T) {
	t.Parallel()

	listRoute, err := resolveRoute("list_investigation_workflows", map[string]any{})
	if err != nil {
		t.Fatalf("resolve list route: %v", err)
	}
	if got, want := listRoute.method, "GET"; got != want {
		t.Fatalf("list method = %q, want %q", got, want)
	}
	if got, want := listRoute.path, "/api/v0/investigation-workflows"; got != want {
		t.Fatalf("list path = %q, want %q", got, want)
	}

	resolveRoute, err := resolveRoute("resolve_investigation_workflow", map[string]any{
		"workflow_id": "guided_incident_context",
		"inputs": map[string]any{
			"incident_id": "INC-1",
		},
		"missing_evidence": []any{"observability"},
	})
	if err != nil {
		t.Fatalf("resolve workflow route: %v", err)
	}
	if got, want := resolveRoute.method, "POST"; got != want {
		t.Fatalf("resolve method = %q, want %q", got, want)
	}
	if got, want := resolveRoute.path, "/api/v0/investigation-workflows/resolve"; got != want {
		t.Fatalf("resolve path = %q, want %q", got, want)
	}
	body, ok := resolveRoute.body.(map[string]any)
	if !ok {
		t.Fatalf("resolve body type = %T, want map", resolveRoute.body)
	}
	if got, want := body["workflow_id"], "guided_incident_context"; got != want {
		t.Fatalf("workflow_id = %#v, want %#v", got, want)
	}
	if got, want := body["missing_evidence"].([]string), []string{"observability"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("missing_evidence = %#v, want %#v", got, want)
	}
}

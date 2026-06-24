// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

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

func TestResolveRouteMapsInvestigationWorkflowChildPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		workflowID      string
		inputs          map[string]any
		missingEvidence []any
	}{
		{
			name:       "vulnerable dependency",
			workflowID: "guided_vulnerable_dependency",
			inputs: map[string]any{
				"advisory_id": "CVE-2026-0001",
				"repo_id":     "repo-checkout",
				"subject":     "CVE-2026-0001",
			},
			missingEvidence: []any{"advisory", "package", "impact"},
		},
		{
			name:       "deployable drift",
			workflowID: "guided_deployable_drift",
			inputs: map[string]any{
				"deployable_unit_id": "workload:checkout",
				"generation_id":      "gen-1",
				"repo_id":            "repo-checkout",
				"scope_id":           "scope-1",
			},
			missingEvidence: []any{"admission", "runtime"},
		},
		{
			name:       "incident context",
			workflowID: "guided_incident_context",
			inputs: map[string]any{
				"incident_id": "INC-1",
				"repo_id":     "repo-checkout",
				"scope_id":    "scope-1",
				"service_id":  "checkout",
			},
			missingEvidence: []any{"incident", "service", "changes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute("resolve_investigation_workflow", map[string]any{
				"workflow_id":      tt.workflowID,
				"inputs":           tt.inputs,
				"missing_evidence": tt.missingEvidence,
			})
			if err != nil {
				t.Fatalf("resolve workflow route: %v", err)
			}
			if got, want := route.method, "POST"; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := route.path, "/api/v0/investigation-workflows/resolve"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			body, ok := route.body.(map[string]any)
			if !ok {
				t.Fatalf("body type = %T, want map", route.body)
			}
			if got := body["workflow_id"]; got != tt.workflowID {
				t.Fatalf("workflow_id = %#v, want %#v", got, tt.workflowID)
			}
			inputs, ok := body["inputs"].(map[string]any)
			if !ok {
				t.Fatalf("inputs type = %T, want map[string]any", body["inputs"])
			}
			for key, want := range tt.inputs {
				if got := inputs[key]; got != want {
					t.Fatalf("input %q = %#v, want %#v", key, got, want)
				}
			}
			missing, ok := body["missing_evidence"].([]string)
			if !ok {
				t.Fatalf("missing_evidence type = %T, want []string", body["missing_evidence"])
			}
			if len(missing) != len(tt.missingEvidence) {
				t.Fatalf("missing_evidence = %#v, want %#v", missing, tt.missingEvidence)
			}
			for i, want := range tt.missingEvidence {
				if missing[i] != want {
					t.Fatalf("missing_evidence[%d] = %#v, want %#v", i, missing[i], want)
				}
			}
		})
	}
}

func TestInvestigationWorkflowNextCallParamsExistInToolSchemas(t *testing.T) {
	t.Parallel()

	registry := map[string]ToolDefinition{}
	for _, tool := range ReadOnlyTools() {
		registry[tool.Name] = tool
	}
	for _, workflow := range query.InvestigationWorkflowCatalog() {
		for _, route := range workflow.MissingEvidenceRoutes {
			for _, call := range route.Calls {
				tool, ok := registry[call.Tool]
				if !ok {
					t.Fatalf("workflow %q call %q references unregistered tool %q", workflow.ID, call.ID, call.Tool)
				}
				schema, ok := tool.InputSchema.(map[string]any)
				if !ok {
					t.Fatalf("tool %q schema type = %T, want map[string]any", tool.Name, tool.InputSchema)
				}
				properties, _ := schema["properties"].(map[string]any)
				params := map[string]struct{}{}
				for _, param := range call.Params {
					params[param.Name] = struct{}{}
					if _, ok := properties[param.Name]; !ok {
						t.Fatalf("workflow %q call %q param %q missing from tool %q schema %#v", workflow.ID, call.ID, param.Name, call.Tool, properties)
					}
				}
				required, _ := schema["required"].([]string)
				for _, name := range required {
					if _, ok := params[name]; ok {
						continue
					}
					if len(call.RequiredInputsAny) == 0 {
						t.Fatalf("workflow %q call %q omits required tool param %q without required input guard", workflow.ID, call.ID, name)
					}
				}
			}
		}
	}
}

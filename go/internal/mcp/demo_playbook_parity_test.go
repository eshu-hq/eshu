// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Demo first-five-questions parity tests (issue #4745).
//
// specs/demo-first-answers.v1.yaml pins each of the five demo questions to a
// bounded call on an existing read surface: two reuse an existing catalog
// playbook verbatim (q1 -> service_story_citation, q3 ->
// incident_context_evidence_path), and three are single-step catalog entries
// that wrap an existing MCP tool with the manifest's exact arguments
// (q2 -> list_kubernetes_correlations, q4 -> list_package_registry_correlations,
// q5 -> list_observability_coverage_correlations). This test proves that every
// one of the five resolves identically through the HTTP /api/v0/query-playbooks
// surface and the MCP resolve_query_playbook dispatch path, and that the
// resolved call for each demo entry carries exactly the bounded arguments the
// manifest requires -- no new query capability, only catalog composition of
// tools that already exist.

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// demoQuestionCase pins one manifest question to its catalog playbook ID, the
// resolver inputs a demo run would supply, and the exact tool + bounded
// arguments the resolved call must carry.
type demoQuestionCase struct {
	manifestID   string
	playbookID   string
	inputs       map[string]string
	wantTool     string
	wantArgs     map[string]any
	reusesExtant bool // true for q1/q3, which reuse an already-shipped playbook verbatim
}

func demoQuestionCases() []demoQuestionCase {
	return []demoQuestionCase{
		{
			manifestID:   "q1_code_to_deployment",
			playbookID:   "service_story_citation",
			inputs:       map[string]string{"service_name": "api-svc", "environment": "prod"},
			wantTool:     "get_service_story",
			wantArgs:     map[string]any{"workload_id": "api-svc", "environment": "prod"},
			reusesExtant: true,
		},
		{
			manifestID: "q2_deployment_to_cloud_resource",
			playbookID: "demo_deployment_to_cloud_resource",
			inputs:     map[string]string{"cluster_id": "supply-chain-demo"},
			wantTool:   "list_kubernetes_correlations",
			wantArgs:   map[string]any{"cluster_id": "supply-chain-demo", "limit": 50},
		},
		{
			manifestID:   "q3_incident_to_service",
			playbookID:   "incident_context_evidence_path",
			inputs:       map[string]string{"incident_id": "PSCD1"},
			wantTool:     "get_incident_context",
			wantArgs:     map[string]any{"incident_id": "PSCD1", "limit": 10},
			reusesExtant: true,
		},
		{
			manifestID: "q4_dependency_cross_repo",
			playbookID: "demo_dependency_cross_repo",
			inputs:     map[string]string{"package_id": "github.com/acme/lib-common"},
			wantTool:   "list_package_registry_correlations",
			wantArgs:   map[string]any{"package_id": "github.com/acme/lib-common", "limit": 50},
		},
		{
			manifestID: "q5_observability_to_workload",
			playbookID: "demo_observability_to_workload",
			inputs:     map[string]string{"provider": "tempo"},
			wantTool:   "list_observability_coverage_correlations",
			wantArgs:   map[string]any{"provider": "tempo", "limit": 50},
		},
	}
}

// TestDemoPlaybookCasesResolveExpectedBoundedCall proves each of the five demo
// entries resolves to exactly the tool and bounded arguments the manifest
// pins, so the guided path never drifts from specs/demo-first-answers.v1.yaml.
func TestDemoPlaybookCasesResolveExpectedBoundedCall(t *testing.T) {
	t.Parallel()

	for _, tc := range demoQuestionCases() {
		tc := tc
		t.Run(tc.manifestID, func(t *testing.T) {
			t.Parallel()

			pb, ok := query.LookupPlaybook(tc.playbookID)
			if !ok {
				t.Fatalf("catalog missing playbook %q for manifest question %q", tc.playbookID, tc.manifestID)
			}
			resolved, err := pb.Resolve(tc.inputs)
			if err != nil {
				t.Fatalf("resolve %q: %v", tc.playbookID, err)
			}
			if len(resolved.Calls) == 0 {
				t.Fatalf("playbook %q resolved zero calls", tc.playbookID)
			}
			first := resolved.Calls[0]
			if first.Tool != tc.wantTool {
				t.Fatalf("playbook %q first call tool = %q, want %q", tc.playbookID, first.Tool, tc.wantTool)
			}
			for key, want := range tc.wantArgs {
				if got := first.Arguments[key]; got != want {
					t.Fatalf("playbook %q arg %q = %#v, want %#v", tc.playbookID, key, got, want)
				}
			}
		})
	}
}

// TestDemoPlaybookHTTPAndMCPParity proves the five demo entries are listed and
// resolved identically over the HTTP /api/v0/query-playbooks surface and the
// MCP resolve_query_playbook dispatch path, following the answer_parity_test.go
// precedent (issue #1795): both surfaces must derive the same canonical
// envelope and the same resolved playbook from one handler instance.
func TestDemoPlaybookHTTPAndMCPParity(t *testing.T) {
	t.Parallel()

	handler := mountQueryPlaybookHandlerForDemoParity(t)

	httpListEnv := httpEnvelope(t, handler, http.MethodGet, "/api/v0/query-playbooks", nil)
	mcpListEnv, listSummary := mcpEnvelope(t, handler, "list_query_playbooks", map[string]any{})
	requireParity(t, "http list", "mcp list", extractComparable(t, httpListEnv), extractComparable(t, mcpListEnv))
	if listSummary == "" {
		t.Fatal("list_query_playbooks MCP convenience summary is empty")
	}

	listedIDs := demoCatalogIDs(t, httpListEnv)
	for _, tc := range demoQuestionCases() {
		if _, ok := listedIDs[tc.playbookID]; !ok {
			t.Fatalf("manifest question %q: playbook %q not present in /api/v0/query-playbooks listing", tc.manifestID, tc.playbookID)
		}
	}

	for _, tc := range demoQuestionCases() {
		tc := tc
		t.Run(tc.manifestID, func(t *testing.T) {
			t.Parallel()

			body := map[string]any{"playbook_id": tc.playbookID, "inputs": stringMapToAny(tc.inputs)}
			args := map[string]any{"playbook_id": tc.playbookID, "inputs": stringMapToAny(tc.inputs)}

			httpEnv := httpEnvelope(t, handler, http.MethodPost, "/api/v0/query-playbooks/resolve", body)
			mcpEnv, mcpSummary := mcpEnvelope(t, handler, "resolve_query_playbook", args)
			requireParity(t, "http resolve", "mcp resolve", extractComparable(t, httpEnv), extractComparable(t, mcpEnv))
			if mcpSummary == "" {
				t.Fatal("resolve_query_playbook MCP convenience summary is empty")
			}

			httpData, _ := httpEnv.Data.(map[string]any)
			mcpData, _ := mcpEnv.Data.(map[string]any)
			if gotHTTP, gotMCP := canonicalJSON(t, httpData), canonicalJSON(t, mcpData); gotHTTP != gotMCP {
				t.Fatalf("resolved playbook body drift for %q:\nhttp=%s\nmcp=%s", tc.playbookID, gotHTTP, gotMCP)
			}
		})
	}
}

// mountQueryPlaybookHandlerForDemoParity mounts the real QueryPlaybookHandler,
// the same handler the API and MCP server both wire in production, so this
// test proves the demo entries the same way the running stack serves them.
func mountQueryPlaybookHandlerForDemoParity(t *testing.T) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.QueryPlaybookHandler{Profile: query.ProfileLocalFullStack}
	handler.Mount(mux)
	return mux
}

// demoCatalogIDs extracts the set of playbook IDs from a query-playbooks list
// envelope's data.playbooks[].id field.
func demoCatalogIDs(t *testing.T, env *query.ResponseEnvelope) map[string]struct{} {
	t.Helper()

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("list envelope data type = %T, want map", env.Data)
	}
	rows, ok := data["playbooks"].([]any)
	if !ok {
		t.Fatalf("list envelope data.playbooks type = %T, want slice", data["playbooks"])
	}
	ids := make(map[string]struct{}, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if id := query.StringVal(row, "id"); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func stringMapToAny(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

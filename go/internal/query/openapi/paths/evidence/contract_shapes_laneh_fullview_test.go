// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneHFullView pins the Lane H gap-5 fix: the workflows[]
// items in GET /api/v0/investigation-workflows give the view=full shape a
// real schema instead of prose. The item is a anyOf keyed on the `view`
// query param: the compact InvestigationWorkflowSummary branch by default,
// the full InvestigationWorkflow branch when view=full. The full branch
// spells out required_inputs, required_evidence, optional_evidence,
// output_packet, tool_groups, starter_prompts, failure_modes, and
// missing_evidence_routes with their nested owners and presence notes.
func TestContractShapesLaneHFullView(t *testing.T) {
	t.Parallel()

	trimmed := strings.TrimSpace(InvestigationWorkflows)
	trimmed = strings.TrimSuffix(trimmed, ",")
	var doc map[string]any
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("fragment is not valid JSON: %v", err)
	}
	route, ok := doc["/api/v0/investigation-workflows"].(map[string]any)
	if !ok {
		t.Fatal("fragment paths missing /api/v0/investigation-workflows")
	}
	get, ok := route["get"].(map[string]any)
	if !ok {
		t.Fatal("route missing get")
	}
	responses, ok := get["responses"].(map[string]any)
	if !ok {
		t.Fatal("get missing responses")
	}
	okResp, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatal("get missing 200")
	}
	content, ok := okResp["content"].(map[string]any)
	if !ok {
		t.Fatal("200 missing content")
	}
	body, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatal("200 missing application/json")
	}
	schema, ok := body["schema"].(map[string]any)
	if !ok {
		t.Fatal("200 missing schema")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("200 schema missing properties")
	}
	workflows, ok := props["workflows"].(map[string]any)
	if !ok {
		t.Fatal("200 schema missing workflows")
	}
	items, ok := workflows["items"].(map[string]any)
	if !ok {
		t.Fatal("workflows has no items")
	}
	anyOf, ok := items["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("workflows items is not a two-branch anyOf keyed on the view param (compact vs full)")
	}
	branches := map[string]map[string]any{}
	for _, branch := range anyOf {
		bm, ok := branch.(map[string]any)
		if !ok {
			t.Fatal("anyOf branch is not an object")
		}
		title, _ := bm["title"].(string)
		if title == "" {
			t.Fatal("anyOf branch has no title naming its view")
		}
		branches[title] = bm
		if desc, _ := bm["description"].(string); desc == "" {
			t.Errorf("anyOf branch %q has no description", title)
		}
	}
	summary, ok := branches["compact"]
	if !ok {
		t.Fatal("anyOf has no compact branch (want title \"compact\")")
	}
	full, ok := branches["full"]
	if !ok {
		t.Fatal("anyOf has no full branch (want title \"full\")")
	}

	summaryProps, ok := summary["properties"].(map[string]any)
	if !ok {
		t.Fatal("compact branch has no properties")
	}
	for _, key := range []string{"id", "name", "version", "domain", "description"} {
		if _, ok := summaryProps[key].(map[string]any); !ok {
			t.Errorf("compact branch missing %q", key)
		}
	}

	fullProps, ok := full["properties"].(map[string]any)
	if !ok {
		t.Fatal("full branch has no properties")
	}
	for _, key := range []string{"id", "name", "version", "domain", "description"} {
		if _, ok := fullProps[key]; !ok {
			t.Errorf("full branch missing base property %q (view=full rows carry the summary keys too)", key)
		}
	}
	for _, key := range []string{
		"required_inputs", "required_evidence", "optional_evidence",
		"output_packet", "tool_groups", "starter_prompts",
		"failure_modes", "missing_evidence_routes",
	} {
		node, ok := fullProps[key].(map[string]any)
		if !ok {
			t.Errorf("full branch missing %q", key)
			continue
		}
		desc, _ := node["description"].(string)
		if !strings.Contains(desc, "query/investigation_workflow") {
			t.Errorf("full.%s description does not name its query/investigation_workflow owner", key)
		}
	}

	nested := func(key string, keys ...string) {
		t.Helper()
		node, ok := fullProps[key].(map[string]any)
		if !ok {
			t.Fatalf("full branch missing %q", key)
		}
		sub, ok := node["items"].(map[string]any)
		if sub == nil {
			sub, ok = node["properties"].(map[string]any)
		}
		if !ok {
			t.Fatalf("full.%s has neither items nor properties to pin", key)
		}
		var subProps map[string]any
		if node["items"] != nil {
			subProps, ok = sub["properties"].(map[string]any)
			if !ok {
				t.Fatalf("full.%s items is a bare object", key)
			}
		} else {
			subProps = sub
		}
		for _, k := range keys {
			if _, ok := subProps[k].(map[string]any); !ok {
				t.Errorf("full.%s missing %q", key, k)
			}
		}
	}
	nested("required_inputs", "name", "type", "required")
	nested("required_evidence", "key", "name", "description")
	nested("optional_evidence", "key", "name", "description")
	nested("tool_groups", "name", "tools")
	nested("failure_modes", "condition", "meaning", "recommended_action")
	nested("missing_evidence_routes", "evidence_key", "calls")

	outputPacket, ok := fullProps["output_packet"].(map[string]any)
	if !ok {
		t.Fatal("full branch missing output_packet")
	}
	outputProps, ok := outputPacket["properties"].(map[string]any)
	if !ok {
		t.Fatal("full.output_packet has no properties")
	}
	for _, key := range []string{"schema", "truth_labels", "sections"} {
		if _, ok := outputProps[key].(map[string]any); !ok {
			t.Errorf("full.output_packet missing %q", key)
		}
	}

	routes, ok := fullProps["missing_evidence_routes"].(map[string]any)
	if !ok {
		t.Fatal("full branch missing missing_evidence_routes")
	}
	routeItems, ok := routes["items"].(map[string]any)
	if !ok {
		t.Fatal("missing_evidence_routes has no items")
	}
	routeProps, ok := routeItems["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing_evidence_routes items has no properties")
	}
	calls, ok := routeProps["calls"].(map[string]any)
	if !ok {
		t.Fatal("missing_evidence_routes items missing calls")
	}
	callItems, ok := calls["items"].(map[string]any)
	if !ok {
		t.Fatal("calls has no items")
	}
	callProps, ok := callItems["properties"].(map[string]any)
	if !ok {
		t.Fatal("calls items has no properties")
	}
	for _, key := range []string{"id", "tool", "reason", "missing_evidence", "params", "expected_evidence"} {
		if _, ok := callProps[key].(map[string]any); !ok {
			t.Errorf("full.missing_evidence_routes.calls missing %q", key)
		}
	}

	// Instance satisfiability: a real compact row validates against the
	// compact branch and a real full row against the full branch, so the
	// anyOf admits both views (a strict oneOf admitted neither).
	compactRow := map[string]any{
		"id": "wf-1", "name": "w", "version": "v1",
		"domain": "d", "description": "desc",
	}
	for key := range compactRow {
		if _, ok := summaryProps[key].(map[string]any); !ok {
			t.Errorf("compact instance key %q not in compact branch", key)
		}
	}
	fullRow := map[string]any{
		"id": "wf-1", "name": "w", "version": "v1",
		"domain": "d", "description": "desc",
		"required_inputs": nil, "required_evidence": nil,
		"optional_evidence": nil, "output_packet": nil,
		"tool_groups": nil, "starter_prompts": nil,
		"failure_modes": nil, "missing_evidence_routes": nil,
	}
	for key := range fullRow {
		if _, ok := fullProps[key].(map[string]any); !ok {
			t.Errorf("full instance key %q not in full branch", key)
		}
	}
}

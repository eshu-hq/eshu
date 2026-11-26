// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"testing"
)

// TestContractShapesLane2Playbooks pins the lane-2 shape-completeness fixes
// for the query-playbooks fragments: the catalog list (Summary in compact
// view, Definition in full view) and the resolve output
// (ResolvedPlaybook).
func TestContractShapesLane2Playbooks(t *testing.T) {
	t.Parallel()

	t.Run("playbooks", func(t *testing.T) {
		t.Parallel()
		// Owner: playbook.Handler.list (query/playbook/handler.go) emits
		// playbook.Summary rows in compact view and playbook.Definition
		// rows in full view, plus playbook.VersionRef rows; the contract
		// documents the full Definition shape, of which the compact view
		// is the id/name/version/prompt_family/description projection.
		doc := lane2Fragment(t, Playbooks, "Playbooks")
		schema := lane2SchemaAt(t, doc, "/api/v0/query-playbooks", "get")
		props := lane2PropsOf(t, schema, "playbooks")
		lane2RequireExactKeys(t, props, "playbooks",
			"schema_version", "playbooks", "versions", "count", "total",
			"limit", "offset", "truncated", "next_offset")
		lane2RequireScalarTypes(t, props, "playbooks", map[string]string{
			"schema_version": "string", "playbooks": "array",
			"versions": "array", "count": "integer", "total": "integer",
			"limit": "integer", "offset": "integer", "truncated": "boolean",
			"next_offset": "integer",
		})
		lane2RequireDescribed(t, props, "playbooks",
			"schema_version", "playbooks", "versions", "count", "total",
			"limit", "offset", "truncated", "next_offset")

		// Owner: playbook.Definition; compact view projects Summary.
		pb := lane2ItemsOf(t, lane2Child(t, props, "playbooks", "playbooks"), "playbooks[]")
		pbProps := lane2PropsOf(t, pb, "playbooks[]")
		lane2RequireExactKeys(t, pbProps, "playbooks[]",
			"id", "name", "version", "prompt_family", "description",
			"required_inputs", "steps", "failure_modes")
		lane2RequireDescribed(t, pbProps, "playbooks[]",
			"id", "name", "version", "prompt_family", "description",
			"required_inputs", "steps", "failure_modes")

		// Owner: playbook.Input.
		in := lane2ItemsOf(t, lane2Child(t, pbProps, "required_inputs", "playbooks.in"), "playbooks.in[]")
		inProps := lane2PropsOf(t, in, "playbooks.in[]")
		lane2RequireExactKeys(t, inProps, "playbooks.in[]",
			"name", "type", "required", "description")
		lane2RequireScalarTypes(t, inProps, "playbooks.in[]", map[string]string{
			"required": "boolean",
		})
		lane2RequireDescribed(t, inProps, "playbooks.in[]",
			"name", "type", "required", "description")

		// Owner: playbook.Step.
		step := lane2ItemsOf(t, lane2Child(t, pbProps, "steps", "playbooks.step"), "playbooks.step[]")
		stepProps := lane2PropsOf(t, step, "playbooks.step[]")
		lane2RequireExactKeys(t, stepProps, "playbooks.step[]",
			"id", "tool", "params", "expected_truth", "evidence_expected",
			"drilldowns")
		lane2RequireDescribed(t, stepProps, "playbooks.step[]",
			"id", "tool", "params", "expected_truth", "evidence_expected",
			"drilldowns")

		// Owner: playbook.Param.
		param := lane2ItemsOf(t, lane2Child(t, stepProps, "params", "playbooks.param"), "playbooks.param[]")
		paramProps := lane2PropsOf(t, param, "playbooks.param[]")
		lane2RequireExactKeys(t, paramProps, "playbooks.param[]",
			"name", "from_input", "const_string", "const_int", "const_bool")
		lane2RequireDescribed(t, paramProps, "playbooks.param[]",
			"name", "from_input", "const_string", "const_int", "const_bool")

		// Owner: playbook.Drilldown.
		drill := lane2ItemsOf(t, lane2Child(t, stepProps, "drilldowns", "playbooks.drill"), "playbooks.drill[]")
		drillProps := lane2PropsOf(t, drill, "playbooks.drill[]")
		lane2RequireExactKeys(t, drillProps, "playbooks.drill[]",
			"tool", "reason")
		lane2RequireDescribed(t, drillProps, "playbooks.drill[]",
			"tool", "reason")

		// Owner: playbook.FailureMode.
		fm := lane2ItemsOf(t, lane2Child(t, pbProps, "failure_modes", "playbooks.fm"), "playbooks.fm[]")
		fmProps := lane2PropsOf(t, fm, "playbooks.fm[]")
		lane2RequireExactKeys(t, fmProps, "playbooks.fm[]",
			"condition", "meaning", "fallback")
		lane2RequireDescribed(t, fmProps, "playbooks.fm[]",
			"condition", "meaning", "fallback")

		// Owner: playbook.VersionRef.
		ver := lane2ItemsOf(t, lane2Child(t, props, "versions", "playbooks.ver"), "playbooks.ver[]")
		verProps := lane2PropsOf(t, ver, "playbooks.ver[]")
		lane2RequireExactKeys(t, verProps, "playbooks.ver[]", "id", "version")
		lane2RequireDescribed(t, verProps, "playbooks.ver[]", "id", "version")

		lane2RequireNoBareObjects(t, schema, "playbooks")
	})

	t.Run("playbook resolve", func(t *testing.T) {
		t.Parallel()
		// Owner: playbook.Handler.resolve (query/playbook/handler.go)
		// emits playbook.ResolvedPlaybook from Definition.Resolve.
		doc := lane2Fragment(t, Playbooks, "Playbooks")
		route, ok := doc["/api/v0/query-playbooks/resolve"].(map[string]any)
		if !ok {
			t.Fatalf("fragment paths missing /api/v0/query-playbooks/resolve")
		}
		op := lane2Child(t, route, "post", "resolve")
		responses := lane2Child(t, op, "responses", "resolve post")
		okResp := lane2Child(t, responses, "200", "resolve post")
		content := lane2Child(t, okResp, "content", "resolve post")
		body := lane2Child(t, content, "application/json", "resolve post")
		schema := lane2Child(t, body, "schema", "resolve post")
		props := lane2PropsOf(t, schema, "resolve")
		lane2RequireExactKeys(t, props, "resolve", "schema_version", "resolved")
		lane2RequireScalarTypes(t, props, "resolve", map[string]string{
			"schema_version": "string", "resolved": "object",
		})
		lane2RequireDescribed(t, props, "resolve", "schema_version", "resolved")

		// Owner: playbook.ResolvedPlaybook.
		resolved := lane2Child(t, props, "resolved", "resolve.resolved")
		resolvedProps := lane2PropsOf(t, resolved, "resolve.resolved")
		lane2RequireExactKeys(t, resolvedProps, "resolve.resolved",
			"playbook_id", "version", "prompt_family", "calls",
			"failure_modes")
		lane2RequireDescribed(t, resolvedProps, "resolve.resolved",
			"playbook_id", "version", "prompt_family", "calls",
			"failure_modes")

		// Owner: playbook.ResolvedCall.
		call := lane2ItemsOf(t, lane2Child(t, resolvedProps, "calls", "resolve.call"), "resolve.call[]")
		callProps := lane2PropsOf(t, call, "resolve.call[]")
		lane2RequireExactKeys(t, callProps, "resolve.call[]",
			"step_id", "tool", "arguments", "expected_truth",
			"evidence_expected", "drilldowns")
		lane2RequireDescribed(t, callProps, "resolve.call[]",
			"step_id", "tool", "arguments", "expected_truth",
			"evidence_expected", "drilldowns")

		args := lane2Child(t, callProps, "arguments", "resolve.args")
		if _, ok := args["additionalProperties"]; !ok {
			t.Errorf("resolve.args: missing additionalProperties for the concrete argument map")
		} else if desc, _ := args["description"].(string); desc == "" {
			t.Errorf("resolve.args: missing description naming key semantics")
		}

		// Owner: playbook.Drilldown.
		drill := lane2ItemsOf(t, lane2Child(t, callProps, "drilldowns", "resolve.drill"), "resolve.drill[]")
		drillProps := lane2PropsOf(t, drill, "resolve.drill[]")
		lane2RequireExactKeys(t, drillProps, "resolve.drill[]",
			"tool", "reason")
		lane2RequireDescribed(t, drillProps, "resolve.drill[]",
			"tool", "reason")

		// Owner: playbook.FailureMode.
		fm := lane2ItemsOf(t, lane2Child(t, resolvedProps, "failure_modes", "resolve.fm"), "resolve.fm[]")
		fmProps := lane2PropsOf(t, fm, "resolve.fm[]")
		lane2RequireExactKeys(t, fmProps, "resolve.fm[]",
			"condition", "meaning", "fallback")
		lane2RequireDescribed(t, fmProps, "resolve.fm[]",
			"condition", "meaning", "fallback")

		lane2RequireNoBareObjects(t, schema, "resolve")
	})
}

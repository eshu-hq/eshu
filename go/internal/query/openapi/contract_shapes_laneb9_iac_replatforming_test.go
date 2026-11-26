// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9IaCReplatforming pins the lane-B9 shape-completeness
// fixes for the replatforming plan, ownership-packet, rollup, and selector
// reads. Bare {"type": "object"} shapes gain properties (or a described
// open-map shape), and every 200-response property carries a description
// naming its Go owner.
func TestContractShapesLaneB9IaCReplatforming(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
	}

	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if node["type"] == "object" {
				_, hasProps := node["properties"]
				_, hasAdditional := node["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
			if node["type"] == "array" {
				items, ok := node["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: property %q is an array with no items", where, key)
					continue
				}
				if items["type"] == "object" {
					_, hasProps := items["properties"]
					_, hasAdditional := items["additionalProperties"]
					if !hasProps && !hasAdditional {
						t.Errorf("%s: property %q has bare object items", where, key)
					}
				}
			}
		}
	}

	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := make(map[string]bool, len(keys))
		for _, key := range keys {
			want[key] = true
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
		for key := range props {
			if !want[key] {
				t.Errorf("%s: phantom property %q not emitted by the owner", where, key)
			}
		}
	}

	schema200 := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			t.Fatal("spec has no paths object")
		}
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content := child(t, okResp, "content", path+" "+method+" 200")
		body := child(t, content, "application/json", path+" "+method+" 200")
		return child(t, body, "schema", path+" "+method+" 200")
	}

	safetyGateKeys := []string{"outcome", "read_only", "review_required", "refused_actions", "warnings", "redactions", "audit_expectation"}

	t.Run("replatforming plans", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/replatforming/plans"
		top := propsOf(t, schema200(t, "/api/v0/replatforming/plans", "post"), where)
		requireExactKeys(t, top, where,
			"story", "scope_kind", "account_id", "scope_id", "region", "arn",
			"finding_kinds", "plan", "plan_truth_level", "items_count",
			"ready_import_count", "refused_import_count", "wave_summaries",
			"blast_radius_summaries", "total_findings_count", "limit", "offset",
			"truncated", "next_offset", "recommended_next_calls", "truth_basis",
			"analysis_status", "limitations")
		requireDescribed(t, top, where,
			"story", "scope_kind", "account_id", "scope_id", "region", "arn",
			"finding_kinds", "plan", "plan_truth_level", "items_count",
			"ready_import_count", "refused_import_count", "wave_summaries",
			"blast_radius_summaries", "total_findings_count", "limit", "offset",
			"truncated", "next_offset", "recommended_next_calls", "truth_basis",
			"analysis_status", "limitations")
		plan := propsOf(t, top["plan"].(map[string]any), where+" plan")
		// Owner: iac.ReplatformingPlan via composeReplatformingPlan.
		requireExactKeys(t, plan, where+" plan",
			"contract_version", "scope", "items", "waves", "blast_radius_groups",
			"recommended_next_calls", "limitations", "non_goals")
		requireDescribed(t, plan, where+" plan",
			"contract_version", "scope", "items", "waves", "blast_radius_groups",
			"recommended_next_calls", "limitations", "non_goals")
		scope := propsOf(t, plan["scope"].(map[string]any), where+" plan.scope")
		// Owner: iac.ReplatformingPlanScope via normalizeReplatformingScope.
		requireExactKeys(t, scope, where+" plan.scope",
			"kind", "account", "region", "service", "workload", "repository",
			"environment", "resource")
		requireDescribed(t, scope, where+" plan.scope",
			"kind", "account", "region", "service", "workload", "repository",
			"environment", "resource")
		items := itemsOf(t, plan["items"].(map[string]any), where+" plan.items")
		iprops := propsOf(t, items, where+" plan.items[]")
		// Owner: iac.MigrationPacketItem via composeReplatformingPlan.
		requireExactKeys(t, iprops, where+" plan.items[]",
			"item_id", "provider", "resource_type", "stable_id", "source_state",
			"management_status", "finding_kind", "confidence", "freshness",
			"safety_gate", "source_layers", "owner_candidates", "import_candidate",
			"wave_id", "blast_radius_group")
		requireDescribed(t, iprops, where+" plan.items[]",
			"item_id", "provider", "resource_type", "stable_id", "source_state",
			"management_status", "finding_kind", "confidence", "freshness",
			"safety_gate", "source_layers", "owner_candidates", "import_candidate",
			"wave_id", "blast_radius_group")
		igate := propsOf(t, iprops["safety_gate"].(map[string]any), where+" plan.items[].safety_gate")
		requireExactKeys(t, igate, where+" plan.items[].safety_gate", safetyGateKeys...)
		requireDescribed(t, igate, where+" plan.items[].safety_gate", safetyGateKeys...)
		fresh := propsOf(t, iprops["freshness"].(map[string]any), where+" plan.items[].freshness")
		// Owner: querycontract.TruthFreshness.
		requireExactKeys(t, fresh, where+" plan.items[].freshness",
			"state", "observed_at", "detail", "cause", "next_check")
		requireDescribed(t, fresh, where+" plan.items[].freshness",
			"state", "observed_at", "detail", "cause", "next_check")
		sl := itemsOf(t, iprops["source_layers"].(map[string]any), where+" plan.items[].source_layers")
		slprops := propsOf(t, sl, where+" plan.items[].source_layers[]")
		// Owner: iac.ReplatformingSourceLayerStatus.
		requireExactKeys(t, slprops, where+" plan.items[].source_layers[]",
			"layer", "state", "detail")
		requireDescribed(t, slprops, where+" plan.items[].source_layers[]",
			"layer", "state", "detail")
		oc := itemsOf(t, iprops["owner_candidates"].(map[string]any), where+" plan.items[].owner_candidates")
		ocprops := propsOf(t, oc, where+" plan.items[].owner_candidates[]")
		// Owner: iac.ReplatformingOwnerCandidate.
		requireExactKeys(t, ocprops, where+" plan.items[].owner_candidates[]",
			"kind", "value", "confidence", "ambiguity_reasons")
		requireDescribed(t, ocprops, where+" plan.items[].owner_candidates[]",
			"kind", "value", "confidence", "ambiguity_reasons")
		ic := propsOf(t, iprops["import_candidate"].(map[string]any), where+" plan.items[].import_candidate")
		// Owner: iac.ReplatformingImportCandidate.
		requireExactKeys(t, ic, where+" plan.items[].import_candidate",
			"status", "resource_type", "import_block", "refusal_reasons")
		requireDescribed(t, ic, where+" plan.items[].import_candidate",
			"status", "resource_type", "import_block", "refusal_reasons")
		waves := itemsOf(t, plan["waves"].(map[string]any), where+" plan.waves")
		wprops := propsOf(t, waves, where+" plan.waves[]")
		// Owner: iac.MigrationWave.
		requireExactKeys(t, wprops, where+" plan.waves[]",
			"id", "order", "rationale", "item_ids")
		requireDescribed(t, wprops, where+" plan.waves[]",
			"id", "order", "rationale", "item_ids")
		brg := itemsOf(t, plan["blast_radius_groups"].(map[string]any), where+" plan.blast_radius_groups")
		bprops := propsOf(t, brg, where+" plan.blast_radius_groups[]")
		// Owner: iac.BlastRadiusGroup.
		requireExactKeys(t, bprops, where+" plan.blast_radius_groups[]",
			"id", "severity", "reason", "item_ids")
		requireDescribed(t, bprops, where+" plan.blast_radius_groups[]",
			"id", "severity", "reason", "item_ids")
		// Owner: replatformingPlanNextCalls emits {route, reason} entries plus
		// an optional params echo map on the paging entry; the contract pins
		// all three keys with params as a described open map.
		for _, holder := range []struct {
			props map[string]any
			where string
		}{{plan, where + " plan"}, {top, where}} {
			node := holder.props["recommended_next_calls"].(map[string]any)
			callItems, ok := node["items"].(map[string]any)
			if !ok {
				t.Errorf("%s: recommended_next_calls must be an array", holder.where)
				continue
			}
			callProps, ok := callItems["properties"].(map[string]any)
			if !ok {
				t.Errorf("%s: recommended_next_calls items must carry properties", holder.where)
				continue
			}
			requireExactKeys(t, callProps, holder.where+" recommended_next_calls[]",
				"route", "reason", "params")
			requireDescribed(t, callProps, holder.where+" recommended_next_calls[]",
				"route", "reason", "params")
			params, ok := callProps["params"].(map[string]any)
			if !ok {
				t.Errorf("%s: recommended_next_calls params must be an object", holder.where)
				continue
			}
			if _, ok := params["additionalProperties"]; !ok {
				t.Errorf("%s: recommended_next_calls params must carry additionalProperties", holder.where)
			}
		}
		ws := itemsOf(t, top["wave_summaries"].(map[string]any), where+" wave_summaries")
		wsprops := propsOf(t, ws, where+" wave_summaries[]")
		// Owner: iac.replatformingWaveSummary via replatformingPlanWaveSummaries.
		requireExactKeys(t, wsprops, where+" wave_summaries[]",
			"wave_id", "order", "item_count")
		requireDescribed(t, wsprops, where+" wave_summaries[]",
			"wave_id", "order", "item_count")
		bs := itemsOf(t, top["blast_radius_summaries"].(map[string]any), where+" blast_radius_summaries")
		bsprops := propsOf(t, bs, where+" blast_radius_summaries[]")
		// Owner: iac.replatformingBlastRadiusSummary via replatformingPlanBlastRadiusSummaries.
		requireExactKeys(t, bsprops, where+" blast_radius_summaries[]",
			"group_id", "severity", "item_count")
		requireDescribed(t, bsprops, where+" blast_radius_summaries[]",
			"group_id", "severity", "item_count")
	})

	t.Run("replatforming ownership packets", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/replatforming/ownership-packets"
		top := propsOf(t, schema200(t, "/api/v0/replatforming/ownership-packets", "post"), where)
		requireExactKeys(t, top, where,
			"scope_id", "account_id", "region", "finding_kinds", "story",
			"ownership_packets", "packets_count", "ambiguous_count",
			"unattributed_count", "rejected_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis",
			"analysis_status", "recommended_next_checks", "limitations")
		requireDescribed(t, top, where,
			"scope_id", "account_id", "region", "finding_kinds", "story",
			"ownership_packets", "packets_count", "ambiguous_count",
			"unattributed_count", "rejected_count", "total_findings_count",
			"limit", "offset", "truncated", "next_offset", "truth_basis",
			"analysis_status", "recommended_next_checks", "limitations")
		// ownership_packets items are a shared-component $ref outside this
		// lane's scope (components_replatforming.go); the route contract pins
		// the array plus its description here.
		requireKeys(t, top, where, "ownership_packets")
	})

	t.Run("replatforming rollups", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/replatforming/rollups"
		top := propsOf(t, schema200(t, "/api/v0/replatforming/rollups", "post"), where)
		requireExactKeys(t, top, where,
			"scope_id", "account_id", "region", "finding_kinds", "story",
			"dimensions", "source_state_totals", "readiness_totals",
			"rollup_findings_count", "total_findings_count", "limit", "offset",
			"truncated", "next_offset", "truth_basis", "analysis_status",
			"recommended_next_checks", "limitations")
		requireDescribed(t, top, where,
			"scope_id", "account_id", "region", "finding_kinds", "story",
			"dimensions", "source_state_totals", "readiness_totals",
			"rollup_findings_count", "total_findings_count", "limit", "offset",
			"truncated", "next_offset", "truth_basis", "analysis_status",
			"recommended_next_checks", "limitations")
		dims := propsOf(t, top["dimensions"].(map[string]any), where+" dimensions")
		// Owner: replatforming rollups handler dimensions map; account,
		// environment, and service values are shared-component $refs outside
		// this lane's scope.
		requireKeys(t, dims, where+" dimensions", "account", "environment", "service")
		requireDescribed(t, dims, where+" dimensions", "account", "environment", "service")
		// source_state_totals is a genuine open map: taxonomy value -> count.
		totals := top["source_state_totals"].(map[string]any)
		if _, ok := totals["additionalProperties"]; !ok {
			t.Errorf("%s: source_state_totals must carry additionalProperties", where)
		}
	})

	t.Run("replatforming selectors", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/replatforming/selectors"
		top := propsOf(t, schema200(t, "/api/v0/replatforming/selectors", "get"), where)
		requireExactKeys(t, top, where,
			"scopes", "count", "limit", "truncated", "empty_scope_count",
			"supported_scope_kinds", "finding_kinds", "page_sizes", "readiness")
		requireDescribed(t, top, where,
			"scopes", "count", "limit", "truncated", "empty_scope_count",
			"supported_scope_kinds", "finding_kinds", "page_sizes", "readiness")
		items := itemsOf(t, top["scopes"].(map[string]any), where+" scopes")
		sprops := propsOf(t, items, where+" scopes[]")
		// Owner: replatforming selectors handler scope rows.
		requireExactKeys(t, sprops, where+" scopes[]",
			"scope_id", "account_id", "region", "service", "label", "finding_count")
		requireDescribed(t, sprops, where+" scopes[]",
			"scope_id", "account_id", "region", "service", "label", "finding_count")
		ready := propsOf(t, top["readiness"].(map[string]any), where+" readiness")
		// Owner: replatformingSelectorReadiness.
		requireExactKeys(t, ready, where+" readiness", "state", "detail", "next_action")
		requireDescribed(t, ready, where+" readiness", "state", "detail", "next_action")
	})
}

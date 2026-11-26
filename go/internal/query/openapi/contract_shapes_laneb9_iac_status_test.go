// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB9IaCStatus pins the lane-B9 shape-completeness
// fixes for the Terraform config-vs-state drift findings and the IaC
// resource browse. Bare {"type": "object"} shapes gain properties (or a
// described open-map shape), and every 200-response property carries a
// description naming its Go owner.
func TestContractShapesLaneB9IaCStatus(t *testing.T) {
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

	t.Run("terraform config-state drift findings", func(t *testing.T) {
		t.Parallel()
		where := "POST /api/v0/terraform/config-state-drift/findings"
		top := propsOf(t, schema200(t, "/api/v0/terraform/config-state-drift/findings", "post"), where)
		requireExactKeys(t, top, where,
			"scope_id", "address", "outcome", "drift_kinds", "story", "outcome_groups",
			"findings_count", "total_findings_count", "limit", "offset", "truncated",
			"next_offset", "truth_basis", "analysis_status", "graph_projection_note",
			"limitations", "drift_findings")
		requireDescribed(t, top, where,
			"scope_id", "address", "outcome", "drift_kinds", "story", "outcome_groups",
			"findings_count", "total_findings_count", "limit", "offset", "truncated",
			"next_offset", "truth_basis", "analysis_status", "graph_projection_note",
			"limitations", "drift_findings")
		groups := itemsOf(t, top["outcome_groups"].(map[string]any), where+" outcome_groups")
		gprops := propsOf(t, groups, where+" outcome_groups[]")
		// Owner: drift.terraformConfigStateDriftOutcomeGroups emits
		// {outcome, count} maps.
		requireExactKeys(t, gprops, where+" outcome_groups[]", "outcome", "count")
		requireDescribed(t, gprops, where+" outcome_groups[]", "outcome", "count")
		items := itemsOf(t, top["drift_findings"].(map[string]any), where+" drift_findings")
		fprops := propsOf(t, items, where+" drift_findings[]")
		// Owner: drift.FindingRow; ambiguous_owner_candidates and evidence are
		// genuine reducer-admitted open maps, so the contract pins the open-map
		// shape plus key-semantics descriptions instead of exact keys.
		requireExactKeys(t, fprops, where+" drift_findings[]",
			"fact_id", "scope_id", "generation_id", "source_system", "canonical_id",
			"candidate_id", "candidate_kind", "outcome", "address", "drift_kind",
			"backend_kind", "locator_hash", "confidence", "ambiguous_owner_candidates",
			"ambiguous_owner_candidates_withheld_count", "evidence")
		requireDescribed(t, fprops, where+" drift_findings[]",
			"fact_id", "scope_id", "generation_id", "source_system", "canonical_id",
			"candidate_id", "candidate_kind", "outcome", "address", "drift_kind",
			"backend_kind", "locator_hash", "confidence", "ambiguous_owner_candidates",
			"ambiguous_owner_candidates_withheld_count", "evidence")
		for _, key := range []string{"ambiguous_owner_candidates", "evidence"} {
			node := fprops[key].(map[string]any)
			itemsNode, ok := node["items"].(map[string]any)
			if !ok {
				t.Errorf("%s: %s must be an array of open maps", where, key)
				continue
			}
			if _, ok := itemsNode["additionalProperties"]; !ok {
				t.Errorf("%s: %s items must carry additionalProperties for the reducer-admitted open map", where, key)
			}
		}
	})

	t.Run("iac resource browse", func(t *testing.T) {
		t.Parallel()
		where := "GET /api/v0/iac/resources"
		top := propsOf(t, schema200(t, "/api/v0/iac/resources", "get"), where)
		requireExactKeys(t, top, where,
			"kind", "count", "limit", "truncated", "summary", "next_cursor", "resources")
		requireDescribed(t, top, where,
			"kind", "count", "limit", "truncated", "summary", "next_cursor", "resources")
		summary := propsOf(t, top["summary"].(map[string]any), where+" summary")
		// Owner: iac.InventorySummary via PostgresIaCInventoryStore.Summary.
		requireExactKeys(t, summary, where+" summary",
			"total", "by_kind", "types", "providers", "modules", "repositories",
			"facet_limit", "truncated")
		requireDescribed(t, summary, where+" summary",
			"total", "by_kind", "types", "providers", "modules", "repositories",
			"facet_limit", "truncated")
		// by_kind is a genuine open map: resourceKind -> count.
		byKind := summary["by_kind"].(map[string]any)
		if _, ok := byKind["additionalProperties"]; !ok {
			t.Errorf("%s: summary.by_kind must carry additionalProperties", where)
		}
		// truncated is a genuine open map: dimension -> capped flag.
		trunc := summary["truncated"].(map[string]any)
		if _, ok := trunc["additionalProperties"]; !ok {
			t.Errorf("%s: summary.truncated must carry additionalProperties", where)
		}
		for _, facet := range []string{"types", "providers", "modules"} {
			fitems := itemsOf(t, summary[facet].(map[string]any), where+" summary."+facet)
			fprops := propsOf(t, fitems, where+" summary."+facet+"[]")
			// Owner: iac.InventoryFacet.
			requireExactKeys(t, fprops, where+" summary."+facet+"[]", "kind", "value", "count")
			requireDescribed(t, fprops, where+" summary."+facet+"[]", "kind", "value", "count")
		}
		ritems := itemsOf(t, summary["repositories"].(map[string]any), where+" summary.repositories")
		rprops := propsOf(t, ritems, where+" summary.repositories[]")
		// Owner: iac.InventoryFacet (kind omitted for repository facets).
		requireExactKeys(t, rprops, where+" summary.repositories[]", "value", "count")
		requireDescribed(t, rprops, where+" summary.repositories[]", "value", "count")
		cursor := propsOf(t, top["next_cursor"].(map[string]any), where+" next_cursor")
		// Owner: iac handle list keyset cursor.
		requireExactKeys(t, cursor, where+" next_cursor", "after_name", "after_id")
		requireDescribed(t, cursor, where+" next_cursor", "after_name", "after_id")
		items := itemsOf(t, top["resources"].(map[string]any), where+" resources")
		resprops := propsOf(t, items, where+" resources[]")
		// Owner: iac.resourceRow via resourceRowFromGraph.
		requireExactKeys(t, resprops, where+" resources[]",
			"id", "kind", "name", "resource_name", "type", "provider",
			"resource_service", "resource_category", "module", "repo_id",
			"relative_path", "line_number")
		requireDescribed(t, resprops, where+" resources[]",
			"id", "kind", "name", "resource_name", "type", "provider",
			"resource_service", "resource_category", "module", "repo_id",
			"relative_path", "line_number")
	})
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesCompareEnvironments pins the compare-environments
// response contract to its emitting owner
// (go/internal/query/compare/{handler,story,workload_lookup}.go): every
// 200-response object names its properties, every property carries a
// description naming the owner, and fixed-key nodes carry exact-set pins.
// Snapshot, evidence, limitation, and next-call rows vary by status and
// kind, so those nodes pin the always-present keys and document the
// conditional ones instead of an exact set the owner cannot satisfy.
func TestContractShapesCompareEnvironments(t *testing.T) {
	t.Parallel()

	doc := map[string]any{}
	trimmed := strings.TrimSpace(Compare)
	// Compare is the last fragment spliced into spec.go, so unlike every
	// other fragment it ends with the paths-object closer ("},", not ",").
	if !strings.HasSuffix(trimmed, "},") {
		t.Fatalf("compare fragment lost its paths-object closer; spec assembly changed")
	}
	trimmed = strings.TrimSuffix(trimmed, "},")
	if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
		t.Fatalf("compare fragment is not valid JSON: %v", err)
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
			prop, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := prop["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := map[string]bool{}
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
	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			prop, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := prop["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	route, ok := doc["/api/v0/compare/environments"].(map[string]any)
	if !ok {
		t.Fatal("fragment paths missing /api/v0/compare/environments")
	}
	op := child(t, route, "post", "compare")
	responses := child(t, op, "responses", "compare post")
	okResp := child(t, responses, "200", "compare post")
	content := child(t, okResp, "content", "compare post 200")
	body := child(t, content, "application/json", "compare post 200")
	schema := child(t, body, "schema", "compare post 200")
	props := propsOf(t, schema, "compare")

	// Owner: environmentCompareResponse builds exactly these keys;
	// answer_metadata comes from answer.AttachAnswerMetadata.
	topKeys := []string{
		"workload", "left", "right", "changed", "confidence",
		"reason", "limit", "truncated", "story", "summary", "shared",
		"dedicated", "evidence", "limitations", "recommended_next_calls",
		"coverage", "answer_metadata",
	}
	requireExactKeys(t, props, "compare", topKeys...)
	requireDescribed(t, props, "compare", topKeys...)
	requireScalarTypes(t, props, "compare", map[string]string{
		"confidence": "number", "reason": "string", "story": "string",
		"limit": "integer", "truncated": "boolean",
	})

	// Owner: workloadRowMap projects the workload graph row.
	workloadNode := child(t, props, "workload", "compare")
	// Owner: handler.go missingWorkloadResponse passes nil workload, so the
	// schema must carry nullable like the snapshot instance does.
	if nullable, _ := workloadNode["nullable"].(bool); !nullable {
		t.Error("compare.workload: schema promises null, so it needs nullable:true")
	}
	workload := propsOf(t, workloadNode, "compare.workload")
	requireExactKeys(t, workload, "compare.workload", "id", "name", "kind", "repo_id")
	requireDescribed(t, workload, "compare.workload", "id", "name", "kind", "repo_id")
	requireScalarTypes(t, workload, "compare.workload", map[string]string{
		"id": "string", "name": "string", "kind": "string", "repo_id": "string",
	})

	// Owner: environmentSnapshot returns these six keys in every status
	// (present, inferred, unsupported, missing); instance is the 5-key
	// instance object when status is present and null otherwise.
	for _, side := range []string{"left", "right"} {
		where := "compare." + side
		snap := propsOf(t, child(t, props, side, "compare"), where)
		requireExactKeys(t, snap, where,
			"environment", "status", "instance", "cloud_resources", "provenance", "reason")
		requireDescribed(t, snap, where,
			"environment", "status", "instance", "cloud_resources", "provenance", "reason")
		requireScalarTypes(t, snap, where, map[string]string{
			"environment": "string", "status": "string", "reason": "string",
		})
		instance := child(t, snap, "instance", where)
		if _, ok := instance["properties"]; !ok {
			t.Errorf("%s.instance: present-status instance needs properties", where)
		} else {
			instProps := propsOf(t, instance, where+".instance")
			requireExactKeys(t, instProps, where+".instance",
				"id", "name", "kind", "environment", "workload_id")
			requireDescribed(t, instProps, where+".instance",
				"id", "name", "kind", "environment", "workload_id")
		}
		// Owner: handler.go resource rows carry exactly 17 keys.
		resProps := propsOf(t, itemsOf(t, child(t, snap, "cloud_resources", where), where), where+".cloud_resources[]")
		requireExactKeys(t, resProps, where+".cloud_resources[]",
			"id", "name", "environment", "kind", "provider", "confidence",
			"reason", "relationship_basis", "resolution_mode", "evidence_source",
			"service_anchor_source", "service_anchor_reason", "source_fact_id",
			"stable_fact_key", "source_system", "source_record_id", "collector_kind")
		requireDescribed(t, resProps, where+".cloud_resources[]",
			"id", "name", "environment", "kind", "provider", "confidence",
			"reason", "relationship_basis", "resolution_mode", "evidence_source",
			"service_anchor_source", "service_anchor_reason", "source_fact_id",
			"stable_fact_key", "source_system", "source_record_id", "collector_kind")
		requireScalarTypes(t, resProps, where+".cloud_resources[]", map[string]string{
			"confidence": "number",
		})
		// Owner: provenance rows vary by snapshot status — present carries
		// kind/source/value/reason, inferred adds environment/relative_path —
		// so the rows stay open maps and the pin requires additionalProperties
		// with the conditional keys documented, not an exact set no status
		// satisfies.
		provItems := itemsOf(t, child(t, snap, "provenance", where), where)
		if _, ok := provItems["additionalProperties"]; !ok {
			t.Errorf("%s.provenance[]: open attribute map needs additionalProperties", where)
		}
		provDesc, _ := provItems["description"].(string)
		for _, key := range []string{"kind", "source", "value", "reason", "environment", "relative_path"} {
			if !strings.Contains(provDesc, key) {
				t.Errorf("%s.provenance[]: description must name conditional key %q", where, key)
			}
		}
	}

	// Owner: environmentCompareResponse wraps changed in cloud_resources;
	// diffCloudResources rows carry exactly these 8 keys.
	changed := propsOf(t, child(t, props, "changed", "compare"), "compare.changed")
	requireExactKeys(t, changed, "compare.changed", "cloud_resources")
	changedProps := propsOf(t, itemsOf(t, child(t, changed, "cloud_resources", "compare.changed"), "compare.changed"), "compare.changed.cloud_resources[]")
	requireExactKeys(t, changedProps, "compare.changed.cloud_resources[]",
		"id", "name", "kind", "provider", "confidence", "reason", "change", "environment")
	requireDescribed(t, changedProps, "compare.changed.cloud_resources[]",
		"id", "name", "kind", "provider", "confidence", "reason", "change", "environment")
	requireScalarTypes(t, changedProps, "compare.changed.cloud_resources[]", map[string]string{
		"confidence": "number",
	})

	// Owner: environmentCompareSummary returns exactly these 7 keys.
	summary := propsOf(t, child(t, props, "summary", "compare"), "compare.summary")
	requireExactKeys(t, summary, "compare.summary",
		"comparison", "left_status", "right_status", "shared_resource_count",
		"left_dedicated_resource_count", "right_dedicated_resource_count",
		"changed_resource_count")
	requireDescribed(t, summary, "compare.summary",
		"comparison", "left_status", "right_status", "shared_resource_count",
		"left_dedicated_resource_count", "right_dedicated_resource_count",
		"changed_resource_count")
	requireScalarTypes(t, summary, "compare.summary", map[string]string{
		"comparison": "string", "left_status": "string", "right_status": "string",
		"shared_resource_count": "integer", "left_dedicated_resource_count": "integer",
		"right_dedicated_resource_count": "integer", "changed_resource_count": "integer",
	})

	// Owner: environmentCompareResponse wraps shared/dedicated rows built
	// by sharedEnvironmentResource (8 keys) and dedicatedEnvironmentResource
	// (7 keys) — projections of the 17-key snapshot rows, not the rows
	// themselves. Dedicated splits left/right.
	shared := propsOf(t, child(t, props, "shared", "compare"), "compare.shared")
	requireExactKeys(t, shared, "compare.shared", "cloud_resources")
	sharedProps := propsOf(t, itemsOf(t, child(t, shared, "cloud_resources", "compare.shared"), "compare.shared"), "compare.shared.cloud_resources[]")
	requireExactKeys(t, sharedProps, "compare.shared.cloud_resources[]",
		"id", "name", "kind", "provider", "left_environment", "right_environment", "confidence", "reason")
	requireDescribed(t, sharedProps, "compare.shared.cloud_resources[]",
		"id", "name", "kind", "provider", "left_environment", "right_environment", "confidence", "reason")
	requireScalarTypes(t, sharedProps, "compare.shared.cloud_resources[]", map[string]string{
		"confidence": "number",
	})
	dedicated := propsOf(t, child(t, props, "dedicated", "compare"), "compare.dedicated")
	requireExactKeys(t, dedicated, "compare.dedicated", "left", "right")
	for _, side := range []string{"left", "right"} {
		sideNode := propsOf(t, child(t, dedicated, side, "compare.dedicated"), "compare.dedicated."+side)
		requireExactKeys(t, sideNode, "compare.dedicated."+side, "environment", "cloud_resources")
		requireDescribed(t, sideNode, "compare.dedicated."+side, "environment", "cloud_resources")
		dedicatedProps := propsOf(t, itemsOf(t, child(t, sideNode, "cloud_resources", "compare.dedicated."+side), "compare.dedicated."+side), "compare.dedicated."+side+".cloud_resources[]")
		requireExactKeys(t, dedicatedProps, "compare.dedicated."+side+".cloud_resources[]",
			"id", "name", "kind", "provider", "environment", "confidence", "reason")
		requireDescribed(t, dedicatedProps, "compare.dedicated."+side+".cloud_resources[]",
			"id", "name", "kind", "provider", "environment", "confidence", "reason")
		requireScalarTypes(t, dedicatedProps, "compare.dedicated."+side+".cloud_resources[]", map[string]string{
			"confidence": "number",
		})
	}

	// Owner: environmentCompareEvidence rows are kind-dispatched — the
	// workload row, snapshot-provenance rows (+side/+environment), and
	// resource rows each carry a fixed set. Items stay open maps because
	// the row shape depends on kind, so the pin requires the dispatch
	// documented on the array instead of an exact set no row satisfies.
	evidence := child(t, props, "evidence", "compare")
	evidenceDesc, _ := evidence["description"].(string)
	if evidenceDesc == "" {
		t.Error("compare.evidence: array needs its kind-dispatched row shapes described")
	}

	// Owner: environmentCompareLimitations rows dispatch on kind; every row
	// carries kind and reason.
	limitations := child(t, props, "limitations", "compare")
	limitationsDesc, _ := limitations["description"].(string)
	if limitationsDesc == "" {
		t.Error("compare.limitations: array needs its kind-dispatched row shapes described")
	}

	// Owner: environmentCompareNextCalls rows carry exactly tool, reason,
	// and arguments; argument keys depend on the tool.
	nextCalls := itemsOf(t, child(t, props, "recommended_next_calls", "compare"), "compare.recommended_next_calls")
	if _, ok := nextCalls["properties"]; !ok {
		if _, ok := nextCalls["additionalProperties"]; !ok {
			t.Error("compare.recommended_next_calls[]: rows need properties or documented additionalProperties")
		}
	}
	nextDesc, _ := nextCalls["description"].(string)
	if nextDesc == "" {
		t.Error("compare.recommended_next_calls[]: rows need tool-conditional argument keys described")
	}

	// Owner: environmentCompareCoverage returns exactly these 9 keys.
	coverage := propsOf(t, child(t, props, "coverage", "compare"), "compare.coverage")
	requireExactKeys(t, coverage, "compare.coverage",
		"query_shape", "comparison_basis", "freshness_state", "left_status",
		"right_status", "left_truncated", "right_truncated", "limit", "truncated")
	requireDescribed(t, coverage, "compare.coverage",
		"query_shape", "comparison_basis", "freshness_state", "left_status",
		"right_status", "left_truncated", "right_truncated", "limit", "truncated")

	// Owner: answer.AttachAnswerMetadata derives the standard 8-key
	// companion from the built response.
	meta := propsOf(t, child(t, props, "answer_metadata", "compare"), "compare.answer_metadata")
	requireExactKeys(t, meta, "compare.answer_metadata",
		"schema_version", "evidence_handles", "missing_evidence", "limitations",
		"truncated", "coverage", "partial_reasons", "recommended_next_calls")
	requireDescribed(t, meta, "compare.answer_metadata",
		"schema_version", "evidence_handles", "missing_evidence", "limitations",
		"truncated", "coverage", "partial_reasons", "recommended_next_calls")
}

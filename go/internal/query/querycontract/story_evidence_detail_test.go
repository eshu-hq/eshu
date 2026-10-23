// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"reflect"
	"testing"
)

func storyDetailBody() map[string]any {
	return map[string]any{
		"service_name":         "checkout-service",
		"dependents":           []map[string]any{{"repo_id": "r1", "repository": "consumer", "sample_paths": []string{"a.yaml"}}},
		"downstream_consumers": map[string]any{"graph_dependent_count": 1, "graph_dependents": []map[string]any{{"repo_id": "r1", "repository": "consumer"}}, "truncated": false},
		"ci_cd_evidence":       map[string]any{"run_count": 3},
		"code_to_runtime_trace": map[string]any{
			"status": "complete",
			"segments": []map[string]any{
				{"name": "ci_cd", "evidence_count": 1, "evidence": []map[string]any{{"evidence_summary": map[string]any{"run_count": 3}}}, "evidence_summary": map[string]any{"run_count": 3}},
				{"name": "code_entrypoints", "evidence_count": 12, "evidence": numberedTraceRows(12)},
			},
		},
		"evidence_graph": map[string]any{
			"edge_count": 1,
			"edges":      []map[string]any{{"id": "e1", "source": "a", "target": "b", "relationship_type": "DEPLOYS_FROM", "resolved_id": "res1", "rationale": "long text"}},
			"nodes": []map[string]any{
				{"id": "a", "label": "A", "kind": "repository", "role": "source_repository", "scope_key": "s"},
				{"id": "b", "label": "B", "kind": "repository", "role": "deployment_configuration"},
				{"id": "c", "label": "C", "kind": "repository", "role": "downstream_consumer"},
			},
		},
	}
}

func numberedTraceRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := range count {
		rows = append(rows, map[string]any{"type": "api_endpoint", "path": "/v1/" + string(rune('a'+i)), "spec_path": "spec.yaml"})
	}
	return rows
}

// TestApplyStoryEvidenceDetailHandlesMarksEveryCut proves each family the
// handles view cuts carries its marker and an omission with its total (a lower
// bound for the investigation repositories, which record no count past 50), and
// that ship-once pointers replace the duplicate row lists (#8798).
func TestApplyStoryEvidenceDetailHandlesMarksEveryCut(t *testing.T) {
	t.Parallel()

	body := storyDetailBody()
	omissions := ApplyStoryEvidenceDetail(body, ContextEvidenceDetailHandles)

	if got := StringVal(body, "evidence_detail"); got != ContextEvidenceDetailHandles {
		t.Fatalf("evidence_detail = %q, want handles", got)
	}
	bySection := map[string]TruthOmission{}
	for _, omission := range omissions {
		bySection[omission.Section] = omission
	}

	segments := MapSliceValue(MapValue(body, "code_to_runtime_trace"), "segments")
	if _, has := segments[0]["evidence_summary"]; has {
		t.Fatal("ci_cd evidence_summary ships twice; want it at ci_cd_evidence only")
	}
	entry := segments[1]
	if got := len(MapSliceValue(entry, "evidence")); got != storyTracePreviewLimit {
		t.Fatalf("code_entrypoints evidence = %d rows, want preview %d", got, storyTracePreviewLimit)
	}
	if !BoolVal(entry, "truncated") || IntVal(entry, "evidence_shipped") != storyTracePreviewLimit || IntVal(entry, "evidence_omitted") != 2 || IntVal(entry, "evidence_count") != 12 {
		t.Fatalf("code_entrypoints cut markers = %#v, want truncated, shipped 10, omitted 2, count 12", entry)
	}
	if got := MapSliceValue(entry, "evidence")[0]; !reflect.DeepEqual(got, map[string]any{"type": "api_endpoint", "path": "/v1/a"}) {
		t.Fatalf("trace handle row = %#v, want identity keys only", got)
	}
	if got := bySection["code_to_runtime_trace.code_entrypoints.evidence"].Total; got != 12 {
		t.Fatalf("code_entrypoints omission total = %d, want 12", got)
	}

	downstream := MapValue(body, "downstream_consumers")
	if _, has := downstream["graph_dependents"]; has || StringVal(downstream, "graph_dependents_shipped_at") != "dependents" {
		t.Fatalf("downstream_consumers = %#v, want rows dropped and graph_dependents_shipped_at=dependents", downstream)
	}
	if bySection["downstream_consumers.graph_dependents"].Detail != contextEvidenceOmitted {
		t.Fatalf("downstream omission = %#v, want detail omitted", bySection["downstream_consumers.graph_dependents"])
	}
	if got := MapSliceValue(body, "dependents")[0]; len(got) != 2 {
		t.Fatalf("dependents row = %#v, want repo_id and repository only", got)
	}

	graph := MapValue(body, "evidence_graph")
	if got := len(MapSliceValue(graph, "nodes")); got != 2 {
		t.Fatalf("evidence_graph nodes = %d, want the 2 nodes an edge touches", got)
	}
	if IntVal(graph, "node_count") != 3 || !BoolVal(graph, "nodes_truncated") || IntVal(graph, "nodes_omitted") != 1 {
		t.Fatalf("evidence_graph markers = %#v, want node_count 3, nodes_truncated, nodes_omitted 1", graph)
	}
	if edge := MapSliceValue(graph, "edges")[0]; edge["rationale"] != nil || edge["resolved_id"] != "res1" {
		t.Fatalf("edge handle = %#v, want resolved_id kept and rationale dropped", edge)
	}
	if drilldown := MapValue(body, "evidence_detail_drilldown"); StringVal(drilldown, "artifact_key") != "resolved_id" {
		t.Fatalf("evidence_detail_drilldown = %#v, want artifact_key resolved_id", drilldown)
	}
}

// TestApplyStoryEvidenceDetailNeverMutatesSharedRows proves the shaper builds
// new maps: a body assembled from read-model rows is left intact.
func TestApplyStoryEvidenceDetailNeverMutatesSharedRows(t *testing.T) {
	t.Parallel()

	body := storyDetailBody()
	shared := body["dependents"].([]map[string]any)[0]
	segment := MapSliceValue(MapValue(body, "code_to_runtime_trace"), "segments")[1]
	node := MapSliceValue(MapValue(body, "evidence_graph"), "nodes")[0]

	ApplyStoryEvidenceDetail(body, ContextEvidenceDetailHandles)

	if _, has := shared["sample_paths"]; !has {
		t.Fatal("dependents row lost sample_paths; the shaper mutated a shared row")
	}
	if len(MapSliceValue(segment, "evidence")) != 12 {
		t.Fatal("trace segment evidence was cut in place; the shaper mutated a shared segment")
	}
	if _, has := node["scope_key"]; !has {
		t.Fatal("evidence_graph node lost scope_key; the shaper mutated a shared row")
	}
}

// TestApplyStoryEvidenceDetailTolerantOfSparseBodies proves a story with none
// of the bulky families neither panics nor reports an omission.
func TestApplyStoryEvidenceDetailTolerantOfSparseBodies(t *testing.T) {
	t.Parallel()

	body := map[string]any{"service_name": "checkout-service"}
	if omissions := ApplyStoryEvidenceDetail(body, ContextEvidenceDetailHandles); len(omissions) != 0 {
		t.Fatalf("omissions = %#v, want none", omissions)
	}
	if _, has := body["evidence_detail_drilldown"]; has {
		t.Fatal("evidence_detail_drilldown set with nothing reduced")
	}
	if got := StringVal(body, "evidence_detail"); got != ContextEvidenceDetailHandles {
		t.Fatalf("evidence_detail = %q, want handles", got)
	}
}

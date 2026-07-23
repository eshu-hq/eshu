// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestGoldenSnapshotTraceDeploymentChainRequiresCanonicalPlatformIdentity(t *testing.T) {
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	mcpShape, ok := snapshot.QueryShapes.MCP["trace_deployment_chain"]
	if !ok {
		t.Fatal("query_shapes.mcp missing trace_deployment_chain")
	}
	if got, want := mcpShape.Arguments["service_name"], "api-svc"; got != want {
		t.Fatalf("MCP trace_deployment_chain service_name = %#v, want rich service fixture %q", got, want)
	}
	if !slices.Contains(mcpShape.RequiredResponseFields, "deployment_source_limits") {
		t.Fatal("MCP trace_deployment_chain required fields missing deployment_source_limits")
	}
	for _, field := range []string{"topology_edges", "provisioned_platforms", "runtime_topology_limits", "cloud_resource_limits", "k8s_resource_limits"} {
		if !slices.Contains(mcpShape.RequiredResponseFields, field) {
			t.Fatalf("MCP trace_deployment_chain required fields missing %s", field)
		}
	}

	shape, ok := snapshot.QueryShapes.HTTP["POST /api/v0/impact/trace-deployment-chain"]
	if !ok {
		t.Fatal("query_shapes.http missing positive deployment topology trace")
	}
	if got, want := shape.RequestBody["service_name"], "deployable-config"; got != want {
		t.Fatalf("HTTP trace service_name = %#v, want positive runtime fixture %q", got, want)
	}
	if !shape.Envelope {
		t.Fatal("HTTP positive deployment topology trace must assert truth envelope")
	}
	for _, identityPath := range []string{
		"data.instances[].platforms[].platform_id",
		"data.instances[].platforms[].topology_basis",
		"data.instances[].platforms[].topology_edges[].relationship_type",
		"data.instances[].platforms[].topology_edges[].source_id",
		"data.instances[].platforms[].topology_edges[].target_id",
		"data.topology_edges[].relationship_type",
		"data.topology_edges[].source_id",
		"data.topology_edges[].target_id",
		"data.topology_edges[].properties",
		"data.topology_edges[].properties.evidence_source",
		"data.runtime_topology_limits.instances.limit",
		"data.runtime_topology_limits.instances.truncated",
		"data.runtime_topology_limits.platform_edges.limit",
		"data.runtime_topology_limits.platform_edges.truncated",
		"data.deployment_sources[].relationship_type",
		"data.deployment_sources[].source_id",
		"data.deployment_sources[].target_id",
		"data.deployment_source_limits.limit",
		"data.deployment_source_limits.returned_count",
		"data.deployment_source_limits.truncated",
		"data.cloud_resource_limits.limit",
		"data.cloud_resource_limits.truncated",
		"data.k8s_resource_limits.limit",
		"data.k8s_resource_limits.query_sentinel_limit",
		"data.k8s_resource_limits.deployment_source_query_sentinel_limit",
		"data.k8s_resource_limits.returned_count",
		"data.k8s_resource_limits.content_observed_count_is_lower_bound",
		"data.k8s_resource_limits.deployment_source_observed_count_is_lower_bound",
		"data.k8s_resource_limits.truncated",
	} {
		if !slices.Contains(shape.RequiredJSONPaths, identityPath) {
			t.Fatalf("trace_deployment_chain.required_json_paths missing %q", identityPath)
		}
	}
	if slices.Contains(shape.RequiredJSONPaths, "data.provisioned_platforms") {
		t.Fatal("trace_deployment_chain must not require non-empty provisioned_platforms; an empty array is truthful when the selected workload has no infrastructure dependency")
	}
	if got, want := shape.RequiredJSONValues["data.instances[].platforms[].topology_edges[].relationship_type"], "RUNS_ON"; got != want {
		t.Fatalf("trace_deployment_chain RUNS_ON pin = %#v, want %#v", got, want)
	}
	if got, want := shape.RequiredJSONValues["data.deployment_fact_summary.deployment_truth_tier"], "runtime_confirmed"; got != want {
		t.Fatalf("trace_deployment_chain deployment_truth_tier pin = %#v, want %#v", got, want)
	}
	if got, want := mcpShape.RequiredJSONValues["deployment_fact_summary.overall_confidence_reason"], "no_deployment_evidence"; got != want {
		t.Fatalf("MCP trace_deployment_chain deployment_truth_tier pin = %#v, want %#v", got, want)
	}
	for path, want := range map[string]any{
		"data.repo_id":                             "repository:r_217415d9",
		"data.workload_id":                         "workload:deployable-config",
		"data.instances[].instance_id":             "workload-instance:deployable-config:prod",
		"data.instances[].platforms[].platform_id": "platform:kubernetes:none:prod:prod:none",
		"data.image_refs[]":                        "ghcr.io/eshu-hq/supply-chain-demo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Fatalf("trace_deployment_chain required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
	// #5471 review round 2 P2: pin exact counts alongside the image_refs[]/
	// k8s_resources[] contains-check above so a P0-class leak (an extra,
	// wrong image_ref or k8s_resource row from a DIFFERENT workload) fails
	// the gate even when the correct row is also present. JSON numbers
	// decode to float64 through LoadSnapshot's json.Unmarshal into `any`.
	for path, want := range map[string]float64{
		"data.deployment_fact_summary.image_ref_count":    1,
		"data.deployment_fact_summary.k8s_resource_count": 1,
		// #5638: pins the read-side live_instance_count derived from the
		// identity-bound Deployment+ReplicaSet facts (both ready_replicas=3,
		// same ArgoCD tracking-id -- MAX not SUM, so 3, never 6).
		"data.deployment_fact_summary.live_instance_count": 3,
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Fatalf("trace_deployment_chain required_json_values[%q] = %#v, want %v", path, got, want)
		}
	}
	// #5663: live_instance_count is a bounded, per-anchor read capped at
	// serviceStoryItemLimit, so the read-side summary now discloses whether the
	// reported count is exact or a truncated lower bound via the mandatory
	// sibling live_instance_count_truncated. This fixture's anchor matches far
	// fewer than the cap, so the truthful pin is false -- pin it explicitly so a
	// regression that drops or misserializes the field (leaving live_instance_count
	// silently unqualified) fails the gate. JSON booleans decode to Go bool
	// through LoadSnapshot's json.Unmarshal into `any`.
	if got, ok := shape.RequiredJSONValues["data.deployment_fact_summary.live_instance_count_truncated"]; !ok || got != false {
		t.Fatalf("trace_deployment_chain required_json_values[live_instance_count_truncated] = %#v (present=%v), want false", got, ok)
	}
	wantObjectMatches := map[string][]map[string]any{
		"data.instances[].platforms[].topology_edges[]": {
			{
				"relationship_type": "RUNS_ON",
				"source_id":         "workload-instance:deployable-config:prod",
				"target_id":         "platform:kubernetes:none:prod:prod:none",
			},
		},
		"data.topology_edges[]": {
			{
				"relationship_type": "DEFINES",
				"source_id":         "repository:r_217415d9",
				"target_id":         "workload:deployable-config",
			},
			{
				"relationship_type": "INSTANCE_OF",
				"source_id":         "workload-instance:deployable-config:prod",
				"target_id":         "workload:deployable-config",
			},
		},
		"data.deployment_sources[]": {
			{
				"relationship_type": "DEPLOYMENT_SOURCE",
				"source_id":         "workload-instance:deployable-config:prod",
				"target_id":         "repository:r_1f68383d",
			},
		},
		"data.k8s_resources[]": {
			{
				"entity_name":     "deployable-source",
				"entity_type":     "K8sResource",
				"kind":            "Deployment",
				"namespace":       "production",
				"repo_id":         "repository:r_217415d9",
				"relative_path":   "k8s/deployment.yaml",
				"source_root":     "k8s",
				"controller_kind": "argocd_application",
				"container_images": []any{
					"ghcr.io/eshu-hq/supply-chain-demo@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
				},
			},
		},
	}
	if !reflect.DeepEqual(shape.RequiredJSONObjectMatches, wantObjectMatches) {
		t.Fatalf("trace_deployment_chain object matches = %#v, want %#v", shape.RequiredJSONObjectMatches, wantObjectMatches)
	}
}

func TestGoldenSnapshotTraceDeploymentChainRejectsReversedExactEndpoints(t *testing.T) {
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.HTTP["POST /api/v0/impact/trace-deployment-chain"]
	response := fakeQueryShapeResponse(shape)
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal valid fixture response: %v", err)
	}
	if finding := EvaluateQueryShape("trace-deployment-chain", shape, raw); !finding.OK {
		t.Fatalf("valid exact endpoint fixture failed: %s", finding.Detail)
	}

	data := response["data"].(map[string]any)
	edges := data["topology_edges"].([]any)
	first := edges[0].(map[string]any)
	first["source_id"] = "workload:deployable-config"
	first["target_id"] = "repository:r_217415d9"
	mutated, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal mutated fixture response: %v", err)
	}
	if finding := EvaluateQueryShape("trace-deployment-chain", shape, mutated); finding.OK {
		t.Fatalf("reversed exact endpoint passed unexpectedly: %s", finding.Detail)
	}
}

// TestGoldenSnapshotTraceDeploymentChainDeclaredObjectPinsLiveInstanceCount
// locks the #5639 declared-object-anchor trace shape (keyed with the
// ?anchor=declared-object query-string suffix so it reaches the same handler
// with the supply-chain-demo-db body). It pins the read-side live_instance_count
// derived through the declared kind+namespace+name anchor and its mandatory
// #5663 truncation sibling, so a regression that drops either the value or the
// truncation qualifier on the declared-object path fails the static gate rather
// than depending only on the live replay comparison.
func TestGoldenSnapshotTraceDeploymentChainDeclaredObjectPinsLiveInstanceCount(t *testing.T) {
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP["POST /api/v0/impact/trace-deployment-chain?anchor=declared-object"]
	if !ok {
		t.Fatal("query_shapes.http missing #5639 declared-object deployment trace")
	}
	if got, want := shape.RequestBody["service_name"], "supply-chain-demo-db"; got != want {
		t.Fatalf("declared-object trace service_name = %#v, want %q", got, want)
	}
	// JSON numbers decode to float64 through LoadSnapshot's json.Unmarshal into `any`.
	if got, want := shape.RequiredJSONValues["data.deployment_fact_summary.live_instance_count"], float64(2); got != want {
		t.Fatalf("declared-object trace live_instance_count pin = %#v, want %v", got, want)
	}
	// #5663: the declared-object anchor's read is bounded by the same
	// serviceStoryItemLimit cap; this fixture matches well under it, so the
	// truthful truncation pin is false. Lock it so the field cannot silently
	// disappear from the declared-object trace.
	if got, ok := shape.RequiredJSONValues["data.deployment_fact_summary.live_instance_count_truncated"]; !ok || got != false {
		t.Fatalf("declared-object trace required_json_values[live_instance_count_truncated] = %#v (present=%v), want false", got, ok)
	}
}

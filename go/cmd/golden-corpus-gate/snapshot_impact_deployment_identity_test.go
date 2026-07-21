// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
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
	if got, want := mcpShape.RequiredJSONValues["data.deployment_fact_summary.overall_confidence_reason"], "no_deployment_evidence"; got != want {
		t.Fatalf("MCP trace_deployment_chain deployment_truth_tier pin = %#v, want %#v", got, want)
	}
}

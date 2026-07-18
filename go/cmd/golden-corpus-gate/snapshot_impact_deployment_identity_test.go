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
		"data.deployment_sources[].relationship_type",
		"data.deployment_sources[].source_id",
		"data.deployment_sources[].target_id",
	} {
		if !slices.Contains(shape.RequiredJSONPaths, identityPath) {
			t.Fatalf("trace_deployment_chain.required_json_paths missing %q", identityPath)
		}
	}
}

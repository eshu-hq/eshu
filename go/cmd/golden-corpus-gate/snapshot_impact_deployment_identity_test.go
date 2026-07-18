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
	shape, ok := snapshot.QueryShapes.MCP["trace_deployment_chain"]
	if !ok {
		t.Fatal("query_shapes.mcp missing trace_deployment_chain")
	}
	for _, identityPath := range []string{
		"instances[].platforms[].platform_id",
		"deployment_sources[].relationship_type",
		"deployment_sources[].source_id",
		"deployment_sources[].target_id",
	} {
		if !slices.Contains(shape.RequiredJSONPaths, identityPath) {
			t.Fatalf("trace_deployment_chain.required_json_paths missing %q", identityPath)
		}
	}
}

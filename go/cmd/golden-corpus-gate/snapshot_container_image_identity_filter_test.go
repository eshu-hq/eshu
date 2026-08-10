// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

func TestGoldenContainerImageIdentityProofUsesExactDigestFilter(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.MCP["list_container_image_identities"]
	if !ok {
		t.Fatal("query_shapes.mcp missing list_container_image_identities")
	}
	wantArguments := map[string]any{
		"digest":               "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
		"source_repository_id": "repository:r_69256c06",
		"limit":                float64(10),
	}
	if !reflect.DeepEqual(shape.Arguments, wantArguments) {
		t.Fatalf("Arguments = %#v, want exact digest-scoped proof %#v", shape.Arguments, wantArguments)
	}
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != "identities" {
		t.Fatalf("bounds/field = [%d,%d] %q, want [1,1] identities", shape.MinimumResults, shape.MaximumResults, shape.ResultsField)
	}
}

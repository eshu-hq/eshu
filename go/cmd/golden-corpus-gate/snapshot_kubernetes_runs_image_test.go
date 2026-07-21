// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

// TestGoldenSnapshotInfraRelationshipsRequiresRunsImageEdge is the #5436 B-12
// proof that the new "POST /api/v0/infra/relationships" query_shapes.http entry
// is non-vacuous: it only passes when the response carries the exact RUNS_IMAGE
// edge that BuildSourceImageDigestJoinIndex
// (go/internal/reducer/kubernetes_workload_source_image_join.go) can only
// produce by correlating the kuberneteslive cassette's digest-pinned
// supply-chain-demo Deployment pod_template fact
// (image_refs sha256:abcdef...ab) with the ociregistry cassette's
// image_manifest fact carrying the same digest and descriptor_id. Neither
// cassette alone contains enough information to produce this edge.
func TestGoldenSnapshotInfraRelationshipsRequiresRunsImageEdge(t *testing.T) {
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP["POST /api/v0/infra/relationships"]
	if !ok {
		t.Fatal("query_shapes.http missing POST /api/v0/infra/relationships")
	}

	if got, want := shape.RequestBody["entity_id"], "kubernetes_live:supply-chain-demo:apps/v1/deployments:default:supply-chain-demo"; got != want {
		t.Fatalf("request_body.entity_id = %#v, want the supply-chain-demo KubernetesWorkload id %q", got, want)
	}
	if got, want := shape.RequestBody["relationship_type"], "what_runs_image"; got != want {
		t.Fatalf("request_body.relationship_type = %#v, want %q", got, want)
	}

	response := fakeQueryShapeResponse(shape)
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal valid fixture response: %v", err)
	}
	if finding := EvaluateQueryShape("infra-relationships-runs-image", shape, raw); !finding.OK {
		t.Fatalf("valid RUNS_IMAGE fixture failed: %s", finding.Detail)
	}

	// Non-vacuous proof: a response with the RUNS_IMAGE edge target swapped for
	// an unrelated image id (as if the digest join failed to correlate the two
	// cassettes, or resolved the wrong OciImageManifest) must fail. This proves
	// the assertion pins the specific correlated identity, not just the
	// presence of some outgoing edge.
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatal("fake response data is not an object")
	}
	outgoing, ok := data["outgoing"].([]any)
	if !ok || len(outgoing) == 0 {
		t.Fatal("fake response data.outgoing is empty")
	}
	edge, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatal("fake response data.outgoing[0] is not an object")
	}
	edge["target_id"] = "oci-descriptor://ghcr.io/eshu-hq/unrelated-image@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	mutated, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal mutated fixture response: %v", err)
	}
	if finding := EvaluateQueryShape("infra-relationships-runs-image", shape, mutated); finding.OK {
		t.Fatalf("uncorrelated RUNS_IMAGE target passed unexpectedly: %s", finding.Detail)
	}

	// A second non-vacuous proof: an empty outgoing edge set (as if the digest
	// join between the two cassettes never happened at all) must also fail.
	data["outgoing"] = []any{}
	empty, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal empty-outgoing fixture response: %v", err)
	}
	if finding := EvaluateQueryShape("infra-relationships-runs-image", shape, empty); finding.OK {
		t.Fatalf("empty outgoing edge set passed unexpectedly: %s", finding.Detail)
	}
}

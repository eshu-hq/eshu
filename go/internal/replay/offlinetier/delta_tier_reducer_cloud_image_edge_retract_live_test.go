// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// AWS cloud-image edge retract coverage (issue #5450, C-14 #4367
// retract-depth backfill pattern): AWS_lambda_function_uses_image.
//
// CloudResourceContainerImageEdgeWriter.RetractCloudResourceContainerImageEdges
// dispatches its single retract DELETE statement through dispatchRetract
// (sequential Execute, never ExecuteGroup) from the start, following the fix
// the sibling #4367 cloud-correlation and IAM writers needed
// (delta_tier_reducer_cloud_edge_retract_live_test.go,
// delta_tier_reducer_iam_edge_retract_live_test.go): on the pinned NornicDB
// v1.1.11, a DELETE dispatched through ExecuteGroup (a managed Bolt
// transaction) under-applies even for a single statement (see
// docs/public/reference/nornicdb-pitfalls.md); the identical statement run as
// an auto-commit transaction (Execute) deletes correctly. This test proves
// the write/retract graph truth on a real NornicDB and additionally proves
// the cross-label endpoint shape this writer introduces (source
// :CloudResource, target :ContainerImage — every prior #4367/#5450-sibling
// writer this offlinetier package covers is CloudResource-to-CloudResource or
// CloudResource-to-ExternalPrincipal; this is the first CloudResource ->
// ContainerImage two-MATCH-MERGE edge writer proven here).
//
// The test drives the REAL production writer constructor and methods
// (cypher.NewCloudResourceContainerImageEdgeWriter) against liveExecutor,
// which implements GroupExecutor exactly like production's
// reducerNeo4jExecutor, so a retract that routed through ExecuteGroup would
// reproduce the under-apply here. The edge type is written and retracted with
// an out-of-scope survivor control (same evidence_source, different
// scope_id), plus both endpoint node-survival assertions (the source
// CloudResource and the target ContainerImage node must both survive an edge
// retract — this writer's retract Cypher DELETEs only the relationship,
// never a node).
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	cloudImageEdgeMarker = "replay-cloud-image-edge"

	cloudImageEvidenceSource = "reducer/aws-cloud-image"
	cloudImageSourceIn       = "replay-cloud-image-edge:source-in"
	cloudImageTargetIn       = "replay-cloud-image-edge:target-in"
	cloudImageSourceOut      = "replay-cloud-image-edge:source-out"
	cloudImageTargetOut      = "replay-cloud-image-edge:target-out"
	cloudImageScopeIn        = "replay-cloud-image-edge:scope-in"
	cloudImageScopeOut       = "replay-cloud-image-edge:scope-out"

	cloudImageEdgeGenerationID = "gen-1"
)

// TestReducerAWSCloudImageEdgeRetractGraphTruth proves the
// AWS_lambda_function_uses_image CloudResource -> ContainerImage retract path
// deletes only the in-scope edge on a real NornicDB, never dispatches through
// ExecuteGroup, and never touches either endpoint node.
func TestReducerAWSCloudImageEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the AWS cloud-image edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupCloudImageEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupCloudImageEdgeScope(cleanCtx, t, exec)
	})

	seedCloudImageEdgeNodes(ctx, t, exec)

	writer := cypher.NewCloudResourceContainerImageEdgeWriter(exec, 0)

	if err := writer.WriteCloudResourceContainerImageEdges(ctx, []map[string]any{{
		"source_uid": cloudImageSourceIn, "target_uid": cloudImageTargetIn,
		"relationship_type": "lambda_function_uses_image", "resolution_mode": "container_image_digest",
	}}, cloudImageScopeIn, cloudImageEdgeGenerationID, cloudImageEvidenceSource); err != nil {
		t.Fatalf("WriteCloudResourceContainerImageEdges(in): %v", err)
	}
	if err := writer.WriteCloudResourceContainerImageEdges(ctx, []map[string]any{{
		"source_uid": cloudImageSourceOut, "target_uid": cloudImageTargetOut,
		"relationship_type": "lambda_function_uses_image", "resolution_mode": "container_image_digest",
	}}, cloudImageScopeOut, cloudImageEdgeGenerationID, cloudImageEvidenceSource); err != nil {
		t.Fatalf("WriteCloudResourceContainerImageEdges(out): %v", err)
	}

	edgeQ := "MATCH (:CloudResource {uid: $s})-[r:AWS_lambda_function_uses_image]->(:ContainerImage {uid: $t}) RETURN count(r)"
	in := map[string]any{"s": cloudImageSourceIn, "t": cloudImageTargetIn}
	out := map[string]any{"s": cloudImageSourceOut, "t": cloudImageTargetOut}

	assertEdgeCount(ctx, t, exec, edgeQ, in, 1, "write: in-scope AWS_lambda_function_uses_image present")
	assertEdgeCount(ctx, t, exec, edgeQ, out, 1, "write: out-of-scope AWS_lambda_function_uses_image present")

	// --- Retract only the in-scope scope_id. ---
	if err := writer.RetractCloudResourceContainerImageEdges(ctx, []string{cloudImageScopeIn}, cloudImageEdgeGenerationID, cloudImageEvidenceSource); err != nil {
		t.Fatalf("RetractCloudResourceContainerImageEdges: %v", err)
	}

	assertEdgeCount(ctx, t, exec, edgeQ, in, 0, "retract: in-scope AWS_lambda_function_uses_image gone")
	// Scoped retract, not a wipe: the out-of-scope edge survives.
	assertEdgeCount(ctx, t, exec, edgeQ, out, 1, "retract: out-of-scope AWS_lambda_function_uses_image survives")

	// Both endpoint node labels always survive an edge retract: the source
	// CloudResource and the target ContainerImage.
	assertEdgeCount(ctx, t, exec, "MATCH (n:CloudResource {uid: $u}) RETURN count(n)", map[string]any{"u": cloudImageSourceIn}, 1, "source node survives")
	assertEdgeCount(ctx, t, exec, "MATCH (n:ContainerImage {uid: $u}) RETURN count(n)", map[string]any{"u": cloudImageTargetIn}, 1, "target node survives")
}

// seedCloudImageEdgeNodes creates the CloudResource source and ContainerImage
// target endpoint nodes the write template MATCHes.
func seedCloudImageEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE
       (:CloudResource {uid: $sourceIn, marker: $marker}),
       (:ContainerImage {uid: $targetIn, marker: $marker}),
       (:CloudResource {uid: $sourceOut, marker: $marker}),
       (:ContainerImage {uid: $targetOut, marker: $marker})`,
		Parameters: map[string]any{
			"sourceIn": cloudImageSourceIn, "targetIn": cloudImageTargetIn,
			"sourceOut": cloudImageSourceOut, "targetOut": cloudImageTargetOut,
			"marker": cloudImageEdgeMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed AWS cloud-image edge nodes: %v", err)
	}
}

// cleanupCloudImageEdgeScope removes every node this test creates.
func cleanupCloudImageEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
		Parameters: map[string]any{"marker": cloudImageEdgeMarker},
	}); err != nil {
		t.Fatalf("cleanup AWS cloud-image edge scope: %v", err)
	}
}

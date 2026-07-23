// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// cloudResourceContainerImageEdgeRows mirrors the rows
// ExtractAWSCloudImageEdgeRows produces: the source CloudResource uid, the
// target ContainerImage uid, the raw AWS relationship_type, and the
// resolution mode. It omits scope_id/generation_id/evidence_source — the
// writer injects those reducer-scoped annotations from its call arguments.
func cloudResourceContainerImageEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "source-" + string(rune('a'+i)),
			"target_uid":        "target-" + string(rune('a'+i)),
			"relationship_type": "lambda_function_uses_image",
			"resolution_mode":   "container_image_digest",
		})
	}
	return rows
}

func TestCloudResourceContainerImageEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceContainerImageEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceContainerImageEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/aws-cloud-image"); err != nil {
		t.Fatalf("WriteCloudResourceContainerImageEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCloudResourceContainerImageEdgeWriterUsesStaticTokenMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceContainerImageEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceContainerImageEdges(
		context.Background(), cloudResourceContainerImageEdgeRows(1), "scope-1", "gen-1", "reducer/aws-cloud-image",
	); err != nil {
		t.Fatalf("WriteCloudResourceContainerImageEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Two MATCHes before the MERGE guarantee a missing endpoint is a no-op,
	// never a fabricated node.
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the source CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:ContainerImage {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target ContainerImage by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:AWS_lambda_function_uses_image]->(target)") {
		t.Fatalf("edge MERGE must use the static AWS_lambda_function_uses_image relationship type:\n%s", cypher)
	}
	for _, want := range []string{
		"rel.relationship_type = row.relationship_type",
		"rel.resolution_mode = row.resolution_mode",
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
}

func TestCloudResourceContainerImageEdgeWriterRejectsForeignRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceContainerImageEdgeWriter(executor, 0)

	// A row whose relationship_type is outside the closed single-member
	// vocabulary (e.g. the tag-only ECS type, which must stay Postgres-only)
	// must be rejected, never interpolated into the schema surface.
	rows := []map[string]any{{
		"source_uid":        "source-a",
		"target_uid":        "target-a",
		"relationship_type": "ecs_task_definition_uses_image",
		"resolution_mode":   "container_image_digest",
	}}
	if err := writer.WriteCloudResourceContainerImageEdges(
		context.Background(), rows, "scope-1", "gen-1", "reducer/aws-cloud-image",
	); err == nil {
		t.Fatal("WriteCloudResourceContainerImageEdges accepted an out-of-vocabulary relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestCloudResourceContainerImageEdgeWriterRejectsInjectionToken(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceContainerImageEdgeWriter(executor, 0)

	rows := []map[string]any{{
		"source_uid":        "source-a",
		"target_uid":        "target-a",
		"relationship_type": "lambda_function_uses_image]->() DELETE n //",
		"resolution_mode":   "container_image_digest",
	}}
	if err := writer.WriteCloudResourceContainerImageEdges(
		context.Background(), rows, "scope-1", "gen-1", "reducer/aws-cloud-image",
	); err == nil {
		t.Fatal("WriteCloudResourceContainerImageEdges accepted an injection-shaped relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestCloudResourceContainerImageEdgeWriterRetractScopedToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceContainerImageEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceContainerImageEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/aws-cloud-image",
	); err != nil {
		t.Fatalf("RetractCloudResourceContainerImageEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "[rel:AWS_lambda_function_uses_image]") {
		t.Fatalf("retract must match the AWS_lambda_function_uses_image relationship type:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by the edge's own scope_id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge, never the nodes:\n%s", cypher)
	}
}

func TestCloudResourceContainerImageEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceContainerImageEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceContainerImageEdges(
		context.Background(), nil, "gen-1", "reducer/aws-cloud-image",
	); err != nil {
		t.Fatalf("RetractCloudResourceContainerImageEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

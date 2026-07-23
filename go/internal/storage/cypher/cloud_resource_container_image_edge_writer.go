// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseCloudResourceContainerImageEdge names the AWS cloud-image edge
// projection phase for grouped-backend statement metadata and diagnostics
// (issue #5450).
const canonicalPhaseCloudResourceContainerImageEdge = "cloud_resource_container_image_edge"

// cloudResourceContainerImageEdgeLabel is the bounded entity-label tag for the
// edge statement metadata.
const cloudResourceContainerImageEdgeLabel = "AWS_LAMBDA_FUNCTION_USES_IMAGE"

// cloudResourceContainerImageRelationshipVocabulary is the closed
// single-member set of RAW aws_relationship relationship_type values the AWS
// cloud-image projection may use (issue #5450,
// docs/internal/design/5472-graph-projection-policy.md EXACT-ONLY promotion).
// Only lambda_function_uses_image resolves to an exact
// registry+repository@digest reference; ecs_task_definition_uses_image is
// tag-only and stays Postgres-only (never reaches this writer, see
// go/internal/reducer/aws_cloud_image_join.go). This is the value the row's
// "relationship_type" field carries (informative/human-readable, matching
// CloudResourceEdgeWriter's rel.relationship_type convention) — distinct from
// the Cypher relationship-type TOKEN interpolated into the MERGE, which is the
// separate, AWS_-namespaced cloudResourceContainerImageRelationshipType()
// constant. A row value outside this set is rejected rather than silently
// mapped to an arbitrary Cypher type — mirroring
// iamCanAssumeRelationshipVocabulary's closed single-member allowlist.
var cloudResourceContainerImageRelationshipVocabulary = map[string]struct{}{
	"lambda_function_uses_image": {},
}

// canonicalCloudResourceContainerImageEdgeUpsertCypherFormat batches
// CloudResource -> ContainerImage edge upserts. Both endpoints are already
// uid-constrained labels (CloudResource and ContainerImage both carry a uid
// uniqueness constraint / NornicDB uid lookup index, internal/graph/schema_tables.go
// uidConstraintLabels), so this engages the identical schema-backed MATCH hot
// path the already-measured AWS relationship and IAM CAN_ASSUME two-MATCH-MERGE
// writers use (design doc §11: a property-map relationship MERGE timed out at
// 20s for 12 rows vs 0-1ms for the static relationship-type token shape used
// here). Two MATCHes precede the MERGE so a row whose source CloudResource or
// target ContainerImage node is absent (an unscanned Lambda function, or an
// image whose OCI registry has not been scanned yet) produces no edge and no
// fabricated node — the same graceful-degradation property every other AWS
// edge writer in this package relies on.
const canonicalCloudResourceContainerImageEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:ContainerImage {uid: row.target_uid})
MERGE (source)-[rel:%s]->(target)
SET rel.relationship_type = row.relationship_type,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractCloudResourceContainerImageEdgesCypher removes this reducer's AWS
// cloud-image edges for a set of scopes before a fresh generation reprojects
// them. The relationship type is a fixed static token (unlike the open
// AWS_<relationship_type> vocabulary the sibling CloudResourceEdgeWriter
// uses), so the retract matches it directly. Neither CloudResource nor
// ContainerImage nodes carry a reducer scope_id (both are cross-generation
// canonical), so the scope predicate filters on the edge's own scope_id, not
// an endpoint node's.
const retractCloudResourceContainerImageEdgesCypher = `MATCH (:CloudResource)-[rel:AWS_lambda_function_uses_image]->(:ContainerImage)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// CloudResourceContainerImageEdgeWriter materializes resolved
// lambda_function_uses_image relationships into canonical CloudResource ->
// ContainerImage edges (issue #5450). It satisfies the reducer-owned
// cloud-image-edge-writer consumer interface and writes through the
// backend-neutral Executor seam.
type CloudResourceContainerImageEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewCloudResourceContainerImageEdgeWriter returns a
// CloudResourceContainerImageEdgeWriter backed by the given Executor. A
// batchSize of 0 or less uses DefaultBatchSize (500).
func NewCloudResourceContainerImageEdgeWriter(executor Executor, batchSize int) *CloudResourceContainerImageEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CloudResourceContainerImageEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceContainerImageEdges upserts CloudResource ->
// ContainerImage edges for the given resolved rows using batched
// MATCH-MATCH-MERGE statements. When the executor implements GroupExecutor
// all batches are dispatched in a single atomic transaction; otherwise they
// run sequentially. The write is idempotent: the same (source_uid, target_uid)
// converges on one edge across batches, retries, and generations, and a
// missing endpoint is a no-op rather than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id. rel.scope_id is what the prior-generation retract
// filters on, so omitting it would make scope-scoped retract a silent no-op.
func (w *CloudResourceContainerImageEdgeWriter) WriteCloudResourceContainerImageEdges(
	ctx context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource container image edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateCloudResourceContainerImageRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	cypher := fmt.Sprintf(canonicalCloudResourceContainerImageEdgeUpsertCypherFormat, cloudResourceContainerImageRelationshipType())
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCloudResourceContainerImageEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = cloudResourceContainerImageEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			cloudResourceContainerImageEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractCloudResourceContainerImageEdges removes this reducer's AWS
// cloud-image edges for the given scopes before a fresh generation reprojects
// them. It is a no-op for an empty scope set (e.g. an empty generation). The
// delete is scoped to the reducer's evidence_source and never touches
// CloudResource or ContainerImage nodes.
func (w *CloudResourceContainerImageEdgeWriter) RetractCloudResourceContainerImageEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource container image edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCloudResourceContainerImageEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCloudResourceContainerImageEdge,
			StatementMetadataEntityLabelKey: cloudResourceContainerImageEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				cloudResourceContainerImageEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

// validateCloudResourceContainerImageRelationshipType screens a row's
// relationship_type against the closed single-member vocabulary and the
// static-token character class before it is interpolated into the
// relationship-type position.
func validateCloudResourceContainerImageRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(
		row, "relationship_type", cloudResourceContainerImageRelationshipVocabulary,
		"cloud resource container image relationship_type",
	)
}

// cloudResourceContainerImageRelationshipType returns the fixed, AWS_-namespaced
// Cypher relationship-type token interpolated into the MERGE, matching the
// "AWS_<raw relationship_type>" convention CloudResourceEdgeWriter's dynamic
// per-type token already establishes
// (canonicalAWSRelationshipCypherType) — this writer only ever has one raw
// type (lambda_function_uses_image, validated against
// cloudResourceContainerImageRelationshipVocabulary above), so the token is a
// fixed constant rather than derived per row.
func cloudResourceContainerImageRelationshipType() string {
	return "AWS_lambda_function_uses_image"
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *CloudResourceContainerImageEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	if ge, ok := w.executor.(GroupExecutor); ok {
		if err := ge.ExecuteGroup(ctx, stmts); err != nil {
			return WrapRetryableNeo4jError(err)
		}
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

// dispatchRetract routes retract statements through sequential Execute calls,
// never ExecuteGroup, mirroring CloudResourceEdgeWriter.dispatchRetract and
// IAMCanAssumeEdgeWriter.dispatchRetract: on the pinned NornicDB v1.1.11 a
// DELETE dispatched through a managed transaction (ExecuteGroup) under-applies
// even for a single statement (docs/public/reference/nornicdb-pitfalls.md).
func (w *CloudResourceContainerImageEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

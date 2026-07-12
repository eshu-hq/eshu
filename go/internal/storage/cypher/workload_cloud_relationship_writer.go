// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

const canonicalPhaseWorkloadCloudRelationshipEdge = "workload_cloud_relationship_edge"

const workloadCloudRelationshipEdgeLabel = "WORKLOAD_USES_CLOUD_RESOURCE"

var workloadCloudRelationshipVocabulary = map[string]struct{}{
	"USES": {},
}

const workloadCloudRelationshipUpsertCypherFormat = `UNWIND $rows AS row
MATCH (resource:CloudResource {uid: row.cloud_resource_uid})
MATCH (workload:Workload {id: row.workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)
WHERE instance.environment = row.environment
MERGE (instance)-[rel:%s]->(resource)
SET rel.resolution_mode = row.resolution_mode,
    rel.environment = row.environment,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source,
    rel.relationship_basis = row.relationship_basis,
    rel.service_anchor_source = row.service_anchor_source,
    rel.service_anchor_reason = row.service_anchor_reason,
    rel.source_fact_id = row.source_fact_id,
    rel.stable_fact_key = row.stable_fact_key,
    rel.source_system = row.source_system,
    rel.source_record_id = row.source_record_id,
    rel.collector_kind = row.collector_kind`

const retractWorkloadCloudRelationshipEdgesCypher = `MATCH (:WorkloadInstance)-[rel:USES]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// WorkloadCloudRelationshipWriter materializes exact workload-anchored
// CloudResource evidence into canonical WorkloadInstance USES CloudResource
// edges. It never creates endpoint nodes; missing workload or cloud-resource
// endpoints are graph no-ops at the MATCH clauses.
type WorkloadCloudRelationshipWriter struct {
	executor  Executor
	batchSize int
}

// NewWorkloadCloudRelationshipWriter returns a writer backed by executor. A
// batchSize of 0 or less uses DefaultBatchSize.
func NewWorkloadCloudRelationshipWriter(executor Executor, batchSize int) *WorkloadCloudRelationshipWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &WorkloadCloudRelationshipWriter{executor: executor, batchSize: batchSize}
}

// WriteWorkloadCloudRelationshipEdges upserts USES edges using a static
// relationship token and batched MATCH-MATCH-MERGE statements.
func (w *WorkloadCloudRelationshipWriter) WriteWorkloadCloudRelationshipEdges(
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
		return fmt.Errorf("workload cloud relationship writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateWorkloadCloudRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	cypher := fmt.Sprintf(
		workloadCloudRelationshipUpsertCypherFormat,
		workloadCloudRelationshipType(),
	)
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseWorkloadCloudRelationshipEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = workloadCloudRelationshipEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			workloadCloudRelationshipEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractWorkloadCloudRelationshipEdges removes this reducer's USES edges for
// the given scopes before a fresh generation reprojects them.
func (w *WorkloadCloudRelationshipWriter) RetractWorkloadCloudRelationshipEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("workload cloud relationship writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractWorkloadCloudRelationshipEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseWorkloadCloudRelationshipEdge,
			StatementMetadataEntityLabelKey: workloadCloudRelationshipEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				workloadCloudRelationshipEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

func validateWorkloadCloudRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(
		row,
		"relationship_type",
		workloadCloudRelationshipVocabulary,
		"workload cloud relationship_type",
	)
}

func workloadCloudRelationshipType() string {
	for token := range workloadCloudRelationshipVocabulary {
		return token
	}
	return ""
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Only the MERGE-shaped write path
// uses this: grouping is safe (and desirable for throughput) for idempotent
// MERGE upserts.
func (w *WorkloadCloudRelationshipWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract runs retract statements sequentially through Execute, each
// in its own auto-commit transaction — never ExecuteGroup. On NornicDB
// v1.1.11 a DELETE inside a managed transaction can under-apply even as a
// single statement (measured for the TAINT_FLOWS_TO retract in
// CodeInterprocEvidenceWriter.dispatchRetract and the SQL-relationship and
// repo-dependency retracts, #4367/#5128/#5146/#5152): the grouped DELETE
// leaves the edge in place while the same statement auto-committed deletes
// it. RetractWorkloadCloudRelationshipEdges routes through this so the USES
// retract is never batched with a sibling write via ExecuteGroup.
func (w *WorkloadCloudRelationshipWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	if len(stmts) == 0 {
		return nil
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseCloudResourceEdge names the AWS relationship edge projection
// phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseCloudResourceEdge = "cloud_resource_edge"

// canonicalCloudResourceEdgeUpsertCypher batches AWS_RELATIONSHIP edge upserts
// between two already-materialized CloudResource nodes. Two MATCHes precede the
// MERGE so a row whose source or target node is absent produces no edge and no
// fabricated node — the graceful-degradation contract from issue #805. The edge
// MERGE identity is (source, relationship_type, target) so duplicate input rows
// and reducer retries converge on one edge. Both endpoints anchor on the
// CloudResource uid uniqueness constraint, so the MATCHes are schema-backed
// lookups rather than label scans (NornicDB uid lookup index / Neo4j backing
// index). This mirrors the proven label-scoped SQL relationship writer shape
// (buildLabelScopedSQLRelationshipCypher).
const canonicalCloudResourceEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (source)-[rel:AWS_RELATIONSHIP {relationship_type: row.relationship_type}]->(target)
SET rel.target_type = row.target_type,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractCloudResourceEdgesCypher removes the reducer-owned AWS_RELATIONSHIP
// edges for a set of scopes before a fresh generation reprojects them. The
// scope predicate filters on the edge's own scope_id, not the endpoint node's:
// CloudResource nodes are cross-scope canonical and carry no scope_id property,
// so a source.scope_id predicate would match nothing and leak stale edges. It
// is also scoped to this reducer's evidence_source so it never touches edges
// owned by other writers, and it DELETEs only the relationship, never the
// endpoint CloudResource nodes.
const retractCloudResourceEdgesCypher = `MATCH (source:CloudResource)-[rel:AWS_RELATIONSHIP]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// CloudResourceEdgeWriter materializes resolved aws_relationship facts into
// canonical AWS_RELATIONSHIP edges between CloudResource nodes. It satisfies the
// reducer-owned edge-writer consumer interface and writes through the
// backend-neutral Executor seam.
type CloudResourceEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewCloudResourceEdgeWriter returns a CloudResourceEdgeWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewCloudResourceEdgeWriter(executor Executor, batchSize int) *CloudResourceEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CloudResourceEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteCloudResourceEdges upserts AWS_RELATIONSHIP edges for the given resolved
// rows using batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same
// (source_uid, relationship_type, target_uid) converges on one edge across
// batches, retries, and generations, and a missing endpoint is a no-op rather
// than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id. The resolution layer does not carry these reducer-scoped
// fields, so the writer is the single place that stamps them; rel.scope_id is
// what the prior-generation retract filters on (CloudResource nodes carry no
// scope_id), so omitting it would make scope-scoped retract a silent no-op.
func (w *CloudResourceEdgeWriter) WriteCloudResourceEdges(
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
		return fmt.Errorf("cloud resource edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned := make(map[string]any, len(row)+3)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["scope_id"] = scopeID
		cloned["generation_id"] = generationID
		cloned["evidence_source"] = evidenceSource
		annotated = append(annotated, cloned)
	}

	stmts := buildBatchedStatements(canonicalCloudResourceEdgeUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCloudResourceEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = "AWS_RELATIONSHIP"
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=AWS_RELATIONSHIP rows=%d",
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractCloudResourceEdges removes this reducer's AWS_RELATIONSHIP edges for
// the given scopes before a fresh generation reprojects them. It is a no-op for
// an empty scope set (e.g. an empty generation). The delete is scoped to the
// reducer's evidence_source and never touches CloudResource nodes.
func (w *CloudResourceEdgeWriter) RetractCloudResourceEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("cloud resource edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCloudResourceEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCloudResourceEdge,
			StatementMetadataEntityLabelKey: "AWS_RELATIONSHIP",
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=AWS_RELATIONSHIP retract scopes=%d generation=%s",
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *CloudResourceEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

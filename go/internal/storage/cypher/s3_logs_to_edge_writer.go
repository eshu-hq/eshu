// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseS3LogsToEdge names the S3 LOGS_TO edge projection phase for
// grouped-backend statement metadata and diagnostics (issue #1144 PR2).
const canonicalPhaseS3LogsToEdge = "s3_logs_to_edge"

// s3LogsToEdgeLabel is the bounded entity-label tag for the LOGS_TO edge
// statement metadata and the edge-projection counter dimension.
const s3LogsToEdgeLabel = "S3_LOGS_TO"

// s3LogsToRelationshipVocabulary is the closed single-member set of Cypher
// relationship types the LOGS_TO projection may use. The type is interpolated
// into the relationship-type position (which cannot be parameterized), so a
// value outside this set is rejected rather than turned into an arbitrary
// relationship type. It mirrors the reducer's s3LogsToRelationshipType constant;
// the duplication is intentional because the cypher writer owns the
// relationship-type position and must not depend on reducer internals.
var s3LogsToRelationshipVocabulary = map[string]struct{}{
	"LOGS_TO": {},
}

// canonicalS3LogsToEdgeUpsertCypherFormat batches LOGS_TO edge upserts between
// two already-materialized S3 bucket CloudResource nodes: the source bucket that
// emits server-access logs and the target log bucket those logs are delivered
// to. The relationship type is a validated static token from the closed
// vocabulary (the single %s); keeping it out of a relationship-property MERGE
// lets NornicDB use its relationship hot path (#805 §5.3: a property-keyed
// relationship MERGE timed out at 20s vs 0–1ms for a static token) while
// preserving one edge per (source uid, LOGS_TO, target uid). Two MATCHes precede
// the MERGE so a row whose source or target node is absent produces no edge and
// no fabricated node.
const canonicalS3LogsToEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (source)-[rel:%s]->(target)
SET rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractS3LogsToEdgesCypher removes this reducer's LOGS_TO edges for a set of
// scopes before a fresh generation reprojects them. LOGS_TO is a fixed
// relationship type between two CloudResource nodes, so the retract matches it
// directly and scopes by the edge's own scope_id and evidence_source.
// CloudResource nodes are cross-generation canonical and carry no reducer
// scope_id, so a node-scoped predicate would leak stale edges across
// generations.
const retractS3LogsToEdgesCypher = `MATCH (:CloudResource)-[rel:LOGS_TO]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// S3LogsToEdgeWriter materializes resolved S3 server-access-log delivery into
// canonical LOGS_TO edges between S3 bucket CloudResource nodes (issue #1144
// PR2). It satisfies the reducer-owned s3-logs-to-edge-writer consumer interface
// and writes through the backend-neutral Executor seam.
type S3LogsToEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewS3LogsToEdgeWriter returns an S3LogsToEdgeWriter backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewS3LogsToEdgeWriter(executor Executor, batchSize int) *S3LogsToEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &S3LogsToEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteS3LogsToEdges upserts LOGS_TO edges for the given resolved rows using
// batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same
// (source_uid, LOGS_TO, target_uid) converges on one edge across batches,
// retries, and generations, and a missing endpoint is a no-op rather than a
// fabricated node.
//
// Every row's relationship_type is screened against the closed single-member
// vocabulary before the static token is interpolated, so a deviating or
// adversarial upstream row can never fabricate a new relationship type.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id. The resolution layer does not carry these reducer-scoped
// fields, so the writer is the single place that stamps them; rel.scope_id is
// what the prior-generation retract filters on (CloudResource nodes carry no
// scope_id), so omitting it would make scope-scoped retract a silent no-op.
func (w *S3LogsToEdgeWriter) WriteS3LogsToEdges(
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
		return fmt.Errorf("s3 logs-to edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateS3LogsToRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	// The vocabulary has a single member, so all validated rows share one token.
	cypher := fmt.Sprintf(canonicalS3LogsToEdgeUpsertCypherFormat, s3LogsToRelationshipType())
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseS3LogsToEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = s3LogsToEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			s3LogsToEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractS3LogsToEdges removes this reducer's LOGS_TO edges for the given scopes
// before a fresh generation reprojects them. It is a no-op for an empty scope
// set (e.g. an empty generation). The delete is scoped to the reducer's
// evidence_source and never touches CloudResource nodes.
func (w *S3LogsToEdgeWriter) RetractS3LogsToEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("s3 logs-to edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractS3LogsToEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseS3LogsToEdge,
			StatementMetadataEntityLabelKey: s3LogsToEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				s3LogsToEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// validateS3LogsToRelationshipType screens a row's relationship_type against the
// closed single-member vocabulary and the static-token character class before it
// is interpolated into the relationship-type position. Membership is checked,
// not just the character class, so a charset-safe but out-of-vocabulary token
// (or injected text) can never reach the relationship-type position.
func validateS3LogsToRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "relationship_type", s3LogsToRelationshipVocabulary, "s3 logs-to relationship_type")
}

// s3LogsToRelationshipType returns the single member of the closed relationship
// vocabulary for interpolation. Centralizing it keeps the upsert Cypher and the
// vocabulary in lockstep.
func s3LogsToRelationshipType() string {
	for token := range s3LogsToRelationshipVocabulary {
		return token
	}
	return ""
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *S3LogsToEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

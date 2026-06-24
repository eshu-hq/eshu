// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseIAMCanAssumeEdge names the IAM CAN_ASSUME edge projection phase
// for grouped-backend statement metadata and diagnostics (issue #1134 PR2).
const canonicalPhaseIAMCanAssumeEdge = "iam_can_assume_edge"

// iamCanAssumeEdgeLabel is the bounded entity-label tag for the CAN_ASSUME edge
// statement metadata and the edge-projection counter dimension.
const iamCanAssumeEdgeLabel = "IAM_CAN_ASSUME"

// iamCanAssumeRelationshipVocabulary is the closed single-member set of Cypher
// relationship types the CAN_ASSUME projection may use. The type is interpolated
// into the relationship-type position (which cannot be parameterized), so a
// value outside this set is rejected rather than turned into an arbitrary
// relationship type. It mirrors the reducer's iamCanAssumeRelationshipType
// constant; the duplication is intentional because the cypher writer owns the
// relationship-type position and must not depend on reducer internals.
var iamCanAssumeRelationshipVocabulary = map[string]struct{}{
	"CAN_ASSUME": {},
}

// canonicalIAMCanAssumeEdgeUpsertCypherFormat batches CAN_ASSUME edge upserts
// between two already-materialized IAM CloudResource nodes: the assuming
// principal (role/user) and the role whose trust policy grants the assume. The
// relationship type is a validated static token from the closed vocabulary (the
// single %s); keeping it out of a relationship-property MERGE lets NornicDB use
// its relationship hot path (#805 §5.3: a property-keyed relationship MERGE
// timed out at 20s vs 0–1ms for a static token) while preserving one edge per
// (principal uid, CAN_ASSUME, role uid). Two MATCHes precede the MERGE so a row
// whose principal or role node is absent produces no edge and no fabricated
// node.
const canonicalIAMCanAssumeEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (principal:CloudResource {uid: row.principal_uid})
MATCH (role:CloudResource {uid: row.role_uid})
MERGE (principal)-[rel:%s]->(role)
SET rel.principal_kind = row.principal_kind,
    rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractIAMCanAssumeEdgesCypher removes this reducer's CAN_ASSUME edges for a
// set of scopes before a fresh generation reprojects them. CAN_ASSUME is a
// fixed relationship type between two CloudResource nodes, so the retract
// matches it directly and scopes by the edge's own scope_id and evidence_source.
// CloudResource nodes are cross-generation canonical and carry no reducer
// scope_id, so a node-scoped predicate would leak stale edges across
// generations.
const retractIAMCanAssumeEdgesCypher = `MATCH (:CloudResource)-[rel:CAN_ASSUME]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// IAMCanAssumeEdgeWriter materializes resolved IAM trust statements into
// canonical CAN_ASSUME edges between IAM CloudResource nodes (issue #1134 PR2).
// It satisfies the reducer-owned can-assume-edge-writer consumer interface and
// writes through the backend-neutral Executor seam.
type IAMCanAssumeEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewIAMCanAssumeEdgeWriter returns an IAMCanAssumeEdgeWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewIAMCanAssumeEdgeWriter(executor Executor, batchSize int) *IAMCanAssumeEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &IAMCanAssumeEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteIAMCanAssumeEdges upserts CAN_ASSUME edges for the given resolved rows
// using batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same
// (principal_uid, CAN_ASSUME, role_uid) converges on one edge across batches,
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
func (w *IAMCanAssumeEdgeWriter) WriteIAMCanAssumeEdges(
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
		return fmt.Errorf("iam can-assume edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateIAMCanAssumeRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	// The vocabulary has a single member, so all validated rows share one token.
	cypher := fmt.Sprintf(canonicalIAMCanAssumeEdgeUpsertCypherFormat, iamCanAssumeRelationshipType())
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseIAMCanAssumeEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = iamCanAssumeEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			iamCanAssumeEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractIAMCanAssumeEdges removes this reducer's CAN_ASSUME edges for the given
// scopes before a fresh generation reprojects them. It is a no-op for an empty
// scope set (e.g. an empty generation). The delete is scoped to the reducer's
// evidence_source and never touches CloudResource nodes.
func (w *IAMCanAssumeEdgeWriter) RetractIAMCanAssumeEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("iam can-assume edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractIAMCanAssumeEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseIAMCanAssumeEdge,
			StatementMetadataEntityLabelKey: iamCanAssumeEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				iamCanAssumeEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// validateIAMCanAssumeRelationshipType screens a row's relationship_type against
// the closed single-member vocabulary and the static-token character class
// before it is interpolated into the relationship-type position. Membership is
// checked, not just the character class, so a charset-safe but out-of-vocabulary
// token (or injected text) can never reach the relationship-type position.
func validateIAMCanAssumeRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "relationship_type", iamCanAssumeRelationshipVocabulary, "iam can-assume relationship_type")
}

// iamCanAssumeRelationshipType returns the single member of the closed
// relationship vocabulary for interpolation. Centralizing it keeps the upsert
// Cypher and the vocabulary in lockstep.
func iamCanAssumeRelationshipType() string {
	for token := range iamCanAssumeRelationshipVocabulary {
		return token
	}
	return ""
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *IAMCanAssumeEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

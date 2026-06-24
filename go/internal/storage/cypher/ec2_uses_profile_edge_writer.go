// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseEC2UsesProfileEdge names the EC2 USES_PROFILE edge projection
// phase for grouped-backend statement metadata and diagnostics (issue #1146
// PR-B).
const canonicalPhaseEC2UsesProfileEdge = "ec2_uses_profile_edge"

// ec2UsesProfileEdgeLabel is the bounded entity-label tag for the USES_PROFILE
// edge statement metadata and the edge-projection counter dimension.
const ec2UsesProfileEdgeLabel = "EC2_USES_PROFILE"

// ec2UsesProfileRelationshipVocabulary is the closed single-member set of Cypher
// relationship types the USES_PROFILE projection may use. The type is
// interpolated into the relationship-type position (which cannot be
// parameterized), so a value outside this set is rejected rather than turned into
// an arbitrary relationship type. It mirrors the reducer's
// ec2UsesProfileRelationshipType constant; the duplication is intentional because
// the cypher writer owns the relationship-type position and must not depend on
// reducer internals.
var ec2UsesProfileRelationshipVocabulary = map[string]struct{}{
	"USES_PROFILE": {},
}

// canonicalEC2UsesProfileEdgeUpsertCypherFormat batches USES_PROFILE edge upserts
// between an EC2 instance CloudResource node and the IAM instance-profile
// CloudResource node it uses. The relationship type is a validated static token
// from the closed vocabulary (the single %s); keeping it out of a
// relationship-property MERGE lets NornicDB use its relationship hot path (#805
// §5.3: a property-keyed relationship MERGE timed out at 20s vs 0–1ms for a
// static token) while preserving one edge per (source uid, USES_PROFILE, target
// uid). Two anchored MATCHes precede the MERGE so a row whose source or target
// node is absent produces no edge and no fabricated node, and so the two
// independent MATCHes never form a cartesian product.
const canonicalEC2UsesProfileEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (source:CloudResource {uid: row.source_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (source)-[rel:%s]->(target)
SET rel.resolution_mode = row.resolution_mode,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractEC2UsesProfileEdgesCypher removes this reducer's USES_PROFILE edges for a
// set of scopes before a fresh generation reprojects them. USES_PROFILE is a fixed
// relationship type between two CloudResource nodes, so the retract matches it
// directly and scopes by the edge's own scope_id and evidence_source.
// CloudResource nodes are cross-generation canonical and carry no reducer
// scope_id, so a node-scoped predicate would leak stale edges across generations.
const retractEC2UsesProfileEdgesCypher = `MATCH (:CloudResource)-[rel:USES_PROFILE]->(:CloudResource)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// EC2UsesProfileEdgeWriter materializes resolved EC2-instance → IAM
// instance-profile usage into canonical USES_PROFILE edges between CloudResource
// nodes (issue #1146 PR-B). It satisfies the reducer-owned ec2-uses-profile edge
// writer consumer interface and writes through the backend-neutral Executor seam.
type EC2UsesProfileEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewEC2UsesProfileEdgeWriter returns an EC2UsesProfileEdgeWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewEC2UsesProfileEdgeWriter(executor Executor, batchSize int) *EC2UsesProfileEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &EC2UsesProfileEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteEC2UsesProfileEdges upserts USES_PROFILE edges for the given resolved rows
// using batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches are dispatched in a single atomic transaction;
// otherwise they run sequentially. The write is idempotent: the same
// (source_uid, USES_PROFILE, target_uid) converges on one edge across batches,
// retries, and generations, and a missing endpoint is a no-op rather than a
// fabricated node.
//
// Every row's relationship_type is screened against the closed single-member
// vocabulary before the static token is interpolated, so a deviating or
// adversarial upstream row can never fabricate a new relationship type.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id. The resolution layer does not carry these reducer-scoped
// fields, so the writer is the single place that stamps them; rel.scope_id is what
// the prior-generation retract filters on (CloudResource nodes carry no scope_id),
// so omitting it would make scope-scoped retract a silent no-op.
func (w *EC2UsesProfileEdgeWriter) WriteEC2UsesProfileEdges(
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
		return fmt.Errorf("ec2 uses-profile edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if _, err := validateEC2UsesProfileRelationshipType(row); err != nil {
			return err
		}
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	// The vocabulary has a single member, so all validated rows share one token.
	cypher := fmt.Sprintf(canonicalEC2UsesProfileEdgeUpsertCypherFormat, ec2UsesProfileRelationshipType())
	stmts := buildBatchedStatements(cypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseEC2UsesProfileEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = ec2UsesProfileEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			ec2UsesProfileEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractEC2UsesProfileEdges removes this reducer's USES_PROFILE edges for the
// given scopes before a fresh generation reprojects them. It is a no-op for an
// empty scope set (e.g. an empty generation). The delete is scoped to the
// reducer's evidence_source and never touches CloudResource nodes.
func (w *EC2UsesProfileEdgeWriter) RetractEC2UsesProfileEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("ec2 uses-profile edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractEC2UsesProfileEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseEC2UsesProfileEdge,
			StatementMetadataEntityLabelKey: ec2UsesProfileEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				ec2UsesProfileEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatch(ctx, []Statement{stmt})
}

// validateEC2UsesProfileRelationshipType screens a row's relationship_type against
// the closed single-member vocabulary and the static-token character class before
// it is interpolated into the relationship-type position. Membership is checked,
// not just the character class, so a charset-safe but out-of-vocabulary token (or
// injected text) can never reach the relationship-type position.
func validateEC2UsesProfileRelationshipType(row map[string]any) (string, error) {
	return validateStaticGraphToken(row, "relationship_type", ec2UsesProfileRelationshipVocabulary, "ec2 uses-profile relationship_type")
}

// ec2UsesProfileRelationshipType returns the single member of the closed
// relationship vocabulary for interpolation. Centralizing it keeps the upsert
// Cypher and the vocabulary in lockstep.
func ec2UsesProfileRelationshipType() string {
	for token := range ec2UsesProfileRelationshipVocabulary {
		return token
	}
	return ""
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *EC2UsesProfileEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

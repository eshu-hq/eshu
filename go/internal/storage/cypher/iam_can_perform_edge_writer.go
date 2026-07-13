// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseIAMCanPerformEdge names the IAM CAN_PERFORM effective-permission
// edge projection phase for grouped-backend statement metadata and diagnostics
// (issue #1134 PR4a).
const canonicalPhaseIAMCanPerformEdge = "iam_can_perform_edge"

// iamCanPerformEdgeLabel is the single static Cypher relationship type the IAM
// CAN_PERFORM edge uses, doubling as the bounded entity-label tag for the
// CAN_PERFORM statement metadata. It is a fixed constant (not derived from upstream
// data) and is baked directly into the const upsert/retract templates below, so
// this writer interpolates NO data-driven token into the Cypher: both endpoints
// are the uniform :CloudResource label and the relationship type is this static
// token. The granted action set lives in an edge PROPERTY (rel.actions), NEVER in
// the relationship type, so the MERGE keys on the stable
// (principal_uid, CAN_PERFORM, resource_uid) identity and stays on NornicDB's
// relationship hot path (the documented property-keyed-relationship rule that
// CAN_ESCALATE_TO §5 navigated; a property-keyed MERGE reproduces the 20s
// validation timeout).
const iamCanPerformEdgeLabel = "CAN_PERFORM"

// canonicalIAMCanPerformEdgeUpsertCypher batches CAN_PERFORM edge upserts between
// an already-materialized IAM principal :CloudResource node and the resource
// :CloudResource node an identity or resource policy grants a catalogued
// sensitive action on.
// The relationship type is the static CAN_PERFORM token; the granted action set is
// written as a sorted/deduped list PROPERTY in the SET clause (never in the MERGE
// key) so the MERGE identity is the stable (principal_uid, CAN_PERFORM,
// resource_uid) triple and several actions to the same resource converge on one
// idempotent edge. rel.grant_sources and rel.evaluation_scope record whether the
// grant came from identity policy, resource policy, or both; permission
// boundaries, SCPs, condition values, and session policies are still outside this
// slice. Two MATCHes precede the MERGE so a row whose principal or resource node
// is absent produces no edge and no fabricated node. Both anchors are
// uid-indexed :CloudResource lookups (no scan, no N+1).
const canonicalIAMCanPerformEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (p:CloudResource {uid: row.principal_uid})
MATCH (r:CloudResource {uid: row.resource_uid})
MERGE (p)-[rel:CAN_PERFORM]->(r)
SET rel.actions = row.actions,
    rel.action_count = row.action_count,
    rel.evaluation_scope = row.evaluation_scope,
    rel.grant_sources = row.grant_sources,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractIAMCanPerformEdgesCypher removes this reducer's CAN_PERFORM edges for a
// set of scopes before a fresh generation reprojects them. The relationship type
// is fixed, so the retract matches that type from any CloudResource and scopes by
// the edge's own scope_id and evidence_source. The CloudResource endpoints are
// cross-generation canonical and carry no reducer scope_id, so a node-scoped
// predicate would make the retract a silent no-op that leaks stale CAN_PERFORM
// edges across generations (the #388/#1135 lesson).
const retractIAMCanPerformEdgesCypher = `MATCH (p:CloudResource)-[rel:CAN_PERFORM]->()
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// IAMCanPerformEdgeWriter materializes resolved IAM CAN_PERFORM decisions into
// canonical CAN_PERFORM edges between an IAM principal :CloudResource node and the
// resource :CloudResource node an identity or resource policy grants a catalogued
// sensitive action on. It satisfies the reducer-owned edge-writer consumer
// interface and writes through the backend-neutral Executor seam.
// Security-sensitive: it only persists the already-conservatively-resolved rows
// the extractor produced; it performs no permission judgment of its own.
type IAMCanPerformEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewIAMCanPerformEdgeWriter returns an IAMCanPerformEdgeWriter backed by the given
// Executor. A batchSize of 0 or less uses DefaultBatchSize (500).
func NewIAMCanPerformEdgeWriter(executor Executor, batchSize int) *IAMCanPerformEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &IAMCanPerformEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteIAMCanPerformEdges upserts CAN_PERFORM edges for the given resolved rows
// using batched MATCH-MATCH-MERGE statements. When the executor implements
// GroupExecutor all batches dispatch in a single atomic transaction; otherwise they
// run sequentially. The write is idempotent: the same
// (principal_uid, CAN_PERFORM, resource_uid) converges on one edge across batches,
// retries, and generations; rel.actions is overwritten wholesale with the
// extractor's deterministic sorted set; and a missing endpoint is a no-op rather
// than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id, and evidenceSource as rel.evidence_source. The extractor does
// not carry these reducer-scoped fields, so the writer is the single place that
// stamps them; rel.scope_id is what the prior-generation retract filters on (the
// endpoint nodes carry no reducer scope_id), so omitting it would make scope-scoped
// retract a silent no-op.
func (w *IAMCanPerformEdgeWriter) WriteIAMCanPerformEdges(
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
		return fmt.Errorf("iam can_perform edge writer executor is required")
	}

	annotated := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		annotated = append(annotated, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	stmts := buildBatchedStatements(canonicalIAMCanPerformEdgeUpsertCypher, annotated, w.batchSize)
	for index := range stmts {
		batchRows := stmts[index].Parameters["rows"].([]map[string]any)
		stmts[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseIAMCanPerformEdge
		stmts[index].Parameters[StatementMetadataEntityLabelKey] = iamCanPerformEdgeLabel
		stmts[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			iamCanPerformEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, stmts)
}

// RetractIAMCanPerformEdges removes this reducer's CAN_PERFORM edges for the given
// scopes before a fresh generation reprojects them. It is a no-op for an empty
// scope set (e.g. an empty generation). The delete is scoped to the reducer's
// evidence_source and never touches CloudResource endpoint nodes.
func (w *IAMCanPerformEdgeWriter) RetractIAMCanPerformEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("iam can_perform edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractIAMCanPerformEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseIAMCanPerformEdge,
			StatementMetadataEntityLabelKey: iamCanPerformEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				iamCanPerformEdgeLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}

	return w.dispatchRetract(ctx, []Statement{stmt})
}

// dispatch runs the prepared statements as one atomic group when the executor
// supports grouping, otherwise sequentially. Transient backend errors are
// classified retryable so the durable reducer queue can re-run the idempotent
// batch.
func (w *IAMCanPerformEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract runs retract statements sequentially through Execute,
// never ExecuteGroup. On the pinned NornicDB v1.1.11 a DELETE dispatched
// through a managed transaction (ExecuteGroup) under-applies even for a
// single statement (docs/public/reference/nornicdb-pitfalls.md); cmd/reducer
// wires GroupExecutor unconditionally for every graph backend including
// NornicDB (reducerNeo4jExecutor.ExecuteGroup), so the write-path dispatch()
// helper above is unsafe for retracts. Retract statements are idempotent and
// independently scoped, so sequential auto-commit execution is safe.
func (w *IAMCanPerformEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// canonicalPhaseCrossplaneSatisfiedByEdge names the Crossplane Claim -> XRD
// SATISFIED_BY edge projection phase for grouped-backend statement metadata
// and diagnostics.
const canonicalPhaseCrossplaneSatisfiedByEdge = "crossplane_satisfied_by_edge"

// crossplaneSatisfiedByEdgeLabel is the fixed relationship type this writer
// produces, doubling as the bounded entity-label tag for statement metadata
// and the edge-projection counter dimension.
const crossplaneSatisfiedByEdgeLabel = "SATISFIED_BY"

// crossplaneSatisfiedByEvidenceSource tags SATISFIED_BY edges written by this
// reducer domain so the prior-generation retract path scopes its delete to
// this writer's edges and never touches an edge owned by another writer.
const crossplaneSatisfiedByEvidenceSource = "reducer/crossplane-satisfied-by"

// canonicalCrossplaneSatisfiedByEdgeUpsertCypher batches SATISFIED_BY edge
// upserts between an already-materialized K8sResource node (the Claim) and
// the CrossplaneXRD node it resolved against. Both node labels are fixed,
// closed-vocabulary constants (never data-driven), so unlike the RUNS_IMAGE
// writer this template needs no runtime label validation. The relationship
// type is the static SATISFIED_BY token, kept out of the MERGE property map
// so NornicDB uses its relationship hot path. Two MATCHes precede the MERGE
// so a row whose Claim or XRD node is absent produces no edge and no
// fabricated node — the edge self-heals on a later generation once both
// endpoints commit.
const canonicalCrossplaneSatisfiedByEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (claim:K8sResource {uid: row.claim_uid})
MATCH (xrd:CrossplaneXRD {uid: row.xrd_uid})
MERGE (claim)-[rel:SATISFIED_BY]->(xrd)
SET rel.resolution_mode = row.resolution_mode,
    rel.claim_group = row.claim_group,
    rel.claim_kind = row.claim_kind,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractCrossplaneSatisfiedByEdgesCypher removes this reducer's SATISFIED_BY
// edges for a set of scopes before a fresh generation reprojects them. The
// scope predicate filters on the edge's own scope_id, not either endpoint
// node's: K8sResource and CrossplaneXRD nodes are cross-generation canonical
// and carry no reducer scope_id, so a node-scoped predicate would make the
// retract a silent no-op that leaks stale edges across generations.
const retractCrossplaneSatisfiedByEdgesCypher = `MATCH (:K8sResource)-[rel:SATISFIED_BY]->(:CrossplaneXRD)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// crossplaneSatisfiedByWriteReasons is the single source of truth for the
// relationship types this writer accepts, mirroring
// sqlRelationshipWriteReasons (edge_writer_sql.go). It backs
// CrossplaneRelationshipMaterializedEdgeTypes, the registry-derived accessor
// the blast-radius edge-materialization coverage registry
// (go/internal/query/edge_materialization_coverage.go) merges in, so
// SATISFIED_BY flips from "no_writer" to materialized:true automatically once
// this writer is wired — no second hand-maintained list.
var crossplaneSatisfiedByWriteReasons = map[string]string{
	"SATISFIED_BY": "Crossplane Claim (K8sResource) resolved against exactly one CrossplaneXRD by (group, kind) == (spec.group, spec.claimNames.kind)",
}

// CrossplaneRelationshipMaterializedEdgeTypes returns a defensive copy of
// crossplaneSatisfiedByWriteReasons: the graph relationship types the
// Crossplane edge writer actually accepts, mapped to the write reason
// recorded on each MERGEd edge. Mirrors SQLRelationshipMaterializedEdgeTypes.
func CrossplaneRelationshipMaterializedEdgeTypes() map[string]string {
	out := make(map[string]string, len(crossplaneSatisfiedByWriteReasons))
	for edgeType, reason := range crossplaneSatisfiedByWriteReasons {
		out[edgeType] = reason
	}
	return out
}

// CrossplaneSatisfiedByEdgeWriter materializes resolved Crossplane Claim ->
// XRD classification decisions into canonical SATISFIED_BY edges between a
// K8sResource node and a CrossplaneXRD node. It writes through the
// backend-neutral Executor seam, mirroring KubernetesCorrelationEdgeWriter.
type CrossplaneSatisfiedByEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewCrossplaneSatisfiedByEdgeWriter returns a CrossplaneSatisfiedByEdgeWriter
// backed by the given Executor. A batchSize of 0 or less uses
// DefaultBatchSize (500).
func NewCrossplaneSatisfiedByEdgeWriter(executor Executor, batchSize int) *CrossplaneSatisfiedByEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CrossplaneSatisfiedByEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteCrossplaneSatisfiedByEdges upserts SATISFIED_BY edges for the given
// resolved rows using batched MATCH-MATCH-MERGE statements. When the executor
// implements GroupExecutor all batches are dispatched in a single atomic
// transaction; otherwise they run sequentially. The write is idempotent: the
// same (claim_uid, SATISFIED_BY, xrd_uid) converges on one edge across
// batches, retries, and generations, and a missing endpoint is a no-op rather
// than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id, and evidenceSource as rel.evidence_source. rel.scope_id
// is what the prior-generation retract filters on (the endpoint nodes carry
// no reducer scope_id), so omitting it would make scope-scoped retract a
// silent no-op.
func (w *CrossplaneSatisfiedByEdgeWriter) WriteCrossplaneSatisfiedByEdges(
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
		return fmt.Errorf("crossplane satisfied-by edge writer executor is required")
	}

	cloned := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		clone := make(map[string]any, len(row)+3)
		for key, value := range row {
			clone[key] = value
		}
		clone["scope_id"] = scopeID
		clone["generation_id"] = generationID
		clone["evidence_source"] = evidenceSource
		cloned = append(cloned, clone)
	}

	batches := buildBatchedStatements(canonicalCrossplaneSatisfiedByEdgeUpsertCypher, cloned, w.batchSize)
	for index := range batches {
		batchRows := batches[index].Parameters["rows"].([]map[string]any)
		batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCrossplaneSatisfiedByEdge
		batches[index].Parameters[StatementMetadataEntityLabelKey] = crossplaneSatisfiedByEdgeLabel
		batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			crossplaneSatisfiedByEdgeLabel,
			len(batchRows),
		)
	}

	return w.dispatch(ctx, batches)
}

// RetractCrossplaneSatisfiedByEdges removes this writer's SATISFIED_BY edges
// for the given scopes before a fresh generation reprojects them. It is a
// no-op for an empty scope set. The delete is scoped to this writer's
// evidence_source and never touches endpoint nodes.
func (w *CrossplaneSatisfiedByEdgeWriter) RetractCrossplaneSatisfiedByEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("crossplane satisfied-by edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCrossplaneSatisfiedByEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCrossplaneSatisfiedByEdge,
			StatementMetadataEntityLabelKey: crossplaneSatisfiedByEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				crossplaneSatisfiedByEdgeLabel,
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
// batch. Mirrors KubernetesCorrelationEdgeWriter.dispatch.
func (w *CrossplaneSatisfiedByEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

// dispatchRetract routes retract statements through sequential Execute
// calls, never ExecuteGroup. On the pinned NornicDB v1.1.11 a DELETE
// dispatched through ExecuteGroup / a managed transaction under-applies —
// even a single statement — while the identical statement run as an
// auto-commit transaction (Execute) deletes correctly. See
// docs/public/reference/nornicdb-pitfalls.md and
// KubernetesCorrelationEdgeWriter.dispatchRetract for the same rationale.
func (w *CrossplaneSatisfiedByEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"

	leaf "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/crossplane"
)

// This file is a thin adapter over the canonical/crossplane leaf: it owns the
// Executor-backed writer API and Statement wrapping, while the leaf owns the
// Cypher templates, row shaping, the write-reason registry, and
// statement-free parts. Existing callers keep compiling unchanged.

const (
	canonicalPhaseCrossplaneSatisfiedByEdge = leaf.Phase
	crossplaneSatisfiedByEdgeLabel          = leaf.EdgeLabel
	crossplaneSatisfiedByEvidenceSource     = leaf.EvidenceSource
)

// CrossplaneRelationshipMaterializedEdgeTypes forwards to the
// canonical/crossplane leaf registry: the graph relationship types the
// Crossplane edge writer actually accepts, mapped to the write reason
// recorded on each MERGEd edge. Mirrors SQLRelationshipMaterializedEdgeTypes.
func CrossplaneRelationshipMaterializedEdgeTypes() map[string]string {
	return leaf.MaterializedEdgeTypes()
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

	parts := leaf.BuildUpsertParts(rows, scopeID, generationID, evidenceSource, w.batchSize)
	return w.dispatch(ctx, wrapCrossplaneParts(parts))
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

	part := leaf.BuildRetractPart(scopeIDs, generationID, evidenceSource)
	return w.dispatchRetract(ctx, wrapCrossplaneParts([]leaf.Part{part}))
}

// wrapCrossplaneParts converts the canonical/crossplane leaf's
// statement-free parts into executable Statements, preserving order.
func wrapCrossplaneParts(parts []leaf.Part) []Statement {
	if len(parts) == 0 {
		return nil
	}
	statements := make([]Statement, 0, len(parts))
	for _, part := range parts {
		operation := OperationCanonicalUpsert
		if part.IsRetract {
			operation = OperationCanonicalRetract
		}
		statements = append(statements, Statement{
			Operation:  operation,
			Cypher:     part.Cypher,
			Parameters: part.Parameters,
			Drain:      part.Drain,
		})
	}
	return statements
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

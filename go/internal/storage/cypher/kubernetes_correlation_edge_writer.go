// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// canonicalPhaseKubernetesCorrelationEdge names the live-workload RUNS_IMAGE edge
// projection phase for grouped-backend statement metadata and diagnostics.
const canonicalPhaseKubernetesCorrelationEdge = "kubernetes_correlation_edge"

// kubernetesCorrelationEdgeLabel is the single static Cypher relationship type the
// live-workload image edge uses, doubling as the bounded entity-label tag for the
// RUNS_IMAGE edge statement metadata and the edge-projection counter dimension. It
// is a fixed constant (not derived from upstream data) and is baked directly into
// the const upsert/retract Cypher templates below, so the only data-driven token
// the writer screens against injection is the source-node label.
const kubernetesCorrelationEdgeLabel = "RUNS_IMAGE"

// kubernetesEdgeSourceLabelVocabulary is the closed set of OCI digest-addressed
// node labels the RUNS_IMAGE edge is allowed to anchor its source endpoint on. It
// is the single enforcement point for the schema-surface contract on the source
// side: the source label is interpolated into the node-label position (which
// cannot be parameterized), so a label outside this set is rejected rather than
// turned into an arbitrary MATCH label that could scan or inject. The members are
// exactly the uid-indexed labels the OCI registry canonical writer commits for
// manifest / multi-arch index / reusable descriptor nodes (each is in the
// schema's uidConstraintLabels, so each MATCH is an indexed uid anchor). The set
// mirrors the reducer's sourceImageNodeLabel* vocabulary; the duplication is
// intentional because the cypher writer owns the node-label position and must not
// depend on reducer internals.
var kubernetesEdgeSourceLabelVocabulary = map[string]struct{}{
	"OciImageManifest":   {},
	"OciImageIndex":      {},
	"OciImageDescriptor": {},
}

// canonicalKubernetesCorrelationEdgeUpsertCypherFormat batches RUNS_IMAGE edge
// upserts between an already-materialized KubernetesWorkload node and the
// digest-addressed OCI source node it was observed running. The relationship type
// is the static RUNS_IMAGE token (kept out of the MERGE property map so NornicDB
// uses its relationship hot path, #805 §5.3) and the source-node label is a
// validated static token from the closed source vocabulary. Two MATCHes precede
// the MERGE so a row whose workload or source node is absent produces no edge and
// no fabricated node. The single %s is the source-node label.
const canonicalKubernetesCorrelationEdgeUpsertCypherFormat = `UNWIND $rows AS row
MATCH (w:KubernetesWorkload {uid: row.workload_uid})
MATCH (img:%s {uid: row.source_uid})
MERGE (w)-[rel:RUNS_IMAGE]->(img)
SET rel.resolution_mode = row.resolution_mode,
    rel.image_ref = row.image_ref,
    rel.source_digest = row.source_digest,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractKubernetesCorrelationEdgesCypher removes the reducer-owned RUNS_IMAGE
// edges for a set of scopes before a fresh generation reprojects them. The
// relationship type is fixed (RUNS_IMAGE), so the retract matches that type from
// any KubernetesWorkload and then scopes by the edge properties this reducer owns.
// The scope predicate filters on the edge's own scope_id, not the endpoint node's:
// KubernetesWorkload and OCI source nodes are cross-generation canonical and carry
// no reducer scope_id, so a node-scoped predicate would make the retract a silent
// no-op that leaks stale edges across generations.
const retractKubernetesCorrelationEdgesCypher = `MATCH (w:KubernetesWorkload)-[rel:RUNS_IMAGE]->()
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// KubernetesCorrelationEdgeWriter materializes resolved exact live-workload image
// correlation decisions into canonical RUNS_IMAGE edges between a
// KubernetesWorkload node and a digest-addressed OCI source node. It satisfies the
// reducer-owned edge-writer consumer interface and writes through the
// backend-neutral Executor seam.
type KubernetesCorrelationEdgeWriter struct {
	executor  Executor
	batchSize int
}

// NewKubernetesCorrelationEdgeWriter returns a KubernetesCorrelationEdgeWriter
// backed by the given Executor. A batchSize of 0 or less uses DefaultBatchSize
// (500).
func NewKubernetesCorrelationEdgeWriter(executor Executor, batchSize int) *KubernetesCorrelationEdgeWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &KubernetesCorrelationEdgeWriter{executor: executor, batchSize: batchSize}
}

// WriteKubernetesCorrelationEdges upserts RUNS_IMAGE edges for the given resolved
// rows using batched MATCH-MATCH-MERGE statements grouped by source-node label.
// When the executor implements GroupExecutor all batches are dispatched in a
// single atomic transaction; otherwise they run sequentially. The write is
// idempotent: the same (workload_uid, RUNS_IMAGE, source_uid) converges on one
// edge across batches, retries, and generations, and a missing endpoint is a
// no-op rather than a fabricated node.
//
// scopeID and generationID are injected onto every edge as rel.scope_id /
// rel.generation_id, and evidenceSource as rel.evidence_source. The resolution
// layer does not carry these reducer-scoped fields, so the writer is the single
// place that stamps them; rel.scope_id is what the prior-generation retract
// filters on (the endpoint nodes carry no reducer scope_id), so omitting it would
// make scope-scoped retract a silent no-op.
func (w *KubernetesCorrelationEdgeWriter) WriteKubernetesCorrelationEdges(
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
		return fmt.Errorf("kubernetes correlation edge writer executor is required")
	}

	grouped := make(map[string][]map[string]any)
	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		label, err := kubernetesCorrelationEdgeSourceLabel(row)
		if err != nil {
			return err
		}
		cloned := make(map[string]any, len(row)+3)
		for key, value := range row {
			cloned[key] = value
		}
		cloned["scope_id"] = scopeID
		cloned["generation_id"] = generationID
		cloned["evidence_source"] = evidenceSource
		if _, exists := grouped[label]; !exists {
			labels = append(labels, label)
		}
		grouped[label] = append(grouped[label], cloned)
	}
	sort.Strings(labels)

	var stmts []Statement
	for _, label := range labels {
		cypher := fmt.Sprintf(canonicalKubernetesCorrelationEdgeUpsertCypherFormat, label)
		batches := buildBatchedStatements(cypher, grouped[label], w.batchSize)
		for index := range batches {
			batchRows := batches[index].Parameters["rows"].([]map[string]any)
			batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseKubernetesCorrelationEdge
			batches[index].Parameters[StatementMetadataEntityLabelKey] = kubernetesCorrelationEdgeLabel
			batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
				"edge=%s source=%s rows=%d",
				kubernetesCorrelationEdgeLabel,
				label,
				len(batchRows),
			)
		}
		stmts = append(stmts, batches...)
	}

	return w.dispatch(ctx, stmts)
}

// kubernetesCorrelationEdgeSourceLabel validates the row's source-node label and
// returns it for interpolation into the source MATCH. The label must be a member
// of the closed OCI source-node vocabulary; membership is checked against
// kubernetesEdgeSourceLabelVocabulary, not just the character class, so a
// charset-safe but out-of-vocabulary token (or injected text) can never reach the
// node-label position, which is interpolated into Cypher and cannot be
// parameterized. The character-class screen runs first to keep the error for
// unsafe input precise before the allowlist check.
func kubernetesCorrelationEdgeSourceLabel(row map[string]any) (string, error) {
	raw, ok := row["source_label"].(string)
	if !ok || raw == "" || raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("kubernetes correlation source_label must be a non-empty string")
	}
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", fmt.Errorf("kubernetes correlation source_label %q contains unsupported character %q", raw, ch)
	}
	if _, ok := kubernetesEdgeSourceLabelVocabulary[raw]; !ok {
		return "", fmt.Errorf("kubernetes correlation source_label %q is outside the closed OCI source-node vocabulary", raw)
	}
	return raw, nil
}

// RetractKubernetesCorrelationEdges removes this reducer's RUNS_IMAGE edges for
// the given scopes before a fresh generation reprojects them. It is a no-op for an
// empty scope set (e.g. an empty generation). The delete is scoped to the
// reducer's evidence_source and never touches endpoint nodes.
func (w *KubernetesCorrelationEdgeWriter) RetractKubernetesCorrelationEdges(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("kubernetes correlation edge writer executor is required")
	}

	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractKubernetesCorrelationEdgesCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseKubernetesCorrelationEdge,
			StatementMetadataEntityLabelKey: kubernetesCorrelationEdgeLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				kubernetesCorrelationEdgeLabel,
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
func (w *KubernetesCorrelationEdgeWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
// never ExecuteGroup. On the pinned NornicDB v1.1.11 a DELETE dispatched
// through ExecuteGroup / a managed transaction under-applies — even a single
// statement — while the identical statement run as an auto-commit transaction
// (Execute) deletes correctly. See
// docs/public/reference/nornicdb-pitfalls.md and
// CodeInterprocEvidenceWriter.dispatchRetract for the same rationale applied
// to the code-interproc evidence retract.
func (w *KubernetesCorrelationEdgeWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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

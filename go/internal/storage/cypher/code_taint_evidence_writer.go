// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"strings"
)

const (
	canonicalPhaseCodeTaintEvidence = "code_taint_evidence"
	codeTaintEvidenceLabel          = "CodeTaintEvidence"
)

// codeTaintEvidenceUpsertCypher upserts one value-flow taint finding as a
// CodeTaintEvidence node attached to the Function it concerns. The Function is
// MATCHed, never created: taint evidence must not invent a code node, so a row
// whose Function is absent from the graph contributes nothing (no orphan
// evidence). The node and edge carry confidence and provenance; they are
// evidence, not canonical truth.
const codeTaintEvidenceUpsertCypher = `UNWIND $rows AS row
MATCH (f:Function {uid: row.function_uid})
MERGE (ev:CodeTaintEvidence {uid: row.uid})
SET ev.id = row.uid,
    ev.function_uid = row.function_uid,
    ev.function_name = row.function_name,
    ev.relative_path = row.relative_path,
    ev.language = row.language,
    ev.kind = row.kind,
    ev.sink_kind = row.sink_kind,
    ev.source_kind = row.source_kind,
    ev.binding = row.binding,
    ev.source_line = row.source_line,
    ev.sink_line = row.sink_line,
    ev.confidence = row.confidence,
    ev.class_context = row.class_context,
    ev.sink_label = row.sink_label,
    ev.source_label = row.source_label,
    ev.guard_reason = row.guard_reason,
    ev.scope_id = row.scope_id,
    ev.generation_id = row.generation_id,
    ev.evidence_source = row.evidence_source
MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)
SET rel.kind = row.kind,
    rel.sink_kind = row.sink_kind,
    rel.source_kind = row.source_kind,
    rel.confidence = row.confidence,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

const retractCodeTaintEvidenceCypher = `MATCH (n:CodeTaintEvidence)
WHERE n.scope_id IN $scope_ids
  AND n.evidence_source = $evidence_source
DETACH DELETE n`

const retractStaleCodeTaintEvidenceCypher = `MATCH (n:CodeTaintEvidence)
WHERE n.scope_id = $scope_id
  AND n.evidence_source = $evidence_source
  AND n.generation_id <> $generation_id
WITH n LIMIT $limit
DETACH DELETE n`

// Anchored-delete variants that enumerate node uids from the ledger instead of
// scanning the entire :CodeTaintEvidence label. Each DETACH DELETE is bounded to
// the nodes reachable from the ledger-enumerated uid anchor via the uid index.

const retractCodeTaintEvidenceByUIDsCypher = `UNWIND $node_uids AS nuid
MATCH (n:CodeTaintEvidence {uid: nuid})
WHERE n.scope_id IN $scope_ids
  AND n.evidence_source = $evidence_source
DETACH DELETE n`

const retractStaleCodeTaintEvidenceByUIDsCypher = `UNWIND $node_uids AS nuid
MATCH (n:CodeTaintEvidence {uid: nuid})
WHERE n.scope_id = $scope_id
  AND n.evidence_source = $evidence_source
  AND n.generation_id <> $generation_id
DETACH DELETE n`

const codeTaintEvidenceRetractUIDBatchSize = 500

// CodeTaintEvidenceWriter materializes reducer-owned value-flow taint evidence
// into graph nodes and Function relationships.
type CodeTaintEvidenceWriter struct {
	executor  Executor
	batchSize int
}

// NewCodeTaintEvidenceWriter returns a CodeTaintEvidenceWriter backed by the
// given Executor. A batchSize of 0 or less uses DefaultBatchSize.
func NewCodeTaintEvidenceWriter(executor Executor, batchSize int) *CodeTaintEvidenceWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CodeTaintEvidenceWriter{executor: executor, batchSize: batchSize}
}

// WriteCodeTaintEvidence upserts reducer-owned taint evidence nodes and their
// Function relationships, stamping each row with the scope, generation, and
// evidence source. The upsert is idempotent on the node uid.
func (w *CodeTaintEvidenceWriter) WriteCodeTaintEvidence(
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
		return fmt.Errorf("code taint evidence writer executor is required")
	}

	stamped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		stamped = append(stamped, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	batches := buildBatchedStatements(codeTaintEvidenceUpsertCypher, stamped, w.batchSize)
	for index := range batches {
		batchRows := batches[index].Parameters["rows"].([]map[string]any)
		batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCodeTaintEvidence
		batches[index].Parameters[StatementMetadataEntityLabelKey] = codeTaintEvidenceLabel
		batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"label=%s rows=%d",
			codeTaintEvidenceLabel,
			len(batchRows),
		)
	}
	return w.dispatch(ctx, batches)
}

// RetractCodeTaintEvidence removes reducer-owned taint evidence nodes for the
// given scopes before a fresh generation reprojects them.
func (w *CodeTaintEvidenceWriter) RetractCodeTaintEvidence(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("code taint evidence writer executor is required")
	}
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCodeTaintEvidenceCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCodeTaintEvidence,
			StatementMetadataEntityLabelKey: codeTaintEvidenceLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s retract scopes=%d generation=%s",
				codeTaintEvidenceLabel,
				len(scopeIDs),
				generationID,
			),
		},
	}
	return w.dispatchRetract(ctx, []Statement{stmt})
}

// RetractStaleCodeTaintEvidence removes one bounded batch of taint evidence
// nodes whose generation is not the current generation for the exact
// scope/source pair. It is safe for side cleanup after current evidence has
// been written; unlike RetractCodeTaintEvidence it must not delete all evidence
// for the scope.
func (w *CodeTaintEvidenceWriter) RetractStaleCodeTaintEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
	evidenceSource string,
	limit int,
) error {
	if err := validateStaleEvidenceRetractInputs(scopeID, generationID, evidenceSource); err != nil {
		return err
	}
	if w.executor == nil {
		return fmt.Errorf("code taint evidence writer executor is required")
	}
	limit = positiveEvidenceCleanupLimit(limit)
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractStaleCodeTaintEvidenceCypher,
		Parameters: map[string]any{
			"scope_id":                      scopeID,
			"generation_id":                 generationID,
			"evidence_source":               evidenceSource,
			"limit":                         limit,
			StatementMetadataPhaseKey:       canonicalPhaseCodeTaintEvidence,
			StatementMetadataEntityLabelKey: codeTaintEvidenceLabel,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"label=%s stale_retract scopes=1 current_generations=1 limit=%d",
				codeTaintEvidenceLabel,
				limit,
			),
		},
	}
	return w.dispatchRetract(ctx, []Statement{stmt})
}

// RetractCodeTaintEvidenceByUIDs removes reducer-owned taint evidence nodes for
// the given scopes, enumerating node uids from the ledger instead of scanning the
// whole :CodeTaintEvidence label. Empty node_uids is a no-op.
func (w *CodeTaintEvidenceWriter) RetractCodeTaintEvidenceByUIDs(
	ctx context.Context,
	nodeUIDs []string,
	scopeIDs []string,
	evidenceSource string,
) error {
	if len(nodeUIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("code taint evidence writer executor is required")
	}
	batches := chunkStrings(nodeUIDs, codeTaintEvidenceRetractUIDBatchSize)
	stmts := make([]Statement, 0, len(batches))
	for _, batch := range batches {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractCodeTaintEvidenceByUIDsCypher,
			Parameters: map[string]any{
				"node_uids":                     batch,
				"scope_ids":                     scopeIDs,
				"evidence_source":               evidenceSource,
				StatementMetadataPhaseKey:       canonicalPhaseCodeTaintEvidence,
				StatementMetadataEntityLabelKey: codeTaintEvidenceLabel,
				StatementMetadataSummaryKey:     fmt.Sprintf("label=%s retract_by_uids scopes=%d uids=%d", codeTaintEvidenceLabel, len(scopeIDs), len(batch)),
			},
		})
	}
	return w.dispatchRetract(ctx, stmts)
}

// RetractStaleCodeTaintEvidenceByUIDs removes stale taint evidence nodes for one
// scope/source pair whose generation is not the current generation, enumerating
// node uids from the ledger. Empty node_uids is a no-op.
func (w *CodeTaintEvidenceWriter) RetractStaleCodeTaintEvidenceByUIDs(
	ctx context.Context,
	nodeUIDs []string,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	if len(nodeUIDs) == 0 {
		return nil
	}
	if err := validateStaleEvidenceRetractInputs(scopeID, generationID, evidenceSource); err != nil {
		return err
	}
	if w.executor == nil {
		return fmt.Errorf("code taint evidence writer executor is required")
	}
	batches := chunkStrings(nodeUIDs, codeTaintEvidenceRetractUIDBatchSize)
	stmts := make([]Statement, 0, len(batches))
	for _, batch := range batches {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractStaleCodeTaintEvidenceByUIDsCypher,
			Parameters: map[string]any{
				"node_uids":                     batch,
				"scope_id":                      scopeID,
				"generation_id":                 generationID,
				"evidence_source":               evidenceSource,
				StatementMetadataPhaseKey:       canonicalPhaseCodeTaintEvidence,
				StatementMetadataEntityLabelKey: codeTaintEvidenceLabel,
				StatementMetadataSummaryKey:     fmt.Sprintf("label=%s stale_retract_by_uids scope=%s generation=%s uids=%d", codeTaintEvidenceLabel, scopeID, generationID, len(batch)),
			},
		})
	}
	return w.dispatchRetract(ctx, stmts)
}

func (w *CodeTaintEvidenceWriter) dispatch(ctx context.Context, stmts []Statement) error {
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
// never ExecuteGroup. See CodeInterprocEvidenceWriter.dispatchRetract for the
// rationale: NornicDB v1.1.9 bolt driver bug with UNWIND/MATCH/DELETE inside
// session.ExecuteWrite / tx.Run.
func (w *CodeTaintEvidenceWriter) dispatchRetract(ctx context.Context, stmts []Statement) error {
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

func validateStaleEvidenceRetractInputs(scopeID string, generationID string, evidenceSource string) error {
	if strings.TrimSpace(scopeID) == "" {
		return fmt.Errorf("scope_id must not be blank")
	}
	if strings.TrimSpace(generationID) == "" {
		return fmt.Errorf("generation_id must not be blank")
	}
	if strings.TrimSpace(evidenceSource) == "" {
		return fmt.Errorf("evidence_source must not be blank")
	}
	return nil
}

func positiveEvidenceCleanupLimit(limit int) int {
	if limit <= 0 {
		return DefaultBatchSize
	}
	return limit
}

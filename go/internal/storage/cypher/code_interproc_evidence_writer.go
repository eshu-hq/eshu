package cypher

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

const (
	canonicalPhaseCodeInterprocEvidence = "code_interproc_evidence"
	codeInterprocEvidenceRelType        = string(edgetype.TaintFlowsTo)
)

// codeInterprocEvidenceUpsertCypher draws a TAINT_FLOWS_TO edge from the source
// Function to the sink Function of one cross-function value-flow finding. Both
// Functions are MATCHed, never created, so a finding whose endpoint is absent
// from the graph contributes nothing (no edge to a phantom node). The edge is
// keyed on its evidence uid so a re-projection is idempotent and distinct flows
// between the same pair (different kinds) stay separate. It is evidence, not
// canonical truth.
const codeInterprocEvidenceUpsertCypher = `UNWIND $rows AS row
MATCH (s:Function {uid: row.source_function_uid})
MATCH (t:Function {uid: row.sink_function_uid})
MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t)
SET rel.sink_kind = row.sink_kind,
    rel.source_kind = row.source_kind,
    rel.confidence = row.confidence,
    rel.cloud = row.cloud,
    rel.relative_path = row.relative_path,
    rel.why_trail_json = row.why_trail_json,
    rel.why_trail_truncated = row.why_trail_truncated,
    rel.scope_id = row.scope_id,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

const retractCodeInterprocEvidenceCypher = `MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function)
WHERE rel.scope_id IN $scope_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractCodeInterprocEvidenceSourceCypher = `MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractStaleCodeInterprocEvidenceCypher = `MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function)
WHERE rel.scope_id = $scope_id
  AND rel.evidence_source = $evidence_source
  AND rel.generation_id <> $generation_id
WITH rel LIMIT $limit
DELETE rel`

// CodeInterprocEvidenceWriter materializes reducer-owned cross-function taint
// evidence as TAINT_FLOWS_TO edges between Function nodes.
type CodeInterprocEvidenceWriter struct {
	executor  Executor
	batchSize int
}

// NewCodeInterprocEvidenceWriter returns a CodeInterprocEvidenceWriter backed by
// the given Executor. A batchSize of 0 or less uses DefaultBatchSize.
func NewCodeInterprocEvidenceWriter(executor Executor, batchSize int) *CodeInterprocEvidenceWriter {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	return &CodeInterprocEvidenceWriter{executor: executor, batchSize: batchSize}
}

// WriteCodeInterprocEvidence upserts the TAINT_FLOWS_TO edges, stamping each row
// with the scope, generation, and evidence source. The upsert is idempotent on
// the edge evidence uid.
func (w *CodeInterprocEvidenceWriter) WriteCodeInterprocEvidence(
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
		return fmt.Errorf("code interproc evidence writer executor is required")
	}

	stamped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		stamped = append(stamped, cloneRowWith(row, map[string]any{
			"scope_id":        scopeID,
			"generation_id":   generationID,
			"evidence_source": evidenceSource,
		}))
	}

	batches := buildBatchedStatements(codeInterprocEvidenceUpsertCypher, stamped, w.batchSize)
	for index := range batches {
		batchRows := batches[index].Parameters["rows"].([]map[string]any)
		batches[index].Parameters[StatementMetadataPhaseKey] = canonicalPhaseCodeInterprocEvidence
		batches[index].Parameters[StatementMetadataEntityLabelKey] = codeInterprocEvidenceRelType
		batches[index].Parameters[StatementMetadataSummaryKey] = fmt.Sprintf(
			"edge=%s rows=%d",
			codeInterprocEvidenceRelType,
			len(batchRows),
		)
	}
	return w.dispatch(ctx, batches)
}

// RetractCodeInterprocEvidence removes reducer-owned TAINT_FLOWS_TO edges for the
// given scopes before a fresh generation reprojects them.
func (w *CodeInterprocEvidenceWriter) RetractCodeInterprocEvidence(
	ctx context.Context,
	scopeIDs []string,
	generationID string,
	evidenceSource string,
) error {
	if len(scopeIDs) == 0 {
		return nil
	}
	if w.executor == nil {
		return fmt.Errorf("code interproc evidence writer executor is required")
	}
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCodeInterprocEvidenceCypher,
		Parameters: map[string]any{
			"scope_ids":                     scopeIDs,
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCodeInterprocEvidence,
			StatementMetadataEntityLabelKey: codeInterprocEvidenceRelType,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract scopes=%d generation=%s",
				codeInterprocEvidenceRelType,
				len(scopeIDs),
				generationID,
			),
		},
	}
	return w.dispatch(ctx, []Statement{stmt})
}

// RetractCodeInterprocEvidenceSource removes all reducer-owned TAINT_FLOWS_TO
// edges for one evidence source. It is used by global fixpoint projection,
// whose solve and write are not scoped to the triggering repository.
func (w *CodeInterprocEvidenceWriter) RetractCodeInterprocEvidenceSource(
	ctx context.Context,
	evidenceSource string,
) error {
	if evidenceSource == "" {
		return fmt.Errorf("evidence_source must not be blank")
	}
	if w.executor == nil {
		return fmt.Errorf("code interproc evidence writer executor is required")
	}
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCodeInterprocEvidenceSourceCypher,
		Parameters: map[string]any{
			"evidence_source":               evidenceSource,
			StatementMetadataPhaseKey:       canonicalPhaseCodeInterprocEvidence,
			StatementMetadataEntityLabelKey: codeInterprocEvidenceRelType,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s retract evidence_source=%s",
				codeInterprocEvidenceRelType,
				evidenceSource,
			),
		},
	}
	return w.dispatch(ctx, []Statement{stmt})
}

// RetractStaleCodeInterprocEvidence removes one bounded batch of TAINT_FLOWS_TO
// edges whose generation is not the current generation for the exact
// scope/source pair. It is safe for side cleanup after current evidence has
// been written; unlike RetractCodeInterprocEvidence it must not delete all
// evidence for the scope.
func (w *CodeInterprocEvidenceWriter) RetractStaleCodeInterprocEvidence(
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
		return fmt.Errorf("code interproc evidence writer executor is required")
	}
	limit = positiveEvidenceCleanupLimit(limit)
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractStaleCodeInterprocEvidenceCypher,
		Parameters: map[string]any{
			"scope_id":                      scopeID,
			"generation_id":                 generationID,
			"evidence_source":               evidenceSource,
			"limit":                         limit,
			StatementMetadataPhaseKey:       canonicalPhaseCodeInterprocEvidence,
			StatementMetadataEntityLabelKey: codeInterprocEvidenceRelType,
			StatementMetadataSummaryKey: fmt.Sprintf(
				"edge=%s stale_retract scopes=1 current_generations=1 limit=%d",
				codeInterprocEvidenceRelType,
				limit,
			),
		},
	}
	return w.dispatch(ctx, []Statement{stmt})
}

func (w *CodeInterprocEvidenceWriter) dispatch(ctx context.Context, stmts []Statement) error {
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

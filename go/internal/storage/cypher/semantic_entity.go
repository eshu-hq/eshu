package cypher

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// SemanticEntityWriter writes Annotation, Typedef, TypeAlias, TypeAnnotation,
// Component, Module, ImplBlock, Protocol, ProtocolImplementation, Variable,
// and semantic Function nodes into the canonical graph.
type SemanticEntityWriter struct {
	executor              Executor
	BatchSize             int
	entityLabelBatchSizes map[string]int
	writeMode             semanticEntityWriteMode
	retractMode           semanticEntityRetractMode
}

// semanticEntityWriteMode names the exact Cypher row shape used by the writer.
// Keep new backend adaptations here instead of layering additional booleans.
type semanticEntityWriteMode int

const (
	semanticEntityWriteModeLegacyRows semanticEntityWriteMode = iota
	semanticEntityWriteModeParameterizedRows
	semanticEntityWriteModeBatchedProperties
	semanticEntityWriteModeMergeFirstRows
	semanticEntityWriteModeCanonicalNodeRows
)

// semanticEntityRetractMode names how stale semantic nodes are removed before
// upserting the current semantic rows.
type semanticEntityRetractMode int

const (
	semanticEntityRetractModeBroadLabels semanticEntityRetractMode = iota
	semanticEntityRetractModeLabelScoped
)

// NewSemanticEntityWriter returns a semantic-entity writer backed by the given Executor.
func NewSemanticEntityWriter(executor Executor, batchSize int) *SemanticEntityWriter {
	return &SemanticEntityWriter{executor: executor, BatchSize: batchSize}
}

// NewSemanticEntityWriterWithParameterizedRows returns a semantic-entity writer
// that avoids inlining row metadata into the query text.
func NewSemanticEntityWriterWithParameterizedRows(executor Executor, batchSize int) *SemanticEntityWriter {
	return &SemanticEntityWriter{
		executor:  executor,
		BatchSize: batchSize,
		writeMode: semanticEntityWriteModeParameterizedRows,
	}
}

// NewSemanticEntityWriterWithBatchedProperties returns a semantic-entity
// writer that batches rows while keeping entity properties in a single map.
func NewSemanticEntityWriterWithBatchedProperties(executor Executor, batchSize int) *SemanticEntityWriter {
	return &SemanticEntityWriter{
		executor:  executor,
		BatchSize: batchSize,
		writeMode: semanticEntityWriteModeBatchedProperties,
	}
}

// NewSemanticEntityWriterWithMergeFirstRows returns a semantic-entity writer
// whose batched Cypher starts with the node MERGE before file containment. This
// keeps NornicDB on its generalized UNWIND/MERGE batch hot path while retaining
// the explicit per-label SET fields used by the legacy row templates.
func NewSemanticEntityWriterWithMergeFirstRows(executor Executor, batchSize int) *SemanticEntityWriter {
	return &SemanticEntityWriter{
		executor:  executor,
		BatchSize: batchSize,
		writeMode: semanticEntityWriteModeMergeFirstRows,
	}
}

// NewSemanticEntityWriterWithCanonicalNodeRows returns a semantic-entity writer
// that enriches canonical source-local nodes by uid without re-owning File
// containment. This keeps source-local projection responsible for node
// lifecycle and CONTAINS edges while letting backend adapters avoid repeated
// relationship-existence checks on already-materialized entities.
func NewSemanticEntityWriterWithCanonicalNodeRows(executor Executor, batchSize int) *SemanticEntityWriter {
	return &SemanticEntityWriter{
		executor:  executor,
		BatchSize: batchSize,
		writeMode: semanticEntityWriteModeCanonicalNodeRows,
	}
}

func (w *SemanticEntityWriter) batchSize() int {
	if w.BatchSize <= 0 {
		return DefaultBatchSize
	}
	return w.BatchSize
}

// WithLabelScopedRetract deletes stale semantic nodes one label at a time.
// This keeps the broad multi-label retract available by default while
// letting adapters with different label-pattern costs use the same writer seam.
func (w *SemanticEntityWriter) WithLabelScopedRetract() *SemanticEntityWriter {
	if w == nil {
		return w
	}
	w.retractMode = semanticEntityRetractModeLabelScoped
	return w
}

// WithEntityLabelBatchSize overrides the per-statement row batch size for one
// semantic entity label.
func (w *SemanticEntityWriter) WithEntityLabelBatchSize(label string, batchSize int) *SemanticEntityWriter {
	if w == nil || label == "" || batchSize <= 0 {
		return w
	}
	if w.entityLabelBatchSizes == nil {
		w.entityLabelBatchSizes = make(map[string]int)
	}
	w.entityLabelBatchSizes[label] = batchSize
	return w
}

func (w *SemanticEntityWriter) batchSizeForLabel(label string) int {
	if w.entityLabelBatchSizes != nil {
		if batchSize := w.entityLabelBatchSizes[label]; batchSize > 0 {
			return batchSize
		}
	}
	return w.batchSize()
}

// WriteSemanticEntities retracts stale semantic nodes for the touched
// repositories and upserts the current rows. When the executor supports
// GroupExecutor, all statements run in a single atomic transaction so
// concurrent workers never see partially retracted or partially written state.
func (w *SemanticEntityWriter) WriteSemanticEntities(
	ctx context.Context,
	write reducer.SemanticEntityWrite,
) (reducer.SemanticEntityWriteResult, error) {
	if len(write.RepoIDs) == 0 && len(write.Rows) == 0 {
		return reducer.SemanticEntityWriteResult{}, nil
	}
	if w.executor == nil {
		return reducer.SemanticEntityWriteResult{}, fmt.Errorf("semantic entity writer executor is required")
	}

	repoIDs := uniqueSemanticRepoIDs(write.RepoIDs)

	// Build the full statement list: retract first, then all upserts.
	var stmts []Statement
	if !write.SkipRetract {
		stmts = append(stmts, w.semanticRetractStatements(repoIDs)...)
	}

	writes := 0
	switch w.writeMode {
	case semanticEntityWriteModeParameterizedRows:
		for _, row := range write.Rows {
			stmt, ok := buildParameterizedSemanticEntityStatement(row)
			if !ok {
				continue
			}
			stmts = append(stmts, stmt)
			writes++
		}
	case semanticEntityWriteModeBatchedProperties:
		rowsByLabel := newSemanticRowsByLabel()
		for _, row := range write.Rows {
			rowMap, ok := buildSemanticEntityPropertyRowMap(row)
			if !ok {
				continue
			}
			rowsByLabel[row.EntityType] = append(rowsByLabel[row.EntityType], rowMap)
		}

		for _, plan := range semanticEntityPlans() {
			rows := rowsByLabel[plan.label]
			batchSize := w.batchSizeForLabel(plan.label)
			for start := 0; start < len(rows); start += batchSize {
				end := start + batchSize
				if end > len(rows) {
					end = len(rows)
				}
				batchRows := rows[start:end]
				stmts = append(stmts, Statement{
					Operation: OperationCanonicalUpsert,
					Cypher:    semanticEntityBatchedPropertiesUpsertCypher(plan.label),
					Parameters: map[string]any{
						"rows":                          batchRows,
						StatementMetadataEntityLabelKey: plan.label,
						StatementMetadataSummaryKey:     semanticEntityStatementSummary(plan.label, batchRows),
					},
				})
			}
			writes += len(rows)
		}
	case semanticEntityWriteModeMergeFirstRows, semanticEntityWriteModeCanonicalNodeRows:
		rowsByLabel := newSemanticRowsByLabel()
		for _, row := range write.Rows {
			rowMap, ok := buildSemanticEntityRowMap(row)
			if !ok {
				continue
			}
			rowsByLabel[row.EntityType] = append(rowsByLabel[row.EntityType], rowMap)
		}

		for _, plan := range semanticEntityPlans() {
			rows := rowsByLabel[plan.label]
			batchSize := w.batchSizeForLabel(plan.label)
			for start := 0; start < len(rows); start += batchSize {
				end := start + batchSize
				if end > len(rows) {
					end = len(rows)
				}
				batchRows := rows[start:end]
				cypher := semanticEntityMergeFirstRowsUpsertCypher(plan.cypher)
				if w.writeMode == semanticEntityWriteModeCanonicalNodeRows &&
					semanticEntityCanonicalNodeOwnedLabel(plan.label) {
					cypher = semanticEntityCanonicalNodeRowsUpsertCypher(plan.label, plan.cypher)
				}
				stmts = append(stmts, Statement{
					Operation: OperationCanonicalUpsert,
					Cypher:    cypher,
					Parameters: map[string]any{
						"rows":                          batchRows,
						StatementMetadataEntityLabelKey: plan.label,
						StatementMetadataSummaryKey:     semanticEntityStatementSummary(plan.label, batchRows),
					},
				})
			}
			writes += len(rows)
		}
	case semanticEntityWriteModeLegacyRows:
		rowsByLabel := newSemanticRowsByLabel()
		for _, row := range write.Rows {
			rowMap, ok := buildSemanticEntityRowMap(row)
			if !ok {
				continue
			}
			rowsByLabel[row.EntityType] = append(rowsByLabel[row.EntityType], rowMap)
		}

		for _, plan := range semanticEntityPlans() {
			rows := rowsByLabel[plan.label]
			batchSize := w.batchSizeForLabel(plan.label)
			for start := 0; start < len(rows); start += batchSize {
				end := start + batchSize
				if end > len(rows) {
					end = len(rows)
				}
				batchRows := rows[start:end]
				stmts = append(stmts, Statement{
					Operation: OperationCanonicalUpsert,
					Cypher:    plan.cypher,
					Parameters: map[string]any{
						"rows":                          batchRows,
						StatementMetadataEntityLabelKey: plan.label,
						StatementMetadataSummaryKey:     semanticEntityStatementSummary(plan.label, batchRows),
					},
				})
			}
			writes += len(rows)
		}
	default:
		return reducer.SemanticEntityWriteResult{}, fmt.Errorf("unsupported semantic entity write mode %d", w.writeMode)
	}

	batchSize := w.batchSize()
	ownershipRows := buildRustImplBlockOwnershipRows(write.Rows)
	for start := 0; start < len(ownershipRows); start += batchSize {
		end := start + batchSize
		if end > len(ownershipRows) {
			end = len(ownershipRows)
		}
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     semanticRustImplBlockOwnershipCypher,
			Parameters: map[string]any{"rows": ownershipRows[start:end]},
		})
	}

	if len(stmts) > 0 {
		// Prefer atomic grouped execution; fall back to sequential for
		// executors that don't support transactions (e.g., test stubs).
		if ge, ok := w.executor.(GroupExecutor); ok {
			if err := ge.ExecuteGroup(ctx, stmts); err != nil {
				return reducer.SemanticEntityWriteResult{}, fmt.Errorf("write semantic entities: %w", WrapRetryableNeo4jError(err))
			}
		} else {
			for _, stmt := range stmts {
				if err := w.executor.Execute(ctx, stmt); err != nil {
					return reducer.SemanticEntityWriteResult{}, fmt.Errorf("write semantic entities: %w", WrapRetryableNeo4jError(err))
				}
			}
		}
	}

	return reducer.SemanticEntityWriteResult{CanonicalWrites: writes}, nil
}

func (w *SemanticEntityWriter) semanticRetractStatements(repoIDs []string) []Statement {
	if w.writeMode == semanticEntityWriteModeCanonicalNodeRows {
		return w.semanticCanonicalNodeRetractStatements(repoIDs)
	}
	if w.retractMode != semanticEntityRetractModeLabelScoped {
		return []Statement{{
			Operation: OperationCanonicalRetract,
			Cypher:    semanticEntityRetractCypher,
			Parameters: map[string]any{
				"repo_ids":                  repoIDs,
				"evidence_source":           semanticEntityEvidenceSource,
				StatementMetadataSummaryKey: semanticEntityRetractStatementSummary("all", repoIDs),
			},
		}}
	}

	plans := semanticEntityPlans()
	stmts := make([]Statement, 0, len(plans))
	for _, plan := range plans {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    semanticEntityLabelRetractCypher(plan.label),
			Parameters: map[string]any{
				"repo_ids":                      repoIDs,
				"evidence_source":               semanticEntityEvidenceSource,
				StatementMetadataEntityLabelKey: plan.label,
				StatementMetadataSummaryKey:     semanticEntityRetractStatementSummary(plan.label, repoIDs),
			},
		})
	}
	return stmts
}

func (w *SemanticEntityWriter) semanticCanonicalNodeRetractStatements(repoIDs []string) []Statement {
	plans := semanticEntityPlans()
	stmts := make([]Statement, 0, len(plans))
	for _, plan := range plans {
		if semanticEntityCanonicalNodeOwnedLabel(plan.label) {
			props := semanticEntityClearPropertiesForLabel(plan.label)
			if len(props) == 0 {
				continue
			}
			stmts = append(stmts, Statement{
				Operation: OperationCanonicalRetract,
				Cypher:    semanticEntityCanonicalNodeClearCypher(plan.label, props),
				Parameters: map[string]any{
					"repo_ids":                      repoIDs,
					StatementMetadataEntityLabelKey: plan.label,
					StatementMetadataSummaryKey:     semanticEntityRetractStatementSummary(plan.label, repoIDs),
				},
			})
			continue
		}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    semanticEntityLabelRetractCypher(plan.label),
			Parameters: map[string]any{
				"repo_ids":                      repoIDs,
				"evidence_source":               semanticEntityEvidenceSource,
				StatementMetadataEntityLabelKey: plan.label,
				StatementMetadataSummaryKey:     semanticEntityRetractStatementSummary(plan.label, repoIDs),
			},
		})
	}
	return stmts
}

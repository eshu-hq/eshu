// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"sort"
)

func uniqueSemanticFilePaths(filePaths []string) []string {
	seen := make(map[string]struct{})
	unique := make([]string, 0, len(filePaths))
	for _, filePath := range filePaths {
		if filePath == "" {
			continue
		}
		if _, ok := seen[filePath]; ok {
			continue
		}
		seen[filePath] = struct{}{}
		unique = append(unique, filePath)
	}
	sort.Strings(unique)
	return unique
}

func (w *SemanticEntityWriter) semanticDeltaRetractStatements(filePaths []string) []Statement {
	if w.writeMode == semanticEntityWriteModeCanonicalNodeRows {
		return w.semanticDeltaCanonicalNodeRetractStatements(filePaths)
	}
	if w.retractMode != semanticEntityRetractModeLabelScoped {
		return []Statement{{
			Operation: OperationCanonicalRetract,
			Cypher:    semanticEntityDeltaRetractCypher,
			Parameters: map[string]any{
				"file_paths":                filePaths,
				"evidence_source":           semanticEntityEvidenceSource,
				StatementMetadataSummaryKey: semanticEntityDeltaRetractStatementSummary("all", filePaths),
			},
		}}
	}

	plans := semanticEntityPlans()
	stmts := make([]Statement, 0, len(plans))
	for _, plan := range plans {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    semanticEntityDeltaLabelRetractCypher(plan.label),
			Parameters: map[string]any{
				"file_paths":                    filePaths,
				"evidence_source":               semanticEntityEvidenceSource,
				StatementMetadataEntityLabelKey: plan.label,
				StatementMetadataSummaryKey:     semanticEntityDeltaRetractStatementSummary(plan.label, filePaths),
			},
		})
	}
	return stmts
}

func (w *SemanticEntityWriter) semanticDeltaCanonicalNodeRetractStatements(filePaths []string) []Statement {
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
				Cypher:    semanticEntityDeltaCanonicalNodeClearCypher(plan.label, props),
				Parameters: map[string]any{
					"file_paths":                    filePaths,
					StatementMetadataEntityLabelKey: plan.label,
					StatementMetadataSummaryKey:     semanticEntityDeltaRetractStatementSummary(plan.label, filePaths),
				},
			})
			continue
		}
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    semanticEntityDeltaLabelRetractCypher(plan.label),
			Parameters: map[string]any{
				"file_paths":                    filePaths,
				"evidence_source":               semanticEntityEvidenceSource,
				StatementMetadataEntityLabelKey: plan.label,
				StatementMetadataSummaryKey:     semanticEntityDeltaRetractStatementSummary(plan.label, filePaths),
			},
		})
	}
	return stmts
}

func semanticEntityDeltaRetractStatementSummary(label string, filePaths []string) string {
	return fmt.Sprintf("semantic_delta_retract label=%s file_paths=%d", label, len(filePaths))
}

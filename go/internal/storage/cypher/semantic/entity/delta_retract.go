// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"fmt"
	"sort"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
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

func (w *SemanticEntityWriter) semanticDeltaRetractStatements(filePaths []string) []sourcecypher.Statement {
	if w.writeMode == semanticEntityWriteModeCanonicalNodeRows {
		return w.semanticDeltaCanonicalNodeRetractStatements(filePaths)
	}
	// Dedicated edge retract first, edgeless-node reap second (#8359).
	if w.retractMode != semanticEntityRetractModeLabelScoped {
		return []sourcecypher.Statement{
			semanticDeltaEdgeRetractStatement(RetractSemanticEntityContainmentDeltaEdgesCypher, "all", filePaths),
			semanticDeltaEdgeRetractStatement(RetractSemanticEntityContainmentDeltaNodeScopedEdgesCypher, "all", filePaths),
			semanticDeltaEdgeRetractStatement(semanticEntityDeltaReapCypher, "all", filePaths),
		}
	}

	stmts := semanticDeltaLabelEdgeRetractStatements(filePaths)
	for _, plan := range semanticEntityPlans() {
		stmts = append(stmts, semanticDeltaEdgeRetractStatement(semanticEntityDeltaLabelReapCypher(plan.label), plan.label, filePaths))
	}
	return stmts
}

func (w *SemanticEntityWriter) semanticDeltaCanonicalNodeRetractStatements(filePaths []string) []sourcecypher.Statement {
	// The dedicated delta edge retract leads in canonical-node mode for
	// the same single-writer fence as the full-repo path (#8359).
	stmts := semanticDeltaLabelEdgeRetractStatements(filePaths)
	for _, plan := range semanticEntityPlans() {
		if semanticEntityCanonicalNodeOwnedLabel(plan.label) {
			props := semanticEntityClearPropertiesForLabel(plan.label)
			if len(props) == 0 {
				continue
			}
			stmts = append(stmts, sourcecypher.Statement{
				Operation: sourcecypher.OperationCanonicalRetract,
				Cypher:    semanticEntityDeltaCanonicalNodeClearCypher(plan.label, props),
				Parameters: map[string]any{
					"file_paths": filePaths,
					sourcecypher.StatementMetadataEntityLabelKey: plan.label,
					sourcecypher.StatementMetadataSummaryKey:     semanticEntityDeltaRetractStatementSummary(plan.label, filePaths),
				},
			})
			continue
		}
		stmts = append(stmts, semanticDeltaEdgeRetractStatement(semanticEntityDeltaLabelReapCypher(plan.label), plan.label, filePaths))
	}
	return stmts
}

func semanticEntityDeltaRetractStatementSummary(label string, filePaths []string) string {
	return fmt.Sprintf("semantic_delta_retract label=%s file_paths=%d", label, len(filePaths))
}

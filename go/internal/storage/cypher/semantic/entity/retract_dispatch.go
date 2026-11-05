// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func (w *SemanticEntityWriter) semanticRetractStatements(repoIDs []string) []sourcecypher.Statement {
	if w.writeMode == semanticEntityWriteModeCanonicalNodeRowsLegacyOrder {
		return w.semanticBroadCanonicalRetractStatements(repoIDs)
	}
	if w.writeMode == semanticEntityWriteModeCanonicalNodeRows {
		return w.semanticCanonicalNodeRetractStatements(repoIDs)
	}
	// Dedicated edge retract first, edgeless-node reap second (#8359): the
	// reap can only delete a node once the edge retract (and every other
	// writer's own retract) has taken its edges, so no foreign edge dies
	// with a shared node.
	if w.retractMode != semanticEntityRetractModeLabelScoped {
		return []sourcecypher.Statement{
			semanticEdgeRetractStatement(RetractSemanticEntityContainmentEdgesCypher, "all", repoIDs),
			semanticEdgeRetractStatement(RetractSemanticEntityContainmentNodeScopedEdgesCypher, "all", repoIDs),
			semanticEdgeRetractStatement(semanticEntityReapCypher, "all", repoIDs),
		}
	}

	stmts := semanticLabelEdgeRetractStatements(repoIDs)
	for _, plan := range semanticEntityPlans() {
		stmts = append(stmts, semanticEdgeRetractStatement(semanticEntityLabelReapCypher(plan.label), plan.label, repoIDs))
	}
	return stmts
}

// semanticBroadCanonicalRetractStatements is the Neo4j full-repo retract for
// canonical-ownership mode (#10110): the broad pipe-label edge retracts and
// edgeless reap stay (live-proven on Neo4j; the reap's evidence_source fence
// now excludes canonical-owned nodes), plus one REMOVE clear per
// canonical-owned label so stale semantic properties leave the surviving
// canonical nodes before the enrichment upserts re-SET them. No statement
// deletes a canonical-owned node.
func (w *SemanticEntityWriter) semanticBroadCanonicalRetractStatements(repoIDs []string) []sourcecypher.Statement {
	stmts := []sourcecypher.Statement{
		semanticEdgeRetractStatement(RetractSemanticEntityContainmentEdgesCypher, "all", repoIDs),
		semanticEdgeRetractStatement(RetractSemanticEntityContainmentNodeScopedEdgesCypher, "all", repoIDs),
		semanticEdgeRetractStatement(semanticEntityReapCypher, "all", repoIDs),
	}
	for _, plan := range semanticEntityPlans() {
		if !semanticEntityCanonicalNodeOwnedLabel(plan.label) {
			continue
		}
		props := semanticEntityClearPropertiesForLabel(plan.label)
		if len(props) == 0 {
			continue
		}
		stmts = append(stmts, sourcecypher.Statement{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    semanticEntityCanonicalNodeClearCypher(plan.label, props),
			Parameters: map[string]any{
				"repo_ids": repoIDs,
				sourcecypher.StatementMetadataEntityLabelKey: plan.label,
				sourcecypher.StatementMetadataSummaryKey:     semanticEntityRetractStatementSummary(plan.label, repoIDs),
			},
		})
	}
	return stmts
}

func (w *SemanticEntityWriter) semanticCanonicalNodeRetractStatements(repoIDs []string) []sourcecypher.Statement {
	// The dedicated edge retract leads even in canonical-node mode: this
	// writer still owns Module/Variable containment and Rust ImplBlock
	// ownership edges there, and the evidence_source fence keeps it off
	// the canonical writer's CONTAINS edges (#8359 single-writer fence).
	stmts := semanticLabelEdgeRetractStatements(repoIDs)
	for _, plan := range semanticEntityPlans() {
		if semanticEntityCanonicalNodeOwnedLabel(plan.label) {
			props := semanticEntityClearPropertiesForLabel(plan.label)
			if len(props) == 0 {
				continue
			}
			stmts = append(stmts, sourcecypher.Statement{
				Operation: sourcecypher.OperationCanonicalRetract,
				Cypher:    semanticEntityCanonicalNodeClearCypher(plan.label, props),
				Parameters: map[string]any{
					"repo_ids": repoIDs,
					sourcecypher.StatementMetadataEntityLabelKey: plan.label,
					sourcecypher.StatementMetadataSummaryKey:     semanticEntityRetractStatementSummary(plan.label, repoIDs),
				},
			})
			continue
		}
		stmts = append(stmts, semanticEdgeRetractStatement(semanticEntityLabelReapCypher(plan.label), plan.label, repoIDs))
	}
	return stmts
}

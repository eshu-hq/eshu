// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// buildRationaleRowMap routes a rationale EXPLAINS edge row (issue #2230) to its
// template. The source is an identity-only Rationale node; the target is the
// code entity the intent comment precedes, matched by uid.
func buildRationaleRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	rationaleUID := payloadString(payload, "rationale_uid")
	targetEntityID := payloadString(payload, "target_entity_id")
	if rationaleUID == "" || targetEntityID == "" {
		return "", nil, false
	}

	rowMap := map[string]any{
		"rationale_uid":    rationaleUID,
		"target_entity_id": targetEntityID,
		"repo_id":          payloadString(payload, "repo_id"),
		"comment_kind":     payloadString(payload, "comment_kind"),
		"excerpt_hash":     payloadString(payload, "excerpt_hash"),
		"evidence_source":  evidenceSource,
	}
	return batchCanonicalRationaleExplainsEdgeCypher, rowMap, true
}

// BuildRetractRationaleEdges builds a repo-scoped EXPLAINS edge retraction
// statement.
func BuildRetractRationaleEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractRationaleEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildRetractRationaleEdgeStatementsByFilePath builds per-target-label
// EXPLAINS edge retraction statements for target code entities owned by the
// given repo-qualified paths.
//
// A single statement cannot bind all target labels on NornicDB: a bare MATCH
// whose target carries a node-label disjunction matches zero rows on v1.1.11
// (probed — the combined statement deleted nothing while these per-label
// statements deleted every edge), so the retract fans out to one statement per
// label in rationaleExplainsTargetLabels, the same list the write template's
// disjunction is built from. The statements run sequentially, each in its own
// transaction, for the managed-transaction reason documented on
// executeCodeCallRetractStatements.
func BuildRetractRationaleEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	stmts := make([]Statement, 0, len(rationaleExplainsTargetLabels))
	for _, label := range rationaleExplainsTargetLabels {
		cypher := "MATCH (rationale:Rationale)-[rel:EXPLAINS]->(target:" + label + ")\n" +
			"WHERE target.path IN $file_paths\n" +
			"  AND rel.evidence_source = $evidence_source\n" +
			"DELETE rel"
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				"file_paths":      filePaths,
				"evidence_source": evidenceSource,
			},
		})
	}
	return stmts
}

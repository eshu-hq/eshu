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

// BuildRetractRationaleEdgesByFilePath builds an EXPLAINS edge retraction
// statement for target code entities owned by the given repo-qualified paths.
func BuildRetractRationaleEdgesByFilePath(filePaths []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractRationaleEdgesByFileCypher,
		Parameters: map[string]any{
			"file_paths":      filePaths,
			"evidence_source": evidenceSource,
		},
	}
}

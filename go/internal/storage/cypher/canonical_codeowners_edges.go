// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// BuildCodeownersOwnershipRowMap converts a codeowners_ownership intent payload
// into the flat UNWIND parameter map for the DECLARES_CODEOWNER upsert. It
// skips the row (ok=false) when any MERGE key — repo_id, owner_ref, pattern,
// or source_path — is empty so an unresolvable edge is never written.
func BuildCodeownersOwnershipRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	repoID := PayloadString(payload, "repo_id")
	ownerRef := PayloadString(payload, "owner_ref")
	pattern := PayloadString(payload, "pattern")
	sourcePath := PayloadString(payload, "source_path")
	if repoID == "" || ownerRef == "" || pattern == "" || sourcePath == "" {
		return "", nil, false
	}
	return node.BatchCanonicalCodeownersOwnershipEdgeCypher, map[string]any{
		"repo_id":         repoID,
		"owner_ref":       ownerRef,
		"pattern":         pattern,
		"source_path":     sourcePath,
		"order_index":     PayloadInt(payload, "order_index"),
		"generation_id":   PayloadString(payload, "generation_id"),
		"evidence_source": evidenceSource,
	}, true
}

// BuildRetractCodeownersOwnershipEdges builds the whole-repository
// DECLARES_CODEOWNER retract statement for the given repositories.
func BuildRetractCodeownersOwnershipEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    node.RetractCodeownersOwnershipEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildRetractCodeownersOwnershipEdgesByFilePath builds the delta-scoped
// DECLARES_CODEOWNER retract statement for the given repo-relative changed or
// deleted file paths, anchored on the owning repositories (repoIDs) so the
// sweep never crosses into another repository's edges at the same relative
// source_path (#5419 P1).
func BuildRetractCodeownersOwnershipEdgesByFilePath(repoIDs, filePaths []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    node.RetractCodeownersOwnershipEdgesByFilePathCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"file_paths":      filePaths,
			"evidence_source": evidenceSource,
		},
	}
}

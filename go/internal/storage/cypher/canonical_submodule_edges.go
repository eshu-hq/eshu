// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// BuildSubmodulePinRowMap converts a submodule_pin intent payload into the
// flat UNWIND parameter map for the PINS_SUBMODULE upsert. It skips the row
// (ok=false) when any MERGE key — parent_repo_id, resolved_repo_id, or
// submodule_path — is empty so an unresolvable edge is never written.
// pinned_sha is always sent, nil when unknown, so a fact with no gitlink
// observes as a Cypher null rather than an empty string. The key must be
// present even without a value: the pinned NornicDB v1.3.3 stores the literal
// text "row.pinned_sha" for a missing UNWIND row key (#6782).
func BuildSubmodulePinRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	parentRepoID := PayloadString(payload, "parent_repo_id")
	resolvedRepoID := PayloadString(payload, "resolved_repo_id")
	submodulePath := PayloadString(payload, "submodule_path")
	if parentRepoID == "" || resolvedRepoID == "" || submodulePath == "" {
		return "", nil, false
	}
	rowMap := map[string]any{
		"parent_repo_id":   parentRepoID,
		"resolved_repo_id": resolvedRepoID,
		"submodule_path":   submodulePath,
		"generation_id":    PayloadString(payload, "generation_id"),
		"evidence_source":  evidenceSource,
	}
	setOptionalRowString(rowMap, payload, "pinned_sha")
	return node.BatchCanonicalSubmodulePinEdgeCypher, rowMap, true
}

// BuildRetractSubmodulePinEdges builds the whole-repository PINS_SUBMODULE
// retract statement for the given parent repositories.
func BuildRetractSubmodulePinEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    node.RetractSubmodulePinEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Batched UNWIND Cypher for submodule PINS_SUBMODULE edges (issue #5420
// Phase 3).
//
// A PINS_SUBMODULE edge links the parent Repository that declares a
// ".gitmodules"/gitlink submodule reference to the Repository its submodule
// URL resolved to. Both endpoints are existing Repository nodes — unlike
// codeowners' DECLARES_CODEOWNER (which MERGEs a new CodeownerTeam node),
// there is no new node label here, so no uniqueness constraint is needed
// beyond the existing Repository.id constraint. Both are MERGEd inline by
// this same statement, so there is no cross-acceptance-unit MATCH dependency
// and this domain needs no readiness gate (mirrors
// batchCanonicalCodeownersOwnershipEdgeCypher and invokes_cloud_action).
//
// The relationship MERGE key intentionally includes path, not just the
// (parent, target) endpoints: a parent repository can pin the same target
// repository at more than one path (a vendored copy checked out twice) or pin
// different targets at different paths, and "one edge per (parent, path)" is
// the correctness contract (#5420 Phase 3 design) — mirroring
// batchCanonicalCodeownersOwnershipEdgeCypher's pattern+source_path
// relationship key for the identical reason (a bare
// `MERGE (parent)-[rel:PINS_SUBMODULE]->(target)` would rebind whichever
// PINS_SUBMODULE relationship already exists between that pair, silently
// collapsing two distinct submodule paths into one graph edge).
const batchCanonicalSubmodulePinEdgeCypher = `UNWIND $rows AS row
MERGE (parent:Repository {id: row.parent_repo_id})
MERGE (target:Repository {id: row.resolved_repo_id})
MERGE (parent)-[rel:PINS_SUBMODULE {path: row.submodule_path}]->(target)
SET rel.pinned_sha = row.pinned_sha,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractSubmodulePinEdgesCypher removes every PINS_SUBMODULE edge this
// evidence source owns for a set of parent repositories before they are
// re-projected (whole-repository retract, used when the generation carries no
// delta scope, or when a delta generation's ".gitmodules" changed or was
// deleted — see submodulePinDeltaScope). The MATCH anchors on the parent
// Repository (parent.id IN $repo_ids), never on an unqualified path or
// unrelated property: mirroring the #5419 P1 fix for
// retractCodeownersOwnershipEdgesByFilePathCypher, an unanchored match here
// would risk retracting an unrelated repository's PINS_SUBMODULE edges. The
// target Repository node is intentionally left in place: it is a real,
// independently-owned repository, not scoped to this evidence source.
const retractSubmodulePinEdgesCypher = `MATCH (parent:Repository)-[rel:PINS_SUBMODULE]->(:Repository)
WHERE parent.id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// buildSubmodulePinRowMap converts a submodule_pin intent payload into the
// flat UNWIND parameter map for the PINS_SUBMODULE upsert. It skips the row
// (ok=false) when any MERGE key — parent_repo_id, resolved_repo_id, or
// submodule_path — is empty so an unresolvable edge is never written.
// pinned_sha is included only when known, so a fact with no gitlink observes
// as a Cypher null (removing any stale property) rather than an empty
// string.
func buildSubmodulePinRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	parentRepoID := payloadString(payload, "parent_repo_id")
	resolvedRepoID := payloadString(payload, "resolved_repo_id")
	submodulePath := payloadString(payload, "submodule_path")
	if parentRepoID == "" || resolvedRepoID == "" || submodulePath == "" {
		return "", nil, false
	}
	rowMap := map[string]any{
		"parent_repo_id":   parentRepoID,
		"resolved_repo_id": resolvedRepoID,
		"submodule_path":   submodulePath,
		"generation_id":    payloadString(payload, "generation_id"),
		"evidence_source":  evidenceSource,
	}
	if pinnedSHA := payloadString(payload, "pinned_sha"); pinnedSHA != "" {
		rowMap["pinned_sha"] = pinnedSHA
	}
	return batchCanonicalSubmodulePinEdgeCypher, rowMap, true
}

// BuildRetractSubmodulePinEdges builds the whole-repository PINS_SUBMODULE
// retract statement for the given parent repositories.
func BuildRetractSubmodulePinEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractSubmodulePinEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

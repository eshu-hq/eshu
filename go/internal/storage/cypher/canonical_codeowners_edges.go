// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Batched UNWIND Cypher for codeowners DECLARES_CODEOWNER edges (issue #5419
// Phase 3).
//
// A DECLARES_CODEOWNER edge links the Repository a CODEOWNERS rule was
// declared in to the CodeownerTeam the rule names as an owner. Both endpoints
// are MERGEd inline by this same statement (Repository is already merged
// elsewhere in the graph by repo-identity writers; CodeownerTeam has no other
// writer), so there is no cross-acceptance-unit MATCH dependency and this
// domain needs no readiness gate (mirrors invokes_cloud_action).
//
// The MERGE key for the relationship itself intentionally includes pattern and
// source_path, not just the (repo, team) endpoints: the same owner can be
// named by more than one rule pattern in one CODEOWNERS file (or across
// multiple CODEOWNERS files in the repo), and "one edge per (rule pattern,
// owner)" is the correctness contract (#5419 Phase 3 design). A bare
// `MERGE (repo)-[rel:DECLARES_CODEOWNER]->(team)` with no relationship
// properties would MERGE onto whichever DECLARES_CODEOWNER relationship
// already exists between that repo and that team — Cypher's MERGE binds an
// existing relationship pattern match the same way MATCH does, so with two
// different patterns owning the same team it would rebind the ALREADY-CREATED
// relationship from the first row and overwrite its pattern/source_path with
// the second row's values, silently collapsing two distinct rule
// declarations into one graph edge. Keying the MERGE on
// (pattern, source_path) makes each (rule, owner) pair its own relationship,
// so parallel DECLARES_CODEOWNER edges between the same repo and team are
// preserved.
const batchCanonicalCodeownersOwnershipEdgeCypher = `UNWIND $rows AS row
MERGE (repo:Repository {id: row.repo_id})
MERGE (team:CodeownerTeam {ref: row.owner_ref})
MERGE (repo)-[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]->(team)
SET rel.order_index = row.order_index,
    rel.generation_id = row.generation_id,
    rel.evidence_source = row.evidence_source`

// retractCodeownersOwnershipEdgesCypher removes every DECLARES_CODEOWNER edge
// this evidence source owns for a set of repositories before they are
// re-projected (whole-repository retract, used when the generation carries no
// delta scope). The CodeownerTeam node is intentionally left in place: it is
// shared, ref-keyed, and identical across repos, so retracting the edge alone
// is correct.
const retractCodeownersOwnershipEdgesCypher = `MATCH (repo:Repository)-[rel:DECLARES_CODEOWNER]->(:CodeownerTeam)
WHERE repo.id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// retractCodeownersOwnershipEdgesByFilePathCypher removes the DECLARES_CODEOWNER
// edges whose source_path matches one of the repo's changed or deleted
// relative paths for this generation (delta retract). This is the sweep that
// keeps a removed or edited CODEOWNERS file from leaking stale edges: the
// handler passes every path the repository delta touched (not just CODEOWNERS
// paths), and this WHERE clause naturally narrows to the edges whose
// source_path actually matches.
//
// The MATCH MUST also anchor on the owning Repository (repo.id IN
// $repo_ids): source_path is a bare repo-relative path (".github/CODEOWNERS",
// "CODEOWNERS", "docs/CODEOWNERS") that is IDENTICAL across every repository
// in the graph, unlike the inheritance delta retract's repo-qualified file
// paths (see canonical_inheritance_retract.go). Without the repository
// anchor, a delta retract triggered by one repo's CODEOWNERS change would
// delete every other repo's DECLARES_CODEOWNER edges at the same relative
// path — a cross-repo over-retraction (#5419 P1) — and those other repos are
// not re-projected in this generation, so they would permanently lose
// ownership edges until their own next generation.
const retractCodeownersOwnershipEdgesByFilePathCypher = `MATCH (repo:Repository)-[rel:DECLARES_CODEOWNER]->(:CodeownerTeam)
WHERE repo.id IN $repo_ids
  AND rel.source_path IN $file_paths
  AND rel.evidence_source = $evidence_source
DELETE rel`

// buildCodeownersOwnershipRowMap converts a codeowners_ownership intent payload
// into the flat UNWIND parameter map for the DECLARES_CODEOWNER upsert. It
// skips the row (ok=false) when any MERGE key — repo_id, owner_ref, pattern,
// or source_path — is empty so an unresolvable edge is never written.
func buildCodeownersOwnershipRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	repoID := payloadString(payload, "repo_id")
	ownerRef := payloadString(payload, "owner_ref")
	pattern := payloadString(payload, "pattern")
	sourcePath := payloadString(payload, "source_path")
	if repoID == "" || ownerRef == "" || pattern == "" || sourcePath == "" {
		return "", nil, false
	}
	return batchCanonicalCodeownersOwnershipEdgeCypher, map[string]any{
		"repo_id":         repoID,
		"owner_ref":       ownerRef,
		"pattern":         pattern,
		"source_path":     sourcePath,
		"order_index":     payloadInt(payload, "order_index"),
		"generation_id":   payloadString(payload, "generation_id"),
		"evidence_source": evidenceSource,
	}, true
}

// BuildRetractCodeownersOwnershipEdges builds the whole-repository
// DECLARES_CODEOWNER retract statement for the given repositories.
func BuildRetractCodeownersOwnershipEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCodeownersOwnershipEdgesCypher,
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
		Cypher:    retractCodeownersOwnershipEdgesByFilePathCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"file_paths":      filePaths,
			"evidence_source": evidenceSource,
		},
	}
}

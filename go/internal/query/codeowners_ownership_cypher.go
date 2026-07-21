// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// codeownersOwnershipCypher builds the bounded Cypher for
// GET /api/v0/codeowners/ownership (issue #5419 Phase 4).
//
// The graph anchors on the Repository.id uniqueness constraint
// (repository_id, nornicdb_repository_id_lookup index,
// go/internal/graph/schema_tables.go) and walks the Phase 3
// DECLARES_CODEOWNER edge Repository -[:DECLARES_CODEOWNER]-> CodeownerTeam
// (go/internal/storage/cypher/canonical_codeowners_edges.go). CodeownerTeam.ref
// is separately uniqueness-constrained (codeowner_team_ref,
// nornicdb_codeowner_team_ref_lookup), so both traversal endpoints resolve
// through an index rather than a label scan.
//
// Ordering is deterministic on (rel.order_index, rel.pattern, team.ref): one
// repository can declare the same pattern with more than one owner token,
// which the reducer materializes as sibling DECLARES_CODEOWNER edges sharing
// order_index and pattern but differing only in the target team.ref
// (buildCodeownersOwnershipRowMap in canonical_codeowners_edges.go), so
// team.ref is required as the final tie-breaker for an unambiguous keyset
// cursor. The keyset predicate compares the same three-column tuple
// lexicographically, mirroring dependenciesCypher's two-column keyset shape.
// $after_order_index defaults to -1 (order_index is always >= 0 per
// sdk/go/factschema/codeowners/v1.Ownership.OrderIndex), the "no cursor"
// sentinel, the same convention dependenciesCypher uses with an empty-string
// sentinel for its string cursor columns.
//
// Expected cardinality: one repository's CODEOWNERS declarations, bounded by
// the caller's limit (max 200, codeownersOwnershipMaxLimit). This is a new
// read surface, not a rewrite of an existing hot-path query, so there is no
// prior-shape baseline to diff against; see the Performance Evidence note in
// go/internal/query/README.md for the anchor/index argument recorded in place
// of a live PROFILE (no local NornicDB/Neo4j checkout was available to
// profile against, per cypher-query-rigor).
func codeownersOwnershipCypher(
	repoID string,
	afterOrderIndex int,
	afterPattern string,
	afterRef string,
	limit int,
) (string, map[string]any) {
	params := map[string]any{
		"repo_id":           repoID,
		"after_order_index": afterOrderIndex,
		"after_pattern":     afterPattern,
		"after_ref":         afterRef,
		"limit":             limit,
	}
	return `MATCH (repo:Repository {id: $repo_id})-[rel:DECLARES_CODEOWNER]->(team:CodeownerTeam)
WHERE team.ref IS NOT NULL AND team.ref <> ''
  AND rel.pattern IS NOT NULL AND rel.pattern <> ''
  AND rel.source_path IS NOT NULL AND rel.source_path <> ''
  AND ($after_order_index < 0
       OR rel.order_index > $after_order_index
       OR (rel.order_index = $after_order_index AND rel.pattern > $after_pattern)
       OR (rel.order_index = $after_order_index AND rel.pattern = $after_pattern AND team.ref > $after_ref))
RETURN rel.pattern AS pattern,
       rel.source_path AS source_path,
       rel.order_index AS order_index,
       team.ref AS owner_ref
ORDER BY rel.order_index, rel.pattern, team.ref
LIMIT $limit`, params
}

// codeownersLastMatchOwnerCypher builds the single-row Cypher the precedence
// resolver (codeowners_ownership_precedence.go) uses to find a repository's
// last-match-wins CODEOWNERS owner: CODEOWNERS resolves ownership by the LAST
// pattern in the file that matches, so the rule with the highest order_index
// is the repository-wide fallback candidate. This is deliberately a separate,
// descending-order, LIMIT-1 query rather than reusing the paginated
// ascending-order codeownersOwnershipCypher list: the highest order_index row
// can be arbitrarily far past the first page a caller happens to have
// fetched, so only a dedicated DESC-ordered read finds it correctly. Same
// anchors and non-null guards as codeownersOwnershipCypher; team.ref ASC
// breaks a tie between two owners declared on the same last-matching line
// deterministically.
func codeownersLastMatchOwnerCypher(repoID string) (string, map[string]any) {
	params := map[string]any{"repo_id": repoID}
	return `MATCH (repo:Repository {id: $repo_id})-[rel:DECLARES_CODEOWNER]->(team:CodeownerTeam)
WHERE team.ref IS NOT NULL AND team.ref <> ''
RETURN team.ref AS owner_ref
ORDER BY rel.order_index DESC, team.ref ASC
LIMIT 1`, params
}

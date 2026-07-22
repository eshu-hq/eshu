// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

type codeownersOwnershipGraphQuery struct {
	cypher string
	params map[string]any
}

const codeownersOwnershipMatch = `MATCH (repo:Repository {id: $repo_id})-[rel:DECLARES_CODEOWNER]->(team:CodeownerTeam)
WHERE team.ref IS NOT NULL AND team.ref <> ''
  AND rel.pattern IS NOT NULL AND rel.pattern <> ''
  AND rel.source_path IS NOT NULL AND rel.source_path <> ''`

const codeownersOwnershipReturn = `RETURN rel.pattern AS pattern,
       rel.source_path AS source_path,
       rel.order_index AS order_index,
       team.ref AS owner_ref
ORDER BY rel.order_index, rel.pattern, team.ref
LIMIT $limit`

// codeownersOwnershipCyphers builds the bounded graph reads for
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
// cursor. A cursor page uses three disjoint predicates whose union is the
// lexicographic keyset suffix. The pinned NornicDB 1.1.11 image evaluates the
// equivalent mixed OR predicate incorrectly, so the handler executes these
// individually and merges their bounded results.
// A page without a cursor remains one query and omits cursor parameters.
//
// Expected cardinality: one repository's CODEOWNERS declarations, bounded by
// the caller's limit (max 200, codeownersOwnershipMaxLimit). Each cursor branch
// returns at most limit rows. With the API's limit+1 truncation probe and
// maximum page size of 200, the merge holds at most 603 rows.
func codeownersOwnershipCyphers(
	repoID string,
	afterOrderIndex int,
	afterPattern string,
	afterRef string,
	limit int,
) []codeownersOwnershipGraphQuery {
	baseParams := map[string]any{
		"repo_id": repoID,
		"limit":   limit,
	}
	if afterOrderIndex == codeownersOwnershipNoCursor {
		return []codeownersOwnershipGraphQuery{{
			cypher: codeownersOwnershipMatch + "\n" + codeownersOwnershipReturn,
			params: baseParams,
		}}
	}

	queries := make([]codeownersOwnershipGraphQuery, 0, 3)
	queries = append(queries, codeownersOwnershipCursorQuery(
		baseParams,
		"rel.order_index > $after_order_index",
		map[string]any{"after_order_index": afterOrderIndex},
	))
	queries = append(queries, codeownersOwnershipCursorQuery(
		baseParams,
		"rel.order_index = $after_order_index AND rel.pattern > $after_pattern",
		map[string]any{
			"after_order_index": afterOrderIndex,
			"after_pattern":     afterPattern,
		},
	))
	queries = append(queries, codeownersOwnershipCursorQuery(
		baseParams,
		"rel.order_index = $after_order_index AND rel.pattern = $after_pattern AND team.ref > $after_ref",
		map[string]any{
			"after_order_index": afterOrderIndex,
			"after_pattern":     afterPattern,
			"after_ref":         afterRef,
		},
	))
	return queries
}

func codeownersOwnershipCursorQuery(
	baseParams map[string]any,
	predicate string,
	cursorParams map[string]any,
) codeownersOwnershipGraphQuery {
	params := make(map[string]any, len(baseParams)+len(cursorParams))
	for key, value := range baseParams {
		params[key] = value
	}
	for key, value := range cursorParams {
		params[key] = value
	}
	return codeownersOwnershipGraphQuery{
		cypher: codeownersOwnershipMatch + "\n  AND " + predicate + "\n" + codeownersOwnershipReturn,
		params: params,
	}
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
// anchors and non-null guards as codeownersOwnershipCyphers; team.ref ASC
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

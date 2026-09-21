// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Compare-code-paths bounds. Depth defaults to the #6834 measured bound
// (fixture `*1..4`); the row cap is the proven bound mechanism (expansion
// stops when the cap fills, flat at 421 db hits), so max paths caps the
// enumeration and the visit budget caps the hop reads behind it.
const (
	CompareDefaultDepth    = 4
	CompareMaxDepth        = 6
	CompareDefaultMaxPaths = 5
	CompareMaxPaths        = 20
	CompareVisitBudget     = 500
)

func normalizeCompareDepth(depth int) int {
	if depth <= 0 {
		return CompareDefaultDepth
	}
	if depth > CompareMaxDepth {
		return CompareMaxDepth
	}
	return depth
}

func normalizeCompareMaxPaths(maxPaths int) int {
	if maxPaths <= 0 {
		return CompareDefaultMaxPaths
	}
	if maxPaths > CompareMaxPaths {
		return CompareMaxPaths
	}
	return maxPaths
}

// BuildComparePathsHopCypher renders the one-hop outgoing read the
// compare-code-paths BFS layers over: the outgoing CALLS edges of one
// anchored source with the callee identity, repository, and edge
// confidence the BFS needs for per-hop grant filtering and weakest-edge
// confidence. Both backends return the same columns.
//
// A Cypher variable-length multi-path read is deliberately NOT the shipped
// shape. Live probes on the pinned NornicDB build (digest in the #6838
// live test) show enumeration works (`*1..4` returns the 4 diamond rows)
// but every path-content projection evaluates empty: nodes(p) and
// relationships(p) come back as `[[]]`, length(p) as 0, and raw `p` as
// nil — the same unevaluated-path family as
// docs/public/reference/nornicdb-path-predicate-pitfalls.md. The BFS over
// this one-hop read returns the same distinct simple paths on both
// backends by construction instead.
func BuildComparePathsHopCypher(
	sourceEntityID, repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"source_entity_id": strings.TrimSpace(sourceEntityID)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(callee.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("callee", "repo_id"))
	}
	returns := "\n\t\tRETURN coalesce(callee.id, callee.uid) as id,\n" +
		"\t\t       callee.name as name,\n" +
		"\t\t       coalesce(callee.repo_id, '') as repo_id,\n" +
		"\t\t       coalesce(rel.confidence, 0) as edge_confidence\n"
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tMATCH " + relationships.NornicDBNodePattern("source", "Function", "$source_entity_id") + "\n")
		cypher.WriteString("\t\tMATCH (source)-[rel:CALLS]->(callee)")
		if len(predicates) > 0 {
			cypher.WriteString("\n\t\tWHERE " + strings.Join(predicates, " AND "))
		}
		cypher.WriteString(returns)
		return cypher.String(), params
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (source:" + wrapperBypassAnchorLabels + ")\n")
	cypher.WriteString("\t\tMATCH (source)-[rel:CALLS]->(callee)\n")
	cypher.WriteString("\t\tWHERE " + codemodel.GraphEntityIDPredicate("source", "$source_entity_id"))
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tAND coalesce(source.repo_id, '') = $repo_id")
	}
	for _, predicate := range predicates {
		cypher.WriteString("\n\t\tAND " + predicate)
	}
	cypher.WriteString(returns)
	return cypher.String(), params
}

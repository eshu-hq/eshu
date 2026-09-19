// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RepositoryDependencyGroupedEdgeCypher is the unscoped repository
// dependency-edge read. It returns one row per source repository with the
// ids of every repository it DEPENDS_ON, and loadRepositoryDependencyEdges
// flattens those groups into the same (source, target)-ordered edge list the
// per-edge read (repositoryDependencyClusterEdgeCypher) returns. It is
// exported so the query-plan manifest (QP-REPOSITORY-DEPENDS-ON-GROUPED-EDGES)
// can bind the exact production statement.
//
// Why this shape: NornicDB v1.3.3 answers a fixed 1-hop typed pattern with
// no WHERE clause and a `RETURN start.prop, collect(end.prop)` projection
// through its relationship-aggregation fast path (pkg/cypher
// traversal_fast_agg.go, tryFastSingleHopAgg): it walks the DEPENDS_ON
// relationship-type index once and checks both endpoint labels in a batch.
// The per-edge `RETURN s.id, t.id` form instead loads every :Repository and
// expands its full adjacency looking for DEPENDS_ON, so its cost grows with
// every file, workload and other edge a repository owns (#6794; #6786 review
// R2-F6). Measured on NornicDB v1.3.3 with the Eshu schema applied, 500
// repositories each holding 200 REPO_CONTAINS files and 600 functions, 500
// workloads, about 2,000 Workload DEPENDS_ON and 300 Repository DEPENDS_ON
// edges: 0.0045s median for this read against 0.635s for the per-edge read,
// with identical flattened row sets at LIMIT 50001, 100 and 7. On Neo4j 2026
// both shapes cost about 5ms. Evidence:
// docs/internal/evidence/6786-repository-dependency-marker-and-relationship-repo-anchor.md.
//
// Keep it exactly this shape. A WHERE clause (including a grant predicate)
// disables the fast path, which is why scoped callers stay on the per-edge
// read; a `WHERE s:Repository AND t:Repository` label filter over unlabelled
// endpoints is worse still, because NornicDB v1.3.3 ignores it and returns
// every DEPENDS_ON edge, including Workload-to-Workload ones.
//
// LIMIT bounds source groups, not edges. flattenGroupedRepositoryDependencyEdges
// reports truncation when either bound is exceeded.
const RepositoryDependencyGroupedEdgeCypher = `
		MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository)
		RETURN s.id AS source_id, collect(t.id) AS target_ids
		ORDER BY source_id
		LIMIT 50001
	`

// flattenGroupedRepositoryDependencyEdges turns grouped rows from
// RepositoryDependencyGroupedEdgeCypher into edges sorted by (source,
// target), clipped to limit. It reports truncated when more than limit
// source groups or more than limit flattened edges came back. Every group
// holds at least one edge, so the first limit groups in source order always
// contain the first limit edges in (source, target) order; the clipped list
// therefore matches the per-edge `ORDER BY source_id, target_id LIMIT limit`
// read. Rows with a blank source, a target list that is not a list, or blank
// or non-string target ids are skipped, as the per-edge parser skips blank
// ids. Parallel edges between the same pair are kept, as the per-edge read
// returns one row per relationship.
func flattenGroupedRepositoryDependencyEdges(rows []map[string]any, limit int) ([]repositoryDependencyEdge, bool) {
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	edges := make([]repositoryDependencyEdge, 0, len(rows))
	for _, row := range rows {
		source := querycontract.StringVal(row, "source_id")
		if source == "" {
			continue
		}
		for _, target := range groupedTargetIDs(row["target_ids"]) {
			if target == "" {
				continue
			}
			edges = append(edges, repositoryDependencyEdge{Source: source, Target: target})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Target < edges[j].Target
	})
	if len(edges) > limit {
		edges = edges[:limit]
		truncated = true
	}
	return edges, truncated
}

// groupedTargetIDs reads a collect(t.id) value. The Bolt driver decodes a
// list as []any; []string is accepted for in-process fakes. Any other type
// yields no targets.
func groupedTargetIDs(value any) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []any:
		ids := make([]string, 0, len(list))
		for _, item := range list {
			if id, ok := item.(string); ok {
				ids = append(ids, id)
			}
		}
		return ids
	default:
		return nil
	}
}

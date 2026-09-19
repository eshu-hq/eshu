// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RepositoryDependencyEdgeCountCypher is the whole-graph DEPENDS_ON cardinality
// probe that gates the repository dependency-edge pre-pass. It is exported so
// the query-plan manifest (QP-REPOSITORY-DEPENDS-ON-EDGE-COUNT) can bind the
// exact production statement and freeze its relationship-type-index plan. The
// bare relationship-type count is answered from the relationship-type index on
// NornicDB (measured 0.09s on a production-scale graph), while the
// Repository-anchored edge scan expands every Repository's full adjacency to
// look for DEPENDS_ON even when none exist (measured 5.3-6.4s at several
// hundred repositories with zero DEPENDS_ON edges; issue #6794). The probe is
// unscoped and therefore only issued for unscoped callers (shared, admin,
// local, and the catalog); scoped callers keep the grant-predicated scan so no
// statement on the repository list path runs without their grant.
const RepositoryDependencyEdgeCountCypher = `MATCH ()-[r:DEPENDS_ON]->() RETURN count(r) AS edge_count`

// probeRepositoryDependencyEdgeCount runs the DEPENDS_ON cardinality probe
// and returns the whole-graph DEPENDS_ON count. known is false when the row
// is missing or its value is not a recognized integer; the caller then treats
// the count as unknown rather than zero, because skipping or under-bounding
// the edge read on an unreadable probe would silently drop real cluster and
// is_dependency evidence. It is a separate symbol so the query-source
// coverage manifest can register the probe as a hot, plan-checked call.
func probeRepositoryDependencyEdgeCount(ctx context.Context, graph querycontract.GraphQuery) (count int64, known bool, err error) {
	rows, err := graph.Run(ctx, RepositoryDependencyEdgeCountCypher, nil)
	if err != nil || len(rows) == 0 {
		return 0, false, err
	}
	count, known = dependencyEdgeCount(rows[0])
	return count, known, nil
}

// dependencyEdgeCount reads the probe row's edge_count. Only a present,
// recognized, non-negative integer is known.
func dependencyEdgeCount(row map[string]any) (int64, bool) {
	var n int64
	switch v := row["edge_count"].(type) {
	case int64:
		n = v
	case int:
		n = int64(v)
	case int32:
		n = int64(v)
	case float64:
		n = int64(v)
	default:
		return 0, false
	}
	return n, n >= 0
}

// RepositoryDependencyGroupedEdgeCypher is the unscoped repository
// dependency-edge read. It returns one row per source repository with the
// ids of every repository it DEPENDS_ON, and loadRepositoryDependencyEdges
// flattens those groups into the same (source, target)-ordered edge list the
// per-edge read (repositoryDependencyClusterEdgeCypher) returns. It is
// exported so the query-plan manifest (QP-REPOSITORY-DEPENDS-ON-GROUPED-EDGES)
// can bind the exact production statement and freeze its plan.
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
// $group_limit bounds source groups, not edges, so it alone does not bound
// how many edges cross the wire: a few sources can hold far more edges than
// the bound. loadUnscopedRepositoryDependencyEdges supplies the bound that
// does. When the probe proves the whole graph has at most
// repositoryDependencyClusterEdgeLimit DEPENDS_ON edges, the Repository
// subset fits too and $group_limit is the fetch limit. Otherwise
// RepositoryDependencyGroupSizeCypher runs first: when its sizes prove more
// edges than the bound, $group_limit is the group prefix
// repositoryDependencyGroupLimit computes, which holds at most the bound plus
// one source's edges (#6786 review R3-F2); when they fit, $group_limit is the
// fetch limit again (#6786 review R4-F1).
const RepositoryDependencyGroupedEdgeCypher = `
		MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository)
		RETURN s.id AS source_id, collect(t.id) AS target_ids
		ORDER BY source_id
		LIMIT $group_limit
	`

// readGroupedRepositoryDependencyEdges runs RepositoryDependencyGroupedEdgeCypher
// for the first groupLimit source groups. It is a separate symbol so the
// query-source coverage manifest can register the grouped read as a hot,
// plan-checked call independent of the scoped per-edge read in
// loadRepositoryDependencyEdges.
func readGroupedRepositoryDependencyEdges(ctx context.Context, graph querycontract.GraphQuery, groupLimit int) ([]map[string]any, error) {
	return graph.Run(ctx, RepositoryDependencyGroupedEdgeCypher, map[string]any{"group_limit": groupLimit})
}

// loadUnscopedRepositoryDependencyEdges is the unscoped branch of
// loadRepositoryDependencyEdges, with the edge bound as a parameter so tests
// can exercise the capped path. It runs the DEPENDS_ON cardinality probe and
// then the grouped read, with the transfer bounded in every case:
//
//   - probe count zero: no read; Skipped.
//   - probe count within limit: the grouped read at the fetch-limit group
//     bound. Repository DEPENDS_ON edges are a subset of all DEPENDS_ON
//     edges, so at most limit edges come back.
//   - probe count over limit, or unknown: RepositoryDependencyGroupSizeCypher
//     first; TransferCapped is set. When the sizes prove more than limit
//     Repository edges, the grouped read is capped at the group prefix that
//     holds the first limit edges and the read is reported truncated. When
//     they fit, the grouped read runs at the same limit+1 group bound as the
//     previous case, never at the group count the size read saw.
//
// The graph can change between the size read and the grouped read (#6786
// review R4-F1). A source that gains its first edge in that window sorts
// somewhere inside the size read's groups, so a grouped read limited to that
// group count would push the last real group off the end and report a
// complete answer without it. With the limit+1 bound, the grouped read either
// returns every group or returns more than limit groups or edges, which
// flattenGroupedRepositoryDependencyEdges reports as truncated. When the
// sizes already prove the bound is exceeded, truncation is reported
// regardless of what the grouped read returns.
//
// A probe error is carried in ProbeErr for telemetry and never degrades the
// response on its own.
func loadUnscopedRepositoryDependencyEdges(ctx context.Context, graph querycontract.GraphQuery, limit int) repositoryDependencyEdgeRead {
	count, known, probeErr := probeRepositoryDependencyEdgeCount(ctx, graph)
	if known && count == 0 {
		return repositoryDependencyEdgeRead{Edges: []repositoryDependencyEdge{}, Skipped: true}
	}
	if known && count <= int64(limit) {
		rows, err := readGroupedRepositoryDependencyEdges(ctx, graph, limit+1)
		if err != nil {
			return repositoryDependencyEdgeRead{Err: err, ProbeErr: probeErr}
		}
		edges, truncated := flattenGroupedRepositoryDependencyEdges(rows, limit)
		return repositoryDependencyEdgeRead{Edges: edges, Truncated: truncated, ProbeErr: probeErr}
	}
	sizes, err := readRepositoryDependencyGroupSizes(ctx, graph)
	if err != nil {
		return repositoryDependencyEdgeRead{Err: err, ProbeErr: probeErr, TransferCapped: true}
	}
	groupLimit, over := repositoryDependencyGroupLimit(sizes, limit)
	if groupLimit == 0 {
		return repositoryDependencyEdgeRead{Edges: []repositoryDependencyEdge{}, ProbeErr: probeErr, TransferCapped: true}
	}
	if !over {
		groupLimit = limit + 1
	}
	rows, err := readGroupedRepositoryDependencyEdges(ctx, graph, groupLimit)
	if err != nil {
		return repositoryDependencyEdgeRead{Err: err, ProbeErr: probeErr, TransferCapped: true}
	}
	edges, truncated := flattenGroupedRepositoryDependencyEdges(rows, limit)
	return repositoryDependencyEdgeRead{Edges: edges, Truncated: truncated || over, ProbeErr: probeErr, TransferCapped: true}
}

// flattenGroupedRepositoryDependencyEdges turns grouped rows from
// RepositoryDependencyGroupedEdgeCypher into edges sorted by (source,
// target), clipped to limit. It reports truncated when more than limit
// source groups or more than limit flattened edges came back. Every MATCHed
// group holds at least one edge, so the first limit groups in source order
// contain the first limit edges in (source, target) order, and the clipped
// list matches the per-edge `ORDER BY source_id, target_id LIMIT limit` read.
//
// That equality needs every group to contribute an id. collect drops nulls,
// so a group whose target ids are all null contributes none; production
// writers always set Repository.id, so this does not happen in practice. If
// it did, the clipped selection could differ from the per-edge read only when
// the read is truncated, and truncation is still reported, because the group
// count check does not depend on ids. Rows with a blank source, a target list
// that is not a list, or blank or non-string target ids are skipped, as the
// per-edge parser skips blank ids. Parallel edges between the same pair are
// kept, as the per-edge read returns one row per relationship.
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

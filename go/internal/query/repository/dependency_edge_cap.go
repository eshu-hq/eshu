// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RepositoryDependencyGroupSizeCypher reads how many Repository DEPENDS_ON
// edges each source repository holds, in source order. The unscoped loader
// runs it only when the DEPENDS_ON cardinality probe cannot prove every edge
// fits in repositoryDependencyClusterEdgeLimit, and uses the sizes to cap the
// grouped read (see repositoryDependencyGroupLimit). It is exported so the
// query-plan manifest (QP-REPOSITORY-DEPENDS-ON-GROUP-SIZES) can bind the
// exact production statement.
//
// It is the same fast-path shape as RepositoryDependencyGroupedEdgeCypher with
// a count(end) aggregate in place of collect(end.prop): NornicDB v1.3.3's
// tryFastSingleHopAgg answers it from the DEPENDS_ON relationship-type index
// and checks the target label with one HasLabelBatch call, so it returns one
// integer per source repository without expanding any adjacency. Keep it
// WHERE-free for the same reason as the grouped read.
//
// LIMIT bounds source groups at the fetch limit. Every MATCHed group holds at
// least one edge, so more than repositoryDependencyClusterEdgeLimit groups
// already means more edges than the bound.
const RepositoryDependencyGroupSizeCypher = `
		MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository)
		RETURN s.id AS source_id, count(t) AS target_count
		ORDER BY source_id
		LIMIT 50001
	`

// readRepositoryDependencyGroupSizes runs RepositoryDependencyGroupSizeCypher.
// It is a separate symbol so the query-source coverage manifest can register
// the group-size read as a hot, plan-checked call.
func readRepositoryDependencyGroupSizes(ctx context.Context, graph querycontract.GraphQuery) ([]map[string]any, error) {
	return graph.Run(ctx, RepositoryDependencyGroupSizeCypher, nil)
}

// repositoryDependencyGroupLimit returns how many source groups the grouped
// read must fetch to hold the first limit edges in (source, target) order:
// the smallest prefix of rows whose target_count sum exceeds limit, or every
// row when the sum never does. over reports that the sizes prove more than
// limit edges exist, either because the sum passed limit or because more than
// limit groups came back; the caller then discloses the read as truncated.
// The caller uses groups only when over is set. When the sizes fit, it asks
// the grouped read for the fetch limit instead, because a source that gains
// its first edge after this read would otherwise push a counted group out of
// a limit of exactly len(rows) (#6786 review R4-F1).
//
// Because the groups arrive in source order and the edges beyond the prefix
// all have larger sources, the prefix holds every edge the (source, target)
// ordered bound keeps. The grouped read then transfers at most limit edges
// plus the prefix's last group, instead of every Repository DEPENDS_ON edge.
//
// A missing or unreadable target_count counts as one edge, the fewest a
// MATCHed group can hold. That can only lengthen the prefix, so it never cuts
// off an edge inside the bound.
func repositoryDependencyGroupLimit(rows []map[string]any, limit int) (groups int, over bool) {
	var total int64
	for i, row := range rows {
		total += groupTargetCount(row["target_count"])
		if total > int64(limit) {
			return i + 1, true
		}
	}
	return len(rows), len(rows) > limit
}

// groupTargetCount reads a count(t) value as decoded by the Bolt driver
// (int64) or supplied by in-process fakes (int, float64). Anything else,
// including a count below one, reads as one edge.
func groupTargetCount(value any) int64 {
	var n int64
	switch v := value.(type) {
	case int64:
		n = v
	case int:
		n = int64(v)
	case float64:
		n = int64(v)
	}
	return max(n, 1)
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

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

// probeRepositoryDependencyEdgesAbsent runs the DEPENDS_ON cardinality probe
// and reports whether it proves the graph holds no DEPENDS_ON edges. It is a
// separate symbol so the query-source coverage manifest can register the probe
// as a hot, plan-checked call independent of the edge scan.
func probeRepositoryDependencyEdgesAbsent(ctx context.Context, graph querycontract.GraphQuery) (bool, error) {
	rows, err := graph.Run(ctx, RepositoryDependencyEdgeCountCypher, nil)
	if err != nil {
		return false, err
	}
	return len(rows) > 0 && dependencyEdgeCountIsZero(rows[0]), nil
}

// dependencyEdgeCountIsZero reports whether the probe row proves the graph has
// no DEPENDS_ON edges. Only a present, recognized integer zero counts: a
// missing column or an unrecognized value type returns false so the edge scan
// still runs, because skipping on an unreadable probe would silently drop real
// cluster and is_dependency evidence.
func dependencyEdgeCountIsZero(row map[string]any) bool {
	switch n := row["edge_count"].(type) {
	case int64:
		return n == 0
	case int:
		return n == 0
	case int32:
		return n == 0
	case float64:
		return n == 0
	default:
		return false
	}
}

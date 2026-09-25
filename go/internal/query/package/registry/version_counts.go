// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// VersionCountsByPackageID resolves HAS_VERSION counts for one explicit page
// of package uids through packageRegistryVersionCountsCypher, the MATCH-only
// UNWIND statement that avoids the pinned NornicDB's OPTIONAL MATCH +
// count(v) zero-group collapse (see docs/public/reference/nornicdb-pitfalls.md).
// A package uid absent from the returned map has zero versions; callers
// zero-fill by indexing the map, which yields 0 for a missing key. An empty
// page skips the round trip and returns an empty map.
//
// It is exported so the code-bundles read (codequery, a sibling family that
// serves the same :Package catalog) reuses this statement and its zero-fill
// contract instead of copying them; both callers therefore version-count a
// page identically. The bound is the caller's page: at most the handler's
// limit-clamped uid list, never a catalog scan.
func VersionCountsByPackageID(
	ctx context.Context,
	graph querycontract.GraphQuery,
	packageIDs []string,
) (map[string]int, error) {
	counts := make(map[string]int, len(packageIDs))
	if len(packageIDs) == 0 {
		return counts, nil
	}
	cypher, params := packageRegistryVersionCountsCypher(packageIDs)
	rows, err := graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[querycontract.StringVal(row, "package_id")] = querycontract.IntVal(row, "version_count")
	}
	return counts, nil
}

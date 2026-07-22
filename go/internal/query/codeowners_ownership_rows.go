// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
)

// loadCodeownersOwnershipRows executes the one initial-page query or the three
// disjoint cursor queries, then restores their shared global keyset order. Each
// query is bounded by fetchLimit, and the merged slice is capped to that same
// limit before the handler applies its limit+1 truncation probe.
func loadCodeownersOwnershipRows(
	ctx context.Context,
	graph GraphQuery,
	repoID string,
	afterOrderIndex int,
	afterPattern string,
	afterRef string,
	fetchLimit int,
) ([]map[string]any, error) {
	queries := codeownersOwnershipCyphers(
		repoID,
		afterOrderIndex,
		afterPattern,
		afterRef,
		fetchLimit,
	)
	rows := make([]map[string]any, 0, fetchLimit)
	for i, query := range queries {
		branchRows, err := graph.Run(ctx, query.cypher, query.params)
		if err != nil {
			return nil, fmt.Errorf("codeowners ownership graph query %d: %w", i+1, err)
		}
		rows = append(rows, branchRows...)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		leftOrder, rightOrder := IntVal(rows[i], "order_index"), IntVal(rows[j], "order_index")
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		leftPattern, rightPattern := StringVal(rows[i], "pattern"), StringVal(rows[j], "pattern")
		if leftPattern != rightPattern {
			return leftPattern < rightPattern
		}
		return StringVal(rows[i], "owner_ref") < StringVal(rows[j], "owner_ref")
	})
	if len(rows) > fetchLimit {
		rows = rows[:fetchLimit]
	}
	return rows, nil
}

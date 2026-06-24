// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "sort"

// relationshipStoryRankBasis names the ordering applied to relationship story
// rows in the response coverage. Centrality is measured within the resolved
// bounded result set, not over the whole graph.
const relationshipStoryRankBasis = "bounded_centrality"

// relationshipStoryRankByCentrality stamps each row with a bounded centrality
// score and stably reorders the rows by that score, descending. Centrality is
// the neighbor's degree within the resolved bounded result set: the number of
// rows that reference the same neighbor entity (counting both directions and all
// requested relationship types). This makes the most-connected neighbors survive
// a small limit or token_budget.
//
// It is intentionally a bounded, in-process measure over the already-fetched
// rows (n <= limit+1 per direction/type), so it introduces no graph query and no
// performance regression. Ties keep the input order, which the bounded query
// already produced deterministically (name then id), so the overall order stays
// deterministic. Rows are mutated in place; callers own them and clone
// downstream.
func relationshipStoryRankByCentrality(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	degree := make(map[string]int, len(rows))
	for _, row := range rows {
		if id := relationshipStoryNeighborID(row); id != "" {
			degree[id]++
		}
	}
	for _, row := range rows {
		row["centrality"] = degree[relationshipStoryNeighborID(row)]
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return relationshipStoryRowCentrality(rows[i]) > relationshipStoryRowCentrality(rows[j])
	})
	return rows
}

// relationshipStoryNeighborID returns the id of the entity on the far side of a
// relationship row: the source for an incoming edge, the target otherwise.
func relationshipStoryNeighborID(row map[string]any) string {
	if StringVal(row, "direction") == "incoming" {
		return StringVal(row, "source_id")
	}
	return StringVal(row, "target_id")
}

// relationshipStoryRowCentrality reads the centrality stamped by
// relationshipStoryRankByCentrality, treating a missing value as zero.
func relationshipStoryRowCentrality(row map[string]any) int {
	switch value := row["centrality"].(type) {
	case int:
		return value
	default:
		return 0
	}
}

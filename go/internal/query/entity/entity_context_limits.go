// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// entityContextResultLimits builds the shared result_limits drilldown block
// for an entity context payload. It caps the relationships fan-out in
// place, reports deterministic ordering, and names the next prompt tool
// plus the self path so callers can drill down without falling back to raw
// Cypher. It moved with the entity family for #6060 (lane B B5): the only
// production callers are the entity context routes.
func entityContextResultLimits(response map[string]any, entityID string) map[string]any {
	relationships := querycontract.MapSliceValue(response, "relationships")
	total := len(relationships)
	if total > querycontract.ContextStoryItemLimit {
		sort.SliceStable(relationships, func(i, j int) bool {
			return relationshipRowLess(relationships[i], relationships[j])
		})
	}
	capped, capTruncated := querycontract.CapMapRows(relationships, querycontract.ContextStoryItemLimit)
	relationshipsComplete, completenessKnown := response["relationships_complete"].(bool)
	truncated := capTruncated || (completenessKnown && !relationshipsComplete)
	if total > 0 {
		response["relationships"] = capped
	}
	return map[string]any{
		"limit":              querycontract.ContextStoryItemLimit,
		"ordering":           "deterministic",
		"relationship_count": total,
		"truncated":          truncated,
		"drilldown_basis":    "target_id",
		"drilldown_tool":     "get_relationship_evidence",
		"context_path":       "/api/v0/entities/" + entityID + "/context",
	}
}

func relationshipRowLess(left, right map[string]any) bool {
	for _, key := range []string{"type", "source_id", "source_name", "target_id", "target_name", "reason"} {
		leftValue := querycontract.StringVal(left, key)
		rightValue := querycontract.StringVal(right, key)
		if leftValue != rightValue {
			return leftValue < rightValue
		}
	}
	return false
}

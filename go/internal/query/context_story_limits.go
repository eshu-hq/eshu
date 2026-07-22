// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "sort"

// contextStoryItemLimit bounds relationship and instance fan-out attached to
// entity and workload context/story payloads so a single prompt-ready read
// stays within the route budget and exposes truncation explicitly.
const contextStoryItemLimit = 50

// entityContextResultLimits builds the shared result_limits drilldown block for
// an entity context payload. It caps the relationships fan-out in place,
// reports deterministic ordering, and names the next prompt tool plus the
// self path so callers can drill down without falling back to raw Cypher.
func entityContextResultLimits(response map[string]any, entityID string) map[string]any {
	relationships := mapSliceValue(response, "relationships")
	total := len(relationships)
	if total > contextStoryItemLimit {
		sort.SliceStable(relationships, func(i, j int) bool {
			return relationshipRowLess(relationships[i], relationships[j])
		})
	}
	capped, capTruncated := capMapRows(relationships, contextStoryItemLimit)
	relationshipsComplete, completenessKnown := response["relationships_complete"].(bool)
	truncated := capTruncated || (completenessKnown && !relationshipsComplete)
	if total > 0 {
		response["relationships"] = capped
	}
	return map[string]any{
		"limit":              contextStoryItemLimit,
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
		leftValue := StringVal(left, key)
		rightValue := StringVal(right, key)
		if leftValue != rightValue {
			return leftValue < rightValue
		}
	}
	return false
}

// workloadContextResultLimits builds the shared result_limits drilldown block
// for a workload context or story payload. The surface argument selects whether
// the drilldown handle points callers at the workload story or back at the
// workload context route.
func workloadContextResultLimits(ctx map[string]any, workloadID, surface string) map[string]any {
	instances := mapSliceValue(ctx, "instances")
	dependents := mapSliceValue(ctx, "dependents")
	consumers := mapSliceValue(ctx, "consumer_repositories")
	instanceTotal := len(instances)
	dependentTotal := len(dependents)
	consumerTotal := len(consumers)

	// Cap each fan-out slice in place so the payload honors the stated limit;
	// truncated then reflects an actual cut rather than claiming truncation
	// while still returning every row.
	cappedInstances, instTrunc := capMapRows(instances, contextStoryItemLimit)
	cappedDependents, depTrunc := capMapRows(dependents, contextStoryItemLimit)
	cappedConsumers, conTrunc := capMapRows(consumers, contextStoryItemLimit)
	if instanceTotal > 0 {
		ctx["instances"] = cappedInstances
	}
	if dependentTotal > 0 {
		ctx["dependents"] = cappedDependents
	}
	if consumerTotal > 0 {
		ctx["consumer_repositories"] = cappedConsumers
	}
	truncated := instTrunc || depTrunc || conTrunc
	drilldownTool := "get_workload_story"
	if surface == "story" {
		drilldownTool = "get_workload_context"
	}
	return map[string]any{
		"limit":             contextStoryItemLimit,
		"ordering":          "deterministic",
		"instance_count":    instanceTotal,
		"dependent_count":   dependentTotal,
		"consumer_count":    consumerTotal,
		"truncated":         truncated,
		"drilldown_basis":   "resolved_id",
		"relationship_tool": "get_relationship_evidence",
		"drilldown_tool":    drilldownTool,
		"context_path":      "/api/v0/workloads/" + workloadID + "/context",
	}
}

// contextPartialReasons promotes the context payload's limitations into an
// explicit partial_reasons array. Callers see missing or unsupported evidence
// directly instead of inferring completeness from an absent field. The result
// is always non-nil so the envelope shape is stable across complete and
// partial reads.
func contextPartialReasons(ctx map[string]any, extra ...string) []string {
	seen := map[string]struct{}{}
	reasons := make([]string, 0)
	for _, reason := range append(StringSliceVal(ctx, "limitations"), extra...) {
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons
}

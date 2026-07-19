// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func directPlatformTopologyEdge(row map[string]any) map[string]any {
	return platformTopologyEdge(
		"RUNS_ON",
		StringVal(row, "instance_id"),
		"",
		StringVal(row, "platform_id"),
		StringVal(row, "platform_name"),
		platformEdgeConfidence(row),
		platformEdgeReason(row),
		mapValue(row, "platform_edge"),
	)
}

func provisionedPlatformTopologyEdges(row map[string]any) []map[string]any {
	return []map[string]any{
		platformTopologyEdge(
			"PROVISIONS_DEPENDENCY_FOR",
			StringVal(row, "platform_source_id"),
			StringVal(row, "platform_source_name"),
			StringVal(row, "platform_dependency_target_id"),
			"",
			floatVal(row, "dependency_confidence"),
			StringVal(row, "dependency_reason"),
			mapValue(row, "dependency_edge"),
		),
		platformTopologyEdge(
			"PROVISIONS_PLATFORM",
			StringVal(row, "platform_source_id"),
			StringVal(row, "platform_source_name"),
			StringVal(row, "platform_id"),
			StringVal(row, "platform_name"),
			floatVal(row, "platform_edge_confidence"),
			StringVal(row, "platform_edge_reason"),
			mapValue(row, "platform_edge"),
		),
	}
}

func platformTopologyEdge(
	relationshipType string,
	sourceID string,
	sourceName string,
	targetID string,
	targetName string,
	confidence float64,
	reason string,
	properties map[string]any,
) map[string]any {
	edge := map[string]any{
		"relationship_type": relationshipType,
		"source_id":         sourceID,
		"target_id":         targetID,
		"confidence":        firstPositiveFloat(confidence, floatVal(properties, "confidence")),
		"reason":            firstNonEmptyString(reason, StringVal(properties, "reason")),
	}
	if len(properties) > 0 {
		edge["properties"] = copyStringAnyMap(properties)
	}
	if sourceName != "" {
		edge["source_name"] = sourceName
	}
	if targetName != "" {
		edge["target_name"] = targetName
	}
	if evidenceSource := StringVal(properties, "evidence_source"); evidenceSource != "" {
		edge["evidence_source"] = evidenceSource
	}
	if sourceTool := StringVal(properties, "source_tool"); sourceTool != "" {
		edge["source_tool"] = sourceTool
	}
	return edge
}

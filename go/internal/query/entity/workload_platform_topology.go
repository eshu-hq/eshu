// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func directPlatformTopologyEdge(row map[string]any) map[string]any {
	return platformTopologyEdge(
		"RUNS_ON",
		querycontract.StringVal(row, "instance_id"),
		"",
		querycontract.StringVal(row, "platform_id"),
		querycontract.StringVal(row, "platform_name"),
		platformEdgeConfidence(row),
		platformEdgeReason(row),
		querycontract.MapValue(row, "platform_edge"),
	)
}

// ProvisionedPlatformTopologyEdges shapes a provisioned-platform row into
// the PROVISIONS_DEPENDENCY_FOR/PROVISIONS_PLATFORM topology edges the
// workload platform read returns. Exported for the staying OpenAPI
// deployment-identity test that pins the edge shape; see #6060.
func ProvisionedPlatformTopologyEdges(row map[string]any) []map[string]any {
	return []map[string]any{
		platformTopologyEdge(
			"PROVISIONS_DEPENDENCY_FOR",
			querycontract.StringVal(row, "platform_source_id"),
			querycontract.StringVal(row, "platform_source_name"),
			querycontract.StringVal(row, "platform_dependency_target_id"),
			"",
			querycontract.FloatVal(row, "dependency_confidence"),
			querycontract.StringVal(row, "dependency_reason"),
			querycontract.MapValue(row, "dependency_edge"),
		),
		platformTopologyEdge(
			"PROVISIONS_PLATFORM",
			querycontract.StringVal(row, "platform_source_id"),
			querycontract.StringVal(row, "platform_source_name"),
			querycontract.StringVal(row, "platform_id"),
			querycontract.StringVal(row, "platform_name"),
			querycontract.FloatVal(row, "platform_edge_confidence"),
			querycontract.StringVal(row, "platform_edge_reason"),
			querycontract.MapValue(row, "platform_edge"),
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
		"confidence":        querycontract.FirstPositiveFloat(confidence, querycontract.FloatVal(properties, "confidence")),
		"reason":            querycontract.FirstNonEmptyString(reason, querycontract.StringVal(properties, "reason")),
		"properties":        copyStringAnyMap(properties),
	}
	if sourceName != "" {
		edge["source_name"] = sourceName
	}
	if targetName != "" {
		edge["target_name"] = targetName
	}
	if evidenceSource := querycontract.StringVal(properties, "evidence_source"); evidenceSource != "" {
		edge["evidence_source"] = evidenceSource
	}
	if sourceTool := querycontract.StringVal(properties, "source_tool"); sourceTool != "" {
		edge["source_tool"] = sourceTool
	}
	return edge
}

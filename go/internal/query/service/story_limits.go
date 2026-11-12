// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func boundedServiceStoryRawValue(key string, value any, downstreamRenderLimit int) (any, map[string]any) {
	switch typed := value.(type) {
	case []map[string]any:
		limit := serviceStoryItemLimit
		if key == "dependents" || key == "consumer_repositories" {
			limit = downstreamRenderLimit
		}
		capped, truncated := CapMapRows(typed, limit)
		return capped, serviceStoryRawLimit(len(typed), truncated, limit)
	case map[string]any:
		return boundedServiceStoryRawMap(typed)
	default:
		return value, nil
	}
}

func boundedServiceStoryRawMap(input map[string]any) (map[string]any, map[string]any) {
	out := copyMap(input)
	limits := map[string]any{}
	for _, key := range []string{"artifacts", "delivery_paths", "delivery_workflows", "shared_config_paths"} {
		rows := querycontract.MapSliceValue(input, key)
		if len(rows) == 0 {
			continue
		}
		capped, truncated := CapMapRows(rows, serviceStoryItemLimit)
		out[key] = capped
		limits[key] = serviceStoryRawLimit(len(rows), truncated, serviceStoryItemLimit)
	}
	if len(limits) > 0 {
		out["raw_limits"] = limits
	}
	return out, limits
}

func serviceStoryRawLimit(count int, truncated bool, limit int) map[string]any {
	return map[string]any{
		"count":     count,
		"limit":     limit,
		"truncated": truncated,
	}
}

func serviceRelationshipKey(row map[string]any) string {
	if resolvedID := querycontract.StringVal(row, "resolved_id"); resolvedID != "" {
		return "resolved:" + resolvedID
	}
	return fmt.Sprintf(
		"%s|%s|%s|%s",
		querycontract.StringVal(row, "relationship_type"),
		querycontract.StringVal(row, "source_repo_id"),
		querycontract.StringVal(row, "target_repo_id"),
		querycontract.StringVal(row, "target_id"),
	)
}

func mergeServiceRelationshipRow(existing map[string]any, incoming map[string]any) {
	if confidence := querycontract.FloatVal(incoming, "confidence"); confidence > querycontract.FloatVal(existing, "confidence") {
		existing["confidence"] = confidence
	}
	if evidenceCount := querycontract.FirstPositiveInt(incoming, "evidence_count"); evidenceCount > querycontract.FirstPositiveInt(existing, "evidence_count") {
		existing["evidence_count"] = evidenceCount
	}
	if querycontract.StringVal(existing, "rationale") == "" && querycontract.StringVal(incoming, "rationale") != "" {
		existing["rationale"] = querycontract.StringVal(incoming, "rationale")
	}
}

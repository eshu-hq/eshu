// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// extractRelationships converts the Neo4j relationships collection to typed structs.
func extractRelationships(row map[string]any) []map[string]any {
	return extractCollection(row, "relationships", func(m map[string]any) (map[string]any, bool) {
		if relType := querycontract.StringVal(m, "type"); relType != "" {
			return map[string]any{
				"type":        relType,
				"target_name": querycontract.StringVal(m, "target_name"),
				"target_id":   querycontract.StringVal(m, "target_id"),
			}, true
		}
		return nil, false
	})
}

// extractInstances converts the Neo4j instances collection to typed structs.
func extractInstances(row map[string]any) []map[string]any {
	return extractCollection(row, "instances", func(m map[string]any) (map[string]any, bool) {
		if instID := querycontract.StringVal(m, "instance_id"); instID != "" {
			instance := map[string]any{
				"instance_id":                instID,
				"platform_name":              querycontract.StringVal(m, "platform_name"),
				"platform_kind":              querycontract.StringVal(m, "platform_kind"),
				"environment":                querycontract.StringVal(m, "environment"),
				"materialization_confidence": querycontract.FloatVal(m, "materialization_confidence"),
				"materialization_provenance": querycontract.StringSliceVal(m, "materialization_provenance"),
				"platform_confidence":        querycontract.FloatVal(m, "platform_confidence"),
				"platform_reason":            querycontract.StringVal(m, "platform_reason"),
			}
			if platforms := querycontract.MapSliceValue(m, "platforms"); len(platforms) > 0 {
				instance["platforms"] = platforms
			} else if platformName := querycontract.StringVal(m, "platform_name"); platformName != "" {
				instance["platforms"] = []map[string]any{{
					"platform_name":       platformName,
					"platform_kind":       querycontract.StringVal(m, "platform_kind"),
					"platform_confidence": querycontract.FloatVal(m, "platform_confidence"),
					"platform_reason":     querycontract.StringVal(m, "platform_reason"),
				}}
			}
			return instance, true
		}
		return nil, false
	})
}

// extractCollection converts list-valued graph columns into typed API maps.
func extractCollection(row map[string]any, key string, transform func(map[string]any) (map[string]any, bool)) []map[string]any {
	raw, ok := row[key]
	if !ok || raw == nil {
		return []map[string]any{}
	}
	items, ok := raw.([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if transformed, valid := transform(m); valid {
				result = append(result, transformed)
			}
		}
	}
	return result
}

// buildWorkloadStory creates a narrative summary of a workload's deployment.
// The implementation moved to querycontract for #6060; this wrapper keeps
// root callers unchanged.
func buildWorkloadStory(ctx map[string]any) string {
	return querycontract.BuildWorkloadStory(ctx)
}

// safeStr extracts a string from a map while filtering empty and nil values.
// The implementation moved to querycontract for #6060; this wrapper keeps
// root callers unchanged.
func safeStr(m map[string]any, key string) string {
	return querycontract.SafeStr(m, key)
}

// platformTargets returns an instance's platform targets. The implementation
// moved to querycontract for #6060; this wrapper keeps root callers unchanged.
func platformTargets(instance map[string]any) []map[string]any {
	return querycontract.PlatformTargets(instance)
}

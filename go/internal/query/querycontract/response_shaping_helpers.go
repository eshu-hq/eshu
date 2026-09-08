// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "strings"

// AppendReason appends reason to reasons, trimmed, deduplicated, and skipped
// entirely when blank. The implementation moved from root's answer_packet.go
// for #6060 so a handler-family subpackage can build a reason list without
// importing root.
func AppendReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

// FilterNullRelationships removes entries where "type" is nil, which OPTIONAL
// MATCH leaves behind when a relationship pattern found no match. It accepts
// both a decoded []map[string]any and the []any shape a Cypher list
// projection returns. The implementation moved from root's
// infra_relationship_filter.go for #6060 so a handler-family subpackage can
// filter a relationship list without importing root.
func FilterNullRelationships(v any) []map[string]any {
	switch slice := v.(type) {
	case []map[string]any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if item["type"] == nil {
				continue
			}
			result = append(result, item)
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Skip entries where type is nil (no relationship matched)
			if m["type"] == nil {
				continue
			}
			result = append(result, m)
		}
		return result
	default:
		return nil
	}
}

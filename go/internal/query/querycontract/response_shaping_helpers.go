// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/rowvalue"
)

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

// StringVal, BoolVal, IntVal, StringSliceVal and FloatVal forward to the
// rowvalue subpackage. The implementations moved there for #6597: this
// directory carries a //nolint:dirgate past the 40-file cap, and these helpers
// were the one file in it with no dependency on anything else here -- verified
// against all 382 exported and unexported symbols declared in the rest of the
// package, none of which rowvalue names. Moving them first makes the later
// family extractions legal, because a subpackage can import rowvalue without
// reaching back through this package.
//
// These wrappers keep the original names. Every existing caller compiles
// unchanged, which matters at this scale: StringVal alone has 235 qualified
// call sites, and the five helpers together are named in 285 files. Package
// query's own forwarders in neo4j.go continue to work through these.

// StringVal safely extracts a string from a map value.
func StringVal(row map[string]any, key string) string {
	return rowvalue.StringVal(row, key)
}

// BoolVal safely extracts a bool from a map value.
func BoolVal(row map[string]any, key string) bool {
	return rowvalue.BoolVal(row, key)
}

// IntVal safely extracts an int from a map value.
func IntVal(row map[string]any, key string) int {
	return rowvalue.IntVal(row, key)
}

// StringSliceVal safely extracts a []string from a map value.
func StringSliceVal(row map[string]any, key string) []string {
	return rowvalue.StringSliceVal(row, key)
}

// FloatVal reads key from row as a float64, coercing the numeric types a graph
// or SQL driver may hand back.
func FloatVal(row map[string]any, key string) float64 {
	return rowvalue.FloatVal(row, key)
}

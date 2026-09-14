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
// were the one file in it that named no identifier declared anywhere else in
// the package. The compiler is the proof of that, not a symbol census:
// rowvalue compiles as a leaf whose only import is "fmt". Moving them first
// makes the later family extractions legal, because a subpackage can import
// rowvalue without reaching back through this package.
//
// These wrappers keep the original names. Every existing caller compiles
// unchanged, which matters at this scale: at the base 514534567, over
// go/**/*.go, a querycontract-qualified StringVal call appears 2057 times in
// 235 files, and the five names together 2705 times in 272 files -- 235 and
// 272 are file counts, not call counts. The git grep patterns are in
// rowvalue/AGENTS.md; a literal copy here would count itself. Package
// query's own forwarders in neo4j.go cover four of the five and continue to
// work through these; it has no exported FloatVal and reaches this one through
// two unexported wrappers instead, floatVal in compare.go and
// relationshipFloatVal in repository_compat.go.
//
// Each wrapper below is a pass-through with no behavior of its own, so the
// contract -- including the edge cases -- is documented on the rowvalue
// function it calls rather than restated here.

// StringVal forwards to [rowvalue.StringVal]. See that function for the
// contract, including why a present non-string is rendered with %v rather than
// discarded.
func StringVal(row map[string]any, key string) string {
	return rowvalue.StringVal(row, key)
}

// BoolVal forwards to [rowvalue.BoolVal]. See that function for the contract.
func BoolVal(row map[string]any, key string) bool {
	return rowvalue.BoolVal(row, key)
}

// IntVal forwards to [rowvalue.IntVal]. See that function for the contract,
// including the numeric shapes it accepts.
func IntVal(row map[string]any, key string) int {
	return rowvalue.IntVal(row, key)
}

// StringSliceVal forwards to [rowvalue.StringSliceVal]. See that function for
// the contract, including how it treats a non-string element of a list column.
func StringSliceVal(row map[string]any, key string) []string {
	return rowvalue.StringSliceVal(row, key)
}

// FloatVal forwards to [rowvalue.FloatVal]. See that function for the contract,
// including the numeric shapes it coerces.
func FloatVal(row map[string]any, key string) float64 {
	return rowvalue.FloatVal(row, key)
}

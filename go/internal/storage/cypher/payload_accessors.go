// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
)

// GraphWriterShapeVersion is the current graph-writer shape version. Bump
// it in the same change as any fix that changes what the UNWIND writers
// persist, so a deployment running an older graph retires its generations
// and reprojects with the fixed writers on the next reducer startup (see
// recovery.EnsureGraphWriterShape, issue #6868). Ordinary operation never
// reopens completed work, so without the retirement persisted stale output
// would keep serving indefinitely.
//
//   - v1: slice-1 UNWIND row-key writers always send every row.<key> the
//     statement reads (nil when absent) instead of omitting absent keys,
//     which NornicDB stored as the literal token text (#6782).
const GraphWriterShapeVersion = 1

// PayloadString reads a string-typed payload field, returning "" when the field
// is absent or not a string.
func PayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// PayloadBool reads a bool-typed payload field, returning false when the field
// is absent or not a bool.
func PayloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// PayloadInt accepts numeric shapes produced by Go maps, JSON decoding, and
// database drivers.
func PayloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

// PayloadFloat accepts numeric shapes produced by Go maps, JSON decoding, and
// database drivers.
func PayloadFloat(payload map[string]any, key string) float64 {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

// PayloadMapSlice normalizes graph-story evidence summaries after JSON
// decoding or direct Go construction in reducer tests.
func PayloadMapSlice(payload map[string]any, key string) []map[string]any {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []map[string]any:
		return value
	case []any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

// PayloadStringSlice normalizes evidence-kind arrays before passing them to
// graph drivers.
func PayloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" || text == "<nil>" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

// SetOptionalRowString copies an optional string payload field into an
// UNWIND row map, always writing the key: the value when non-empty, else an
// explicit nil. A statement that SETs `rel.x = row.x` must receive the key
// even when it has no value. Neo4j reads a missing key as null, but the
// pinned NornicDB v1.3.3 stores the literal expression text ("row.x") for a
// missing key, which is how package-consumption DEPENDS_ON edges gained the
// bogus source_tool "row.source_tool" (#6782). An explicit nil leaves the
// property absent on Neo4j and null-valued on NornicDB, so `x IS NOT NULL`
// reads agree. The leaf writer packages call this exported form; the
// unexported alias below keeps existing in-package callers unchanged.
func SetOptionalRowString(rowMap map[string]any, payload map[string]any, key string) {
	if value := PayloadString(payload, key); value != "" {
		rowMap[key] = value
		return
	}
	rowMap[key] = nil
}

// setOptionalRowString is the in-package alias of SetOptionalRowString.
func setOptionalRowString(rowMap map[string]any, payload map[string]any, key string) {
	SetOptionalRowString(rowMap, payload, key)
}

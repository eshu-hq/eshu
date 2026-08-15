// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import "strings"

// The four readers below are a copy of four of the five envelope value readers
// that go/cmd/eshu declares as traceMap / traceSlice / traceString / traceInt,
// and that go/internal/cli/change and go/internal/cli/freshness each copy again
// as mapValue / sliceValue / stringValue / intValue. cmd/eshu is package main,
// so nothing can import the originals and every family that renders an envelope
// takes its own set. Change one and you must change the rest:
// TestEnvelopeReaderParity in go/cmd/eshu compares all four copies at the source
// level and names the one that drifted.
//
// The fifth reader of that set -- traceBool / boolValue -- is deliberately NOT
// copied here. This family reads no boolean out of an envelope, and the `unused`
// linter rejects a reader carried for symmetry alone. The parity test records
// the absence explicitly and fails if a bool reader ever appears here without
// being registered, so the gap is pinned rather than merely unnoticed.

// mapValue returns parent[key] as a nested object, or nil when the key is
// missing or holds another type.
func mapValue(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	if typed, ok := parent[key].(map[string]any); ok {
		return typed
	}
	return nil
}

// sliceValue returns parent[key] as a list. It accepts []map[string]any as well
// as []any because a caller building an envelope in Go writes the former, while
// a decoded API response always yields the latter.
func sliceValue(parent map[string]any, key string) []any {
	if parent == nil {
		return nil
	}
	switch typed := parent[key].(type) {
	case []any:
		return typed
	case []map[string]any:
		rows := make([]any, 0, len(typed))
		for _, row := range typed {
			rows = append(rows, row)
		}
		return rows
	default:
		return nil
	}
}

// stringValue returns parent[key] as a trimmed string, or "" when the key is
// missing or holds another type.
func stringValue(parent map[string]any, key string) string {
	if parent == nil {
		return ""
	}
	if value, ok := parent[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// intValue returns parent[key] as an int. It accepts float64 because that is
// what encoding/json decodes a JSON number into.
func intValue(parent map[string]any, key string) int {
	if parent == nil {
		return 0
	}
	switch value := parent[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

// stringsValue returns value as a list of non-empty trimmed strings. Entries
// that are not strings, and strings that are blank once trimmed, are dropped
// rather than rendered as empty bullets.
//
// It mirrors traceStrings in go/cmd/eshu, which keeps its own copy for the map
// and component_api families. It is not part of the five-reader set
// TestEnvelopeReaderParity pins.
func stringsValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
		return values
	default:
		return nil
	}
}

// firstString returns the first value that is non-empty once trimmed, or "" when
// every candidate is blank. Callers use it to fall back across the several key
// spellings the API has used for one field.
//
// It mirrors traceFirstString in go/cmd/eshu, which the map family still calls.
func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

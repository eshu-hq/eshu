// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import "strings"

// The readers below are local copies of the trace* helpers in
// go/cmd/eshu/trace.go. They are copied rather than shared because that file
// is package main and unimportable, and because every original still has
// callers in commands that have not been extracted. When a shared
// internal/cli helper package for them lands, these become its first
// candidates for deletion.

// mapField returns parent[key] when it is a JSON object, and nil otherwise.
// A missing, null, or wrong-typed member reads as an empty section rather
// than an error: the renderers degrade to printing less, never to failing.
func mapField(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	if typed, ok := parent[key].(map[string]any); ok {
		return typed
	}
	return nil
}

// sliceField returns parent[key] as a slice of JSON values. It accepts a
// []map[string]any as well as the []any that encoding/json produces, so a
// caller that built the envelope in Go rather than decoding it renders the
// same way.
func sliceField(parent map[string]any, key string) []any {
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

// stringField returns parent[key] trimmed of surrounding whitespace, or the
// empty string when the member is missing or is not a string.
func stringField(parent map[string]any, key string) string {
	if parent == nil {
		return ""
	}
	if value, ok := parent[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// intField returns parent[key] as an int, accepting the float64 that
// encoding/json decodes a JSON number into. Anything else reads as 0.
func intField(parent map[string]any, key string) int {
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

// stringList returns value as a list of trimmed, non-empty strings. Entries
// that are not strings, and entries that trim to nothing, are dropped.
func stringList(value any) []string {
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

// firstNonEmpty returns the first argument that is not blank after trimming,
// or the empty string when every argument is blank.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

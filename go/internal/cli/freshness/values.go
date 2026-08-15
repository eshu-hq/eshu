// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import "strings"

// The freshness summaries read a decoded JSON envelope, so every field arrives
// as an any behind a map key that may not exist. These five readers each take
// the parent map and a key and return the zero value when the key is missing
// or holds a different type, which is what lets a renderer print a partial row
// instead of panicking on a server-side shape change.
//
// They are private copies of go/cmd/eshu's traceMap / traceSlice / traceString
// / traceInt / traceBool, and they are not the only copies: a third set with
// these same five names lives in go/internal/cli/change/envelope.go, and the
// entitymap family keeps a fourth, differently named set (without the bool
// reader) in go/internal/cli/entitymap/values.go. The originals stay in
// go/cmd/eshu and keep their other callers, because that is package main and
// a copy is the only way to share anything out of it. Copying is deliberate
// rather than a shared helper package: the reading shape is four lines and
// the coupling would not be.
//
// An edit to any one set belongs in every set that has the reader you touched.
// TestEnvelopeReaderParity in go/cmd/eshu compares the three five-reader sets
// and goes red when one drifts, which is the part a comment on its own cannot
// do; TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers pins the
// entitymap set.

// mapValue returns parent[key] when it holds a JSON object, else nil.
func mapValue(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	if typed, ok := parent[key].(map[string]any); ok {
		return typed
	}
	return nil
}

// sliceValue returns parent[key] as a slice of elements. It accepts the
// []map[string]any a Go caller may construct as well as the []any a JSON
// decode produces, so a test fixture and a real response take the same path.
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
// missing or holds another type. Trimming here is what lets callers treat ""
// as "the server did not report this field".
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
// what encoding/json produces for every JSON number.
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

// boolValue returns parent[key] as a bool, or false when the key is missing or
// holds another type.
func boolValue(parent map[string]any, key string) bool {
	if parent == nil {
		return false
	}
	if value, ok := parent[key].(bool); ok {
		return value
	}
	return false
}

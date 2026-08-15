// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import "strings"

// The component API summaries read a decoded JSON envelope, so every field
// arrives as an any behind a map key that may not exist. These readers each
// take the parent map and a key and return the zero value when the key is
// missing or holds a different type, which is what lets a renderer print a
// partial row instead of panicking on a server-side shape change.
//
// They are private copies of go/cmd/eshu's traceMap / traceSlice /
// traceString / traceInt (the bool reader's original left cmd/eshu with its
// last caller in #6059), and not the only ones: sets with these same names
// live in go/internal/cli/change/envelope.go and
// go/internal/cli/freshness/values.go. The surviving originals stay in
// go/cmd/eshu and keep their other callers, because that is package main and
// a copy is the only way to share anything out of it. Copying is deliberate
// rather than a shared helper package: the reading shape is four lines and
// the coupling would not be.
//
// An edit to any one set belongs in every set. TestEnvelopeReaderParity in
// go/cmd/eshu compares the declarations across the copies and goes red when
// one drifts, which is the part a comment on its own cannot do.

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

// intValue returns parent[key] as an int. A decoded JSON number arrives as a
// float64, so both int and float64 are accepted; anything else reads as 0.
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

// boolValue returns parent[key] as a bool, or false when the key is missing
// or holds another type.
func boolValue(parent map[string]any, key string) bool {
	if parent == nil {
		return false
	}
	if value, ok := parent[key].(bool); ok {
		return value
	}
	return false
}

// stringsValue coerces a decoded JSON array into its non-empty trimmed string
// elements. It is a private copy of go/cmd/eshu's traceStrings, taken for the
// same reason as the five readers above; the strings role of
// TestEnvelopeReaderParity in go/cmd/eshu compares the two declarations and
// goes red when one drifts.
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

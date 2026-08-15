// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import "strings"

// The readers below are one family's copy of the envelope value readers that
// go/internal/cli/change and go/internal/cli/freshness carry as mapValue /
// sliceValue / stringValue / intValue and go/internal/cli/component carries
// with stringsValue as well; entitymap keeps a differently named set. All of
// them were forked from cmd/eshu originals (traceMap and friends) that nothing
// could import -- package main -- and that are gone now the component (#6139)
// and trace (#6059) extractions removed their last cmd/eshu callers. Every
// family that renders an envelope takes its own set. Change one and you must
// change the rest: TestEnvelopeReaderParity in go/cmd/eshu compares the
// same-named copies at the source level per reader and names the one that
// drifted, and the entitymap twin tests pin that family's set against this
// one.
//
// The bool reader of that family -- boolValue -- is deliberately NOT copied
// here. This family reads no boolean out of an envelope, and the `unused`
// linter rejects a reader carried for symmetry alone. The parity test records
// the absence explicitly: re-introducing the reader under a sibling package's
// spelling -- boolValue -- fails there until it is registered. It matches on
// the names the other copies already use, so a bool reader added here under a
// fresh name goes unnoticed; register any bool reader you add rather than
// relying on that test to catch it.

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

// stringsValue returns value as a list of strings, and nil for anything that is
// not a list.
//
// The two list arms differ, which matters to a caller building an envelope in
// Go. A []any is filtered: entries that are not strings, and strings blank once
// trimmed, are dropped rather than rendered as empty bullets. A []string is
// returned as it arrived, blanks included, so RenderServiceSummary prints a
// bare "- " for each one. Only a Go caller can reach that arm -- encoding/json
// decodes every JSON array into []any -- so no API response produces it.
//
// Its sibling copies are stringsValue in go/internal/cli/component and
// stringList in go/internal/cli/entitymap; the cmd/eshu original,
// traceStrings, left with its last caller. TestEnvelopeReaderParity pins the
// strings role across this copy and component's, and the entitymap twin tests
// pin stringList against this one.
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
// Its cmd/eshu original, traceFirstString, was deleted when this extraction
// and the entitymap extraction removed its last callers. The one other copy is
// firstNonEmpty in go/internal/cli/entitymap/values.go;
// TestEntityMapValueReadersAreTokenIdenticalToTraceHelpers in go/cmd/eshu pins
// the two copies to each other, so an edit here belongs there too.
func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

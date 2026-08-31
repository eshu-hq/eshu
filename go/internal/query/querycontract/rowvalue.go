// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "fmt"

// Graph drivers hand a row back as map[string]any, so every read path has to
// assert each column's type before using it. These four helpers do that
// assertion once, returning the zero value rather than panicking on a missing
// key, a nil, or an unexpected type -- a graph read that lost one column
// should degrade that field, not fail the whole request.
//
// They live here rather than in package query because they carry no
// dependency on anything: no driver type, no handler, no store. Epic #6053
// (#6060) moves each handler family in go/internal/query into its own
// subpackage, and a subpackage cannot import the root package back without an
// import cycle, because root names family symbols in its compatibility
// aliases. StringVal alone is called from about 325 root files, so leaving
// these in root would block every family move. Package query keeps forwarding
// wrappers under the original names, so its own callers and the 28 files
// outside the package that call these four functions all compile unchanged.

// StringVal safely extracts a string from a map value. A missing key or a nil
// yields "". A present value of some other type is rendered with %v rather
// than discarded, because a driver returning a number where a string was
// expected still carries the value the caller asked for.
func StringVal(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// BoolVal safely extracts a bool from a map value. A missing key, a nil, or a
// non-bool value yields false.
func BoolVal(row map[string]any, key string) bool {
	v, ok := row[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// IntVal safely extracts an int from a map value. It accepts the three numeric
// shapes a graph driver actually returns -- int64 over Bolt, int from an
// in-process fake, float64 after a JSON round trip -- and yields 0 for a
// missing key, a nil, or any other type.
func IntVal(row map[string]any, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// StringSliceVal safely extracts a []string from a map value. It accepts an
// already-typed []string and the []any a driver hands back for a list column,
// skipping any element of that list which is not a string. A missing key, a
// nil, or any other type yields nil.
func StringSliceVal(row map[string]any, key string) []string {
	v, ok := row[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

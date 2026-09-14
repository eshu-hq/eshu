// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rowvalue

import "fmt"

// Graph drivers hand a row back as map[string]any, so every read path has to
// assert each column's type before using it. These helpers do that
// assertion once and never panic. A missing key or a nil always yields the
// zero value. An unexpected type yields the zero value too, except in
// StringVal, which renders it with %v -- see that function's own comment for
// why. A graph read that lost one column should degrade that field, not fail
// the whole request.
//
// They live here rather than in package query because they carry no
// dependency on anything: no driver type, no handler, no store. Epic #6053
// (#6060) moves each handler family in go/internal/query into its own
// subpackage, and a subpackage cannot import the root package back without an
// import cycle, because root names family symbols in its compatibility
// aliases. A call to StringVal alone appeared in 195 of the 866 non-test root
// files when this comment was written -- a frozen snapshot of root, unrelated
// to the test-file count below -- so leaving these in root would block every
// family move. Package querycontract and package query both keep forwarding
// wrappers under the original names, so every call site outside keeps its
// shape: the 437 .go files outside go/internal/query/querycontract that name
// StringVal, BoolVal, IntVal or StringSliceVal -- 242 non-test and 195 test --
// all compile unchanged. Both counts below are run from the repo root, because
// the go path argument is cwd-relative: the same commands from go/ match
// nothing and wc prints 0. The 437 is
//
//	rg -l '\b(StringVal|BoolVal|IntVal|StringSliceVal)\(' \
//		-g '*.go' -g '!go/internal/query/querycontract/**' go | wc -l
//
// measured at this head. It is a superset of the population this argument is
// about: the pattern counts any receiver, the declarations themselves and any
// mention inside a comment. Of those files, 28 -- 5 non-test and 23 test --
// reach these helpers through package query's exported wrappers,
//
//	rg -l '\bquery\.(StringVal|BoolVal|IntVal|StringSliceVal)\(' \
//		-g '*.go' go | wc -l
//
// and that 28/5/23 split is identical at 514534567, at origin/main d3d4c2d3e
// and at this head. The 437 likewise returns the identical file list at
// d3d4c2d3e, which is the frame this argument needs: the move touched no
// caller. At the older base 514534567 it read 435 -- 241 non-test, 194 test --
// a gap that is #6060's rename churn on main, not callers this branch added.
// The \b is load-bearing: unanchored, the pattern also counts cStringVal,
// cppStringVal and compareStringVal, each of which exists in this tree. It
// replaces the [^[:alnum:]_] an ERE tool needs in its place, POSIX ERE having
// no \b; the two select the same 437 files here, differing only on a match at
// the start of a line, which this tree has none of. Re-measuring either count
// at one of the commits named above is the one search that stays on git grep,
// because rg cannot read a commit -- see AGENTS.md in this directory.
// FloatVal is the exception: package query has no exported wrapper for it and
// reaches it through two unexported ones instead, floatVal in compare.go and
// relationshipFloatVal in repository_compat.go, named by 11 call sites across
// 3 root files.

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

// FloatVal reads key from row as a float64, coercing the numeric types a graph
// or SQL driver may hand back. A missing key, a nil value, or a non-numeric
// value yields 0 rather than an error: callers use it for scoring and
// thresholds where absence and zero are the same decision.
func FloatVal(row map[string]any, key string) float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Story row helpers shared by the query root and the handler-family
// subpackages (#6060 lane B2). The collection-level builders live in
// story_collection_helpers.go.
//
// They live here rather than in package query for the same reason the row
// decoders in rowvalue.go do: a family subpackage cannot import the root
// package back without an import cycle, because root names family symbols in
// its compatibility aliases. These helpers carry no dependency on anything
// beyond the standard library -- no driver type, no handler, no store -- so
// they move without behavior change. Package query keeps forwarding wrappers
// under the original names, so its own callers compile unchanged.

// ServiceStoryItemLimit bounds relationship and instance fan-out attached to
// service workload context/story payloads so a single prompt-ready read stays
// within the route budget and exposes truncation explicitly.
const ServiceStoryItemLimit = 50

// ContextStoryItemLimit bounds relationship and instance fan-out attached to
// entity and workload context/story payloads so a single prompt-ready read
// stays within the route budget and exposes truncation explicitly.
const ContextStoryItemLimit = 50

// SafeStr extracts a string from a map while filtering empty and nil values.
func SafeStr(m map[string]any, key string) string {
	v := fmt.Sprintf("%v", m[key])
	if v == "" || v == "<nil>" {
		return ""
	}
	return v
}

// MapValue extracts a nested map from a map value, tolerating a missing key,
// a wrong type, or an empty map.
func MapValue(value map[string]any, key string) map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	typed, ok := raw.(map[string]any)
	if !ok || len(typed) == 0 {
		return nil
	}
	return typed
}

// LowerStrings lowercases every value and returns the set sorted.
func LowerStrings(values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = strings.ToLower(v)
	}
	sort.Strings(result)
	return result
}

// UniqueSortedStrings drops blank and duplicate values and returns the set
// sorted.
func UniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// CopyMap returns a shallow copy of the input map.
func CopyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// JoinSentenceFragments joins parts in English prose style ("a", "a and b",
// "a, b, and c").
func JoinSentenceFragments(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

// FirstPositiveFloat returns the first positive candidate, or zero when none
// is positive.
func FirstPositiveFloat(candidates ...float64) float64 {
	for _, candidate := range candidates {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}

// AppendUniqueString appends candidate unless it is blank or already present.
func AppendUniqueString(values *[]string, candidate string) {
	if candidate = strings.TrimSpace(candidate); candidate == "" {
		return
	}
	for _, existing := range *values {
		if existing == candidate {
			return
		}
	}
	*values = append(*values, candidate)
}

// ContainsString reports whether values holds candidate.
func ContainsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// ContainsAllSubstrings reports whether value contains every part. It lives
// here (not in querytestutil) because production code — the query root's
// family_impact_shim.go — calls it, and the source-coverage gate rejects
// production imports of the test-only helper package. See #6060.
func ContainsAllSubstrings(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

// CapMapRows truncates rows to limit, reporting whether truncation happened.
func CapMapRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

// MapSliceValue extracts a []map[string]any from a map value, tolerating a
// missing key, a wrong type, non-map elements (skipped), or an empty map.
func MapSliceValue(value map[string]any, key string) []map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	typed, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(typed))
	for _, item := range typed {
		row, ok := item.(map[string]any)
		if ok {
			result = append(result, row)
		}
	}
	return result
}

// FirstNonEmptyString returns the first value with non-blank trimmed content,
// or "" when every value is blank.
func FirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// FiniteGraphFloat reads key from row as a float64 and rejects NaN/Inf with
// an error naming subject and key, so a non-finite graph value fails the read
// instead of poisoning scoring downstream.
func FiniteGraphFloat(row map[string]any, key, subject string) (float64, error) {
	value := FloatVal(row, key)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s %s must be finite", subject, key)
	}
	return value, nil
}

// CleanMetadataString trims a metadata string, mapping empty and "<nil>" to "".
func CleanMetadataString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}

// MetadataNonEmptyString extracts a cleaned non-empty string from a metadata
// map, reporting whether one was present.
func MetadataNonEmptyString(metadata map[string]any, key string) (string, bool) {
	value, ok := metadata[key].(string)
	if !ok {
		return "", false
	}
	value = CleanMetadataString(value)
	if value == "" {
		return "", false
	}
	return value, true
}

// AddUniqueStringField appends value to the row's key slice (deduped,
// sorted), skipping blank values.
func AddUniqueStringField(row map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	values := StringSliceVal(row, key)
	values = append(values, value)
	row[key] = UniqueSortedStrings(values)
}

// SortStringFields sorts the row's string-slice columns in place.
func SortStringFields(row map[string]any, keys ...string) {
	for _, key := range keys {
		values := StringSliceVal(row, key)
		sort.Strings(values)
		row[key] = values
	}
}

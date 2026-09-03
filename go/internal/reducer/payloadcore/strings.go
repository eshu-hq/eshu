// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// UniqueSortedStrings returns the trimmed, non-empty values deduplicated and
// sorted ascending, or nil when nothing survives.
func UniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return nil
	}

	slices.Sort(normalized)
	return normalized
}

// AppendUniqueString appends value to values unless it is empty or already
// present, preserving insertion order.
func AppendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// CompactStringSlice returns the trimmed, non-empty values in argument order.
// The result is always non-nil, so an encoded payload carries [] rather than
// null.
func CompactStringSlice(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// CleanFactFilterValues trims the values, drops the empty ones, and removes
// duplicates while preserving first-seen order. It is the normalization applied
// to fact-filter inputs before they reach a query.
func CleanFactFilterValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

// MissingStrings returns the values in current that are not present in
// initial, preserving current's order, or nil when current is empty. Unlike
// CleanFactFilterValues it does no trimming or deduplication of its own — it
// is a plain set difference over whatever current already holds.
func MissingStrings(current []string, initial []string) []string {
	if len(current) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(initial))
	for _, value := range initial {
		seen[value] = struct{}{}
	}
	missing := make([]string, 0, len(current))
	for _, value := range current {
		if _, ok := seen[value]; ok {
			continue
		}
		missing = append(missing, value)
	}
	return missing
}

// NonNilStrings returns values, substituting an empty slice for nil so an
// encoded payload carries [] rather than null.
func NonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// NonNilMapSlice returns values, substituting an empty slice for nil so an
// encoded payload carries [] rather than null.
func NonNilMapSlice(values []map[string]any) []map[string]any {
	if values == nil {
		return []map[string]any{}
	}
	return values
}

// FirstNonBlank returns the first value that is non-empty once trimmed, or "".
func FirstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// DerefString returns the pointed-to string, or "" when the pointer is nil.
func DerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// DerefBool returns the pointed-to bool, or false when the pointer is nil.
func DerefBool(value *bool) bool {
	return value != nil && *value
}

// DerefInt returns the pointed-to int, or 0 when the pointer is nil.
func DerefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// DerefStringTrimmed dereferences a *string and trims it, returning "" for
// nil. Mirrors DerefString plus a universal TrimSpace for callers decoding an
// optional string field through a typed contracts seam, where a padded value
// must not flow through untrimmed.
func DerefStringTrimmed(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// SortedKeys returns the keys of a set in ascending order, or nil when empty.
func SortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FormatTally renders a count map as a stable, key-sorted string so the same
// tally always produces the same log line.
func FormatTally(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

// DedupeNonEmptyStrings returns the trimmed, non-empty values with duplicates
// removed, preserving first-seen order. It differs from UniqueSortedStrings
// only in that it does NOT sort: callers that derive a deterministic key from
// the result must sort it themselves, and callers that need the producer's
// order (an inheritance trait-adaptation list, for instance) depend on it being
// preserved here.
func DedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	if len(deduped) == 0 {
		return nil
	}
	return deduped
}

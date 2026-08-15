// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"sort"
	"strings"
)

// mapFromAny reads a nested object out of a JSON-decoded value, returning nil
// when the value is absent or another type. Callers index the nil map
// directly, which is safe in Go and reads as "no value for that key".
func mapFromAny(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

// floatFromAny reads a number out of a JSON-decoded value. The int cases exist
// because a value can reach here from a hand-built map in a test as well as
// from encoding/json, which always decodes to float64.
func floatFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

// boolFromAny reads a bool out of a JSON-decoded value, treating a missing key
// or another type as false.
func boolFromAny(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

// firstNonEmpty returns the first value that is not blank after trimming. The
// SARIF mapping uses it to walk a preference order across the finding and its
// provenance block.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// firstPositiveFloat returns the first value greater than zero, so a zero CVSS
// score does not shadow a real one reported further down the preference order.
func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// firstPositiveInt is firstPositiveFloat for line numbers, where zero means
// "not reported" rather than line zero.
func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// cloneAndSortStrings returns a sorted copy, leaving the caller's slice
// untouched. Export output must be byte-stable across runs, and the server
// does not promise an order.
func cloneAndSortStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

// cloneUniqueSortedStrings is cloneAndSortStrings plus trimming and
// de-duplication, used where two sources of missing evidence are concatenated
// before export.
func cloneUniqueSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

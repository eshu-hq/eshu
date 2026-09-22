// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"fmt"
	"sort"
	"strings"
)

// NamedCount captures one status bucket and its count.
type NamedCount struct {
	Name  string
	Count int
}

// CountMap folds named buckets into a total per name, dropping blank names.
// Repeated names accumulate rather than overwrite, so a reader that returns
// the same bucket from two queries reports their sum.
func CountMap(rows []NamedCount) map[string]int {
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		counts[name] += row.Count
	}

	return counts
}

// FormatTotals renders a count map as operator text in lifecycle order
// (active, pending, completed, succeeded, failed, then anything else
// alphabetically). Non-positive buckets are omitted; it returns "none" when
// nothing is left to show.
func FormatTotals(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}

	keys := make([]string, 0, len(values))
	for key, value := range values {
		if value <= 0 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return countOrder(keys[i]) < countOrder(keys[j]) ||
			(countOrder(keys[i]) == countOrder(keys[j]) && keys[i] < keys[j])
	})

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, " ")
}

// countOrder ranks the well-known lifecycle buckets so operator text reads in
// pipeline order instead of alphabetically.
func countOrder(name string) int {
	switch name {
	case "active":
		return 0
	case "pending":
		return 1
	case "completed":
		return 2
	case "succeeded":
		return 3
	case "failed":
		return 4
	default:
		return 100
	}
}

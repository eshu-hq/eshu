// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

func normalizeImpactListLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func trimImpactRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const (
	complexityDefaultListLimit = 10
	complexityMaxListLimit     = 100
)

func normalizeComplexityListLimit(limit int) int {
	if limit <= 0 {
		return complexityDefaultListLimit
	}
	if limit > complexityMaxListLimit {
		return complexityMaxListLimit
	}
	return limit
}

func trimComplexityResults(results []map[string]any, limit int) ([]map[string]any, bool) {
	if len(results) <= limit {
		return results, false
	}
	return results[:limit], true
}

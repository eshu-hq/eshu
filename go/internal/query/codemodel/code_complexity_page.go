// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

const (
	complexityDefaultListLimit = 10
	complexityMaxListLimit     = 100
)

// NormalizeComplexityListLimit clamps the requested complexity list limit.
func NormalizeComplexityListLimit(limit int) int {
	if limit <= 0 {
		return complexityDefaultListLimit
	}
	if limit > complexityMaxListLimit {
		return complexityMaxListLimit
	}
	return limit
}

// TrimComplexityResults applies the complexity list limit with a truncation flag.
func TrimComplexityResults(results []map[string]any, limit int) ([]map[string]any, bool) {
	if len(results) <= limit {
		return results, false
	}
	return results[:limit], true
}

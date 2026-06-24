// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const (
	resolveEntityDefaultLimit = 10
	resolveEntityMaxLimit     = 100
)

func normalizeResolveEntityLimit(limit int) int {
	if limit <= 0 {
		return resolveEntityDefaultLimit
	}
	if limit > resolveEntityMaxLimit {
		return resolveEntityMaxLimit
	}
	return limit
}

func resolvedEntityResponse(entities []map[string]any, limit int, truncated bool) map[string]any {
	return map[string]any{
		"entities":  entities,
		"matches":   entities,
		"count":     len(entities),
		"limit":     limit,
		"truncated": truncated,
	}
}

func entityResolveTruthEnvelope(profile QueryProfile) *TruthEnvelope {
	return BuildTruthEnvelope(
		profile,
		"code_search.fuzzy_symbol",
		TruthBasisHybrid,
		"resolved from bounded graph and content entity resolution",
	)
}

func trimResolvedEntityPage(entities []map[string]any, limit int) ([]map[string]any, bool) {
	if len(entities) <= limit {
		return entities, false
	}
	return entities[:limit], true
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	resolveEntityDefaultLimit = 10
	resolveEntityMaxLimit     = 100
)

// NormalizeResolveEntityLimit clamps a resolve limit to the sane range. Exported for the staying queryplan execution test via the root forwarder; see #6060.
func NormalizeResolveEntityLimit(limit int) int {
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

func entityResolveTruthEnvelope(profile querycontract.QueryProfile) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		profile,
		"code_search.fuzzy_symbol",
		querycontract.TruthBasisHybrid,
		"resolved from bounded graph and content entity resolution",
	)
}

func globalContentEntityResolveTruthEnvelope(profile querycontract.QueryProfile) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		profile,
		"code_search.exact_symbol",
		querycontract.TruthBasisContentIndex,
		"resolved by exact case-sensitive name from the current content entity index",
	)
}

func canonicalContentEntityResolveTruthEnvelope(profile querycontract.QueryProfile, graphHydrated bool) *querycontract.TruthEnvelope {
	if graphHydrated {
		return querycontract.BuildTruthEnvelope(
			profile,
			"code_search.exact_symbol",
			querycontract.TruthBasisHybrid,
			"resolved by canonical content entity ID with graph-backed workload repository hydration",
		)
	}
	return querycontract.BuildTruthEnvelope(
		profile,
		"code_search.exact_symbol",
		querycontract.TruthBasisContentIndex,
		"resolved by canonical content entity ID from the current content index",
	)
}

func trimResolvedEntityPage(entities []map[string]any, limit int) ([]map[string]any, bool) {
	if len(entities) <= limit {
		return entities, false
	}
	return entities[:limit], true
}

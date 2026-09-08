// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func entityContextTruthEnvelope(profile querycontract.QueryProfile) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		profile,
		"code_search.fuzzy_symbol",
		querycontract.TruthBasisHybrid,
		"resolved from graph or content-backed entity context",
	)
}

func workloadContextTruthEnvelope(profile querycontract.QueryProfile, surface string) *querycontract.TruthEnvelope {
	reason := "resolved from workload context and platform evidence"
	if surface == "story" {
		reason = "resolved from workload story and platform evidence"
	}
	return querycontract.BuildTruthEnvelope(
		profile,
		"platform_impact.context_overview",
		querycontract.TruthBasisHybrid,
		reason,
	)
}

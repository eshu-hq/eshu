// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func entityContextTruthEnvelope(profile QueryProfile) *TruthEnvelope {
	return BuildTruthEnvelope(
		profile,
		"code_search.fuzzy_symbol",
		TruthBasisHybrid,
		"resolved from graph or content-backed entity context",
	)
}

func workloadContextTruthEnvelope(profile QueryProfile, surface string) *TruthEnvelope {
	reason := "resolved from workload context and platform evidence"
	if surface == "story" {
		reason = "resolved from workload story and platform evidence"
	}
	return BuildTruthEnvelope(
		profile,
		"platform_impact.context_overview",
		TruthBasisHybrid,
		reason,
	)
}

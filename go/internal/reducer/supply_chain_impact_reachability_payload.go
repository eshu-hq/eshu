// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

func supplyChainReachabilityPayload(reachability *SupplyChainReachability) map[string]any {
	if reachability == nil || reachability.State == "" {
		return nil
	}
	out := map[string]any{
		"state":             string(reachability.State),
		"confidence":        reachability.Confidence,
		"source":            reachability.Source,
		"evidence":          reachability.Evidence,
		"reason":            reachability.Reason,
		"language_maturity": reachability.LanguageMaturity,
	}
	if len(reachability.MissingEvidence) > 0 {
		out["missing_evidence"] = uniqueSortedStrings(reachability.MissingEvidence)
	}
	return out
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

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
		out["missing_evidence"] = payloadcore.UniqueSortedStrings(reachability.MissingEvidence)
	}
	return out
}

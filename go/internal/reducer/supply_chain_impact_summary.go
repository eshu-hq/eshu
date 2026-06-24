// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strconv"
	"strings"
)

func supplyChainImpactCounts(findings []SupplyChainImpactFinding) map[SupplyChainImpactStatus]int {
	counts := make(map[SupplyChainImpactStatus]int, len(supplyChainImpactStatuses()))
	for _, finding := range findings {
		counts[finding.Status]++
	}
	return counts
}

func supplyChainSuppressionCounts(findings []SupplyChainImpactFinding) map[SupplyChainSuppressionState]int {
	counts := make(map[SupplyChainSuppressionState]int, len(SupplyChainSuppressionStates()))
	for _, finding := range findings {
		state := finding.Suppression.State
		if state == "" {
			state = SupplyChainSuppressionStateActive
		}
		counts[state]++
	}
	return counts
}

func supplyChainImpactSummary(
	evaluated int,
	counts map[SupplyChainImpactStatus]int,
	suppressionCounts map[SupplyChainSuppressionState]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"supply chain impact evaluated=%d affected_exact=%d affected_derived=%d possibly_affected=%d not_affected_known_fixed=%d unknown_impact=%d "+
			"suppression_active=%d suppression_not_affected=%d suppression_accepted_risk=%d suppression_false_positive=%d "+
			"suppression_ignored=%d suppression_expired=%d suppression_provider_dismissed=%d suppression_scope_mismatch=%d "+
			"canonical_writes=%d",
		evaluated,
		counts[SupplyChainImpactAffectedExact],
		counts[SupplyChainImpactAffectedDerived],
		counts[SupplyChainImpactPossiblyAffected],
		counts[SupplyChainImpactNotAffectedKnownFixed],
		counts[SupplyChainImpactUnknown],
		suppressionCounts[SupplyChainSuppressionStateActive],
		suppressionCounts[SupplyChainSuppressionStateNotAffected],
		suppressionCounts[SupplyChainSuppressionStateAcceptedRisk],
		suppressionCounts[SupplyChainSuppressionStateFalsePositive],
		suppressionCounts[SupplyChainSuppressionStateIgnored],
		suppressionCounts[SupplyChainSuppressionStateExpired],
		suppressionCounts[SupplyChainSuppressionStateProviderDismissed],
		suppressionCounts[SupplyChainSuppressionStateScopeMismatch],
		canonicalWrites,
	)
}

func supplyChainImpactCanonicalWrites(findings []SupplyChainImpactFinding) int {
	total := 0
	for _, finding := range findings {
		total += finding.CanonicalWrites
	}
	return total
}

func supplyChainCVEID(payload map[string]any) string {
	return firstNonBlank(payloadStr(payload, "cve_id"), payloadStr(payload, "advisory_id"))
}

func supplyChainFloat(payload map[string]any, key string) float64 {
	raw := strings.TrimSpace(fmt.Sprint(payload[key]))
	if raw == "" || raw == "<nil>" {
		return 0
	}
	value, _ := strconv.ParseFloat(raw, 64)
	return value
}

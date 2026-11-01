// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// TestAdvisoryRetractedFindingsAreHiddenFromEveryDefaultView proves a finding
// whose advisory the source retracted (#9695) is filtered out of every read
// that applies the hidden-suppression-state list, and that the filter and the
// state vocabulary both know the value.
func TestAdvisoryRetractedFindingsAreHiddenFromEveryDefaultView(t *testing.T) {
	t.Parallel()

	if !IsSupportedSuppressionState("advisory_retracted") {
		t.Fatalf("advisory_retracted must be a valid suppression_state filter value")
	}
	if !strings.Contains(supplyChainImpactHiddenSuppressionStatesSQL, "'advisory_retracted'") {
		t.Fatalf("hidden suppression states = %s, want advisory_retracted hidden by default",
			supplyChainImpactHiddenSuppressionStatesSQL)
	}
	for name, query := range map[string]string{
		"list direct":       ListFindingsQuery,
		"list materialized": ListFindingsFromWinnersQuery,
		"aggregate shared":  AggregateCanonicalFactsCTE,
	} {
		if !strings.Contains(query, supplyChainImpactHiddenSuppressionStatesSQL) {
			t.Fatalf("%s query does not apply the hidden suppression state list", name)
		}
	}
}

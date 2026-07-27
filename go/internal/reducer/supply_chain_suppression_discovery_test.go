// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSupplyChainImpactFilterSeedsAdvisoryOnlySuppressionDiscovery(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactFilter([]facts.Envelope{{
		FactID:   "suppression-advisory-only",
		FactKind: facts.VulnerabilitySuppressionFactKind,
		Payload: map[string]any{
			"scope": map[string]any{"advisory_id": "GHSA-2026-aaaa-bbbb"},
		},
	}})

	if !slices.Equal(filter.AdvisoryIDs, []string{"GHSA-2026-aaaa-bbbb"}) {
		t.Fatalf("AdvisoryIDs = %#v, want bounded advisory identity", filter.AdvisoryIDs)
	}
	if filter.empty() {
		t.Fatal("advisory-only suppression filter is empty; reducer discovery would silently stop")
	}
}

func TestSupplyChainImpactAdvisoryFilterSurvivesMergeAndFollowUp(t *testing.T) {
	t.Parallel()

	initial := SupplyChainImpactFactFilter{AdvisoryIDs: []string{"GHSA-2026-aaaa-bbbb"}}
	current := SupplyChainImpactFactFilter{
		AdvisoryIDs: []string{"GHSA-2026-aaaa-bbbb", "GHSA-2026-cccc-dddd"},
	}
	followUp := supplyChainImpactFollowUpFilter(initial, current)
	if !slices.Equal(followUp.AdvisoryIDs, []string{"GHSA-2026-cccc-dddd"}) {
		t.Fatalf("follow-up AdvisoryIDs = %#v, want only newly discovered advisory", followUp.AdvisoryIDs)
	}

	merged := mergeSupplyChainImpactFactFilters(initial, followUp)
	if !slices.Equal(merged.AdvisoryIDs, current.AdvisoryIDs) {
		t.Fatalf("merged AdvisoryIDs = %#v, want %#v", merged.AdvisoryIDs, current.AdvisoryIDs)
	}
}

func TestSupplyChainImpactFilterSeedsAdvisoryFromSourceFacts(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactFilter([]facts.Envelope{
		{
			FactID:   "source-advisory",
			FactKind: facts.VulnerabilityCVEFactKind,
			Payload:  map[string]any{"advisory_id": "GHSA-2026-aaaa-bbbb"},
		},
		{
			FactID:   "affected-advisory",
			FactKind: facts.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"advisory_id": "GHSA-2026-aaaa-bbbb",
				"package_id":  "pkg:npm/example@1.2.3",
			},
		},
	})

	if !slices.Equal(filter.AdvisoryIDs, []string{"GHSA-2026-aaaa-bbbb"}) {
		t.Fatalf("AdvisoryIDs = %#v, want source advisory identity", filter.AdvisoryIDs)
	}
}

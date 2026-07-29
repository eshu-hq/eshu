// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactFilterCollectsAdvisoryIDsSeparatelyFromCVEIDs proves
// the #5466 round-4 review F-10 fix: supplyChainImpactFilter must collect a
// vulnerability.cve fact's raw advisory_id into AdvisoryIDs even when its
// advisory_id is DISTINCT from cve_id (e.g. a GHSA ID). supplyChainCVEID
// (firstNonBlank(cve_id, advisory_id)) already feeds cve_id into CVEIDs, so
// without a separate AdvisoryIDs collection, a distinct advisory_id value
// would never reach any filter list at all -- leaving a suppression scoped
// ONLY by advisory_id unreachable by the active-evidence SQL prefilter.
func TestSupplyChainImpactFilterCollectsAdvisoryIDsSeparatelyFromCVEIDs(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "cve-1",
			FactKind: facts.VulnerabilityCVEFactKind,
			Payload: map[string]any{
				"cve_id":      "CVE-2026-1234",
				"advisory_id": "GHSA-demo-1111-2222",
				"cvss_score":  7.5,
			},
		},
	}

	filter := supplyChainImpactFilter(envelopes)

	if got, want := filter.CVEIDs, []string{"CVE-2026-1234"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("CVEIDs = %v, want %v", got, want)
	}
	if got, want := filter.AdvisoryIDs, []string{"GHSA-demo-1111-2222"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("AdvisoryIDs = %v, want %v (distinct from cve_id, must not be dropped)", got, want)
	}
}

// TestSupplyChainImpactFilterAdvisoryIDOnlyIsNotEmpty proves a filter
// carrying ONLY AdvisoryIDs is still treated as non-empty.
func TestSupplyChainImpactFilterAdvisoryIDOnlyIsNotEmpty(t *testing.T) {
	t.Parallel()

	filter := SupplyChainImpactFactFilter{AdvisoryIDs: []string{"GHSA-demo-1111-2222"}}
	if filter.empty() {
		t.Fatalf("filter %#v reported empty, want non-empty so the active-evidence query still runs", filter)
	}
}

// TestSupplyChainImpactFollowUpFilterTracksAdvisoryIDs proves the
// iterative until-stable expansion loop narrows AdvisoryIDs to only the
// NOT-YET-REQUESTED values across passes, mirroring
// TestSupplyChainImpactFollowUpFilterTracksEnvironmentWorkloadServiceAnchors.
func TestSupplyChainImpactFollowUpFilterTracksAdvisoryIDs(t *testing.T) {
	t.Parallel()

	requested := SupplyChainImpactFactFilter{AdvisoryIDs: []string{"GHSA-demo-1111-2222"}}
	current := SupplyChainImpactFactFilter{AdvisoryIDs: []string{"GHSA-demo-1111-2222", "GHSA-other-9999-8888"}}

	followUp := supplyChainImpactFollowUpFilter(requested, current)

	if got, want := followUp.AdvisoryIDs, []string{"GHSA-other-9999-8888"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("follow-up AdvisoryIDs = %v, want %v (only the newly discovered value)", got, want)
	}
}

func equalSuppressionAnchorSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactFilterCollectsEnvironmentWorkloadServiceAnchors proves
// the #5466 P0 follow-up: supplyChainImpactFilter must surface known
// environment/workload_id/service_id values from already-loaded deployment
// evidence (reducer_ci_cd_run_correlation, reducer_workload_identity,
// reducer_service_catalog_correlation) as SupplyChainImpactFactFilter
// anchors, so the active-evidence SQL prefilter
// (ListActiveSupplyChainImpactFacts) has something to match a
// vulnerability.suppression fact scoped ONLY by environment/workload_id/
// service_id against. Without this, such a suppression can never enter the
// reducer's working set: the scope struct and matcher would accept it, but
// the loader would never fetch it.
func TestSupplyChainImpactFilterCollectsEnvironmentWorkloadServiceAnchors(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		cicdRunCorrelationImpactFact("ci-run-1", "sha256:digest", "registry/image:tag", "repo://acme/api", "stage", "exact"),
		workloadIdentityImpactFact("workload-1", "repo://acme/api", "workload:acme-api"),
		serviceCatalogCorrelationImpactFact("service-1", "repo://acme/api", "service:acme-api", "workload:acme-api-svc", "exact", "in_sync", false),
	}

	filter := supplyChainImpactFilter(envelopes)

	if got, want := filter.Environments, []string{"stage"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("Environments = %v, want %v", got, want)
	}
	if got, want := filter.WorkloadIDs, []string{"workload:acme-api", "workload:acme-api-svc"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("WorkloadIDs = %v, want %v", got, want)
	}
	if got, want := filter.ServiceIDs, []string{"service:acme-api"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("ServiceIDs = %v, want %v", got, want)
	}
}

// TestSupplyChainImpactFilterEnvironmentWorkloadServiceOnlyIsNotEmpty proves
// a filter carrying ONLY the three new anchor kinds (no package/purl/cve/
// digest/repository anchors at all) is still treated as a non-empty,
// dispatchable filter -- otherwise SupplyChainImpactFactFilter.empty() would
// silently skip the query even when a real anchor is present.
func TestSupplyChainImpactFilterEnvironmentWorkloadServiceOnlyIsNotEmpty(t *testing.T) {
	t.Parallel()

	for _, filter := range []SupplyChainImpactFactFilter{
		{Environments: []string{"stage"}},
		{WorkloadIDs: []string{"workload:acme-api"}},
		{ServiceIDs: []string{"service:acme-api"}},
	} {
		if filter.empty() {
			t.Fatalf("filter %#v reported empty, want non-empty so the active-evidence query still runs", filter)
		}
	}
}

// TestSupplyChainImpactFollowUpFilterTracksEnvironmentWorkloadServiceAnchors
// proves the iterative until-stable expansion loop
// (loadActiveSupplyChainImpactFactsUntilStable) correctly narrows the new
// anchor kinds to only the NOT-YET-REQUESTED values across passes, matching
// the existing behavior for package/purl/cve/digest/repository anchors --
// otherwise a later pass could either re-request the same values forever
// (never converging) or silently drop a newly discovered environment/
// workload/service value.
func TestSupplyChainImpactFollowUpFilterTracksEnvironmentWorkloadServiceAnchors(t *testing.T) {
	t.Parallel()

	requested := SupplyChainImpactFactFilter{
		Environments: []string{"stage"},
		WorkloadIDs:  []string{"workload:acme-api"},
		ServiceIDs:   []string{"service:acme-api"},
	}
	current := SupplyChainImpactFactFilter{
		Environments: []string{"stage", "prod"},
		WorkloadIDs:  []string{"workload:acme-api", "workload:acme-worker"},
		ServiceIDs:   []string{"service:acme-api"},
	}

	followUp := supplyChainImpactFollowUpFilter(requested, current)

	if got, want := followUp.Environments, []string{"prod"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("follow-up Environments = %v, want %v (only the newly discovered value)", got, want)
	}
	if got, want := followUp.WorkloadIDs, []string{"workload:acme-worker"}; !equalSuppressionAnchorSlices(got, want) {
		t.Fatalf("follow-up WorkloadIDs = %v, want %v (only the newly discovered value)", got, want)
	}
	if len(followUp.ServiceIDs) != 0 {
		t.Fatalf("follow-up ServiceIDs = %v, want empty (nothing new discovered)", followUp.ServiceIDs)
	}
}

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
// carrying ONLY AdvisoryIDs is still treated as non-empty, mirroring
// TestSupplyChainImpactFilterEnvironmentWorkloadServiceOnlyIsNotEmpty for
// the three #5466 P1-1 anchors.
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

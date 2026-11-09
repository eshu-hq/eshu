// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packet

import "testing"

// TestFreshenAdvisoryFamilyFromGHSAFetchFreshens pins eshu-hq/eshu#10464:
// a fetched, current GHSA source confirms the advisory evidence family the
// way OSV, NVD, and GLAD do. An unchanged upstream answer stores no new
// generation, so the active facts keep the observed_at of the last real
// change; without GHSA in fetchConfirmedSources a fetched GHSA stream reads
// stale after the freshness window.
func TestFreshenAdvisoryFamilyFromGHSAFetchFreshens(t *testing.T) {
	t.Parallel()

	families := map[string]EvidenceFamily{
		EvidenceFamilyVulnerabilityAdvisory: {
			Family:           EvidenceFamilyVulnerabilityAdvisory,
			FactCount:        1,
			LatestObservedAt: "2026-01-01T00:00:00Z",
			Freshness:        FreshnessLabelStale,
		},
	}
	states := []SourceState{{
		CollectorInstanceID: "ghsa-instance",
		ScopeID:             "vuln-intel://ghsa/reviewed",
		Source:              "ghsa",
		FreshnessState:      "fresh",
		TerminalStatus:      "succeeded",
		LastSuccessAt:       "2026-11-01T00:00:00Z",
	}}
	FreshenFamiliesFromFetch(families, states)
	got := families[EvidenceFamilyVulnerabilityAdvisory]
	if got.Freshness != FreshnessLabelFresh {
		t.Fatalf("advisory family freshness = %q, want %q after a fresh GHSA fetch", got.Freshness, FreshnessLabelFresh)
	}
	if got.LatestObservedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("LatestObservedAt = %q, want the fact time kept (facts are not re-observed)", got.LatestObservedAt)
	}
}

// TestFreshenAdvisoryFamilyIgnoresStaleGHSAFetch proves the other direction:
// a stale GHSA state must not confirm the advisory family.
func TestFreshenAdvisoryFamilyIgnoresStaleGHSAFetch(t *testing.T) {
	t.Parallel()

	families := map[string]EvidenceFamily{
		EvidenceFamilyVulnerabilityAdvisory: {
			Family:           EvidenceFamilyVulnerabilityAdvisory,
			FactCount:        1,
			LatestObservedAt: "2026-01-01T00:00:00Z",
			Freshness:        FreshnessLabelStale,
		},
	}
	states := []SourceState{{
		CollectorInstanceID: "ghsa-instance",
		ScopeID:             "vuln-intel://ghsa/reviewed",
		Source:              "ghsa",
		FreshnessState:      "stale",
		TerminalStatus:      "succeeded",
		LastSuccessAt:       "2026-01-01T00:00:00Z",
	}}
	FreshenFamiliesFromFetch(families, states)
	if got := families[EvidenceFamilyVulnerabilityAdvisory].Freshness; got != FreshnessLabelStale {
		t.Fatalf("advisory family freshness = %q, want stale when the GHSA fetch is stale", got)
	}
}

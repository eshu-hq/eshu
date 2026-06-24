// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildSupplyChainImpactReadinessFailsClosedForStalePackageMetadata(t *testing.T) {
	t.Parallel()

	// A package-anchored local scan requires package-registry metadata as a
	// join source. A stale registry observation must remain visible as stale
	// freshness and must not be converted into a clean ready_zero_findings
	// answer just because advisory and registry fact counts are non-zero.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{PackageID: "pkg:npm/example"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: FreshnessLabelStale},
			},
		},
	)
	if envelope.State != ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateEvidenceIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, "package_registry_metadata") {
		t.Fatalf("missing_evidence = %#v, want package_registry_metadata", envelope.MissingEvidence)
	}
	if envelope.Freshness != FreshnessLabelStale {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelStale)
	}
}

func TestBuildSupplyChainImpactReadinessFailsClosedForMissingPackageMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scope SupplyChainImpactTargetScope
	}{
		{name: "package anchor", scope: SupplyChainImpactTargetScope{PackageID: "pkg:npm/example"}},
		{name: "repository target", scope: SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envelope := BuildSupplyChainImpactReadiness(
				tt.scope,
				nil,
				false,
				SupplyChainImpactReadinessSnapshot{
					EvidenceSources: []SupplyChainImpactEvidenceFamily{
						{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
						{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
					},
				},
			)
			if envelope.State != ReadinessStateEvidenceIncomplete {
				t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateEvidenceIncomplete)
			}
			if !readinessMissingContains(envelope.MissingEvidence, "package_registry_metadata") {
				t.Fatalf("missing_evidence = %#v, want package_registry_metadata", envelope.MissingEvidence)
			}
		})
	}
}

func TestBuildSupplyChainImpactReadinessKeepsFreshPackageMetadataReady(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if readinessMissingContains(envelope.MissingEvidence, "package_registry_metadata") {
		t.Fatalf("missing_evidence = %#v, must not include package_registry_metadata", envelope.MissingEvidence)
	}
}

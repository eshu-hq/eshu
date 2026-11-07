// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

func TestBuildSupplyChainImpactReadinessFailsClosedForStalePackageMetadata(t *testing.T) {
	t.Parallel()

	// A package-anchored local scan requires package-registry metadata as a
	// join source. A stale registry observation must remain visible as stale
	// freshness and must not be converted into a clean ready_zero_findings
	// answer just because advisory and registry fact counts are non-zero.
	envelope := packet.BuildReadiness(
		packet.TargetScope{PackageID: "pkg:npm/example"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: packet.FreshnessLabelStale},
			},
		},
	)
	if envelope.State != packet.ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateEvidenceIncomplete)
	}
	if !packet.ReadinessMissingContains(envelope.MissingEvidence, "package_registry_metadata") {
		t.Fatalf("missing_evidence = %#v, want package_registry_metadata", envelope.MissingEvidence)
	}
	if envelope.Freshness != packet.FreshnessLabelStale {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, packet.FreshnessLabelStale)
	}
}

func TestBuildSupplyChainImpactReadinessFailsClosedForMissingPackageMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scope packet.TargetScope
	}{
		{name: "package anchor", scope: packet.TargetScope{PackageID: "pkg:npm/example"}},
		{name: "repository target", scope: packet.TargetScope{RepositoryID: "repo://example/api"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envelope := packet.BuildReadiness(
				tt.scope,
				nil,
				false,
				packet.ReadinessSnapshot{
					EvidenceSources: []packet.EvidenceFamily{
						{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
						{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
					},
				},
			)
			if envelope.State != packet.ReadinessStateEvidenceIncomplete {
				t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateEvidenceIncomplete)
			}
			if !packet.ReadinessMissingContains(envelope.MissingEvidence, "package_registry_metadata") {
				t.Fatalf("missing_evidence = %#v, want package_registry_metadata", envelope.MissingEvidence)
			}
		})
	}
}

func TestBuildSupplyChainImpactReadinessKeepsFreshPackageMetadataReady(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != packet.ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, packet.ReadinessStateReadyZeroFindings)
	}
	if packet.ReadinessMissingContains(envelope.MissingEvidence, "package_registry_metadata") {
		t.Fatalf("missing_evidence = %#v, must not include package_registry_metadata", envelope.MissingEvidence)
	}
}

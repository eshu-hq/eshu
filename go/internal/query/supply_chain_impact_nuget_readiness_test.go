// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildSupplyChainImpactReadinessClassifiesNuGetReadyZeroFindings(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/dotnet-worker"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 8, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 2, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 2, Freshness: FreshnessLabelFresh},
			},
		},
	)

	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q for NuGet evidence with no impact findings", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", envelope.MissingEvidence)
	}
	if len(envelope.UnsupportedTargets) != 0 {
		t.Fatalf("unsupported_targets = %#v, want empty for supported NuGet evidence", envelope.UnsupportedTargets)
	}
}

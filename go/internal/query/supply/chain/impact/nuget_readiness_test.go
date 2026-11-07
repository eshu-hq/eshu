// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

func TestBuildSupplyChainImpactReadinessClassifiesNuGetReadyZeroFindings(t *testing.T) {
	t.Parallel()

	envelope := packet.BuildReadiness(
		packet.TargetScope{RepositoryID: "repo://example/dotnet-worker"},
		nil,
		false,
		packet.ReadinessSnapshot{
			EvidenceSources: []packet.EvidenceFamily{
				{Family: packet.EvidenceFamilyVulnerabilityAdvisory, FactCount: 8, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageConsumption, FactCount: 2, Freshness: packet.FreshnessLabelFresh},
				{Family: packet.EvidenceFamilyPackageRegistry, FactCount: 2, Freshness: packet.FreshnessLabelFresh},
			},
		},
	)

	if envelope.State != packet.ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q for NuGet evidence with no impact findings", envelope.State, packet.ReadinessStateReadyZeroFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", envelope.MissingEvidence)
	}
	if len(envelope.UnsupportedTargets) != 0 {
		t.Fatalf("unsupported_targets = %#v, want empty for supported NuGet evidence", envelope.UnsupportedTargets)
	}
}

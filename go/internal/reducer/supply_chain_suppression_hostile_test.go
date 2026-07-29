// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEvaluateSupplyChainSuppressionUnknownJustificationStaysVisible(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:        "CVE-2026-0001",
		PackageID:    testImpactPackageID,
		RepositoryID: testImpactRepositoryID,
	}
	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-unknown",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: "external_unknown",
		AuthoredAt:    time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:        "CVE-2026-0001",
			PackageID:    testImpactPackageID,
			RepositoryID: testImpactRepositoryID,
		},
	}

	decision := EvaluateSupplyChainSuppression(
		finding,
		[]vulnerabilitySuppression{suppression},
		time.Date(2026, 7, 27, 13, 0, 0, 0, time.UTC),
	)
	if decision.State != SupplyChainSuppressionStateActive {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateActive)
	}
}

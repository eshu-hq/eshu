// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestBuildSupplyChainImpactExplanationMapsDeploymentOnlyCatalogAnchorGap(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-repository-catalog-only"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-repository-catalog-only",
				CVEID:               "CVE-2026-1548",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				DeploymentIDs:       []string{"deployment:example-api"},
				EvidencePath: []string{
					"reducer_package_consumption_correlation",
					"reducer_platform_materialization",
					serviceCatalogCorrelationFactKind,
				},
				MissingEvidence: []string{
					"environment evidence missing",
					serviceCatalogAnchorMissingReason,
				},
				EvidenceFactIDs: []string{"catalog-1"},
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("catalog-1", serviceCatalogCorrelationFactKind, map[string]any{
					"repository_id": "repo://example/api",
					"outcome":       "exact",
				}),
			},
		},
		SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
	)

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	impactPath, ok := payload["impact_path"].([]any)
	if !ok {
		t.Fatalf("impact_path = %#v, want structured path", payload["impact_path"])
	}
	assertImpactPathContainsHop(t, impactPath, "deployment", "present")
	assertImpactPathContainsHop(t, impactPath, "environment", "missing_evidence")
	assertImpactPathContainsHop(t, impactPath, "service", "missing_evidence")
	assertImpactPathContainsMissingEvidence(t, impactPath, "service", serviceCatalogAnchorMissingReason)
	assertJSONListContains(t, payload["missing_evidence"], serviceCatalogAnchorMissingReason)
}

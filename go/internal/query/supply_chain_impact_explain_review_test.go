// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestBuildSupplyChainImpactExplanationOmitsEmptyDependencyChain(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-empty-chain"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:    "finding-empty-chain",
				CVEID:        "CVE-2026-0101",
				PackageID:    "pkg:npm/no-chain",
				ImpactStatus: "possibly_affected",
			},
		},
		SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
	)

	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := fields["dependency_chain"]; ok {
		t.Fatalf("payload contains top-level dependency_chain for empty evidence: %s", payload)
	}
}

func TestBuildSupplyChainImpactExplanationUsesEvidenceDerivedDependencyChainForMissingEvidence(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-evidence-chain"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:    "finding-evidence-chain",
				CVEID:        "CVE-2026-0102",
				PackageID:    "pkg:npm/transitive",
				ImpactStatus: "affected_exact",
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("consume-chain", "reducer_package_consumption_correlation", map[string]any{
					"dependency_path":   []any{"api", "framework", "transitive"},
					"dependency_depth":  float64(3),
					"direct_dependency": false,
					"relative_path":     "package-lock.json",
				}),
			},
		},
		SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
	)

	if got.DependencyChain == nil {
		t.Fatal("DependencyChain = nil, want evidence-derived chain")
	}
	if got.DependencyChain.Depth != 3 {
		t.Fatalf("DependencyChain.Depth = %d, want 3", got.DependencyChain.Depth)
	}
	if containsString(got.MissingEvidence, "dependency_chain") {
		t.Fatalf("MissingEvidence = %#v, must not include dependency_chain when evidence provides a path", got.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactExplanationDoesNotTreatClockPathAsLockfile(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-clock-path"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:    "finding-clock-path",
				CVEID:        "CVE-2026-0103",
				PackageID:    "pkg:golang/example",
				ImpactStatus: "possibly_affected",
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("source-path", "reducer_package_consumption_correlation", map[string]any{
					"relative_path": "src/clock.go",
				}),
			},
		},
		SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
	)

	if !containsString(got.Anchors.ManifestPaths, "src/clock.go") {
		t.Fatalf("ManifestPaths = %#v, want src/clock.go", got.Anchors.ManifestPaths)
	}
	if containsString(got.Anchors.LockfilePaths, "src/clock.go") {
		t.Fatalf("LockfilePaths = %#v, must not classify src/clock.go as a lockfile", got.Anchors.LockfilePaths)
	}
}

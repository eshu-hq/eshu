// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestBuildSupplyChainImpactExplanationReturnsRuntimePathAndMissingHops(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-runtime"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-runtime",
				CVEID:               "CVE-2026-0598",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "deployed_image",
				RepositoryID:        "repo://example/api",
				SubjectDigest:       "sha256:runtime",
				EvidencePath: []string{
					"vulnerability.cve",
					"vulnerability.affected_package",
					"reducer_package_consumption_correlation",
					"sbom.component",
					"reducer_sbom_attestation_attachment",
					"reducer_container_image_identity",
					"reducer_ci_cd_run_correlation",
					"reducer_service_catalog_correlation",
				},
				MissingEvidence: []string{"fixed_version"},
				EvidenceFactIDs: []string{"deploy-1", "catalog-1"},
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("deploy-1", "reducer_ci_cd_run_correlation", map[string]any{
					"artifact_digest": "sha256:runtime",
					"image_ref":       "registry.example/api@sha256:runtime",
					"environment":     "prod",
					"outcome":         "exact",
				}),
				explanationFact("catalog-1", "reducer_service_catalog_correlation", map[string]any{
					"repository_id": "repo://example/api",
					"service_id":    "service:example-api",
					"workload_id":   "workload:example-api",
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
	assertImpactPathExcludesHop(t, impactPath, "fixed_version")
	assertImpactPathContainsHop(t, impactPath, "repository", "present")
	assertImpactPathContainsHop(t, impactPath, "image", "present")
	assertImpactPathContainsHop(t, impactPath, "workload", "present")
	assertImpactPathContainsHop(t, impactPath, "service", "present")
	assertImpactPathContainsHop(t, impactPath, "environment", "present")
	anchors := payload["anchors"].(map[string]any)
	assertJSONListContains(t, anchors["services"], "service:example-api")
	assertJSONListContains(t, anchors["environments"], "prod")
	assertJSONListContains(t, payload["missing_evidence"], "fixed_version")
}

func TestBuildSupplyChainImpactExplanationReturnsSemanticMissingHops(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-repo-only"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-repo-only",
				CVEID:               "CVE-2026-0682",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				EvidencePath: []string{
					"vulnerability.cve",
					"vulnerability.affected_package",
					"reducer_package_consumption_correlation",
				},
				MissingEvidence: []string{
					"image or SBOM attachment evidence missing",
					"deployment exposure evidence missing",
					"workload evidence missing",
					"service evidence missing",
				},
				EvidenceFactIDs: []string{"consume-1"},
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("consume-1", "reducer_package_consumption_correlation", map[string]any{
					"repository_id": "repo://example/api",
					"package_id":    "pkg:npm/example",
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
	assertImpactPathContainsHop(t, impactPath, "repository", "present")
	assertImpactPathContainsHop(t, impactPath, "image", "missing_evidence")
	assertImpactPathContainsHop(t, impactPath, "workload", "missing_evidence")
	assertImpactPathContainsHop(t, impactPath, "service", "missing_evidence")
	assertImpactPathContainsHop(t, impactPath, "environment", "missing_evidence")
}

func TestBuildSupplyChainImpactExplanationMapsPreciseRuntimeMissingHops(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-workload-only"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-workload-only",
				CVEID:               "CVE-2026-1420",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				WorkloadIDs:         []string{"workload:example-api"},
				MissingEvidence: []string{
					"runtime deployment evidence not linked to vulnerable package",
					"service catalog correlation evidence missing",
				},
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
	assertImpactPathContainsHop(t, impactPath, "workload", "present")
	assertImpactPathContainsHop(t, impactPath, "service", "missing_evidence")
	assertImpactPathContainsHop(t, impactPath, "environment", "missing_evidence")
}

func TestBuildSupplyChainImpactExplanationUsesCatalogAnchorMissingReason(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-catalog-anchor"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-catalog-anchor",
				CVEID:               "CVE-2026-1548",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				WorkloadIDs:         []string{"workload:example-api"},
				EvidencePath: []string{
					"reducer_package_consumption_correlation",
					serviceCatalogCorrelationFactKind,
				},
				EvidenceFactIDs: []string{
					"consume-1",
					serviceCatalogCorrelationFactKind + ":catalog-1",
				},
				MissingEvidence: []string{
					serviceCatalogCorrelationMissingReason,
				},
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("consume-1", "reducer_package_consumption_correlation", map[string]any{
					"repository_id": "repo://example/api",
					"package_id":    "pkg:npm/example",
				}),
				explanationFact("catalog-1", serviceCatalogCorrelationFactKind, map[string]any{
					"repository_id": "repo://example/api",
					"workload_id":   "workload:example-api",
					"outcome":       "exact",
				}),
			},
		},
		SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
	)

	if got.Finding == nil {
		t.Fatal("Finding = nil, want explained finding")
	}
	if containsString(got.Finding.MissingEvidence, serviceCatalogCorrelationMissingReason) {
		t.Fatalf("Finding.MissingEvidence = %#v, must not claim present catalog correlation is missing", got.Finding.MissingEvidence)
	}
	if !containsString(got.Finding.MissingEvidence, serviceCatalogAnchorMissingReason) {
		t.Fatalf("Finding.MissingEvidence = %#v, want %s", got.Finding.MissingEvidence, serviceCatalogAnchorMissingReason)
	}
	assertImpactPathHopMissingReason(t, got.ImpactPath, "service", serviceCatalogAnchorMissingReason)
}

func TestBuildSupplyChainImpactExplanationKeepsRepositoryOnlyCatalogHop(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-catalog-repo-only"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-catalog-repo-only",
				CVEID:               "CVE-2026-1548",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				WorkloadIDs:         []string{"workload:example-api"},
				EvidencePath: []string{
					"reducer_package_consumption_correlation",
					"reducer_workload_identity",
					serviceCatalogCorrelationFactKind,
				},
				EvidenceFactIDs: []string{"consume-1", "workload-1", "catalog-1"},
				MissingEvidence: []string{
					"runtime deployment evidence not linked to vulnerable package",
					serviceCatalogAnchorMissingReason,
				},
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("catalog-1", serviceCatalogCorrelationFactKind, map[string]any{
					"scope_id": "git-repository-scope:repo://example/api",
					"outcome":  "exact",
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
	assertImpactPathContainsHop(t, impactPath, "workload", "present")
	assertImpactPathContainsHop(t, impactPath, "service", "missing_evidence")
	assertImpactPathContainsMissingEvidence(t, impactPath, "service", serviceCatalogAnchorMissingReason)
	assertJSONListContains(t, payload["missing_evidence"], serviceCatalogAnchorMissingReason)
}

func TestBuildSupplyChainImpactExplanationReturnsDeploymentLaneHopWithoutEnvironment(t *testing.T) {
	t.Parallel()

	got := BuildSupplyChainImpactExplanation(
		SupplyChainImpactExplanationFilter{FindingID: "finding-deployment-lane"},
		SupplyChainImpactExplanationRow{
			Finding: SupplyChainImpactFindingRow{
				FindingID:           "finding-deployment-lane",
				CVEID:               "CVE-2026-1491",
				PackageID:           "pkg:npm/example",
				ImpactStatus:        "affected_exact",
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				WorkloadIDs:         []string{"workload:example-api"},
				DeploymentIDs:       []string{"deployment:example-api"},
				EvidencePath: []string{
					"reducer_package_consumption_correlation",
					"reducer_workload_identity",
					"reducer_platform_materialization",
				},
				EvidenceFactIDs: []string{"consume-1", "workload-1", "deployment-1"},
				MissingEvidence: []string{
					"environment evidence missing",
					"service catalog evidence unresolved",
				},
			},
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("consume-1", "reducer_package_consumption_correlation", map[string]any{
					"repository_id": "repo://example/api",
					"package_id":    "pkg:npm/example",
				}),
				explanationFact("workload-1", "reducer_workload_identity", map[string]any{
					"scope_id":    "git-repository-scope:repo://example/api",
					"entity_keys": []any{"workload:example-api"},
				}),
				explanationFact("deployment-1", "reducer_platform_materialization", map[string]any{
					"scope_id":    "git-repository-scope:repo://example/api",
					"entity_keys": []any{"deployment:example-api"},
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
	anchors := payload["anchors"].(map[string]any)
	assertJSONListContains(t, anchors["deployments"], "deployment:example-api")
}

func assertImpactPathExcludesHop(t *testing.T, raw []any, unwanted string) {
	t.Helper()
	for _, entry := range raw {
		hop, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("impact_path entry = %T, want object", entry)
		}
		if hop["hop"] == unwanted {
			t.Fatalf("impact_path unexpectedly includes %q: %#v", unwanted, raw)
		}
	}
}

func assertImpactPathContainsHop(t *testing.T, raw []any, wantHop string, wantStatus string) {
	t.Helper()
	for _, entry := range raw {
		hop, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("impact_path entry = %T, want object", entry)
		}
		if hop["hop"] == wantHop {
			if hop["status"] != wantStatus {
				t.Fatalf("impact_path hop %q status = %#v, want %#v", wantHop, hop["status"], wantStatus)
			}
			return
		}
	}
	t.Fatalf("impact_path missing hop %q: %#v", wantHop, raw)
}

func assertImpactPathHopMissingReason(
	t *testing.T,
	impactPath []SupplyChainImpactPathHop,
	wantHop string,
	wantReason string,
) {
	t.Helper()
	for _, hop := range impactPath {
		if hop.Hop != wantHop {
			continue
		}
		if hop.Status != "missing_evidence" {
			t.Fatalf("impact_path hop %q status = %#v, want missing_evidence", wantHop, hop.Status)
		}
		if !containsString(hop.MissingEvidence, wantReason) {
			t.Fatalf("impact_path hop %q missing_evidence = %#v, want %q", wantHop, hop.MissingEvidence, wantReason)
		}
		return
	}
	t.Fatalf("impact_path missing hop %q: %#v", wantHop, impactPath)
}

func assertImpactPathContainsMissingEvidence(t *testing.T, raw []any, wantHop string, wantReason string) {
	t.Helper()
	for _, entry := range raw {
		hop, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("impact_path entry = %T, want object", entry)
		}
		if hop["hop"] != wantHop {
			continue
		}
		assertJSONListContains(t, hop["missing_evidence"], wantReason)
		return
	}
	t.Fatalf("impact_path missing hop %q: %#v", wantHop, raw)
}

func assertJSONListContains(t *testing.T, raw any, want string) {
	t.Helper()
	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("value = %T, want []any containing %q", raw, want)
	}
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}

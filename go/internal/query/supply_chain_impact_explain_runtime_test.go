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
	anchors := payload["anchors"].(map[string]any)
	assertJSONListContains(t, anchors["services"], "service:example-api")
	assertJSONListContains(t, anchors["environments"], "prod")
	assertJSONListContains(t, payload["missing_evidence"], "fixed_version")
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

package reducer

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testImpactServiceID  = "service:example-api"
	testImpactWorkloadID = "workload:example-api"
	testImpactEnv        = "prod"
)

func TestBuildSupplyChainImpactFindingsConnectsRuntimeEvidencePath(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0598", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0598", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithChain("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3", []string{"api", "example"}, 2, false),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			"registry.example/api@"+testImpactSubjectDigest,
			string(ContainerImageIdentityExactDigest),
		),
		cicdRunCorrelationImpactFact(
			"deploy-1",
			testImpactSubjectDigest,
			"registry.example/api@"+testImpactSubjectDigest,
			testImpactRepositoryID,
			testImpactEnv,
			string(CICDRunCorrelationExact),
		),
		serviceCatalogCorrelationImpactFact(
			"catalog-1",
			testImpactRepositoryID,
			testImpactServiceID,
			testImpactWorkloadID,
			string(ServiceCatalogCorrelationExact),
			"matches",
			false,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0598"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want deployed_image", got.RuntimeReachability)
	}
	if got.ImageRef != "registry.example/api@"+testImpactSubjectDigest {
		t.Fatalf("ImageRef = %q, want digest reference", got.ImageRef)
	}
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.ServiceIDs, testImpactServiceID)
	assertContainsString(t, got.Environments, testImpactEnv)
	assertContainsString(t, got.EvidencePath, cicdRunCorrelationFactKind)
	assertContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertNotContainsString(t, got.MissingEvidence, "deployment exposure evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
}

func TestBuildSupplyChainImpactFindingsKeepsImageOnlyRuntimeHopsMissing(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0599", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0599", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			"",
			"registry.example/api@"+testImpactSubjectDigest,
			string(ContainerImageIdentityExactDigest),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0599"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want image-only evidence to remain undeployed", got.RuntimeReachability)
	}
	assertContainsString(t, got.MissingEvidence, "repository dependency evidence missing")
	assertContainsString(t, got.MissingEvidence, "deployment exposure evidence missing")
	assertContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertContainsString(t, got.MissingEvidence, "service evidence missing")
}

func TestBuildSupplyChainImpactFindingsDoesNotPromoteAmbiguousImageIdentity(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0600", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0600", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFactWithOutcome(
			"image-1",
			testImpactSubjectDigest,
			testImpactRepositoryID,
			"registry.example/api:latest",
			string(ContainerImageIdentityAmbiguousTag),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0600"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.SubjectDigest != "" {
		t.Fatalf("SubjectDigest = %q, want blank when image identity is ambiguous", got.SubjectDigest)
	}
	assertNotContainsString(t, got.EvidencePath, containerImageIdentityFactKind)
	assertContainsString(t, got.MissingEvidence, "image identity evidence ambiguous")
}

func TestBuildSupplyChainImpactFindingsKeepsStaleServiceEvidenceMissing(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0601", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0601", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		serviceCatalogCorrelationImpactFact(
			"catalog-1",
			testImpactRepositoryID,
			testImpactServiceID,
			testImpactWorkloadID,
			string(ServiceCatalogCorrelationStale),
			"stale",
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0601"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertNotContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertContainsString(t, got.MissingEvidence, "service catalog evidence stale")
}

func assertNotContainsString(t *testing.T, values []string, wantMissing string) {
	t.Helper()
	if slices.Contains(values, wantMissing) {
		t.Fatalf("%#v unexpectedly contains %q", values, wantMissing)
	}
}

func cicdRunCorrelationImpactFact(
	factID string,
	artifactDigest string,
	imageRef string,
	repositoryID string,
	environment string,
	outcome string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: cicdRunCorrelationFactKind,
		Payload: map[string]any{
			"artifact_digest":  artifactDigest,
			"image_ref":        imageRef,
			"repository_id":    repositoryID,
			"environment":      environment,
			"outcome":          outcome,
			"canonical_target": "container_image",
			"canonical_writes": 1,
		},
	}
}

func serviceCatalogCorrelationImpactFact(
	factID string,
	repositoryID string,
	serviceID string,
	workloadID string,
	outcome string,
	driftStatus string,
	provenanceOnly bool,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: serviceCatalogCorrelationFactKind,
		Payload: map[string]any{
			"repository_id":     repositoryID,
			"service_id":        serviceID,
			"workload_id":       workloadID,
			"outcome":           outcome,
			"drift_status":      driftStatus,
			"provenance_only":   provenanceOnly,
			"evidence_fact_ids": []any{strings.TrimPrefix(factID, "catalog-")},
		},
	}
}

func containerImageIdentityImpactFactWithOutcome(
	factID string,
	digest string,
	repositoryID string,
	imageRef string,
	outcome string,
) facts.Envelope {
	envelope := containerImageIdentityImpactFact(factID, digest, repositoryID)
	envelope.Payload["image_ref"] = imageRef
	envelope.Payload["outcome"] = outcome
	if outcome == string(ContainerImageIdentityAmbiguousTag) || outcome == string(ContainerImageIdentityStaleTag) {
		envelope.Payload["canonical_writes"] = 0
	}
	return envelope
}

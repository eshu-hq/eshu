// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestBuildSupplyChainImpactFindingsKeepsRepositoryOnlyRuntimeHopsMissing(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0682", 8.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0682", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0682"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	if len(got.WorkloadIDs) != 0 {
		t.Fatalf("WorkloadIDs = %#v, want no workload without workload evidence", got.WorkloadIDs)
	}
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no service without service evidence", got.ServiceIDs)
	}
	assertContainsString(t, got.MissingEvidence, "deployment exposure evidence missing")
	assertContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertContainsString(t, got.MissingEvidence, "service evidence missing")
}

func TestBuildSupplyChainImpactFindingsAttachesWorkloadIdentityWithoutServiceCatalog(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0680", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0680", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		workloadIdentityImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0680"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "workload-1")
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "deployment exposure evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	assertContainsString(t, got.MissingEvidence, "runtime deployment evidence not linked to vulnerable package")
	assertContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no fabricated service identity", got.ServiceIDs)
	}
}

func TestBuildSupplyChainImpactFindingsReportsScopedUnresolvedServiceCatalogEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1491", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1491", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		workloadIdentityRepositoryScopeImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
		serviceCatalogCorrelationRepositoryScopeImpactFact(
			"catalog-1",
			testImpactRepositoryID,
			string(ServiceCatalogCorrelationUnresolved),
			"missing",
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1491"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertNotContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertContainsString(t, got.MissingEvidence, "service catalog evidence unresolved")
	assertNotContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no fabricated service identity from unresolved service evidence", got.ServiceIDs)
	}
}

func TestBuildSupplyChainImpactFindingsConsumesRepositoryOnlyServiceCatalogEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1548", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1548", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		workloadIdentityRepositoryScopeImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
		serviceCatalogCorrelationRepositoryScopeImpactFact(
			"catalog-1",
			testImpactRepositoryID,
			string(ServiceCatalogCorrelationExact),
			"matches",
			false,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1548"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "catalog-1")
	assertContainsString(t, got.MissingEvidence, "service/workload catalog anchor missing")
	assertNotContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no fabricated service identity from repository-only catalog evidence", got.ServiceIDs)
	}
}

func TestBuildSupplyChainImpactFindingsAttachesDeploymentLaneEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1492", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1492", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		workloadIdentityRepositoryScopeImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
		platformMaterializationImpactFact("deployment-1", testImpactRepositoryID, "deployment:example-api"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1492"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.DeploymentIDs, "deployment:example-api")
	assertContainsString(t, got.EvidencePath, "reducer_platform_materialization")
	assertContainsString(t, got.EvidenceFactIDs, "deployment-1")
	assertNotContainsString(t, got.MissingEvidence, "runtime deployment evidence not linked to vulnerable package")
	assertContainsString(t, got.MissingEvidence, "environment evidence missing")
	assertContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	if len(got.Environments) != 0 {
		t.Fatalf("Environments = %#v, want no fabricated environment from deployment lane", got.Environments)
	}
}

func TestBuildSupplyChainImpactFindingsAttachesDeploymentAndWorkloadWithoutServiceCatalog(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0681", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0681", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
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
		workloadIdentityImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0681"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, want deployed_image", got.RuntimeReachability)
	}
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.Environments, testImpactEnv)
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertContainsString(t, got.EvidencePath, cicdRunCorrelationFactKind)
	assertNotContainsString(t, got.MissingEvidence, "deployment exposure evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	assertContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no fabricated service identity", got.ServiceIDs)
	}
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
			"entity_ref":        "api:default/example-api",
			"owner_ref":         "team:default/platform",
			"outcome":           outcome,
			"drift_status":      driftStatus,
			"provenance_only":   provenanceOnly,
			"evidence_fact_ids": []any{strings.TrimPrefix(factID, "catalog-")},
		},
	}
}

func serviceCatalogCorrelationRepositoryScopeImpactFact(
	factID string,
	repositoryID string,
	outcome string,
	driftStatus string,
	provenanceOnly bool,
) facts.Envelope {
	scopeID := "git-repository-scope:" + repositoryID
	return facts.Envelope{
		FactID:   factID,
		FactKind: serviceCatalogCorrelationFactKind,
		ScopeID:  scopeID,
		Payload: map[string]any{
			"scope_id":        scopeID,
			"outcome":         outcome,
			"drift_status":    driftStatus,
			"provenance_only": provenanceOnly,
		},
	}
}

func workloadIdentityImpactFact(factID string, repositoryID string, workloadID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: workloadIdentityFactKind,
		ScopeID:  repositoryID,
		Payload: map[string]any{
			"scope_id":    repositoryID,
			"entity_keys": []any{workloadID},
		},
	}
}

func workloadIdentityRepositoryScopeImpactFact(factID string, repositoryID string, workloadID string) facts.Envelope {
	scopeID := "git-repository-scope:" + repositoryID
	return facts.Envelope{
		FactID:   factID,
		FactKind: workloadIdentityFactKind,
		ScopeID:  scopeID,
		Payload: map[string]any{
			"scope_id":          scopeID,
			"related_scope_ids": []any{scopeID},
			"entity_keys":       []any{workloadID},
		},
	}
}

func platformMaterializationImpactFact(factID string, repositoryID string, deploymentID string) facts.Envelope {
	scopeID := "git-repository-scope:" + repositoryID
	return facts.Envelope{
		FactID:   factID,
		FactKind: platformMaterializationFactKind,
		ScopeID:  scopeID,
		Payload: map[string]any{
			"scope_id":          scopeID,
			"entity_keys":       []any{deploymentID},
			"related_scope_ids": []any{scopeID},
			"reducer_domain":    "deployment_mapping",
			"canonical_writes":  1,
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

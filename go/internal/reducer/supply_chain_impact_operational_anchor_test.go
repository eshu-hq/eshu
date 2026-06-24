// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsAttachesRepositoryScopedOperationalAnchors(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1668", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1668", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		workloadIdentityRepositoryScopeImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
		serviceCatalogCorrelationImpactFact(
			"catalog-1",
			testImpactRepositoryID,
			testImpactServiceID,
			testImpactWorkloadID,
			string(ServiceCatalogCorrelationExact),
			"matches",
			false,
		),
		cicdRunCorrelationImpactFact(
			"deploy-1",
			"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"registry.example/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			testImpactRepositoryID,
			testImpactEnv,
			string(CICDRunCorrelationExact),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1668"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.ServiceIDs, testImpactServiceID)
	assertContainsString(t, got.Environments, testImpactEnv)
	assertContainsString(t, got.CatalogEntityRefs, "api:default/example-api")
	assertContainsString(t, got.CatalogOwnerRefs, "team:default/platform")
	assertContainsString(t, got.EvidencePath, workloadIdentityFactKind)
	assertContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertContainsString(t, got.EvidencePath, cicdRunCorrelationFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "deploy-1")
	assertNotContainsString(t, got.MissingEvidence, "runtime deployment evidence not linked to vulnerable package")
	assertNotContainsString(t, got.MissingEvidence, "environment evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	if got.RuntimeReachability == "deployed_image" {
		t.Fatalf("RuntimeReachability = %q, must not promote without package-to-image proof", got.RuntimeReachability)
	}
}

func TestBuildSupplyChainImpactFindingsDoesNotAttachRepositoryOnlyEnvironment(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1669", 8.7),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1669", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		cicdRunCorrelationImpactFact(
			"deploy-1",
			"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"registry.example/api@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			testImpactRepositoryID,
			testImpactEnv,
			string(CICDRunCorrelationExact),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1669"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if len(got.Environments) != 0 {
		t.Fatalf("Environments = %#v, want no environment without workload, service, or deployment anchor", got.Environments)
	}
	assertContainsString(t, got.MissingEvidence, "deployment exposure evidence missing")
	assertContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertContainsString(t, got.MissingEvidence, "service evidence missing")
}

func TestBuildSupplyChainImpactFindingsKeepsProvenanceOnlyDeploymentEnvironmentMissing(t *testing.T) {
	t.Parallel()

	deployment := cicdRunCorrelationImpactFact(
		"deploy-1",
		"",
		"",
		testImpactRepositoryID,
		testImpactEnv,
		string(CICDRunCorrelationDerived),
	)
	deployment.Payload["provenance_only"] = true
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1670", 8.9),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1670", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		workloadIdentityRepositoryScopeImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
		deployment,
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-1670"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if len(got.Environments) != 0 {
		t.Fatalf("Environments = %#v, want no environment from provenance-only deployment evidence", got.Environments)
	}
	assertContainsString(t, got.MissingEvidence, "deployment evidence provenance-only")
	assertContainsString(t, got.MissingEvidence, "runtime deployment evidence not linked to vulnerable package")
}

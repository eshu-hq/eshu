package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsConsumesDeploymentOnlyExactServiceCatalogEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-1548", 9.1),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-1548", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		platformMaterializationImpactFact("deployment-1", testImpactRepositoryID, "deployment:example-api"),
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
	assertContainsString(t, got.DeploymentIDs, "deployment:example-api")
	assertContainsString(t, got.EvidencePath, serviceCatalogCorrelationFactKind)
	assertContainsString(t, got.EvidencePath, platformMaterializationFactKind)
	assertContainsString(t, got.EvidenceFactIDs, "catalog-1")
	assertContainsString(t, got.MissingEvidence, "service/workload catalog anchor missing")
	assertNotContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no fabricated service identity from repository-only catalog evidence", got.ServiceIDs)
	}
	if len(got.WorkloadIDs) != 0 {
		t.Fatalf("WorkloadIDs = %#v, want no fabricated workload identity from repository-only catalog evidence", got.WorkloadIDs)
	}
}

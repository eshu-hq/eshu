package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Production fact shapes differ from the simplified in-repo fixtures: the
// vulnerability affected_package carries the canonical identity package_id
// (e.g. npm://registry.npmjs.org/<name>) and OSV's versionless package purl,
// while the SBOM component carries a version-qualified purl and historically no
// package_id. These constants reproduce that mismatch so the SBOM->image path
// is exercised the way the live ingester produces it.
const (
	crossScopeAffectedPackageID = "npm://registry.npmjs.org/lodash"
	crossScopeAffectedPURL      = "pkg:npm/lodash"
	crossScopeComponentPURL     = "pkg:npm/lodash@4.17.11"
)

// TestBuildSupplyChainImpactFindingsMatchesSBOMComponentByCanonicalPackageID
// proves the canonical-identity bridge: when the SBOM component carries the same
// package_id the vulnerability facts use, the impact finding resolves the
// image_sbom path even though the purls differ by version.
func TestBuildSupplyChainImpactFindingsMatchesSBOMComponentByCanonicalPackageID(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		affectedPackageWithPURLFact("affected-1", "CVE-2026-0001", crossScopeAffectedPackageID, crossScopeAffectedPURL, "npm", "lodash", "4.17.11", "4.17.21"),
		sbomComponentImpactFactWithPackageID("component-1", "doc-1", crossScopeComponentPURL, crossScopeAffectedPackageID),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.RuntimeReachability != "image_sbom" {
		t.Fatalf("RuntimeReachability = %q, want image_sbom", got.RuntimeReachability)
	}
	if got.SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, testImpactSubjectDigest)
	}
	if !strings.Contains(strings.Join(got.EvidencePath, " -> "), "sbom.component") {
		t.Fatalf("EvidencePath = %#v, want SBOM component path", got.EvidencePath)
	}
}

// TestBuildSupplyChainImpactFindingsMatchesSBOMComponentByVersionStrippedPURL
// proves the defense-in-depth purl bridge: a component that carries only a
// version-qualified purl (no canonical package_id) still resolves against an
// affected_package whose purl is versionless.
func TestBuildSupplyChainImpactFindingsMatchesSBOMComponentByVersionStrippedPURL(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		affectedPackageWithPURLFact("affected-1", "CVE-2026-0001", crossScopeAffectedPackageID, crossScopeAffectedPURL, "npm", "lodash", "4.17.11", "4.17.21"),
		sbomComponentImpactFact("component-1", "doc-1", crossScopeComponentPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	if got.RuntimeReachability != "image_sbom" {
		t.Fatalf("RuntimeReachability = %q, want image_sbom via version-stripped purl bridge", got.RuntimeReachability)
	}
}

// TestBuildSupplyChainImpactFindingsRefusesImagePathWithoutAttachment proves the
// widened component match does not weaken the honesty contract: a component that
// matches the affected package by version-stripped purl but carries no
// subject-digest/referrer attachment must not resolve the image_sbom path.
func TestBuildSupplyChainImpactFindingsRefusesImagePathWithoutAttachment(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		affectedPackageWithPURLFact("affected-1", "CVE-2026-0001", crossScopeAffectedPackageID, crossScopeAffectedPURL, "npm", "lodash", "4.17.11", "4.17.21"),
		sbomComponentImpactFact("component-1", "doc-1", crossScopeComponentPURL),
		// No sbom_attestation_attachment and no container_image_identity: the
		// SBOM is present but not attached to an owned image.
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	if got.RuntimeReachability == "image_sbom" {
		t.Fatalf("RuntimeReachability = image_sbom without attachment evidence; honesty contract regressed")
	}
	if got.ImageRef != "" {
		t.Fatalf("ImageRef = %q, want empty without attachment evidence", got.ImageRef)
	}
}

// TestSupplyChainImpactFilterExpandsSBOMComponentByCanonicalPackageID proves the
// cross-scope active-load filter follows the component's canonical package_id so
// the storage active-load can reach affected_package facts in a sibling scope.
func TestSupplyChainImpactFilterExpandsSBOMComponentByCanonicalPackageID(t *testing.T) {
	t.Parallel()

	filter := supplyChainImpactFilter([]facts.Envelope{
		sbomComponentImpactFactWithPackageID("component-1", "doc-1", crossScopeComponentPURL, crossScopeAffectedPackageID),
	})

	if !stringSliceContains(filter.PackageIDs, crossScopeAffectedPackageID) {
		t.Fatalf("filter.PackageIDs = %#v, want canonical package_id %q from SBOM component", filter.PackageIDs, crossScopeAffectedPackageID)
	}
}

// affectedPackageWithPURLFact builds an affected_package fact that, unlike the
// simplified helper, sets the versionless package purl the OSV collector emits.
func affectedPackageWithPURLFact(
	factID string,
	cveID string,
	packageID string,
	purl string,
	ecosystem string,
	name string,
	affectedVersion string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":            cveID,
			"package_id":        packageID,
			"purl":              purl,
			"ecosystem":         ecosystem,
			"package_name":      name,
			"affected_versions": []any{affectedVersion},
			"fixed_versions":    []any{fixedVersion},
		},
	}
}

// sbomComponentImpactFactWithPackageID builds an SBOM component fact that carries
// the canonical package_id, mirroring the collector after the canonical-identity
// fix.
func sbomComponentImpactFactWithPackageID(factID string, documentID string, purl string, packageID string) facts.Envelope {
	envelope := sbomComponentImpactFact(factID, documentID, purl)
	envelope.Payload["package_id"] = packageID
	return envelope
}

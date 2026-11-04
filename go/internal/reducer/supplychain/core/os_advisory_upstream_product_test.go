// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// nvdProductEnvelopes is an NVD record with a CPE match an SBOM component on an
// identified image satisfies: the input that reads one affected_derived product
// finding on its own.
func nvdProductEnvelopes(cve string, score float64) []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-product", cve, score),
		vulnerabilityAffectedProductFact("product-1", cve, testImpactProductCriteria, testImpactMatchCriteriaID, true),
		sbomComponentCPEImpactFact("component-1", "doc-1", testImpactProductCriteria),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	}
}

// TestUpstreamCopiesDoNotHideProductFindings: the classifier skips an NVD
// product when the group has package facts. A package filed under the CVE only
// because a distro record names it must not trip that, or a real CPE match on an
// image disappears, even when the distro package is not installed.
//
// Measured on the input below: base e3d816172a reads 1 finding (affected_derived,
// cvss 8.6); c35a68c258 read 0.
func TestUpstreamCopiesDoNotHideProductFindings(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-0101"
	base := nvdProductEnvelopes(cve, 8.6)
	want := BuildSupplyChainImpactFindings(base)
	if len(want) != 1 {
		t.Fatalf("baseline findings = %d, want 1: %#v", len(want), want)
	}
	distro := convertedOSVRecord(t, debianRecord("DEBIAN-CVE-2026-0101", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)
	for order, shuffled := range orderings(append(slices.Clone(base), distro...)) {
		got := BuildSupplyChainImpactFindings(shuffled)
		if len(got) != 1 {
			t.Fatalf("%s: findings = %d, want the 1 product finding: %#v", order, len(got), got)
		}
		assertSupplyChainImpactStatus(t, got[0], SupplyChainImpactAffectedDerived)
		if got[0].CVSSScore != 8.6 {
			t.Errorf("%s: CVSSScore = %v, want the NVD record's 8.6", order, got[0].CVSSScore)
		}
		if got[0].AdvisoryID != want[0].AdvisoryID {
			t.Errorf("%s: AdvisoryID = %q, want %q: the distro copy must not name a product finding", order, got[0].AdvisoryID, want[0].AdvisoryID)
		}
		if gotID, wantID := supplyChainImpactFindingID(got[0]), supplyChainImpactFindingID(want[0]); gotID != wantID {
			t.Errorf("%s: finding_id = %q, want the baseline %q", order, gotID, wantID)
		}
	}
}

// TestUpstreamCopiesBesideAnInstalledPackageReadTwoFindings: with the distro
// package installed the same CVE reads a package finding and a product finding.
func TestUpstreamCopiesBesideAnInstalledPackageReadTwoFindings(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-0101"
	envelopes := append(nvdProductEnvelopes(cve, 8.6),
		convertedOSVRecord(t, debianRecord("DEBIAN-CVE-2026-0101", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)...)
	envelopes = append(envelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope())
	for order, shuffled := range orderings(envelopes) {
		got := BuildSupplyChainImpactFindings(shuffled)
		if len(got) != 2 {
			t.Fatalf("%s: findings = %d, want one package and one product finding: %#v", order, len(got), got)
		}
		var pkg, product SupplyChainImpactFinding
		for _, finding := range got {
			if finding.ProductCriteria != "" {
				product = finding
			} else {
				pkg = finding
			}
		}
		if pkg.PackageID == "" || pkg.AdvisoryID != "DEBIAN-CVE-2026-0101" {
			t.Errorf("%s: package finding = %#v, want one named by the distro record", order, pkg)
		}
		if product.ProductCriteria == "" || product.Status != SupplyChainImpactAffectedDerived {
			t.Errorf("%s: product finding = %#v, want affected_derived", order, product)
		}
		if supplyChainImpactFindingID(pkg) == supplyChainImpactFindingID(product) {
			t.Errorf("%s: the two findings share a finding_id", order)
		}
		if pkg.CVSSScore != 8.6 || product.CVSSScore != 8.6 {
			t.Errorf("%s: scores = %v/%v, want both 8.6 from the NVD record", order, pkg.CVSSScore, product.CVSSScore)
		}
	}
}

// TestNativePackagesStillSkipProducts is the base-parity control: a group with a
// package natively keyed to it still emits no product finding, for a language
// CVE and for a key that holds both a native package and a copy. Emitting them
// would be a new row class for every language CVE.
func TestNativePackagesStillSkipProducts(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-0101"
	const packageID = "npm://registry.npmjs.org/lodash"
	native := append(nvdProductEnvelopes(cve, 8.6),
		vulnerabilityAffectedPackageFact("ghsa-pkg", cve, packageID, "npm", "lodash", "4.17.20", "4.17.21"),
		packageConsumptionFactWithRange("consumption-1", packageID, testImpactRepositoryID, "4.17.20"))
	tests := map[string][]facts.Envelope{
		"language package alone": native,
		"native package beside a distro copy": append(slices.Clone(native),
			convertedOSVRecord(t, debianRecord("DEBIAN-CVE-2026-0101", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)...),
	}
	for name, envelopes := range tests {
		for order, shuffled := range orderings(envelopes) {
			got := BuildSupplyChainImpactFindings(shuffled)
			if len(got) != 1 {
				t.Fatalf("%s %s: findings = %d, want the 1 package finding: %#v", name, order, len(got), got)
			}
			if got[0].ProductCriteria != "" || got[0].PackageID != packageID {
				t.Errorf("%s %s: finding = %#v, want the package finding only", name, order, got[0])
			}
		}
	}
}

// TestProductPathJudgesOnlyNativeObservations: a product finding reads the facts
// natively keyed to the CVE. A distro copy with no NVD record beside it adds no
// finding, and a live distro copy does not lift the NVD record's retraction.
func TestProductPathJudgesOnlyNativeObservations(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-0101"
	distro := convertedOSVRecord(t, debianRecord("DEBIAN-CVE-2026-0101", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)

	withoutNVD := append(slices.Clone(nvdProductEnvelopes(cve, 8.6)[1:]), distro...)
	if got := BuildSupplyChainImpactFindings(withoutNVD); len(got) != 0 {
		t.Errorf("no NVD record: findings = %#v, want none, as with no distro record", got)
	}

	rejected := nvdProductEnvelopes(cve, 8.6)
	rejected[0].Payload["status"] = "Rejected"
	want := BuildSupplyChainImpactFindings(rejected)
	if len(want) != 1 || want[0].AdvisoryRetraction == nil {
		t.Fatalf("baseline = %#v, want one retracted product finding", want)
	}
	got := BuildSupplyChainImpactFindings(append(slices.Clone(rejected), distro...))
	if len(got) != 1 || got[0].AdvisoryRetraction == nil {
		t.Errorf("findings = %#v, want the product finding still retracted", got)
	}
}

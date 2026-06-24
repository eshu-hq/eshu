// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// These tests prove that supply-chain impact admission preserves per-source
// advisory provenance instead of flattening severity, fixed-version, and
// range truth into anonymous fields.
//
// The reducer must:
//
//   - select one severity using a documented per-ecosystem priority and
//     report the selected source;
//   - keep every other source's severity as an alternate;
//   - keep every source's fixed-version branches with the originating source;
//   - record the source-update timestamp for each contributing source;
//   - exclude withdrawn advisories from selection while still listing them;
//   - prefer a vendor advisory over upstream NVD for vendor ecosystems.

func TestSupplyChainCVEGroupRepresentativeUsesSourcePriority(t *testing.T) {
	t.Parallel()

	group := supplyChainCVEGroup{
		cveID: "CVE-2026-4242",
		observations: []supplyChainImpactCVE{
			{
				cveID:       "CVE-2026-4242",
				source:      "osv",
				advisoryID:  "GHSA-withdrawn",
				factID:      "fact-001",
				withdrawnAt: "2026-05-24T10:00:00Z",
			},
			{
				cveID:      "CVE-2026-4242",
				source:     "nvd",
				advisoryID: "CVE-2026-4242",
				factID:     "fact-002",
			},
			{
				cveID:      "CVE-2026-4242",
				source:     "glad",
				advisoryID: "GMS-2026-4242",
				factID:     "fact-003",
			},
		},
	}

	representative := group.representative()
	if representative.source != "glad" {
		t.Fatalf("representative source = %q, want glad priority over nvd while skipping withdrawn ghsa", representative.source)
	}
	if representative.factID != "fact-003" {
		t.Fatalf("representative factID = %q, want fact-003", representative.factID)
	}
}

func TestAdvisorySourcePriorityDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(1000, func() {
		_ = advisorySourcePriority("npm", "ghsa")
		_ = advisorySourcePriority("rpm", "redhat")
		_ = advisorySourcePriority("rpm", "nvd")
	})
	if allocations != 0 {
		t.Fatalf("advisorySourcePriority allocations = %v, want 0", allocations)
	}
}

func TestSupplyChainImpactPreservesGHSAvsNVDSeveritySources(t *testing.T) {
	t.Parallel()

	ghsa := vulnerabilityCVEFactWithProvenance(
		"ghsa-cve",
		"CVE-2026-7777",
		"osv",
		"GHSA-test-1",
		9.8,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CRITICAL",
		"2026-05-20T12:00:00Z",
	)
	nvd := vulnerabilityCVEFactWithProvenance(
		"nvd-cve",
		"CVE-2026-7777",
		"nvd",
		"CVE-2026-7777",
		5.5,
		"CVSS:3.1/AV:N/AC:L/PR:L/UI:R/S:U/C:L/I:L/A:N",
		"MEDIUM",
		"2026-05-18T09:00:00Z",
	)
	pkg := vulnerabilityAffectedPackageFactWithSource(
		"ghsa-affected",
		"CVE-2026-7777",
		"osv",
		"GHSA-test-1",
		"npm://registry.npmjs.org/parse-server",
		"npm",
		"parse-server",
		"5.4.0",
		"5.4.1",
	)
	consume := packageConsumptionFactWithRange("consume-1", "npm://registry.npmjs.org/parse-server", testImpactRepositoryID, "5.4.0")

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{ghsa, nvd, pkg, consume})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 consolidated finding (GHSA + NVD must collapse to one provenance row)", len(findings))
	}
	finding := findings[0]
	if finding.SeveritySource != "ghsa" {
		t.Fatalf("SeveritySource = %q, want ghsa (npm priority)", finding.SeveritySource)
	}
	if finding.CVSSScore != 9.8 {
		t.Fatalf("CVSSScore = %v, want 9.8 from selected GHSA source", finding.CVSSScore)
	}
	if finding.SeverityLabel != "CRITICAL" {
		t.Fatalf("SeverityLabel = %q, want CRITICAL from GHSA", finding.SeverityLabel)
	}
	if !findingHasAlternateSeverity(finding, "nvd", 5.5) {
		t.Fatalf("AlternateSeverities missing nvd=5.5: %#v", finding.AlternateSeverities)
	}
	if findingHasAlternateSeverity(finding, "ghsa", 9.8) {
		t.Fatalf("AlternateSeverities should not include the selected source: %#v", finding.AlternateSeverities)
	}
	if !findingHasAdvisorySource(finding, "ghsa", "GHSA-test-1", "2026-05-20T12:00:00Z") {
		t.Fatalf("AdvisorySources missing ghsa observation with update timestamp: %#v", finding.AdvisorySources)
	}
	if !findingHasAdvisorySource(finding, "nvd", "CVE-2026-7777", "2026-05-18T09:00:00Z") {
		t.Fatalf("AdvisorySources missing nvd observation with update timestamp: %#v", finding.AdvisorySources)
	}
}

func TestSupplyChainImpactVendorAdvisoryOverridesUpstream(t *testing.T) {
	t.Parallel()

	// Vendor advisory (Red Hat) for an OS-vendor ecosystem must beat upstream
	// NVD severity because vendor backports change applicability.
	vendor := vulnerabilityCVEFactWithProvenance(
		"redhat-cve",
		"CVE-2026-8888",
		"redhat",
		"RHSA-2026:1234",
		7.0,
		"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H",
		"HIGH",
		"2026-05-19T08:00:00Z",
	)
	nvd := vulnerabilityCVEFactWithProvenance(
		"nvd-cve",
		"CVE-2026-8888",
		"nvd",
		"CVE-2026-8888",
		9.8,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CRITICAL",
		"2026-05-15T10:00:00Z",
	)
	pkg := vulnerabilityAffectedPackageFactWithSource(
		"redhat-affected",
		"CVE-2026-8888",
		"redhat",
		"RHSA-2026:1234",
		"rpm://access.redhat.com/openssl",
		"rpm",
		"openssl",
		"1.1.1k-1",
		"1.1.1k-2.el8_5",
	)
	upstream := vulnerabilityAffectedPackageFactWithSource(
		"nvd-affected",
		"CVE-2026-8888",
		"nvd",
		"CVE-2026-8888",
		"rpm://access.redhat.com/openssl",
		"rpm",
		"openssl",
		"1.1.1k-1",
		"3.0.0",
	)
	consume := packageConsumptionFactWithRange("consume-1", "rpm://access.redhat.com/openssl", testImpactRepositoryID, "1.1.1k-1")

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{vendor, nvd, pkg, upstream, consume})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 consolidated finding", len(findings))
	}
	finding := findings[0]
	if finding.SeveritySource != "redhat" {
		t.Fatalf("SeveritySource = %q, want redhat (vendor override for rpm)", finding.SeveritySource)
	}
	if finding.FixedVersionSource != "redhat" {
		t.Fatalf("FixedVersionSource = %q, want redhat (vendor backport branch wins for rpm)", finding.FixedVersionSource)
	}
	if finding.FixedVersion != "1.1.1k-2.el8_5" {
		t.Fatalf("FixedVersion = %q, want vendor backport version", finding.FixedVersion)
	}
	if !findingHasFixedVersionBranch(finding, "1.1.1k-2.el8_5", "redhat") {
		t.Fatalf("FixedVersionBranches missing vendor branch: %#v", finding.FixedVersionBranches)
	}
	if !findingHasFixedVersionBranch(finding, "3.0.0", "nvd") {
		t.Fatalf("FixedVersionBranches missing upstream branch: %#v", finding.FixedVersionBranches)
	}
}

func TestSupplyChainImpactFallsBackWhenSelectedSourceLacksSeverity(t *testing.T) {
	t.Parallel()

	// GHSA outranks NVD for npm, but if GHSA does not publish a CVSS score
	// the reducer must fall back to the next-best source rather than
	// silently emit a zero severity.
	ghsaNoSeverity := vulnerabilityCVEFactWithProvenance(
		"ghsa-cve",
		"CVE-2026-9999",
		"osv",
		"GHSA-empty",
		0,
		"",
		"",
		"2026-05-22T12:00:00Z",
	)
	nvd := vulnerabilityCVEFactWithProvenance(
		"nvd-cve",
		"CVE-2026-9999",
		"nvd",
		"CVE-2026-9999",
		7.2,
		"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"HIGH",
		"2026-05-21T10:00:00Z",
	)
	pkg := vulnerabilityAffectedPackageFactWithSource(
		"ghsa-affected",
		"CVE-2026-9999",
		"osv",
		"GHSA-empty",
		"npm://registry.npmjs.org/example",
		"npm",
		"example",
		"1.2.3",
		"1.2.4",
	)
	consume := packageConsumptionFactWithRange("consume-1", "npm://registry.npmjs.org/example", testImpactRepositoryID, "1.2.3")

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{ghsaNoSeverity, nvd, pkg, consume})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	finding := findings[0]
	if finding.SeveritySource != "nvd" {
		t.Fatalf("SeveritySource = %q, want nvd fallback when ghsa has no severity", finding.SeveritySource)
	}
	if finding.CVSSScore != 7.2 {
		t.Fatalf("CVSSScore = %v, want 7.2 from nvd fallback", finding.CVSSScore)
	}
	if !findingHasAdvisorySource(finding, "ghsa", "GHSA-empty", "2026-05-22T12:00:00Z") {
		t.Fatalf("AdvisorySources missing ghsa observation even when not selected: %#v", finding.AdvisorySources)
	}
}

func TestSupplyChainImpactExcludesWithdrawnAdvisoryFromSelection(t *testing.T) {
	t.Parallel()

	// A withdrawn advisory must not be selected for severity or
	// fixed-version, but its observation (with withdrawn_at) must remain
	// visible so operators can see why it was excluded.
	withdrawn := vulnerabilityCVEFactWithdrawn(
		"ghsa-withdrawn",
		"CVE-2026-1010",
		"osv",
		"GHSA-withdrawn",
		9.0,
		"CRITICAL",
		"2026-05-10T12:00:00Z",
		"2026-05-22T08:00:00Z",
	)
	nvd := vulnerabilityCVEFactWithProvenance(
		"nvd-cve",
		"CVE-2026-1010",
		"nvd",
		"CVE-2026-1010",
		6.4,
		"CVSS:3.1/AV:N/AC:H/PR:L/UI:R/S:U/C:H/I:H/A:N",
		"MEDIUM",
		"2026-05-18T10:00:00Z",
	)
	pkg := vulnerabilityAffectedPackageFactWithSource(
		"nvd-affected",
		"CVE-2026-1010",
		"nvd",
		"CVE-2026-1010",
		"npm://registry.npmjs.org/widget",
		"npm",
		"widget",
		"1.0.0",
		"1.0.1",
	)
	consume := packageConsumptionFactWithRange("consume-1", "npm://registry.npmjs.org/widget", testImpactRepositoryID, "1.0.0")

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{withdrawn, nvd, pkg, consume})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	finding := findings[0]
	if finding.SeveritySource != "nvd" {
		t.Fatalf("SeveritySource = %q, want nvd (withdrawn ghsa must be excluded)", finding.SeveritySource)
	}
	if !findingHasAdvisorySource(finding, "ghsa", "GHSA-withdrawn", "2026-05-10T12:00:00Z") {
		t.Fatalf("AdvisorySources should still list withdrawn ghsa observation: %#v", finding.AdvisorySources)
	}
	for _, advisory := range finding.AdvisorySources {
		if advisory.Source == "ghsa" && advisory.WithdrawnAt == "" {
			t.Fatalf("withdrawn ghsa observation missing WithdrawnAt: %#v", advisory)
		}
	}
}

func TestSupplyChainImpactPreservesMultipleFixedVersionBranches(t *testing.T) {
	t.Parallel()

	// One source publishes two fixed-version branches (stable + prerelease);
	// another source publishes only the stable branch. Both branches must
	// appear in FixedVersionBranches with the originating source.
	ghsa := vulnerabilityCVEFactWithProvenance(
		"ghsa-cve",
		"CVE-2026-2020",
		"osv",
		"GHSA-multi",
		8.5,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N",
		"HIGH",
		"2026-05-24T09:00:00Z",
	)
	glad := vulnerabilityCVEFactWithProvenance(
		"glad-cve",
		"CVE-2026-2020",
		"glad",
		"GMS-2026-99",
		8.4,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N",
		"HIGH",
		"2026-05-24T08:00:00Z",
	)
	ghsaPkg := vulnerabilityAffectedPackageMultiFixed(
		"ghsa-affected",
		"CVE-2026-2020",
		"osv",
		"GHSA-multi",
		"npm://registry.npmjs.org/parse-server",
		"npm",
		"parse-server",
		[]string{"8.6.77"},
	)
	gladPkg := vulnerabilityAffectedPackageMultiFixed(
		"glad-affected",
		"CVE-2026-2020",
		"glad",
		"GMS-2026-99",
		"npm://registry.npmjs.org/parse-server",
		"npm",
		"parse-server",
		[]string{"8.6.77", "9.9.1-alpha.1"},
	)
	consume := packageConsumptionFactWithRange("consume-1", "npm://registry.npmjs.org/parse-server", testImpactRepositoryID, "8.0.0")

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{ghsa, glad, ghsaPkg, gladPkg, consume})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 consolidated finding", len(findings))
	}
	finding := findings[0]
	if !findingHasFixedVersionBranch(finding, "8.6.77", "ghsa") {
		t.Fatalf("FixedVersionBranches missing 8.6.77@ghsa: %#v", finding.FixedVersionBranches)
	}
	if !findingHasFixedVersionBranch(finding, "8.6.77", "glad") {
		t.Fatalf("FixedVersionBranches missing 8.6.77@glad: %#v", finding.FixedVersionBranches)
	}
	if !findingHasFixedVersionBranch(finding, "9.9.1-alpha.1", "glad") {
		t.Fatalf("FixedVersionBranches missing 9.9.1-alpha.1@glad (GLAD-only branch): %#v", finding.FixedVersionBranches)
	}
	if finding.FixedVersionSource != "ghsa" {
		t.Fatalf("FixedVersionSource = %q, want ghsa (npm priority)", finding.FixedVersionSource)
	}
	if finding.FixedVersion != "8.6.77" {
		t.Fatalf("FixedVersion = %q, want 8.6.77", finding.FixedVersion)
	}
}

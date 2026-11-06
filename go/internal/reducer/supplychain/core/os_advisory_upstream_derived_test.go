// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"maps"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// authorizedAdvisories keeps the kinds the authorized pass accepts from the
// converted record and returns the record's one affected_package envelope.
func authorizedAdvisories(envelopes []facts.Envelope) ([]facts.Envelope, facts.Envelope) {
	var advisories []facts.Envelope
	var affected facts.Envelope
	for _, envelope := range envelopes {
		if !authorizedImpactAdvisoryKind(envelope.FactKind) {
			continue
		}
		advisories = append(advisories, envelope)
		if envelope.FactKind == chain.VulnerabilityAffectedPackageFactKind {
			affected = envelope
		}
	}
	return advisories, affected
}

// secondAffectedFact is a second fact of the same advisory and package carrying
// a different fix: what two scopes of one record look like.
func secondAffectedFact(affected facts.Envelope, fixed string) facts.Envelope {
	payload := maps.Clone(affected.Payload)
	payload["fixed_versions"] = []any{fixed}
	affected.Payload = payload
	affected.FactID += "-second"
	return affected
}

func authorizedOSVAuthority(advisories []facts.Envelope) []AdvisoryAuthorityDependency {
	return []AdvisoryAuthorityDependency{{
		Kind: "public_registration", ID: "registration-osv", RevisionID: "revision-osv",
		AdvisoryScopeID: advisories[0].ScopeID, AdvisoryGenerationID: advisories[0].GenerationID,
		ConfigFingerprint: "config-osv", ProviderID: "osv", TrustedSourceClass: "osv",
		ContentProfile: "supply_chain_impact_v1",
	}}
}

// TestDerivedConflictGateSeesGroupedFindings: the derived pass withholds a fix
// when two facts of one advisory and package disagree. It must see the facts the
// primary path grouped, under the CVE the finding carries.
//
// Measured on the input below with upstream set: base e3d816172a reads
// possibly_affected / advisory_fixed_version_conflict with no fix; c35a68c258 read
// affected_exact / dpkg_affected_range naming one of the two disputed fixes.
func TestDerivedConflictGateSeesGroupedFindings(t *testing.T) {
	t.Parallel()

	const id = "UBUNTU-CVE-2025-99991"
	record := func(upstream ...string) []facts.Envelope {
		if len(upstream) == 0 {
			return convertedOSVFixture(t, "built-derived-noupstream", false)
		}
		return convertedOSVFixture(t, "built-derived-upstream", false)
	}
	for name, tc := range map[string]struct {
		envelopes []facts.Envelope
		wantCVE   string
	}{
		"without upstream": {envelopes: record(), wantCVE: id},
		"with upstream":    {envelopes: record("CVE-2025-99991"), wantCVE: "CVE-2025-99991"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			advisories, affected := authorizedAdvisories(tc.envelopes)
			advisories = append(advisories, secondAffectedFact(affected, "7.81.0-1ubuntu1.25"))
			target := authorizedOSVTarget(0, advisories, authorizedOSVAuthority(advisories), ubuntuCurl("22.04", "7.81.0-1ubuntu1.1"))
			findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
			if err != nil {
				t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
			}
			got := findings[0]
			if got.CVEID != tc.wantCVE {
				t.Errorf("CVEID = %q, want %q", got.CVEID, tc.wantCVE)
			}
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
			if got.MatchReason != supplyChainVersionReasonAdvisoryFixedConflict {
				t.Errorf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonAdvisoryFixedConflict)
			}
			if got.FixedVersion != "" {
				t.Errorf("FixedVersion = %q, want none: the two facts disagree", got.FixedVersion)
			}
		})
	}
}

// TestDerivedConflictGateFiltersAnotherReleaseLineInAGroup: a grouped fact of the
// deciding record whose ranges name another release line does not contend, as it
// does not without grouping, and a second record in the group is not its rival.
func TestDerivedConflictGateFiltersAnotherReleaseLineInAGroup(t *testing.T) {
	t.Parallel()

	const id = "UBUNTU-CVE-2025-99991"
	jammy, _ := authorizedAdvisories(convertedOSVFixture(t, "built-derived-upstream", false))
	_, noble := authorizedAdvisories(convertedOSVFixture(t, "built-derived-noble", false))
	noble.FactID += "-noble"
	usn, _ := authorizedAdvisories(convertedOSVFixture(t, "built-derived-usn-noble", false))

	advisories := append(append(jammy, noble), usn...)
	target := authorizedOSVTarget(0, advisories, authorizedOSVAuthority(advisories), ubuntuCurl("22.04", "7.81.0-1ubuntu1.1"))
	findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
	if err != nil {
		t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.AdvisoryID != id || got.FixedVersion != "7.81.0-1ubuntu1.20" {
		t.Errorf("advisory/fix = %q/%q, want %q with the jammy fix", got.AdvisoryID, got.FixedVersion, id)
	}
}

// TestDerivedConflictGateIgnoresARetractedRecordsFacts: the gate reads the group
// after the liveness filter, so a finding still named by a rejected record (the
// #9695 naming rule on a non-OS ecosystem) is not marked disputed by that record's
// own facts when a live record asserts the package.
func TestDerivedConflictGateIgnoresARetractedRecordsFacts(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-1"
	const packageID = "npm://registry.npmjs.org/lodash"
	index := supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{CVEID: cve, AdvisoryID: "GHSA-rejected", Status: "Rejected"},
			{CVEID: cve, AdvisoryID: "GHSA-live"},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{cve: {
			{FactID: "r1", CVEID: cve, AdvisoryID: "GHSA-rejected", PackageID: packageID, Ecosystem: "npm", FixedVersions: []string{"1.0.0"}},
			{FactID: "r2", CVEID: cve, AdvisoryID: "GHSA-rejected", PackageID: packageID, Ecosystem: "npm", FixedVersions: []string{"2.0.0"}},
			{FactID: "l1", CVEID: cve, AdvisoryID: "GHSA-live", PackageID: packageID, Ecosystem: "npm", FixedVersions: []string{"3.0.0"}},
		}},
	}
	findings := []SupplyChainImpactFinding{{
		CVEID: cve, AdvisoryID: "GHSA-rejected", PackageID: packageID, Ecosystem: "npm",
		Status: SupplyChainImpactAffectedExact, FixedVersion: "3.0.0",
	}}
	markAuthorizedFixedVersionConflicts(findings, index, nil)
	if findings[0].MatchReason == supplyChainVersionReasonAdvisoryFixedConflict || findings[0].Status != SupplyChainImpactAffectedExact {
		t.Errorf("finding = %#v, want it untouched: only a retracted record's facts disagree", findings[0])
	}
}

// TestDerivedConflictGateJudgesLivenessPerPackage: liveness is per package, as in
// the primary path. A package only a retracted record asserts keeps that record's
// verdict, so its own disagreeing facts still dispute the fix, whatever a live
// record says about another package in the group.
func TestDerivedConflictGateJudgesLivenessPerPackage(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-1"
	const packageID = "npm://registry.npmjs.org/lodash"
	index := supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{
			{CVEID: cve, AdvisoryID: "GHSA-rejected", Status: "Rejected"},
			{CVEID: cve, AdvisoryID: "GHSA-live"},
		},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{cve: {
			{FactID: "r1", CVEID: cve, AdvisoryID: "GHSA-rejected", PackageID: packageID, Ecosystem: "npm", FixedVersions: []string{"1.0.0"}},
			{FactID: "r2", CVEID: cve, AdvisoryID: "GHSA-rejected", PackageID: packageID, Ecosystem: "npm", FixedVersions: []string{"2.0.0"}},
			{FactID: "l1", CVEID: cve, AdvisoryID: "GHSA-live", PackageID: "npm://registry.npmjs.org/other", Ecosystem: "npm", FixedVersions: []string{"3.0.0"}},
		}},
	}
	findings := []SupplyChainImpactFinding{{
		CVEID: cve, AdvisoryID: "GHSA-rejected", PackageID: packageID, Ecosystem: "npm",
		Status: SupplyChainImpactAffectedExact, FixedVersion: "2.0.0",
	}}
	markAuthorizedFixedVersionConflicts(findings, index, nil)
	if findings[0].MatchReason != supplyChainVersionReasonAdvisoryFixedConflict {
		t.Errorf("finding = %#v, want the fix disputed: the live record says nothing about this package", findings[0])
	}
}

// TestDerivedConflictGateSkipsAProductFinding: a product finding has no package
// id, and a fact with no package id is not its package, so two of them that
// disagree do not dispute it. The old decode skipped those facts outright.
func TestDerivedConflictGateSkipsAProductFinding(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2026-2"
	index := supplyChainImpactIndex{
		cves: []supplychainmodel.ImpactCVE{{CVEID: cve, AdvisoryID: cve}},
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{cve: {
			{FactID: "p1", CVEID: cve, AdvisoryID: cve, FixedVersions: []string{"1.0.0"}},
			{FactID: "p2", CVEID: cve, AdvisoryID: cve, FixedVersions: []string{"2.0.0"}},
		}},
	}
	findings := []SupplyChainImpactFinding{{
		CVEID: cve, AdvisoryID: cve, ProductCriteria: testImpactProductCriteria,
		Status: SupplyChainImpactAffectedDerived,
	}}
	markAuthorizedFixedVersionConflicts(findings, index, nil)
	if findings[0].Status != SupplyChainImpactAffectedDerived || findings[0].MatchReason != "" {
		t.Errorf("finding = %#v, want it untouched", findings[0])
	}
}

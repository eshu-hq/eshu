// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestRetractedFindingStaysOnlyWithoutALiveAnchor holds the rule's edges: a live fact
// with any anchor of its own still decides the group, the rule stays inside the OS
// class, and an SBOM path anchors the retracted record as an installed row does.
func TestRetractedFindingStaysOnlyWithoutALiveAnchor(t *testing.T) {
	t.Parallel()

	withdrawn := unjoinableRetractedCVE("withdrawn")
	host := ubuntuCurl("22.04", unjoinableVersion).envelope()

	t.Run("a live record that joins decides, as ruling f156 decision 5 reads it", func(t *testing.T) {
		t.Parallel()
		envelopes := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("osv"), unjoinableRetractedPackage("os"), unjoinableLivePackage("osv", "os", unjoinablePURL), host,
		}
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) != 1 {
				t.Fatalf("%s: findings = %d, want 1", order, len(findings))
			}
			got := findings[0]
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
			if got.AdvisoryID != "EXT-1" || got.FixedVersion != "1.0-2" || got.AdvisoryRetraction != nil {
				t.Errorf("%s: advisory/fix/retraction = %q/%q/%#v, want the live record's, with no retraction", order, got.AdvisoryID, got.FixedVersion, got.AdvisoryRetraction)
			}
			if !slices.Contains(got.EvidenceFactIDs, "affected-own") {
				t.Errorf("%s: evidence %v is missing the retired fact", order, got.EvidenceFactIDs)
			}
		}
	})

	t.Run("a live record anchored only by a consuming repository decides", func(t *testing.T) {
		t.Parallel()
		envelopes := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("os"), unjoinableLivePackage("nvd", "os", ""), host,
			packageConsumptionFactWithRange("consumption-1", unjoinablePackageID, testImpactRepositoryID, unjoinableVersion),
		}
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) != 1 {
				t.Fatalf("%s: findings = %d, want 1", order, len(findings))
			}
			got := findings[0]
			if got.AdvisoryID != "EXT-1" || got.RepositoryID != testImpactRepositoryID || got.AdvisoryRetraction != nil {
				t.Errorf("%s: advisory/repository/retraction = %q/%q/%#v, want the live record's on the repository, with no retraction", order, got.AdvisoryID, got.RepositoryID, got.AdvisoryRetraction)
			}
			if !slices.Contains(got.EvidenceFactIDs, "affected-own") {
				t.Errorf("%s: evidence %v is missing the retired fact", order, got.EvidenceFactIDs)
			}
		}
	})

	t.Run("a live record anchored only by an SBOM path decides", func(t *testing.T) {
		t.Parallel()
		const digest = "sha256:ab000000000000000000000000000000000000000000000000000000000000ab"
		// The component carries the group's package id, so every fact of the group
		// reaches it, the live one with no purl included.
		component := sbomComponentImpactFactWithPackageIDVersion("comp", "doc-1", "pkg:deb/other/curl", unjoinablePackageID, unjoinableVersion)
		component.Payload["name"] = "curl"
		envelopes := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("os"), unjoinableLivePackage("nvd", "os", ""), host, component,
			sbomAttachmentImpactFact("att", "doc-1", digest),
			containerImageIdentityImpactFact("img", digest, "oci-registry://registry.example/app"),
		}
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) == 0 {
				t.Fatalf("%s: no findings, want the live record's", order)
			}
			for _, got := range findings {
				if got.AdvisoryID != "EXT-1" || got.AdvisoryRetraction != nil {
					t.Errorf("%s: advisory/retraction = %q/%#v, want the live record's, with no retraction", order, got.AdvisoryID, got.AdvisoryRetraction)
				}
			}
		}
	})

	t.Run("a language group keeps the live record's verdict", func(t *testing.T) {
		t.Parallel()
		const npm = "npm://registry.npmjs.org/lodash"
		envelopes := []facts.Envelope{
			vulnerabilityCVEFactWithdrawn("ghsa-cve", unjoinableCVE, "osv", "GHSA-1", 7.5, "HIGH", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"),
			vulnerabilityCVEFactWithProvenance("nvd-cve", unjoinableCVE, "nvd", unjoinableCVE, 7.5, "", "HIGH", "2026-10-01T00:00:00Z"),
			vulnerabilityAffectedPackageFactWithSource("ghsa-pkg", unjoinableCVE, "osv", "GHSA-1", npm, "npm", "lodash", "4.17.20", "4.17.21"),
			vulnerabilityAffectedPackageFactWithSource("nvd-pkg", unjoinableCVE, "nvd", unjoinableCVE, npm, "npm", "lodash", "4.17.20", "4.17.22"),
			packageConsumptionFactWithRange("consumption-npm", npm, testImpactRepositoryID, "4.17.20"),
		}
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) != 1 {
				t.Fatalf("%s: findings = %d, want 1", order, len(findings))
			}
			got := findings[0]
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
			if got.AdvisoryID != unjoinableCVE || got.FixedVersion != "4.17.22" || got.AdvisoryRetraction != nil {
				t.Errorf("%s: advisory/fix/retraction = %q/%q/%#v, want the live NVD record's", order, got.AdvisoryID, got.FixedVersion, got.AdvisoryRetraction)
			}
		}
	})

	t.Run("a spelling outside the OS class never reads a finding with no retraction", func(t *testing.T) {
		t.Parallel()
		envelopes := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("dpkg"), unjoinableLivePackage("nvd", "dpkg", ""), host,
		}
		for order, shuffled := range orderings(envelopes) {
			for _, got := range BuildSupplyChainImpactFindings(shuffled) {
				if got.AdvisoryRetraction == nil {
					t.Errorf("%s: %s %s reads no retraction, taken from a withdrawn list", order, got.AdvisoryID, got.Status)
				}
			}
		}
	})

	t.Run("an SBOM path anchors the retracted record", func(t *testing.T) {
		t.Parallel()
		const digest = "sha256:ab000000000000000000000000000000000000000000000000000000000000ab"
		// The component's own package id differs from the group's, so only the
		// retracted fact's purl reaches it.
		component := sbomComponentImpactFactWithPackageIDVersion("comp", "doc-1", unjoinablePURL, "os://ubuntu/curl", unjoinableVersion)
		component.Payload["name"] = "curl"
		base := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("os"), component,
			sbomAttachmentImpactFact("att", "doc-1", digest),
			containerImageIdentityImpactFact("img", digest, "oci-registry://registry.example/app"),
		}
		control := BuildSupplyChainImpactFindings(slices.Clone(base))
		for _, shuffled := range orderings(append(base, unjoinableLivePackage("nvd", "os", ""))) {
			assertRetractedRecordFinding(t, BuildSupplyChainImpactFindings(shuffled), control)
		}
	})
}

// TestAuthorizedScanKeepsARetractedFindingBesideAnUnjoinableLiveRecord runs the rule on
// the authorized path: the Ubuntu target reads the retracted finding, the derived
// conflict pass leaves it alone, and a Debian target reads nothing.
func TestAuthorizedScanKeepsARetractedFindingBesideAnUnjoinableLiveRecord(t *testing.T) {
	t.Parallel()

	const (
		ownScope, ownGeneration     = "vuln-intel://ubuntu/feed", "gen-ubuntu"
		otherScope, otherGeneration = "vuln-intel://nvd/feed", "gen-nvd"
	)
	place := func(envelope facts.Envelope, scope, generation string) facts.Envelope {
		envelope.ScopeID, envelope.GenerationID = scope, generation
		envelope.SourceRef.SourceRecordID = envelope.FactID
		return envelope
	}
	advisories := []facts.Envelope{
		place(unjoinableRetractedCVE("withdrawn"), ownScope, ownGeneration),
		place(unjoinableRetractedPackage("os"), ownScope, ownGeneration),
		place(unjoinableLiveCVE("nvd"), otherScope, otherGeneration),
		place(unjoinableLivePackage("nvd", "os", ""), otherScope, otherGeneration),
	}
	authority := []AdvisoryAuthorityDependency{
		{
			Kind: "public_registration", ID: "registration-ubuntu", RevisionID: "revision-ubuntu",
			AdvisoryScopeID: ownScope, AdvisoryGenerationID: ownGeneration, ConfigFingerprint: "config-ubuntu",
			ProviderID: "ubuntu", TrustedSourceClass: "ubuntu", ContentProfile: "supply_chain_impact_v1",
		},
		{
			Kind: "public_registration", ID: "registration-nvd", RevisionID: "revision-nvd",
			AdvisoryScopeID: otherScope, AdvisoryGenerationID: otherGeneration, ConfigFingerprint: "config-nvd",
			ProviderID: "nvd", TrustedSourceClass: "nvd", ContentProfile: "supply_chain_impact_v1",
		},
	}
	for _, reverse := range []bool{false, true} {
		ordered := slices.Clone(advisories)
		if reverse {
			slices.Reverse(ordered)
		}
		findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{
			authorizedOSVTarget(0, ordered, authority, ubuntuCurl("22.04", unjoinableVersion)),
		})
		if err != nil {
			t.Fatalf("ubuntu target: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("ubuntu target findings = %d, want 1: %#v", len(findings), findings)
		}
		got := findings[0]
		if got.AdvisoryID != unjoinableAdvisory || got.AdvisoryRetraction == nil {
			t.Errorf("ubuntu target advisory/retraction = %q/%#v, want the retracted record with its retraction", got.AdvisoryID, got.AdvisoryRetraction)
		}
		assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
		if got.MatchReason == supplyChainVersionReasonAdvisoryFixedConflict {
			t.Errorf("the derived conflict pass changed the finding: %#v", got)
		}
		findings, err = BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{
			authorizedOSVTarget(1, ordered, authority, debianCurl("12", unjoinableVersion)),
		})
		if err != nil {
			t.Fatalf("debian target: %v", err)
		}
		if len(findings) != 0 {
			t.Errorf("debian target findings = %d, want none: %#v", len(findings), findings)
		}
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestRetractedFindingStaysOnlyWithoutALiveAnchor holds the rule's edges: a live fact
// with a joined row or an SBOM path of its own still decides the group, a consuming
// repository alone does not beat a retracted record that joins the row (#10034), the
// rule stays inside the OS class, and an SBOM path anchors the retracted record as an
// installed row does.
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

	t.Run("a live record anchored only by a consuming repository loses to a joined retracted record", func(t *testing.T) {
		t.Parallel()
		// Consumption says the package is depended upon, but it does not
		// speak to the scanned row: the row the retracted record joins reads
		// that record's finding with the retraction shown (#10034, the
		// live-record-with-consumer limit).
		base := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("os"), host,
			packageConsumptionFactWithRange("consumption-1", unjoinablePackageID, testImpactRepositoryID, unjoinableVersion),
		}
		control := BuildSupplyChainImpactFindings(slices.Clone(base))
		for _, shuffled := range orderings(append(slices.Clone(base), unjoinableLivePackage("nvd", "os", ""))) {
			assertRetractedRecordFinding(t, BuildSupplyChainImpactFindings(shuffled), control)
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
// the authorized path: the Ubuntu target reads the finding it reads with no live
// package fact at all, and a Debian target reads nothing.
//
// A retracted record with two facts offering different fixes is the row that holds the
// derived conflict gate to the index the primary path classified with. Without that
// index the gate reads only the live fact, which is another advisory's, and the
// conflict the control shows disappears. The retraction shows on the control and on
// the findings of both rows, because the record is retracted as a whole (ruling
// 9914-retract).
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
	secondFix := unjoinableRetractedPackage("os")
	secondFix.FactID = "affected-own-second"
	secondFix.Payload["fixed_versions"] = []any{"1.0-5"}
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
	build := func(t *testing.T, advisories []facts.Envelope, target int, row installedOSRow) []SupplyChainImpactFinding {
		t.Helper()
		findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{
			authorizedOSVTarget(target, advisories, authority, row),
		})
		if err != nil {
			t.Fatalf("build findings: %v", err)
		}
		return findings
	}

	for _, tc := range []struct {
		name         string
		retracted    []facts.Envelope
		wantConflict bool
	}{
		{name: "one retracted fact", retracted: []facts.Envelope{unjoinableRetractedPackage("os")}},
		{
			name:         "a retracted record with two facts offering different fixes",
			retracted:    []facts.Envelope{unjoinableRetractedPackage("os"), secondFix},
			wantConflict: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			withoutLivePackage := []facts.Envelope{
				place(unjoinableRetractedCVE("withdrawn"), ownScope, ownGeneration),
				place(unjoinableLiveCVE("nvd"), otherScope, otherGeneration),
			}
			for _, fact := range tc.retracted {
				withoutLivePackage = append(withoutLivePackage, place(fact, ownScope, ownGeneration))
			}
			control := build(t, withoutLivePackage, 0, ubuntuCurl("22.04", unjoinableVersion))
			if len(control) != 1 {
				t.Fatalf("control findings = %d, want 1: %#v", len(control), control)
			}
			if got := control[0].MatchReason == supplyChainVersionReasonAdvisoryFixedConflict; got != tc.wantConflict {
				t.Fatalf("control conflict = %v (reason %q), want %v", got, control[0].MatchReason, tc.wantConflict)
			}
			if control[0].AdvisoryRetraction == nil {
				t.Fatalf("control shows no retraction: %#v", control[0])
			}
			advisories := append(slices.Clone(withoutLivePackage), place(unjoinableLivePackage("nvd", "os", ""), otherScope, otherGeneration))
			for _, reverse := range []bool{false, true} {
				ordered := slices.Clone(advisories)
				if reverse {
					slices.Reverse(ordered)
				}
				findings := build(t, ordered, 0, ubuntuCurl("22.04", unjoinableVersion))
				assertRetractedRecordFinding(t, findings, control)
				if !tc.wantConflict {
					assertSupplyChainImpactStatus(t, findings[0], SupplyChainImpactAffectedExact)
				}
				if findings := build(t, ordered, 1, debianCurl("12", unjoinableVersion)); len(findings) != 0 {
					t.Errorf("debian target findings = %d, want none: %#v", len(findings), findings)
				}
			}
		})
	}
}

// TestAnchorRuleReadsEveryFactOfTheGroup holds the rule to the whole group: one live
// fact with an anchor keeps the group live wherever it sorts, one retracted fact with
// an anchor is enough for the others to stay, and the OS-class check reads the
// retracted facts, not the live record's spelling.
func TestAnchorRuleReadsEveryFactOfTheGroup(t *testing.T) {
	t.Parallel()

	host := ubuntuCurl("22.04", unjoinableVersion).envelope()
	withdrawn := unjoinableRetractedCVE("withdrawn")

	t.Run("a second live record that joins decides", func(t *testing.T) {
		t.Parallel()
		second := unjoinableLivePackage("osv", "os", unjoinablePURL)
		second.FactID = "affected-second"
		second.Payload["advisory_id"] = "OSV-1"
		envelopes := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("os"),
			vulnerabilityCVEFactWithProvenance("cve-second", unjoinableCVE, "osv", "OSV-1", 7.5, "", "HIGH", "2026-10-01T00:00:00Z"),
			unjoinableLivePackage("nvd", "os", ""), second, host,
		}
		for order, shuffled := range orderings(envelopes) {
			findings := BuildSupplyChainImpactFindings(shuffled)
			if len(findings) != 1 {
				t.Fatalf("%s: findings = %d, want 1", order, len(findings))
			}
			if got := findings[0]; got.AdvisoryID != "OSV-1" || got.AdvisoryRetraction != nil {
				t.Errorf("%s: advisory/retraction = %q/%#v, want the joining live record's, with no retraction", order, got.AdvisoryID, got.AdvisoryRetraction)
			}
		}
	})

	t.Run("one retracted fact with an anchor keeps a retracted fact without one", func(t *testing.T) {
		t.Parallel()
		// The sibling record is withdrawn too and has no purl. Its fact id sorts
		// first, so a check that reads only the first retracted fact sees no anchor.
		sibling := unjoinableLivePackage("nvd", "os", "")
		sibling.FactID = "affected-a-sibling"
		sibling.Payload["advisory_id"] = "W2-1"
		base := []facts.Envelope{
			withdrawn, unjoinableLiveCVE("debian"), unjoinableRetractedPackage("os"), sibling, host,
			vulnerabilityCVEFactWithdrawn("cve-sibling", unjoinableCVE, "nvd", "W2-1", 7.5, "HIGH", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"),
		}
		control := BuildSupplyChainImpactFindings(slices.Clone(base))
		if len(control) != 1 || control[0].AdvisoryRetraction == nil {
			t.Fatalf("control = %#v, want one retracted finding", control)
		}
		for order, shuffled := range orderings(append(slices.Clone(base), unjoinableLivePackage("debian", "os", ""))) {
			if got := BuildSupplyChainImpactFindings(shuffled); !reflect.DeepEqual(got, control) {
				t.Errorf("%s: findings differ from the control without the live package fact:\n got %#v\nwant %#v", order, got, control)
			}
		}
	})

	t.Run("the class check reads the retracted facts, not the live spelling", func(t *testing.T) {
		t.Parallel()
		base := []facts.Envelope{withdrawn, unjoinableLiveCVE("nvd"), unjoinableRetractedPackage("os"), host}
		control := BuildSupplyChainImpactFindings(slices.Clone(base))
		for _, shuffled := range orderings(append(slices.Clone(base), unjoinableLivePackage("nvd", "dpkg", ""))) {
			assertRetractedRecordFinding(t, BuildSupplyChainImpactFindings(shuffled), control)
		}
	})
}

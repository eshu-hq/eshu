// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestOSVOSAdvisoryVersionListNeverExact pins ruling D on the exact-version
// list: an OSV OS entry that lists the installed version but carries no
// release evidence reads possibly_affected, as its ranges would (#9848 review
// F2). A vendor-sourced fact listing the same version stays affected_exact.
func TestOSVOSAdvisoryVersionListNeverExact(t *testing.T) {
	t.Parallel()

	events := func(introduced, fixed string) []vulnerabilityintelligence.OSVRange {
		return []vulnerabilityintelligence.OSVRange{{Type: "ECOSYSTEM", Events: []vulnerabilityintelligence.OSVEvent{
			{Introduced: introduced}, {Fixed: fixed},
		}}}
	}
	entry := func(ecosystem, distro string, versions []string, ranges []vulnerabilityintelligence.OSVRange) vulnerabilityintelligence.OSVAffected {
		return vulnerabilityintelligence.OSVAffected{
			Package: vulnerabilityintelligence.OSVPackage{
				Name: "curl", Ecosystem: ecosystem, PURL: "pkg:deb/debian/curl?arch=source&distro=" + distro,
			},
			Versions: versions,
			Ranges:   ranges,
		}
	}
	osvRecord := func(t *testing.T, record vulnerabilityintelligence.OSVVulnerability) []facts.Envelope {
		t.Helper()
		return convertedOSVRecord(t, record, false)
	}
	vendorFact := derivedPartitionAdvisory("vendor", "vuln-intel://debian/dsa", "3.0.11-1~deb12u3")
	vendorRow := installedOSRow{vendor: "debian", release: "11", manager: "dpkg", name: "openssl", arch: "amd64", version: "3.0.11-1~deb12u2"}

	tests := []struct {
		name       string
		envelopes  []facts.Envelope
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		{
			name: "osv versions-only entry, host on an unlisted release",
			envelopes: append(osvRecord(t, vulnerabilityintelligence.OSVVulnerability{ID: "DEBIAN-CVE-2099-0001", Affected: []vulnerabilityintelligence.OSVAffected{
				entry("Debian:12", "bookworm", []string{"7.88.1-10+deb12u2"}, nil),
			}}), debianCurl("11", "7.88.1-10+deb12u2").envelope()),
			wantStatus: SupplyChainImpactPossiblyAffected,
			wantReason: supplyChainVersionReasonOSReleaseUnverified,
		},
		{
			name: "osv versions list plus two releases' fixes at or below the version",
			envelopes: append(osvRecord(t, vulnerabilityintelligence.OSVVulnerability{ID: "DEBIAN-CVE-2099-0012", Affected: []vulnerabilityintelligence.OSVAffected{
				entry("Debian:12", "bookworm", []string{"7.88.1-10+deb12u2"}, events("0", "7.88.1-10+deb12u1")),
				entry("Debian:11", "bullseye", nil, events("0", "7.74.0-1.3+deb11u7")),
			}}), debianCurl("12", "7.88.1-10+deb12u2").envelope()),
			wantStatus: SupplyChainImpactPossiblyAffected,
			wantReason: supplyChainVersionReasonOSReleaseAmbiguous,
		},
		{
			name:       "vendor-sourced fact listing the version stays exact",
			envelopes:  append(vendorFact, vendorRow.envelope()),
			wantStatus: SupplyChainImpactAffectedExact,
			wantReason: supplyChainVersionReasonDPKGExactAffected,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := BuildSupplyChainImpactFindings(tc.envelopes)
			if len(findings) != 1 {
				t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
			}
			assertSupplyChainImpactStatus(t, findings[0], tc.wantStatus)
			if findings[0].MatchReason != tc.wantReason {
				t.Fatalf("MatchReason = %q, want %q", findings[0].MatchReason, tc.wantReason)
			}
		})
	}
}

// TestOSPackageVerdictPrefersAmbiguousOverUnverified pins the precedence when
// two OSV facts for one CVE disagree: one says only "affected", the other has
// a range that contains the version and a range fixed at or below it. The
// disagreement wins, in either fact order (#9848 review F3).
func TestOSPackageVerdictPrefersAmbiguousOverUnverified(t *testing.T) {
	t.Parallel()

	osvRange := func(introduced, fixed string) supplychainmodel.AffectedRange {
		return supplychainmodel.AffectedRange{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: introduced}, {Fixed: fixed},
		}}
	}
	unverified := supplychainmodel.AffectedPackage{
		Source: "osv", AdvisoryID: "DEBIAN-CVE-2099-0013",
		AffectedRanges: []supplychainmodel.AffectedRange{osvRange("0", "7.88.1-10+deb12u3")},
	}
	ambiguous := supplychainmodel.AffectedPackage{
		Source: "osv", AdvisoryID: "UBUNTU-CVE-2099-0013",
		AffectedRanges: []supplychainmodel.AffectedRange{
			osvRange("0", "7.88.1-10+deb12u3"),
			osvRange("0", "7.74.0-1.3+deb11u7"),
		},
	}
	for _, pkgs := range [][]supplychainmodel.AffectedPackage{{unverified, ambiguous}, {ambiguous, unverified}} {
		decision := evaluateOSPackageVersionMatch(
			"7.88.1-10+deb12u2", "", pkgs,
			supplyChainVersionReasonDPKGExactAffected, supplyChainVersionReasonDPKGExactKnownFixed,
			supplyChainVersionReasonDPKGAffectedRange, supplyChainVersionReasonDPKGKnownFixed,
			compareDPKGVersion, osFamilyDPKG,
		)
		if decision.Status != SupplyChainImpactPossiblyAffected || decision.Reason != supplyChainVersionReasonOSReleaseAmbiguous {
			t.Fatalf("first fact %s: decision = %s/%s, want possibly_affected/%s",
				pkgs[0].AdvisoryID, decision.Status, decision.Reason, supplyChainVersionReasonOSReleaseAmbiguous)
		}
	}
}

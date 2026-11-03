// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestOSVOSAdvisoryJoinsInstalledRowAndStatesReleaseAmbiguity pins #9848 on
// live records. Each merged fact ORs the ranges of several releases with no
// release tag, so the reducer cannot tell whether the host's release is one
// the advisory lists. An OSV OS fact therefore never yields affected_exact
// (ruling D): every range containing the version is possibly_affected with
// os_release_unverified, ranges that disagree are os_release_ambiguous, and a
// fix at or below the version with no containing range is known fixed. Hosts
// on a release the record does not list (bullseye, buster, Alpine v3.13,
// bionic) fall below every listed fix and must read unverified, not exact.
func TestOSVOSAdvisoryJoinsInstalledRowAndStatesReleaseAmbiguity(t *testing.T) {
	t.Parallel()

	const (
		ambiguous  = supplyChainVersionReasonOSReleaseAmbiguous
		unverified = "os_release_unverified"
	)
	tests := []struct {
		name       string
		fixture    string
		installed  installedOSRow
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		// DEBIAN-CVE-2023-38039: bookworm fixed 7.88.1-10+deb12u3, trixie
		// and forky fixed 8.3.0-1.
		{"debian bookworm below every fix", "debian-cve-2023-38039.json", debianCurl("12", "7.88.1-10+deb12u2"), SupplyChainImpactPossiblyAffected, unverified},
		{"debian bullseye, a release the record does not list", "debian-cve-2023-38039.json", debianCurl("11", "7.74.0-1.3+deb11u7"), SupplyChainImpactPossiblyAffected, unverified},
		{"debian buster, a release the record does not list", "debian-cve-2023-38039.json", debianCurl("10", "7.64.0-4+deb10u2"), SupplyChainImpactPossiblyAffected, unverified},
		{"debian bookworm at its backported fix", "debian-cve-2023-38039.json", debianCurl("12", "7.88.1-10+deb12u3"), SupplyChainImpactPossiblyAffected, ambiguous},
		{"debian trixie below its fix", "debian-cve-2023-38039.json", debianCurl("13", "8.2.0-1"), SupplyChainImpactPossiblyAffected, ambiguous},
		{"debian trixie at its fix", "debian-cve-2023-38039.json", debianCurl("13", "8.3.0-1"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGKnownFixed},
		// ALPINE-CVE-2023-27537: v3.14-v3.16 fixed 8.0.1-r0, v3.17
		// 7.88.1-r1, v3.18 onward 8.0.0-r0. Purl pkg:apk/alpine/curl?arch=source.
		{"alpine below every fix", "alpine-cve-2023-27537.json", alpineCurl("3.18.4", "7.88.1-r0"), SupplyChainImpactPossiblyAffected, unverified},
		{"alpine v3.13, a release the record does not list", "alpine-cve-2023-27537.json", alpineCurl("3.13.12", "7.79.1-r0"), SupplyChainImpactPossiblyAffected, unverified},
		{"alpine v3.18 at its fix", "alpine-cve-2023-27537.json", alpineCurl("3.18.4", "8.0.0-r0"), SupplyChainImpactPossiblyAffected, ambiguous},
		{"alpine above every fix", "alpine-cve-2023-27537.json", alpineCurl("3.18.4", "8.0.1-r0"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonAPKKnownFixed},
		// UBUNTU-CVE-2022-27775: versionless purls; focal fixed
		// 7.68.0-1ubuntu2.10, jammy 7.81.0-1ubuntu1.1.
		{"ubuntu versionless purl below every fix", "ubuntu-cve-2022-27775.json", ubuntuCurl("20.04", "7.68.0-1ubuntu2.7"), SupplyChainImpactPossiblyAffected, unverified},
		{"ubuntu bionic, a release the record does not list", "ubuntu-cve-2022-27775.json", ubuntuCurl("18.04", "7.58.0-2ubuntu3.16"), SupplyChainImpactPossiblyAffected, unverified},
		{"ubuntu versionless purl jammy below its fix", "ubuntu-cve-2022-27775.json", ubuntuCurl("22.04", "7.81.0-1ubuntu1"), SupplyChainImpactPossiblyAffected, ambiguous},
		{"ubuntu versionless purl jammy at its fix", "ubuntu-cve-2022-27775.json", ubuntuCurl("22.04", "7.81.0-1ubuntu1.1"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGKnownFixed},
		// UBUNTU-CVE-2016-5419: versioned purls (pkg:deb/ubuntu/curl@<v>?...);
		// trusty fixed 7.35.0-1ubuntu2.8, xenial 7.47.0-1ubuntu2.1. This
		// shape joined before #9848; the trusty-at-fix row was an exact
		// claim on a fixed host.
		{"ubuntu versioned purl below every fix", "ubuntu-cve-2016-5419.json", ubuntuCurl("14.04", "7.35.0-1ubuntu2.7"), SupplyChainImpactPossiblyAffected, unverified},
		{"ubuntu versioned purl trusty at its fix", "ubuntu-cve-2016-5419.json", ubuntuCurl("14.04", "7.35.0-1ubuntu2.8"), SupplyChainImpactPossiblyAffected, ambiguous},
		{"ubuntu versioned purl above every fix", "ubuntu-cve-2016-5419.json", ubuntuCurl("16.04", "7.47.0-1ubuntu2.1"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGKnownFixed},
	}
	for _, tc := range tests {
		for _, reverse := range []bool{false, true} {
			name := tc.name
			if reverse {
				name += " (entries reversed)"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				envelopes := append(convertedOSVFixture(t, tc.fixture, reverse), tc.installed.envelope())
				findings := BuildSupplyChainImpactFindings(envelopes)
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want 1 OS package finding: %#v", len(findings), findings)
				}
				got := findings[0]
				assertSupplyChainImpactStatus(t, got, tc.wantStatus)
				if got.MatchReason != tc.wantReason {
					t.Fatalf("MatchReason = %q, want %q", got.MatchReason, tc.wantReason)
				}
				if got.ObservedVersion != tc.installed.version {
					t.Fatalf("ObservedVersion = %q, want %q", got.ObservedVersion, tc.installed.version)
				}
				if tc.wantReason != ambiguous && tc.wantReason != unverified {
					return
				}
				if got.RuntimeReachability != "image_os_package" {
					t.Fatalf("RuntimeReachability = %q, want image_os_package for an installed row", got.RuntimeReachability)
				}
				if !slices.Contains(got.MissingEvidence, supplyChainMissingOSReleaseLine) {
					t.Fatalf("MissingEvidence = %v, want %q", got.MissingEvidence, supplyChainMissingOSReleaseLine)
				}
			})
		}
	}
}

// TestOSVOSAdvisoryJoinNegatives pins the rows the advisory-side key must not
// join: another vendor, a purl-less OS entry with no vendor, and a registry
// advisory that shares an installed package's name.
func TestOSVOSAdvisoryJoinNegatives(t *testing.T) {
	t.Parallel()

	purlLess := vulnerabilityintelligence.OSVVulnerability{
		ID: "SYNTHETIC-OS-PURLLESS",
		Affected: []vulnerabilityintelligence.OSVAffected{{
			Package: vulnerabilityintelligence.OSVPackage{Name: "curl", Ecosystem: "Debian:12"},
			Ranges: []vulnerabilityintelligence.OSVRange{{Type: "ECOSYSTEM", Events: []vulnerabilityintelligence.OSVEvent{
				{Introduced: "0"}, {Fixed: "7.88.1-10+deb12u3"},
			}}},
		}},
	}
	purlLessEnvelopes, _, err := vulnerabilityintelligence.OSVRecordEnvelopesReport(vulnerabilityintelligence.OSVEnvelopeContext{
		ScopeID: "vuln-intel://osv/fixture", GenerationID: "osv:fixture:1",
		CollectorInstanceID: "public-osv", ObservedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
	}, purlLess)
	if err != nil {
		t.Fatalf("convert purl-less record: %v", err)
	}
	lodashRow := installedOSRow{vendor: "debian", release: "12", manager: "dpkg", name: "lodash", arch: "all", version: "4.17.20-1"}

	tests := []struct {
		name      string
		envelopes []facts.Envelope
	}{
		{"debian advisory against an ubuntu row", append(convertedOSVFixture(t, "debian-cve-2023-38039.json", false), ubuntuCurl("22.04", "7.88.1-10+deb12u2").envelope())},
		{"ubuntu advisory against a debian row", append(convertedOSVFixture(t, "ubuntu-cve-2022-27775.json", false), debianCurl("12", "7.68.0-1ubuntu2.7").envelope())},
		{"purl-less os entry", append(purlLessEnvelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope())},
		{"npm advisory against a same-named os row", append(convertedOSVFixture(t, "ghsa-35jh-r3h4-6jhm.json", false), lodashRow.envelope())},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if findings := BuildSupplyChainImpactFindings(tc.envelopes); len(findings) != 0 {
				t.Fatalf("findings = %#v, want none: no installed row of this vendor and package", findings)
			}
		})
	}
}

// TestOSVOSAdvisoryLookupKeysMeetTheInstalledKey pins the advisory-side key
// the narrowed Postgres read sends as $1: it must contain the installed key
// split_part(purl, '@', 1) yields for the producer's versioned purl.
func TestOSVOSAdvisoryLookupKeysMeetTheInstalledKey(t *testing.T) {
	t.Parallel()

	for fixture, want := range map[string][]string{
		"debian-cve-2023-38039.json": {"os://debian/curl", "pkg:deb/debian/curl"},
		"alpine-cve-2023-27537.json": {"os://alpine/curl", "pkg:apk/alpine/curl"},
		"ubuntu-cve-2022-27775.json": {"os://ubuntu/curl", "pkg:deb/ubuntu/curl"},
		"ubuntu-cve-2016-5419.json":  {"os://ubuntu/curl", "pkg:deb/ubuntu/curl"},
	} {
		sources, keys := supplyChainImpactOSPackageAdvisoryTargets(convertedOSVFixture(t, fixture, false))
		if len(sources) != 1 {
			t.Fatalf("%s: sources = %v, want one vendor", fixture, sources)
		}
		if !slices.Equal(keys, want) {
			t.Fatalf("%s: keys = %v, want %v", fixture, keys, want)
		}
	}
}

// TestPURLPackageKeyShapes pins the advisory-side key on the shapes OSV and
// the producers publish, and on the ones it must leave alone.
func TestPURLPackageKeyShapes(t *testing.T) {
	t.Parallel()

	for purl, want := range map[string]string{
		"pkg:deb/debian/curl?arch=source&distro=bookworm":                   "pkg:deb/debian/curl",
		"pkg:apk/alpine/curl?arch=source":                                   "pkg:apk/alpine/curl",
		"pkg:deb/ubuntu/curl?arch=source&distro=esm-infra%2Fbionic":         "pkg:deb/ubuntu/curl",
		"pkg:deb/ubuntu/curl@7.58.0-2ubuntu3.15?arch=source&distro=bionic":  "pkg:deb/ubuntu/curl",
		"pkg:deb/debian/curl@7.88.1-10+deb12u3?arch=amd64&distro=debian-12": "pkg:deb/debian/curl",
		"pkg:deb/debian/curl":               "pkg:deb/debian/curl",
		"pkg:npm/%40scope/name@1.0.0":       "pkg:npm/%40scope/name",
		"pkg:npm/@scope/name":               "pkg:npm/@scope/name",
		"pkg:golang/github.com/a/b#sub/dir": "pkg:golang/github.com/a/b",
		"  pkg:apk/alpine/curl@8.0.0-r0  ":  "pkg:apk/alpine/curl",
		"":                                  "",
	} {
		if got := purlPackageKey(purl); got != want {
			t.Errorf("purlPackageKey(%q) = %q, want %q", purl, got, want)
		}
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestOSVOSAdvisoryJoinsInstalledRowAndStatesReleaseAmbiguity pins the
// release-aware verdicts on live records after the #9806 PR-2b-1 activation.
// Every range now names its release lines, so only the host line's ranges
// and fixes decide. A host on a listed line reads the precise verdict —
// affected_exact when its line's range contains the version with no fix at
// or below it, known-fixed when the version meets its line's own fix — and
// only a listed line can read exact. A host on a release the record does not
// list (bullseye, buster, Alpine v3.13, bionic) reads possibly_affected with
// os_release_not_in_advisory, never exact and never known-fixed. Both entry
// orders run every row, so the table also pins merge-order invariance.
func TestOSVOSAdvisoryJoinsInstalledRowAndStatesReleaseAmbiguity(t *testing.T) {
	t.Parallel()

	const (
		ambiguous = supplyChainVersionReasonOSReleaseAmbiguous
		unlisted  = supplyChainVersionReasonOSReleaseNotInAdvisory
	)
	tests := []struct {
		name       string
		fixture    string
		installed  installedOSRow
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		// DEBIAN-CVE-2023-38039: bookworm fixed 7.88.1-10+deb12u3, trixie
		// and forky fixed 8.3.0-1. The fixture lists Debian 12/13/14.
		// Listed host below its own fix: the host line decides, exact.
		{"debian bookworm below every fix", "file-debian-cve-2023-38039", debianCurl("12", "7.88.1-10+deb12u2"), SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange},
		// Unlisted host: no host line to decide, never exact/known-fixed.
		{"debian bullseye, a release the record does not list", "file-debian-cve-2023-38039", debianCurl("11", "7.74.0-1.3+deb11u7"), SupplyChainImpactPossiblyAffected, unlisted},
		{"debian buster, a release the record does not list", "file-debian-cve-2023-38039", debianCurl("10", "7.64.0-4+deb10u2"), SupplyChainImpactPossiblyAffected, unlisted},
		// Listed host at its own fix (equal, hence the exact known-fixed
		// reason, not the above-fix range reason).
		{"debian bookworm at its backported fix", "file-debian-cve-2023-38039", debianCurl("12", "7.88.1-10+deb12u3"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
		{"debian trixie below its fix", "file-debian-cve-2023-38039", debianCurl("13", "8.2.0-1"), SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange},
		{"debian trixie at its fix", "file-debian-cve-2023-38039", debianCurl("13", "8.3.0-1"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
		// ALPINE-CVE-2023-27537: v3.14-v3.16 fixed 8.0.1-r0, v3.17
		// 7.88.1-r1, v3.18 onward 8.0.0-r0. Purl pkg:apk/alpine/curl?arch=source.
		// The fixture lists v3.14-v3.24.
		{"alpine below every fix", "file-alpine-cve-2023-27537", alpineCurl("3.18.4", "7.88.1-r0"), SupplyChainImpactAffectedExact, supplyChainVersionReasonAPKAffectedRange},
		{"alpine v3.13, a release the record does not list", "file-alpine-cve-2023-27537", alpineCurl("3.13.12", "7.79.1-r0"), SupplyChainImpactPossiblyAffected, unlisted},
		{"alpine v3.18 at its fix", "file-alpine-cve-2023-27537", alpineCurl("3.18.4", "8.0.0-r0"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonAPKExactKnownFixed},
		// Listed host above its own fix: known-fixed via the range path.
		{"alpine above every fix", "file-alpine-cve-2023-27537", alpineCurl("3.18.4", "8.0.1-r0"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonAPKKnownFixed},
		// UBUNTU-CVE-2022-27775: versionless purls; focal fixed
		// 7.68.0-1ubuntu2.10, jammy 7.81.0-1ubuntu1.1. The fixture lists
		// focal and jammy only.
		{"ubuntu versionless purl below every fix", "file-ubuntu-cve-2022-27775", ubuntuCurl("20.04", "7.68.0-1ubuntu2.7"), SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange},
		{"ubuntu bionic, a release the record does not list", "file-ubuntu-cve-2022-27775", ubuntuCurl("18.04", "7.58.0-2ubuntu3.16"), SupplyChainImpactPossiblyAffected, unlisted},
		{"ubuntu versionless purl jammy below its fix", "file-ubuntu-cve-2022-27775", ubuntuCurl("22.04", "7.81.0-1ubuntu1"), SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange},
		{"ubuntu versionless purl jammy at its fix", "file-ubuntu-cve-2022-27775", ubuntuCurl("22.04", "7.81.0-1ubuntu1.1"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
		// UBUNTU-CVE-2016-5419: versioned purls (pkg:deb/ubuntu/curl@<v>?...);
		// trusty fixed 7.35.0-1ubuntu2.8, xenial 7.47.0-1ubuntu2.1. The
		// fixture lists trusty and xenial.
		{"ubuntu versioned purl below every fix", "file-ubuntu-cve-2016-5419", ubuntuCurl("14.04", "7.35.0-1ubuntu2.7"), SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange},
		{"ubuntu versioned purl trusty at its fix", "file-ubuntu-cve-2016-5419", ubuntuCurl("14.04", "7.35.0-1ubuntu2.8"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
		{"ubuntu versioned purl above every fix", "file-ubuntu-cve-2016-5419", ubuntuCurl("16.04", "7.47.0-1ubuntu2.1"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
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
				if tc.wantReason != ambiguous && tc.wantReason != unlisted {
					return
				}
				if got.RuntimeReachability != "image_os_package" {
					t.Fatalf("RuntimeReachability = %q, want image_os_package for an installed row", got.RuntimeReachability)
				}
				wantMissing := supplyChainMissingOSReleaseLine
				if tc.wantReason == unlisted {
					// An unlisted host names the advisory's gap, not an
					// unknown host line: the record provably omits it.
					wantMissing = supplyChainMissingOSReleaseNotListed
				}
				if !slices.Contains(got.MissingEvidence, wantMissing) {
					t.Fatalf("MissingEvidence = %v, want %q", got.MissingEvidence, wantMissing)
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

	purlLessEnvelopes := convertedOSVFixture(t, "built-purlless", false)
	lodashRow := installedOSRow{vendor: "debian", release: "12", manager: "dpkg", name: "lodash", arch: "all", version: "4.17.20-1"}

	tests := []struct {
		name      string
		envelopes []facts.Envelope
	}{
		{"debian advisory against an ubuntu row", append(convertedOSVFixture(t, "file-debian-cve-2023-38039", false), ubuntuCurl("22.04", "7.88.1-10+deb12u2").envelope())},
		{"ubuntu advisory against a debian row", append(convertedOSVFixture(t, "file-ubuntu-cve-2022-27775", false), debianCurl("12", "7.68.0-1ubuntu2.7").envelope())},
		{"purl-less os entry", append(purlLessEnvelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope())},
		{"npm advisory against a same-named os row", append(convertedOSVFixture(t, "file-ghsa-35jh-r3h4-6jhm", false), lodashRow.envelope())},
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
		"file-debian-cve-2023-38039": {"os://debian/curl", "pkg:deb/debian/curl"},
		"file-alpine-cve-2023-27537": {"os://alpine/curl", "pkg:apk/alpine/curl"},
		"file-ubuntu-cve-2022-27775": {"os://ubuntu/curl", "pkg:deb/ubuntu/curl"},
		"file-ubuntu-cve-2016-5419":  {"os://ubuntu/curl", "pkg:deb/ubuntu/curl"},
		// The rpm advisories carry both the raw upstream namespace purl and
		// the canonical pkg:rpm/<vendor>/<name> key; the canonical key is
		// the one the installed row's split_part(purl, '@', 1) yields
		// (#9844). The os:// form keeps the advisory package id, which
		// uses the raw namespace.
		"file-alsa-2021-4511": {
			"os://almalinux/curl", "os://almalinux/libcurl", "os://almalinux/libcurl-devel", "os://almalinux/libcurl-minimal",
			"pkg:rpm/alma/curl", "pkg:rpm/alma/libcurl", "pkg:rpm/alma/libcurl-devel", "pkg:rpm/alma/libcurl-minimal",
			"pkg:rpm/almalinux/curl", "pkg:rpm/almalinux/libcurl", "pkg:rpm/almalinux/libcurl-devel", "pkg:rpm/almalinux/libcurl-minimal",
		},
		"file-rlsa-2021-1610": {
			"os://rocky-linux/curl",
			"pkg:rpm/rocky-linux/curl", "pkg:rpm/rocky/curl",
		},
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

// TestOSVOSAdvisoryJoinsAlmaRockyRows pins #9844 on live records. Each
// advisory purl names the upstream distro namespace (almalinux, rocky-linux)
// while the installed row keys on the canonical vendor (alma, rocky), so the
// join meets on pkg:rpm/<vendor>/curl, never on the raw purl. A vulnerable
// row reads possibly_affected with os_release_unverified, a row at the fix
// reads not_affected_known_fixed, and no OSV rpm fact is ever
// affected_exact (ruling D, inherited from #9848/#9900).
func TestOSVOSAdvisoryJoinsAlmaRockyRows(t *testing.T) {
	t.Parallel()

	const unverified = "os_release_unverified"
	tests := []struct {
		name       string
		fixture    string
		installed  installedOSRow
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		// ALSA-2021:4511 fixes curl at 7.61.1-22.el8.
		{"alma below the fix", "file-alsa-2021-4511", almaCurl("8", "7.61.1-18.el8"), SupplyChainImpactPossiblyAffected, unverified},
		{"alma at the fix", "file-alsa-2021-4511", almaCurl("8", "7.61.1-22.el8"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonRPMKnownFixed},
		{"alma above the fix", "file-alsa-2021-4511", almaCurl("8", "7.61.1-23.el8"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonRPMKnownFixed},
		// RLSA-2021:1610 fixes curl at 0:7.61.1-18.el8.
		{"rocky below the fix", "file-rlsa-2021-1610", rockyCurl("8", "0:7.61.1-16.el8"), SupplyChainImpactPossiblyAffected, unverified},
		{"rocky at the fix", "file-rlsa-2021-1610", rockyCurl("8", "0:7.61.1-18.el8"), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonRPMKnownFixed},
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
				// An ALSA or RLSA bulletin names its CVEs in upstream, so the
				// grouping pass (#9914) splits it into one finding per CVE; the
				// verdict is the same on every one.
				upstream := slices.Clone(osvFixtureUpstream(t, tc.fixture))
				slices.Sort(upstream)
				if len(findings) != len(upstream) {
					t.Fatalf("findings = %d, want one OS package finding per upstream CVE %v: %#v", len(findings), upstream, findings)
				}
				var gotCVEs []string
				for _, finding := range findings {
					gotCVEs = append(gotCVEs, finding.CVEID)
					assertSupplyChainImpactStatus(t, finding, tc.wantStatus)
					if finding.MatchReason != tc.wantReason {
						t.Fatalf("MatchReason = %q, want %q", finding.MatchReason, tc.wantReason)
					}
					if finding.ObservedVersion != tc.installed.version {
						t.Fatalf("ObservedVersion = %q, want %q", finding.ObservedVersion, tc.installed.version)
					}
				}
				slices.Sort(gotCVEs)
				if !slices.Equal(gotCVEs, upstream) {
					t.Fatalf("finding CVEIDs = %v, want %v", gotCVEs, upstream)
				}
				got := findings[0]
				if tc.wantReason != unverified {
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

// TestOSVOSAdvisoryJoinAlmaRockyNegatives pins the rows the rpm
// advisory-side key must not join: the other rpm distro, a redhat row, and a
// dpkg row for the same package name.
func TestOSVOSAdvisoryJoinAlmaRockyNegatives(t *testing.T) {
	t.Parallel()

	redhatRow := installedOSRow{vendor: "redhat", release: "9.2", manager: "rpm", name: "curl", arch: "x86_64", version: "1:3.0.7-18.el9_2"}
	tests := []struct {
		name      string
		envelopes []facts.Envelope
	}{
		{"alma advisory against a rocky row", append(convertedOSVFixture(t, "file-alsa-2021-4511", false), rockyCurl("8", "7.61.1-18.el8").envelope())},
		{"rocky advisory against an alma row", append(convertedOSVFixture(t, "file-rlsa-2021-1610", false), almaCurl("8", "0:7.61.1-16.el8").envelope())},
		{"alma advisory against a redhat row", append(convertedOSVFixture(t, "file-alsa-2021-4511", false), redhatRow.envelope())},
		{"alma advisory against a dpkg row", append(convertedOSVFixture(t, "file-alsa-2021-4511", false), debianCurl("12", "7.61.1-18.el8").envelope())},
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

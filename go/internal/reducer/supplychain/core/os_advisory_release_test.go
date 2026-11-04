// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestOSHostReleaseLine pins the installed side of the #9806 PR-2b key: the
// producer's distro_version (os-release VERSION_ID, else VERSION) maps to the
// same "<vendor>:<line>" the OSV converter tags ranges with.
func TestOSHostReleaseLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		distro, version, want string
	}{
		{"debian", "12", "debian:12"},
		{"debian", "12 (bookworm)", "debian:12"},
		{"debian", "6.0", "debian:6"},
		{"alpine", "3.18.4", "alpine:3.18"},
		{"alpine", "3.21", "alpine:3.21"},
		{"ubuntu", "22.04", "ubuntu:22.04"},
		{"Ubuntu", "24.04", "ubuntu:24.04"},
		{"alpine", "edge", ""},
		{"alpine", "3", ""},
		{"debian", "", ""},
		{"redhat", "9.2", ""},
	} {
		if got := osHostReleaseLine(tc.distro, tc.version); got != tc.want {
			t.Errorf("osHostReleaseLine(%q, %q) = %q, want %q", tc.distro, tc.version, got, tc.want)
		}
	}
}

func taggedRange(fixed string, releases ...string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind:     "ECOSYSTEM",
		Events:   []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: fixed}},
		Releases: releases,
	}
}

// osvDebianCurlFact is DEBIAN-CVE-2023-38039 as the converter emits it with
// release tags: bookworm fixed at deb12u3, trixie and forky at 8.3.0-1.
func osvDebianCurlFact() supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "DEBIAN-CVE-2023-38039", Source: "osv", AdvisoryID: "DEBIAN-CVE-2023-38039",
		PackageID: "os://debian/curl", Ecosystem: "os", Name: "curl",
		PURL:             "pkg:deb/debian/curl?arch=source&distro=bookworm",
		AffectedVersions: []string{"7.88.1-10", "7.88.1-10+deb12u1"},
		AffectedRanges: []supplychainmodel.AffectedRange{
			taggedRange("7.88.1-10+deb12u3", "debian:12"),
			taggedRange("8.3.0-1", "debian:13", "debian:14"),
		},
		FixedVersions: []string{"7.88.1-10+deb12u3", "8.3.0-1"},
	}
}

// TestSelectOSAdvisoryRelease pins the narrowing step: a fact whose ranges are
// all tagged keeps only the host line's ranges and fixes, a fact that names no
// range for the host's line is marked unlisted and left whole, and a fact the
// rule does not cover is returned unchanged.
func TestSelectOSAdvisoryRelease(t *testing.T) {
	t.Parallel()

	bookworm := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12"}
	bullseye := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "11"}

	t.Run("host line listed", func(t *testing.T) {
		t.Parallel()
		got := selectOSAdvisoryRelease([]supplychainmodel.AffectedPackage{osvDebianCurlFact()}, bookworm)
		if len(got) != 1 {
			t.Fatalf("packages = %d, want 1", len(got))
		}
		want := osvDebianCurlFact()
		want.AffectedRanges = want.AffectedRanges[:1]
		want.FixedVersions = []string{"7.88.1-10+deb12u3"}
		want.AffectedVersions = nil
		want.ReleaseLine = "debian:12"
		if !reflect.DeepEqual(got[0], want) {
			t.Fatalf("selected = %+v, want %+v", got[0], want)
		}
	})
	t.Run("host line unlisted", func(t *testing.T) {
		t.Parallel()
		got := selectOSAdvisoryRelease([]supplychainmodel.AffectedPackage{osvDebianCurlFact()}, bullseye)
		want := osvDebianCurlFact()
		// The fact keeps every range for the weak read, but its fixed
		// versions are blanked: every one names another line's fix (#9806,
		// ruling Q2.9-Q2.11).
		want.FixedVersions = nil
		want.ReleaseUnlisted = true
		if !reflect.DeepEqual(got[0], want) {
			t.Fatalf("selected = %+v, want the whole fact marked unlisted %+v", got[0], want)
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*supplychainmodel.AffectedPackage)
		row  supplychainmodel.OSPackage
	}{
		{"one range untagged", func(p *supplychainmodel.AffectedPackage) { p.AffectedRanges[1].Releases = nil }, bookworm},
		{"no range tagged", func(p *supplychainmodel.AffectedPackage) {
			for i := range p.AffectedRanges {
				p.AffectedRanges[i].Releases = nil
			}
		}, bookworm},
		{"host release unknown", func(*supplychainmodel.AffectedPackage) {}, supplychainmodel.OSPackage{Distro: "debian"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkg := osvDebianCurlFact()
			tc.edit(&pkg)
			want := osvDebianCurlFact()
			tc.edit(&want)
			got := selectOSAdvisoryRelease([]supplychainmodel.AffectedPackage{pkg}, tc.row)
			if !reflect.DeepEqual(got[0], want) {
				t.Fatalf("selected = %+v, want the fact unchanged %+v", got[0], want)
			}
		})
	}
	t.Run("input not mutated", func(t *testing.T) {
		t.Parallel()
		in := []supplychainmodel.AffectedPackage{osvDebianCurlFact()}
		_ = selectOSAdvisoryRelease(in, bookworm)
		if !reflect.DeepEqual(in[0], osvDebianCurlFact()) {
			t.Fatalf("input changed to %+v; the shared index slice must stay whole for other rows", in[0])
		}
	})
}

// TestOSVSelectedReleaseDecidesExactly pins the verdicts a narrowed fact gives:
// the host line's own range is release evidence, so a version below its fix is
// affected_exact and a version at or above it is known-fixed on the host's own
// fix, where PR-2a could only say possibly_affected.
func TestOSVSelectedReleaseDecidesExactly(t *testing.T) {
	t.Parallel()

	bookworm := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12"}
	for _, tc := range []struct {
		observed   string
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		{"7.88.1-10+deb12u2", SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGAffectedRange},
		{"7.88.1-10+deb12u3", SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
		{"7.88.1-10+deb12u12", SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGKnownFixed},
	} {
		pkgs := selectOSAdvisoryRelease([]supplychainmodel.AffectedPackage{osvDebianCurlFact()}, bookworm)
		decision := evaluateSupplyChainVersionMatch("os", tc.observed, "", singleFixedVersion(pkgs[0].FixedVersions), pkgs)
		if decision.Status != tc.wantStatus || decision.Reason != tc.wantReason {
			t.Errorf("observed %s: decision = %s/%s, want %s/%s", tc.observed, decision.Status, decision.Reason, tc.wantStatus, tc.wantReason)
		}
	}
}

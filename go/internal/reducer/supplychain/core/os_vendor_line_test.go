// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestOSAdvisoryPURLReleaseLine pins the line reader (#9945): the distro
// qualifier is read through the same vector table that keys the installed row,
// and anything it cannot map reads "" so the join fails closed.
func TestOSAdvisoryPURLReleaseLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, purl, want string }{
		{"debian major", "pkg:deb/debian/curl?arch=amd64&distro=debian-12", "debian:12"},
		{"debian point release keeps the major", "pkg:deb/debian/curl?distro=debian-12.5", "debian:12"},
		{"ubuntu major.minor", "pkg:deb/ubuntu/curl?distro=ubuntu-22.04", "ubuntu:22.04"},
		{"alpine patch release keeps major.minor", "pkg:apk/alpine/curl?distro=alpine-3.18.4", "alpine:3.18"},
		{"alpine major.minor", "pkg:apk/alpine/curl?distro=alpine-3.18", "alpine:3.18"},
		{"upper case value", "pkg:deb/debian/curl?distro=DEBIAN-11", "debian:11"},
		{"upper case key", "pkg:deb/debian/curl?DISTRO=debian-11", "debian:11"},
		{"qualifier first", "pkg:deb/debian/curl?distro=debian-11&arch=amd64", "debian:11"},
		{"qualifier last", "pkg:deb/debian/curl?arch=amd64&distro=debian-11", "debian:11"},
		{"versioned purl", "pkg:deb/debian/curl@1:1.0-3?distro=debian-11", "debian:11"},
		{"subpath after the qualifiers", "pkg:deb/debian/curl?distro=debian-11#a/b", "debian:11"},
		{"surrounding space", "  pkg:deb/debian/curl?distro=debian-11  ", "debian:11"},
		{"percent-encoded value", "pkg:deb/debian/curl?distro=debian%2D11", "debian:11"},
		{"repeated qualifier naming one line", "pkg:deb/debian/curl?distro=debian-11&distro=Debian-11.4", "debian:11"},

		{"empty purl", "", ""},
		{"no qualifier", "pkg:deb/debian/curl", ""},
		{"qualifier without distro", "pkg:deb/debian/curl?arch=amd64", ""},
		{"empty distro value", "pkg:deb/debian/curl?distro=", ""},
		{"distro key without a value", "pkg:deb/debian/curl?distro", ""},
		{"codename", "pkg:deb/debian/curl?distro=bookworm", ""},
		{"vendor and nothing else", "pkg:deb/debian/curl?distro=debian", ""},
		{"vendor with an empty release", "pkg:deb/debian/curl?distro=debian-", ""},
		{"non-numeric release", "pkg:apk/alpine/curl?distro=alpine-edge", ""},
		{"alpine release without minor", "pkg:apk/alpine/curl?distro=alpine-3", ""},
		{"vendor with no vector row", "pkg:rpm/rocky/curl?distro=rocky-9", ""},
		{"bad percent escape", "pkg:deb/debian/curl?distro=debian%zz11", ""},
		{"conflicting repeated qualifiers", "pkg:deb/debian/curl?distro=debian-11&distro=debian-12", ""},
		{"one readable and one unreadable repeat", "pkg:deb/debian/curl?distro=debian-11&distro=bookworm", ""},
		{"qualifier after the fragment is ignored", "pkg:deb/debian/curl#frag?distro=debian-11", ""},
	} {
		if got := osAdvisoryPURLReleaseLine(tc.purl); got != tc.want {
			t.Errorf("%s: osAdvisoryPURLReleaseLine(%q) = %q, want %q", tc.name, tc.purl, got, tc.want)
		}
	}
}

func vendorLineFact(source, purl string, versions ...string) supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "CVE-2099-9945", Source: source, AdvisoryID: "VENDOR-9945-1",
		PackageID: "os://debian/curl", Ecosystem: "os", Name: "curl", PURL: purl,
		AffectedVersions: versions, FixedVersions: []string{"1:1.0-9"},
		AffectedRanges: []supplychainmodel.AffectedRange{osIntroducedFixedRange("0", "1:1.0-9")},
	}
}

var bookwormRow = supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12", VendorAdvisorySource: "debian"}

// TestSelectOSVendorFactLine pins the three outcomes of the line check on the
// fact copy the matcher reads, and that the input slice is left alone.
func TestSelectOSVendorFactLine(t *testing.T) {
	t.Parallel()
	const dir = "pkg:deb/debian/curl?distro="
	for _, tc := range []struct {
		name  string
		fact  supplychainmodel.AffectedPackage
		row   supplychainmodel.OSPackage
		check func(t *testing.T, got supplychainmodel.AffectedPackage)
	}{
		{
			"line matches the host", vendorLineFact("debian", dir+"debian-12", "1:1.0-3"), bookwormRow,
			func(t *testing.T, got supplychainmodel.AffectedPackage) {
				want := vendorLineFact("debian", dir+"debian-12", "1:1.0-3")
				want.ReleaseLine = "debian:12"
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %+v, want only ReleaseLine set: %+v", got, want)
				}
			},
		},
		{
			"line is another release", vendorLineFact("debian", dir+"debian-11", "1:1.0-3"), bookwormRow,
			func(t *testing.T, got supplychainmodel.AffectedPackage) {
				if !got.ReleaseUnlisted || !got.VendorLineUnverified || got.ReleaseLine != "" {
					t.Fatalf("flags = unlisted %v unverified %v line %q, want unlisted and unverified", got.ReleaseUnlisted, got.VendorLineUnverified, got.ReleaseLine)
				}
				// Another release's list and fix must not reach the host's verdict.
				if got.FixedVersions != nil || got.AffectedVersions != nil {
					t.Fatalf("fixed %v, versions %v, want both cleared", got.FixedVersions, got.AffectedVersions)
				}
				if osAffectedFactCarriesReleaseEvidence(got) {
					t.Fatal("a foreign-line vendor fact must carry no release evidence")
				}
			},
		},
		{
			"fact line unreadable", vendorLineFact("debian", dir+"bookworm", "1:1.0-3"), bookwormRow,
			func(t *testing.T, got supplychainmodel.AffectedPackage) {
				want := vendorLineFact("debian", dir+"bookworm", "1:1.0-3")
				want.VendorLineUnverified = true
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %+v, want only VendorLineUnverified set: %+v", got, want)
				}
				if osAffectedFactCarriesReleaseEvidence(got) {
					t.Fatal("an unverified vendor fact must carry no release evidence")
				}
			},
		},
		{
			"host line unknown", vendorLineFact("debian", dir+"debian-12", "1:1.0-3"),
			supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "testing", VendorAdvisorySource: "debian"},
			func(t *testing.T, got supplychainmodel.AffectedPackage) {
				if !got.VendorLineUnverified || got.ReleaseLine != "" || osAffectedFactCarriesReleaseEvidence(got) {
					t.Fatalf("got %+v, want unverified with no evidence", got)
				}
			},
		},
		{
			"source spelled in upper case", vendorLineFact("Debian", dir+"debian-12", "1:1.0-3"), bookwormRow,
			func(t *testing.T, got supplychainmodel.AffectedPackage) {
				if got.ReleaseLine != "debian:12" {
					t.Fatalf("ReleaseLine = %q, want debian:12", got.ReleaseLine)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := []supplychainmodel.AffectedPackage{tc.fact}
			snapshot := slices.Clone(in)
			got := selectOSAdvisoryRelease(in, tc.row)
			if len(got) != 1 {
				t.Fatalf("packages = %d, want 1", len(got))
			}
			if !reflect.DeepEqual(in, snapshot) {
				t.Fatalf("input slice was modified: %+v, want %+v", in, snapshot)
			}
			tc.check(t, got[0])
			// Selecting again changes nothing: a retry cannot move a verdict.
			if again := selectOSAdvisoryRelease(got, tc.row); !reflect.DeepEqual(again, got) {
				t.Fatalf("selection is not idempotent: %+v then %+v", got, again)
			}
		})
	}
}

// TestSelectOSVendorFactLineLeavesOtherFactsAlone pins the negative: only the
// installed row's own vendor facts are line-checked. An OSV fact, an NVD fact
// and a fact of another vendor are returned as they were, and an input with
// none of them allocates nothing.
func TestSelectOSVendorFactLineLeavesOtherFactsAlone(t *testing.T) {
	const purl = "pkg:deb/debian/curl?distro=debian-11"
	untaggedOSV := osvDebianCurlFact()
	untaggedOSV.AffectedRanges = []supplychainmodel.AffectedRange{{Kind: "ECOSYSTEM", Events: untaggedOSV.AffectedRanges[0].Events}}
	pkgs := []supplychainmodel.AffectedPackage{
		untaggedOSV,
		vendorLineFact("nvd", purl, "1:1.0-3"),
		vendorLineFact("glad", purl, "1:1.0-3"),
		vendorLineFact("ubuntu", purl, "1:1.0-3"),
		vendorLineFact("", purl, "1:1.0-3"),
	}
	got := selectOSAdvisoryRelease(pkgs, bookwormRow)
	if !reflect.DeepEqual(got, pkgs) {
		t.Fatalf("selection changed facts that are not the host's vendor facts: %+v", got)
	}
	if allocs := testing.AllocsPerRun(50, func() { _ = selectOSAdvisoryRelease(pkgs, bookwormRow) }); allocs != 0 {
		t.Fatalf("allocs = %v, want 0 when no fact needs the line check", allocs)
	}
	// A row with no vendor advisory source has no vendor facts of its own.
	if got := selectOSAdvisoryRelease(pkgs[1:2], supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12"}); !reflect.DeepEqual(got, pkgs[1:2]) {
		t.Fatalf("a row without a vendor source line-checked a fact: %+v", got)
	}
}

// TestSelectOSVendorFactLineIsOrderIndependent pins that a verdict never
// depends on where a fact sits among its siblings: every permutation of a
// mixed group reads the same per-fact result.
func TestSelectOSVendorFactLineIsOrderIndependent(t *testing.T) {
	t.Parallel()
	mixed := []supplychainmodel.AffectedPackage{
		vendorLineFact("debian", "pkg:deb/debian/curl?distro=debian-12", "1:1.0-3"),
		vendorLineFact("debian", "pkg:deb/debian/curl?distro=debian-11", "1:1.0-3"),
		vendorLineFact("debian", "pkg:deb/debian/curl", "1:1.0-3"),
		osvDebianCurlFact(),
	}
	for i := range mixed {
		mixed[i].AdvisoryID = "ID-" + string(rune('A'+i))
	}
	byID := func(pkgs []supplychainmodel.AffectedPackage) map[string]supplychainmodel.AffectedPackage {
		out := map[string]supplychainmodel.AffectedPackage{}
		for _, p := range pkgs {
			out[p.AdvisoryID] = p
		}
		return out
	}
	want := byID(selectOSAdvisoryRelease(mixed, bookwormRow))
	for _, perm := range [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}, {0, 1, 2, 3}} {
		ordered := make([]supplychainmodel.AffectedPackage, 0, len(perm))
		for _, at := range perm {
			ordered = append(ordered, mixed[at])
		}
		if got := byID(selectOSAdvisoryRelease(ordered, bookwormRow)); !reflect.DeepEqual(got, want) {
			t.Fatalf("order %v changed the selection: %+v, want %+v", perm, got, want)
		}
	}
}

// TestOSAdvisoryReleaseResultVendorLine pins the new closed result values for a
// vendor fact joined to a row whose vendor advisory source it carries.
func TestOSAdvisoryReleaseResultVendorLine(t *testing.T) {
	t.Parallel()
	unknownHost := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "testing", VendorAdvisorySource: "debian"}
	for _, tc := range []struct {
		name string
		pkg  supplychainmodel.AffectedPackage
		row  supplychainmodel.OSPackage
		want string
	}{
		{"line matches", vendorLineFact("debian", "pkg:deb/debian/curl?distro=debian-12"), bookwormRow, osAdvisoryReleaseVendorSource},
		{"line differs", vendorLineFact("debian", "pkg:deb/debian/curl?distro=debian-11"), bookwormRow, osAdvisoryReleaseVendorLineMismatch},
		{"another vendor's line", vendorLineFact("debian", "pkg:deb/debian/curl?distro=ubuntu-22.04"), bookwormRow, osAdvisoryReleaseVendorLineMismatch},
		{"no qualifier", vendorLineFact("debian", "pkg:deb/debian/curl"), bookwormRow, osAdvisoryReleaseVendorLineUnknown},
		{"codename", vendorLineFact("debian", "pkg:deb/debian/curl?distro=bookworm"), bookwormRow, osAdvisoryReleaseVendorLineUnknown},
		{"no purl", vendorLineFact("debian", ""), bookwormRow, osAdvisoryReleaseVendorLineUnknown},
		{"host line unknown", vendorLineFact("debian", "pkg:deb/debian/curl?distro=debian-12"), unknownHost, osAdvisoryReleaseVendorLineUnknown},
		// A non-OSV fact that is not the row's own vendor source is not
		// line-checked and keeps its earlier label.
		{"other source", vendorLineFact("nvd", "pkg:deb/debian/curl?distro=debian-11"), bookwormRow, osAdvisoryReleaseVendorSource},
	} {
		if got := osAdvisoryReleaseResult(tc.pkg, tc.row); got != tc.want {
			t.Errorf("%s: osAdvisoryReleaseResult = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestVendorLineJoinStatsCountEachOutcome drives the production count path: a
// vendor fact joined to a host reports vendor_source, vendor_line_mismatch or
// vendor_line_unknown on the joined cell, and the success-line sub-signals carry
// the same counts. A fact that the old code counted as vendor_source for every
// line would fail the mismatch and unknown rows.
func TestVendorLineJoinStatsCountEachOutcome(t *testing.T) {
	t.Parallel()
	s := vendorLineScenarios[0]
	for _, tc := range []struct {
		name, purl, wantResult, subSignal string
	}{
		{"matching line", s.purl(s.matchLine), osAdvisoryReleaseVendorSource, ""},
		{"other line", s.purl(s.otherLine), osAdvisoryReleaseVendorLineMismatch, osAdvisoriesVendorLineMismatchSubSignal},
		{"no line", "pkg:deb/debian/curl?arch=amd64", osAdvisoryReleaseVendorLineUnknown, osAdvisoriesVendorLineUnknownSubSignal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes := []facts.Envelope{
				vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5),
				s.affectedEnvelope(tc.purl),
				s.row(s.hostRelease).envelope(),
			}
			_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(envelopes)
			if err != nil {
				t.Fatalf("build findings: %v", err)
			}
			want := map[osAdvisoryJoinKey]int{{"debian", osAdvisoryJoinOutcomeJoined, tc.wantResult}: 1}
			if !maps.Equal(stats.osAdvisoryJoins, want) {
				t.Fatalf("osAdvisoryJoins = %v, want %v", stats.osAdvisoryJoins, want)
			}
			subSignals := map[string]float64{}
			SupplyChainImpactHandler{}.recordBuildStats(context.Background(), stats, subSignals)
			for _, name := range []string{osAdvisoriesVendorLineMismatchSubSignal, osAdvisoriesVendorLineUnknownSubSignal} {
				wantCount := 0.0
				if name == tc.subSignal {
					wantCount = 1
				}
				if got, ok := subSignals[name]; !ok || got != wantCount {
					t.Errorf("%s = %v (present %v), want %v", name, got, ok, wantCount)
				}
			}
		})
	}
}

// TestVendorLineAgainstHostLineOSV pins the sharper variant (#9945): a
// host-line-tagged OSV fact that says the host is fixed, against a vendor fact
// whose list names the host's version string. The vendor fact only overrides
// the OSV fact when its own line is verified as the host's.
func TestVendorLineAgainstHostLineOSV(t *testing.T) {
	t.Parallel()
	const host = "7.88.1-10+deb12u3" // exactly the bookworm fix in the tagged OSV record
	osv := convertedOSVFixture(t, "debian-cve-2023-38039.json", false)
	vendor := func(purl string) facts.Envelope {
		return facts.Envelope{FactID: "vendor-9945", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
			"cve_id": "DEBIAN-CVE-2023-38039", "advisory_id": "DSA-9945-1", "source": "debian",
			"package_id": "os://debian/curl", "ecosystem": "os", "package_name": "curl",
			"affected_versions": []any{host}, "purl": purl,
		}}
	}
	row := debianCurl("12", host).envelope()
	for _, tc := range []struct {
		name       string
		purl       string
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		// Verified for the host's line: the vendor list is exact evidence (#9929).
		{"vendor line is the host's", "pkg:deb/debian/curl?arch=source&distro=debian-12", SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGExactAffected},
		// Another release's list is excluded; the host-line OSV fact decides.
		{"vendor line is another release", "pkg:deb/debian/curl?arch=source&distro=debian-11", SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGExactKnownFixed},
		// No readable line: the list is no release evidence, so it only holds
		// the host at possibly_affected like any untagged list that names it.
		{"vendor line unreadable", "pkg:deb/debian/curl?arch=source&distro=bookworm", SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseAmbiguous},
		{"vendor purl has no distro qualifier", "pkg:deb/debian/curl?arch=source", SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseAmbiguous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var verdicts []SupplyChainImpactFinding
			for _, order := range [][]facts.Envelope{
				append(slices.Clone(osv), vendor(tc.purl), row),
				{row, vendor(tc.purl), osv[2], osv[1], osv[0]},
			} {
				findings := BuildSupplyChainImpactFindings(order)
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
				}
				verdicts = append(verdicts, findings[0])
				assertVendorLineVerdict(t, findings[0], tc.wantStatus, tc.wantReason)
			}
			if verdicts[0].FixedVersion != verdicts[1].FixedVersion {
				t.Fatalf("fact order changed the fixed version: %q vs %q", verdicts[0].FixedVersion, verdicts[1].FixedVersion)
			}
		})
	}
}

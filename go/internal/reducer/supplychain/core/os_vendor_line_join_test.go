// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// vendorLineCVE is the CVE every vendor-line scenario fact names.
const vendorLineCVE = "CVE-2099-9945"

// vendorLineScenario is one OS family and join derivation for the vendor
// release-line check (#9945). advisoryPURL has one %s for the distro
// qualifier value; row builds the installed row for a host release.
type vendorLineScenario struct {
	name       string
	source     string // vendor source of the advisory fact
	packageID  string
	ecosystem  string
	advisory   string // advisory package name
	purlFormat string
	version    string // installed version the fact's list names
	// matchLine and otherLine are distro qualifier values for the host's own
	// release and for a different release of the same vendor.
	matchLine, otherLine string
	row                  func(release string) installedOSRow
	hostRelease          string
	derivation           string
	exactReason          string
}

var vendorLineScenarios = []vendorLineScenario{
	{
		name: "dpkg binary", source: "debian", packageID: "pkg:deb/debian/curl", ecosystem: "deb", advisory: "curl",
		purlFormat: "pkg:deb/debian/curl?arch=amd64&distro=%s", version: "1:1.0-3",
		matchLine: "debian-12", otherLine: "debian-11", hostRelease: "12",
		row:        func(release string) installedOSRow { return debianCurl(release, "1:1.0-3") },
		derivation: osPackageMatchDerivationBinary, exactReason: supplyChainVersionReasonDPKGExactAffected,
	},
	{
		name: "dpkg source", source: "debian", packageID: "pkg:deb/debian/openssl", ecosystem: "deb", advisory: "openssl",
		purlFormat: "pkg:deb/debian/openssl?arch=source&distro=%s", version: "3.0.9-1",
		matchLine: "debian-12", otherLine: "debian-11", hostRelease: "12",
		row:        func(release string) installedOSRow { return libsslRow("debian", release, "3.0.9-1", "openssl") },
		derivation: osPackageMatchDerivationSource, exactReason: supplyChainVersionReasonDPKGExactAffected,
	},
	{
		name: "apk binary", source: "alpine", packageID: "pkg:apk/alpine/curl", ecosystem: "apk", advisory: "curl",
		purlFormat: "pkg:apk/alpine/curl?arch=x86_64&distro=%s", version: "8.15.0-r4",
		matchLine: "alpine-3.18", otherLine: "alpine-3.19", hostRelease: "3.18.4",
		row:        func(release string) installedOSRow { return alpineCurl(release, "8.15.0-r4") },
		derivation: osPackageMatchDerivationBinary, exactReason: supplyChainVersionReasonAPKExactAffected,
	},
	{
		name: "apk source", source: "alpine", packageID: "pkg:apk/alpine/curl", ecosystem: "apk", advisory: "curl",
		purlFormat: "pkg:apk/alpine/curl?arch=source&distro=%s", version: "8.15.0-r4",
		matchLine: "alpine-3.18", otherLine: "alpine-3.19", hostRelease: "3.18.4",
		row: func(release string) installedOSRow {
			return installedOSRow{
				vendor: "alpine", release: release, manager: "apk", name: "libcurl", arch: "x86_64",
				version: "8.15.0-r4", source: "curl",
			}
		},
		derivation: osPackageMatchDerivationSource, exactReason: supplyChainVersionReasonAPKExactAffected,
	},
}

// affectedEnvelope is the vendor-sourced affected_package fact. purl is the
// full purl, or "" for a fact that carries none.
func (s vendorLineScenario) affectedEnvelope(purl string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "VENDOR-9945-1", "source": s.source,
		"package_id": s.packageID, "ecosystem": s.ecosystem, "package_name": s.advisory,
		"affected_versions": []any{s.version},
	}
	if purl != "" {
		payload["purl"] = purl
	}
	return facts.Envelope{FactID: "affected-vendor-9945", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// assertVendorLineVerdict fails with the verdict, not the whole finding.
func assertVendorLineVerdict(t *testing.T, got SupplyChainImpactFinding, status SupplyChainImpactStatus, reason string) {
	t.Helper()
	if got.Status != status || got.MatchReason != reason {
		t.Fatalf("verdict = %s/%s, want %s/%s", got.Status, got.MatchReason, status, reason)
	}
}

func (s vendorLineScenario) purl(line string) string {
	return fmt.Sprintf(s.purlFormat, line)
}

// vendorLineFindings joins the vendor fact against one host release in both
// fact orders and returns the one finding each order produced, after checking
// the two orders agree on the verdict.
func vendorLineFindings(t *testing.T, s vendorLineScenario, purl, hostRelease string) SupplyChainImpactFinding {
	t.Helper()
	cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
	affected := s.affectedEnvelope(purl)
	host := s.row(hostRelease).envelope()
	var got []SupplyChainImpactFinding
	for _, order := range [][]facts.Envelope{{cve, affected, host}, {host, affected, cve}} {
		findings := BuildSupplyChainImpactFindings(slices.Clone(order))
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
		}
		got = append(got, findings[0])
	}
	if got[0].Status != got[1].Status || got[0].MatchReason != got[1].MatchReason ||
		got[0].OSPackageMatchDerivation != got[1].OSPackageMatchDerivation {
		t.Fatalf("fact order changed the verdict: %s/%s/%s vs %s/%s/%s",
			got[0].Status, got[0].MatchReason, got[0].OSPackageMatchDerivation,
			got[1].Status, got[1].MatchReason, got[1].OSPackageMatchDerivation)
	}
	return got[0]
}

// TestVendorFactForAnotherReleaseLineIsNotExactForTheHost is the #9945 RED
// case: a vendor-sourced fact published for one release whose exact-version
// list names the installed version must not read affected_exact for a host on
// a different release. It runs the binary and source-package joins for dpkg
// and apk, in both fact orders.
func TestVendorFactForAnotherReleaseLineIsNotExactForTheHost(t *testing.T) {
	t.Parallel()
	for _, s := range vendorLineScenarios {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			got := vendorLineFindings(t, s, s.purl(s.otherLine), s.hostRelease)
			assertVendorLineVerdict(t, got, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
			if got.OSPackageMatchDerivation != s.derivation {
				t.Fatalf("OSPackageMatchDerivation = %q, want %q", got.OSPackageMatchDerivation, s.derivation)
			}
		})
	}
}

// TestVendorFactForAnotherVendorsReleaseIsNotExactForTheHost pins that a
// readable line of a different vendor is a line mismatch, not an unknown line.
func TestVendorFactForAnotherVendorsReleaseIsNotExactForTheHost(t *testing.T) {
	t.Parallel()
	for _, s := range vendorLineScenarios {
		base, _, _ := strings.Cut(s.purl(s.matchLine), "?")
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			got := vendorLineFindings(t, s, base+"?distro=ubuntu-22.04", s.hostRelease)
			assertVendorLineVerdict(t, got, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
		})
	}
}

// TestVendorFactForTheHostsReleaseLineStaysExact is the GREEN control: the
// same fact read for a host on its own release keeps affected_exact.
func TestVendorFactForTheHostsReleaseLineStaysExact(t *testing.T) {
	t.Parallel()
	for _, s := range vendorLineScenarios {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			got := vendorLineFindings(t, s, s.purl(s.matchLine), s.hostRelease)
			assertVendorLineVerdict(t, got, SupplyChainImpactAffectedExact, s.exactReason)
			if got.OSPackageMatchDerivation != s.derivation {
				t.Fatalf("OSPackageMatchDerivation = %q, want %q", got.OSPackageMatchDerivation, s.derivation)
			}
		})
	}
}

// TestVendorFactWithoutLineEvidenceIsNeverExact pins fail-closed: a vendor
// fact whose release line cannot be read carries no release evidence, so its
// list match is possibly_affected like an untagged OSV fact's, never exact.
func TestVendorFactWithoutLineEvidenceIsNeverExact(t *testing.T) {
	t.Parallel()
	for _, s := range vendorLineScenarios {
		base, _, _ := strings.Cut(s.purl(s.matchLine), "?")
		cases := []struct{ name, purl string }{
			{"purl without a qualifier", base},
			{"empty distro qualifier", base + "?arch=amd64&distro="},
			{"codename the vector table cannot read", base + "?distro=bookworm"},
			{"vendor without a vector row", base + "?distro=rocky-9"},
			{"conflicting distro qualifiers", base + "?distro=" + s.matchLine + "&distro=" + s.otherLine},
		}
		if s.derivation == osPackageMatchDerivationBinary {
			// A source-package join needs the purl's key, so a fact without
			// one never joins and has no verdict to check.
			cases = append(cases, struct{ name, purl string }{"no purl at all", ""})
		}
		for _, tc := range cases {
			t.Run(s.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				got := vendorLineFindings(t, s, tc.purl, s.hostRelease)
				assertVendorLineVerdict(t, got, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified)
			})
		}
	}
}

// TestVendorFactLineSpellingsAllMatchTheHost pins the accepted spellings of a
// matching line: case, a versioned purl, a subpath, and a repeated identical
// qualifier all read the same line.
func TestVendorFactLineSpellingsAllMatchTheHost(t *testing.T) {
	t.Parallel()
	s := vendorLineScenarios[0]
	for name, purl := range map[string]string{
		"upper case":             "pkg:deb/debian/curl?arch=amd64&distro=DEBIAN-12",
		"versioned purl":         "pkg:deb/debian/curl@1:1.0-3?arch=amd64&distro=debian-12",
		"subpath":                "pkg:deb/debian/curl?distro=debian-12#sub/path",
		"repeated same distro":   "pkg:deb/debian/curl?distro=debian-12&distro=Debian-12",
		"minor of a major line":  "pkg:deb/debian/curl?distro=debian-12.5",
		"qualifier order varies": "pkg:deb/debian/curl?distro=debian-12&arch=amd64",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := vendorLineFindings(t, s, purl, s.hostRelease)
			assertVendorLineVerdict(t, got, SupplyChainImpactAffectedExact, s.exactReason)
		})
	}
}

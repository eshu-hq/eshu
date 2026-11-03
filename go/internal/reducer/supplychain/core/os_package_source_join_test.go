// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// opensslSourceAdvisoryRecord is a synthetic OSV record in the exact shape
// Debian publishes for a source package: the affected package names the
// source (openssl), not any binary built from it, with a versionless
// arch=source purl. It goes through the real in-tree converter like the
// committed fixtures, so the reducer sees the production advisory shape.
func opensslSourceAdvisoryRecord(ecosystem, purl string) vulnerabilityintelligence.OSVVulnerability {
	return vulnerabilityintelligence.OSVVulnerability{
		ID: "DEBIAN-CVE-2023-0464",
		Affected: []vulnerabilityintelligence.OSVAffected{{
			Package: vulnerabilityintelligence.OSVPackage{
				Name:      "openssl",
				Ecosystem: ecosystem,
				PURL:      purl,
			},
			Ranges: []vulnerabilityintelligence.OSVRange{{Type: "ECOSYSTEM", Events: []vulnerabilityintelligence.OSVEvent{
				{Introduced: "0"}, {Fixed: "3.0.11-1+deb12u2"},
			}}},
		}},
	}
}

func convertSourceAdvisoryRecord(t *testing.T, record vulnerabilityintelligence.OSVVulnerability) []facts.Envelope {
	t.Helper()
	return convertedOSVRecord(t, record, false)
}

// libsslRow is a Debian libssl3 installed row built from the openssl source
// package: the binary name differs from the source name, so only the
// source-key arm can join it (#9859).
func libsslRow(vendor, release, version, source string) installedOSRow {
	return installedOSRow{
		vendor:  vendor,
		release: release,
		manager: "dpkg",
		name:    "libssl3",
		arch:    "amd64",
		version: version,
		source:  source,
	}
}

// TestOSSourcePackageAdvisoryJoinsInstalledBinaryRow pins #9859: an OSV
// advisory naming a source package joins the installed binary rows built
// from it. The finding records the derivation so a reader can tell it apart
// from a binary-joined row without re-deriving the join.
func TestOSSourcePackageAdvisoryJoinsInstalledBinaryRow(t *testing.T) {
	t.Parallel()

	envelopes := append(
		convertSourceAdvisoryRecord(t, opensslSourceAdvisoryRecord(
			"Debian:12", "pkg:deb/debian/openssl?arch=source&distro=bookworm")),
		libsslRow("debian", "12", "3.0.9-1~deb12u1", "openssl").envelope(),
	)
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 source-joined OS package finding: %#v", len(findings), findings)
	}
	got := findings[0]
	if got.OSPackageMatchDerivation != osPackageMatchDerivationSource {
		t.Fatalf("OSPackageMatchDerivation = %q, want %q", got.OSPackageMatchDerivation, osPackageMatchDerivationSource)
	}
	if got.InstalledPackageName != "libssl3" {
		t.Fatalf("InstalledPackageName = %q, want %q", got.InstalledPackageName, "libssl3")
	}
	if got.SourcePackage != "openssl" {
		t.Fatalf("SourcePackage = %q, want %q", got.SourcePackage, "openssl")
	}
	// PackageName follows the advisory (openssl); the installed row's own
	// Name stays the binary key and is never rewritten to the source name.
	if got.PackageName != "openssl" {
		t.Fatalf("PackageName = %q, want the advisory name %q", got.PackageName, "openssl")
	}
	if got.InstalledPackageName == got.SourcePackage {
		t.Fatalf("InstalledPackageName = %q must stay the installed binary key, not the source name", got.InstalledPackageName)
	}
}

// TestInstalledOSPackageSourceKeyShapes pins the installed-side source key
// and its strict rejection set: only a clean source name re-keys the row,
// and the key is the advisory-side form the OSV purl derives to.
func TestInstalledOSPackageSourceKeyShapes(t *testing.T) {
	t.Parallel()

	const libssl = "pkg:deb/debian/libssl3@3.0.11"
	tests := []struct {
		name   string
		purl   string
		source string
		want   string
	}{
		// Last-segment replace on the version-stripped prefix.
		{"versioned", libssl, "openssl", "pkg:deb/debian/openssl"},
		{"qualified", libssl + "?arch=amd64", "openssl", "pkg:deb/debian/openssl"},
		{"unversioned", "pkg:deb/debian/openssl", "openssl", "pkg:deb/debian/openssl"},
		{"apk", "pkg:apk/alpine/libcurl@8.0", "curl", "pkg:apk/alpine/curl"},
		// Strict rejection set: the SQL CASE mirrors it, so any gap joins
		// in one engine and not the other.
		{"empty", libssl, "", ""},
		{"blank", libssl, "   ", ""},
		{"slash", libssl, "open/ssl", ""},
		{"at", libssl, "open@ssl", ""},
		{"question", libssl, "open?ssl", ""},
		{"hash", libssl, "open#ssl", ""},
		{"space", libssl, "open ssl", ""},
		{"nbsp", libssl, "open\u00a0ssl", ""},
		{"padded", libssl, "\u00a0openssl\u00a0", "pkg:deb/debian/openssl"},
		{"no slash in prefix", "garbage-without-slash", "openssl", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := InstalledOSPackageSourceKey(tc.purl, tc.source); got != tc.want {
				t.Fatalf("InstalledOSPackageSourceKey(%q, %q) = %q, want %q", tc.purl, tc.source, got, tc.want)
			}
		})
	}
}

// TestOSSourcePackageAdvisoryJoinCountsViaSourceSignal pins the #9859
// operator signal end to end: a source-joined finding counts
// os_advisories_joined and os_advisories_joined_via_source, while a
// binary-joined finding counts only os_advisories_joined.
func TestOSSourcePackageAdvisoryJoinCountsViaSourceSignal(t *testing.T) {
	t.Parallel()

	sourceEnvelopes := append(
		convertSourceAdvisoryRecord(t, opensslSourceAdvisoryRecord(
			"Debian:12", "pkg:deb/debian/openssl?arch=source&distro=bookworm")),
		libsslRow("debian", "12", "3.0.9-1~deb12u1", "openssl").envelope(),
	)
	_, _, sourceStats, err := buildSupplyChainImpactFindingsAndExclusions(sourceEnvelopes)
	if err != nil {
		t.Fatalf("build source-joined findings: %v", err)
	}
	if got := sourceStats.osAdvisoriesJoinedViaSource; got != 1 {
		t.Fatalf("osAdvisoriesJoinedViaSource = %d, want 1", got)
	}

	binaryEnvelopes := append(
		convertedOSVFixture(t, "debian-cve-2023-38039.json", false),
		debianCurl("12", "7.88.1-10+deb12u2").envelope(),
	)
	_, _, binaryStats, err := buildSupplyChainImpactFindingsAndExclusions(binaryEnvelopes)
	if err != nil {
		t.Fatalf("build binary-joined findings: %v", err)
	}
	if got := binaryStats.osAdvisoriesJoinedViaSource; got != 0 {
		t.Fatalf("osAdvisoriesJoinedViaSource = %d, want 0 on a binary join", got)
	}
}

// TestOSBinaryPackageAdvisoryJoinRecordsBinaryDerivation pins the unchanged
// arm: an advisory naming the installed binary row itself still joins on the
// binary key, with an empty SourcePackage.
func TestOSBinaryPackageAdvisoryJoinRecordsBinaryDerivation(t *testing.T) {
	t.Parallel()

	envelopes := append(
		convertedOSVFixture(t, "debian-cve-2023-38039.json", false),
		debianCurl("12", "7.88.1-10+deb12u2").envelope(),
	)
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 binary-joined OS package finding: %#v", len(findings), findings)
	}
	got := findings[0]
	if got.OSPackageMatchDerivation != osPackageMatchDerivationBinary {
		t.Fatalf("OSPackageMatchDerivation = %q, want %q", got.OSPackageMatchDerivation, osPackageMatchDerivationBinary)
	}
	if got.InstalledPackageName != "curl" {
		t.Fatalf("InstalledPackageName = %q, want %q", got.InstalledPackageName, "curl")
	}
	if got.SourcePackage != "" {
		t.Fatalf("SourcePackage = %q, want empty on a binary-joined row", got.SourcePackage)
	}
}

// TestOSSourcePackageAdvisoryJoinNegatives pins the rows the source-key arm
// must not join: another vendor's row, a lookalike source name, a row with
// no source at all, and a same-vendor binary row that must keep joining on
// the binary key.
func TestOSSourcePackageAdvisoryJoinNegatives(t *testing.T) {
	t.Parallel()

	advisory := func() []facts.Envelope {
		return convertSourceAdvisoryRecord(t, opensslSourceAdvisoryRecord(
			"Debian:12", "pkg:deb/debian/openssl?arch=source&distro=bookworm"))
	}

	tests := []struct {
		name      string
		installed installedOSRow
	}{
		{"vendor-cross row stays unmatched", libsslRow("ubuntu", "22.04", "3.0.9-1ubuntu1", "openssl")},
		// U+043E CYRILLIC SMALL LETTER O: visually identical to "openssl"
		// but a different key, so the verbatim source arm must not join it.
		{"lookalike source stays unmatched", libsslRow("debian", "12", "3.0.9-1~deb12u1", "\u043epenssl")},
		{"sourceless row stays unmatched", libsslRow("debian", "12", "3.0.9-1~deb12u1", "")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if findings := BuildSupplyChainImpactFindings(append(advisory(), tc.installed.envelope())); len(findings) != 0 {
				t.Fatalf("findings = %#v, want none: the source-key arm must not join this row", findings)
			}
		})
	}
}

// TestOSSourcePackageAdvisorySameNameRowJoinsOnce pins the no-dup rule: a
// row whose binary name equals its source name is indexed under both keys,
// but one advisory still yields exactly one finding.
func TestOSSourcePackageAdvisorySameNameRowJoinsOnce(t *testing.T) {
	t.Parallel()

	row := installedOSRow{
		vendor:  "debian",
		release: "12",
		manager: "dpkg",
		name:    "openssl",
		arch:    "amd64",
		version: "3.0.9-1~deb12u1",
		source:  "openssl",
	}
	envelopes := append(
		convertSourceAdvisoryRecord(t, opensslSourceAdvisoryRecord(
			"Debian:12", "pkg:deb/debian/openssl?arch=source&distro=bookworm")),
		row.envelope(),
	)
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1: name==source must not double-join: %#v", len(findings), findings)
	}
}

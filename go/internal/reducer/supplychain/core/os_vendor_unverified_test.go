// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestReleaseEvidenceIsOnlyAVerifiedLine pins the fail-closed rule (#9945): on
// the OS path a fact is release evidence only when ReleaseLine is set, which
// only a verified join sets. A vendor-sourced fact, a blank-source fact and a
// fact of any other non-OSV source that skipped selection carry none.
func TestReleaseEvidenceIsOnlyAVerifiedLine(t *testing.T) {
	t.Parallel()
	const purl = "pkg:deb/debian/curl?distro=debian-12"
	verified := vendorLineFact("debian", purl, "1:1.0-3")
	verified.ReleaseLine = "debian:12"
	for _, tc := range []struct {
		name string
		pkg  supplychainmodel.AffectedPackage
		want bool
	}{
		{"vendor fact with a verified line", verified, true},
		{"vendor fact that skipped selection", vendorLineFact("debian", purl, "1:1.0-3"), false},
		{"blank-source fact", vendorLineFact("", purl, "1:1.0-3"), false},
		{"nvd fact", vendorLineFact("nvd", purl, "1:1.0-3"), false},
		{"untagged osv fact", osvDebianCurlFact(), false},
	} {
		if got := osAffectedFactCarriesReleaseEvidence(tc.pkg); got != tc.want {
			t.Errorf("%s: osAffectedFactCarriesReleaseEvidence = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// withVendorLinePURL returns the affected_package envelope with its purl set,
// the carrier a vendor fact needs for the join to verify its release line.
func withVendorLinePURL(envelope facts.Envelope, purl string) facts.Envelope {
	envelope.Payload["purl"] = purl
	return envelope
}

// verifiedForOSHost marks every fact as verified against a host line, the
// release evidence a join grants, for tests that exercise the range matcher
// itself rather than the line check.
func verifiedForOSHost(pkgs []supplychainmodel.AffectedPackage) []supplychainmodel.AffectedPackage {
	out := slices.Clone(pkgs)
	for i := range out {
		out[i].ReleaseLine = "debian:12"
	}
	return out
}

// sbomOnlyVendorFinding builds the finding for a vendor fact that meets an SBOM
// component and no installed OS row, so the join and its line check never run
// on a host row.
func sbomOnlyVendorFinding(t *testing.T, s vendorLineScenario, purl string, mutate func(map[string]any)) SupplyChainImpactFinding {
	t.Helper()
	const digest = "sha256:ab000000000000000000000000000000000000000000000000000000000000ab"
	cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
	affected := s.affectedEnvelope(purl)
	if mutate != nil {
		mutate(affected.Payload)
	}
	component := sbomComponentImpactFactWithPackageIDVersion(
		"comp", "doc-1", s.packageID+"@"+s.version+"?distro="+s.matchLine, s.packageID, s.version)
	component.Payload["name"] = s.advisory
	attachment := sbomAttachmentImpactFact("att", "doc-1", digest)
	image := containerImageIdentityImpactFact("img", digest, "oci-registry://registry.example/app")
	var got []SupplyChainImpactFinding
	for _, order := range [][]facts.Envelope{
		{cve, affected, component, attachment, image},
		{image, attachment, component, affected, cve},
	} {
		findings := BuildSupplyChainImpactFindings(slices.Clone(order))
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
		}
		got = append(got, findings[0])
	}
	if got[0].Status != got[1].Status || got[0].MatchReason != got[1].MatchReason || got[0].FixedVersion != got[1].FixedVersion {
		t.Fatalf("fact order changed the finding: %s/%s/%q vs %s/%s/%q",
			got[0].Status, got[0].MatchReason, got[0].FixedVersion, got[1].Status, got[1].MatchReason, got[1].FixedVersion)
	}
	return got[0]
}

// TestSBOMOnlyVendorFactIsNeverExact pins the SBOM-only path (#9945). With no
// installed row there is no host line to verify a vendor fact against, so a
// list that names the component's version is possibly_affected whatever the
// fact's purl says: its own line, another release, a codename, or none. Reading
// the component purl's own distro qualifier as the host line is a separate
// design this change does not make.
func TestSBOMOnlyVendorFactIsNeverExact(t *testing.T) {
	t.Parallel()
	for _, s := range []vendorLineScenario{vendorLineScenarios[0], vendorLineScenarios[2]} {
		for name, purl := range map[string]string{
			"the component's own line": s.purl(s.matchLine),
			"another release":          s.purl(s.otherLine),
			"an unreadable line":       s.purl("edge-x"),
			"no purl":                  "",
		} {
			t.Run(s.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				got := sbomOnlyVendorFinding(t, s, purl, nil)
				assertVendorLineVerdict(t, got, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified)
			})
		}
	}
}

// TestSBOMOnlyVendorFixIsNeverPresentedAsTheHosts pins that the SBOM-only path
// withholds an unverifiable vendor fix: a fix at or below the component version
// must not read not_affected_known_fixed, and no fix is offered.
func TestSBOMOnlyVendorFixIsNeverPresentedAsTheHosts(t *testing.T) {
	t.Parallel()
	s := vendorLineScenarios[0]
	got := sbomOnlyVendorFinding(t, s, s.purl(s.matchLine), func(payload map[string]any) {
		delete(payload, "affected_versions")
		payload["fixed_versions"] = []any{versionBelowHost(s)}
	})
	// A record that carries only a fix has nothing left once the fix is withheld,
	// so the host reads possibly_affected with the no-match reason, not known fixed.
	assertVendorLineVerdict(t, got, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonNoAffectedMatch)
	if got.FixedVersion != "" || got.Remediation.FirstPatchedVersion != "" {
		t.Fatalf("fixed version %q, remediation %q, want both withheld", got.FixedVersion, got.Remediation.FirstPatchedVersion)
	}
}

// versionBelowHost is a version of the scenario's package family that sorts
// below the installed version its list names, a fix the host already has.
func versionBelowHost(s vendorLineScenario) string {
	if s.ecosystem == "apk" {
		return "8.15.0-r3"
	}
	return "1:1.0-2"
}

// unknownLineFixPayload turns the scenario fact into an unknown-line vendor
// record that carries only a fix at or below the host's version, as a scalar
// fix or as an ECOSYSTEM range.
func unknownLineFixPayload(fixed string, asRange bool) func(map[string]any) {
	return func(payload map[string]any) {
		delete(payload, "affected_versions")
		payload["fixed_versions"] = []any{fixed}
		if asRange {
			payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
				map[string]any{"introduced": "0"}, map[string]any{"fixed": fixed},
			}}}
		}
	}
}

// TestUnknownLineVendorFixIsNeverTheHostsFix pins F7 (#9945). A vendor record
// whose purl names no readable line describes one release of unknown identity,
// so its fix says nothing about the host's line. A fix at or below the host's
// version, scalar or range, must read possibly_affected with no fix offered,
// never not_affected_known_fixed: a range leaves os_release_unverified, a bare
// scalar fix leaves nothing to match once it is withheld. A verified own-line
// fact that names the host still decides.
func TestUnknownLineVendorFixIsNeverTheHostsFix(t *testing.T) {
	t.Parallel()
	for _, s := range []vendorLineScenario{vendorLineScenarios[0], vendorLineScenarios[2]} {
		for name, asRange := range map[string]bool{"scalar fix": false, "range fix": true} {
			wantReason := supplyChainVersionReasonOSReleaseUnverified
			if !asRange {
				wantReason = supplyChainVersionReasonNoAffectedMatch
			}
			t.Run(s.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
				unknown := s.affectedEnvelope(s.purl("edge-x"))
				unknownLineFixPayload(versionBelowHost(s), asRange)(unknown.Payload)
				host := s.row(s.hostRelease).envelope()
				var got []SupplyChainImpactFinding
				for _, order := range [][]facts.Envelope{{cve, unknown, host}, {host, unknown, cve}} {
					findings := BuildSupplyChainImpactFindings(slices.Clone(order))
					if len(findings) != 1 {
						t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
					}
					assertVendorLineVerdict(t, findings[0], SupplyChainImpactPossiblyAffected, wantReason)
					if findings[0].FixedVersion != "" || findings[0].Remediation.FirstPatchedVersion != "" {
						t.Fatalf("fixed version %q, remediation %q, want both withheld", findings[0].FixedVersion, findings[0].Remediation.FirstPatchedVersion)
					}
					got = append(got, findings[0])
				}
				if got[0].Status != got[1].Status || got[0].MatchReason != got[1].MatchReason {
					t.Fatalf("fact order changed the verdict: %s/%s vs %s/%s", got[0].Status, got[0].MatchReason, got[1].Status, got[1].MatchReason)
				}

				// A verified own-line fact that lists the host decides as before.
				own := s.affectedEnvelope(s.purl(s.matchLine))
				own.FactID, own.Payload["advisory_id"] = "affected-vendor-own", "VENDOR-9945-2"
				findings := BuildSupplyChainImpactFindings([]facts.Envelope{cve, unknown, own, host})
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
				}
				assertVendorLineVerdict(t, findings[0], SupplyChainImpactAffectedExact, s.exactReason)
			})
		}
	}
}

// TestUnknownLineVendorMalformedRangeStillFailsClosed pins ruling C2 (#9945):
// an unverified vendor fact gives the range verdict an unverified signal, but
// an unreadable range still reads possibly_affected with the malformed-range
// reason, so a corrupt advisory is never hidden behind the unverified read. It
// runs dpkg and apk through the joined row, in both fact orders.
func TestUnknownLineVendorMalformedRangeStillFailsClosed(t *testing.T) {
	t.Parallel()
	for _, s := range []vendorLineScenario{vendorLineScenarios[0], vendorLineScenarios[2]} {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
			unknown := s.affectedEnvelope(s.purl("edge-x"))
			delete(unknown.Payload, "affected_versions")
			unknown.Payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
				map[string]any{"introduced": "not a version"}, map[string]any{"fixed": "also not a version"},
			}}}
			host := s.row(s.hostRelease).envelope()
			for _, order := range [][]facts.Envelope{{cve, unknown, host}, {host, unknown, cve}} {
				findings := BuildSupplyChainImpactFindings(slices.Clone(order))
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
				}
				assertVendorLineVerdict(t, findings[0], SupplyChainImpactPossiblyAffected, supplyChainVersionReasonMalformedRange)
			}
		})
	}
}

// TestLanguageComponentPURLSkipsTheOSBoundaryWithoutAllocating pins the cost of
// the SBOM-only guard in classifySupplyChainImpactPackage (#9945): it runs for
// every component finding with no installed row, language packages included,
// so deciding that a purl is not an OS family must not allocate.
func TestLanguageComponentPURLSkipsTheOSBoundaryWithoutAllocating(t *testing.T) {
	purls := []string{
		"pkg:npm/%40scope/example@1.2.3",
		"pkg:pypi/example@1.2.3",
		"pkg:golang/github.com/example/module@v1.2.3",
		"pkg:maven/org.example/example@1.2.3?type=jar",
		"",
	}
	for _, purl := range purls {
		if got := osPackageFamilyFromPURL(purl); got != "" {
			t.Fatalf("osPackageFamilyFromPURL(%q) = %q, want a language purl to read as no OS family", purl, got)
		}
		if allocs := testing.AllocsPerRun(100, func() { _ = osPackageFamilyFromPURL(purl) }); allocs != 0 {
			t.Errorf("osPackageFamilyFromPURL(%q) allocs = %v, want 0", purl, allocs)
		}
	}
}

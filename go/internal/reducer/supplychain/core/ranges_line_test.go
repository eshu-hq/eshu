// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// semverLine builds one SEMVER range with an introduced..fixed pair.
func semverLine(introduced, fixed string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind: "SEMVER",
		Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: introduced},
			{Fixed: fixed},
		},
	}
}

// semverLastAffectedLine builds one SEMVER range closed by last_affected.
func semverLastAffectedLine(introduced, lastAffected string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind: "SEMVER",
		Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: introduced},
			{LastAffected: lastAffected},
		},
	}
}

func affectedPackageWithRanges(ranges ...supplychainmodel.AffectedRange) []supplychainmodel.AffectedPackage {
	return []supplychainmodel.AffectedPackage{{
		PackageID:      "pkg:npm/example",
		Ecosystem:      "npm",
		Name:           "example",
		AffectedRanges: ranges,
	}}
}

// TestNPMPerLineKnownFixedDecision pins the per-line known-fixed contract
// (#9854): with a blank scalar fix, an observed version outside every range
// and at or above a line's Fixed event is known-fixed, and every other shape
// keeps its prior status.
func TestNPMPerLineKnownFixedDecision(t *testing.T) {
	t.Parallel()
	const (
		fixedByLine = "not_affected_known_fixed"
		reasonLine  = "npm_semver_known_fixed"
		possibly    = "possibly_affected"
		affected    = "affected_exact"
		reasonNone  = "version_not_in_advisory_range"
		reasonHit   = "npm_semver_affected_range"
	)
	openLine := func(introduced string) supplychainmodel.AffectedRange {
		return supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: introduced}}}
	}
	jsYAML := affectedPackageWithRanges(semverLine("0", "3.14.2"), semverLine("4.0.0", "4.1.1"))
	reintroduced := affectedPackageWithRanges(semverLine("0", "1.5.0"), semverLine("2.0.0", "2.3.0"), openLine("3.0.0"))
	preLines := affectedPackageWithRanges(semverLine("0", "1.0.0"), semverLine("2.0.0-alpha.0", "2.0.0-alpha.4"))
	overlap := affectedPackageWithRanges(semverLine("0", "2.0.0"), semverLine("1.0.0", "3.0.0"))
	mixedLastAffected := affectedPackageWithRanges(semverLine("0", "0.9.0"), semverLastAffectedLine("1.0.0", "1.2.0"))
	babel := affectedPackageWithRanges(semverLine("0", "7.23.2"), semverLine("8.0.0-alpha.0", "8.0.0-alpha.4"))
	cases := []struct {
		name       string
		observed   string
		scalar     string
		pkgs       []supplychainmodel.AffectedPackage
		wantStatus string
		wantReason string
	}{
		{"babel-traverse 7.29.7 pin (#9431)", "7.29.7", "", babel, fixedByLine, reasonLine},
		{"babel-traverse prerelease line stays affected", "8.0.0-alpha.2", "", babel, affected, reasonHit},
		{"js-yaml line 1 patched", "3.15.0", "", jsYAML, fixedByLine, reasonLine},
		{"js-yaml line 2 exactly fixed", "4.1.1", "", jsYAML, fixedByLine, reasonLine},
		{"js-yaml between lines", "3.99.0", "", jsYAML, fixedByLine, reasonLine},
		{"js-yaml above both lines", "5.0.0", "", jsYAML, fixedByLine, reasonLine},
		{"js-yaml inside line 2 stays affected", "4.0.5", "", jsYAML, affected, reasonHit},
		{"js-yaml inside line 1 stays affected", "3.0.0", "", jsYAML, affected, reasonHit},
		{"equal to the only fix", "3.14.2", "", affectedPackageWithRanges(semverLine("0", "3.14.2")), fixedByLine, reasonLine},
		{"scalar fix path unchanged", "3.15.0", "3.14.2", affectedPackageWithRanges(semverLine("0", "3.14.2")), fixedByLine, reasonLine},
		{"below every introduced stays possibly", "1.0.0", "", affectedPackageWithRanges(semverLine("2.0.0", "3.0.0")), possibly, reasonNone},
		{"below every introduced with scalar stays possibly", "1.0.0", "3.0.0", affectedPackageWithRanges(semverLine("2.0.0", "3.0.0")), possibly, reasonNone},
		{"introduced zero only is affected", "99.0.0", "", affectedPackageWithRanges(openLine("0")), affected, reasonHit},
		{"fixed without introduced is not a line", "2.0.0", "", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Fixed: "1.5.0"}}}), possibly, reasonNone},
		{"overlapping lines above both", "3.5.0", "", overlap, fixedByLine, reasonLine},
		{"overlapping lines inside second", "2.5.0", "", overlap, affected, reasonHit},
		{"prerelease below fix is in range", "1.5.0-rc.1", "", affectedPackageWithRanges(semverLine("0", "1.5.0")), affected, reasonHit},
		{"prerelease fix equal", "2.0.0-alpha.4", "", preLines, fixedByLine, reasonLine},
		{"prerelease above prerelease fix", "2.0.0-alpha.5", "", preLines, fixedByLine, reasonLine},
		{"re-introduced later range: fixed between", "2.5.0", "", reintroduced, fixedByLine, reasonLine},
		{"re-introduced later range: open line affected", "3.5.0", "", reintroduced, affected, reasonHit},
		{"two pairs in one range", "2.5.0", "", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "1.5.0"}, {Introduced: "2.0.0"}, {Fixed: "2.3.0"}}}), fixedByLine, reasonLine},
		{"git range is ignored", "1.0.0", "", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "GIT", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "abc123def"}}}), possibly, reasonNone},
		{"git range does not feed the line", "1.0.0", "", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "GIT", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "0.5.0"}}}), possibly, reasonNone},
		{"malformed fixed event fails closed", "1.0.0", "", affectedPackageWithRanges(semverLine("0", "not-a-version")), possibly, "malformed_advisory_range"},
		{"malformed introduced event fails closed", "1.0.0", "", affectedPackageWithRanges(semverLine("bogus", "2.0.0")), possibly, "malformed_advisory_range"},
		{"empty ranges", "1.0.0", "", affectedPackageWithRanges(), possibly, reasonNone},
		{"range without events", "1.0.0", "", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER"}), possibly, reasonNone},
		{"no packages", "1.0.0", "", nil, possibly, reasonNone},
		{"last_affected only stays fail-closed", "1.2.4", "", affectedPackageWithRanges(semverLastAffectedLine("0", "1.2.3")), possibly, reasonNone},
		{"last_affected line is the nearest boundary", "1.3.0", "", mixedLastAffected, possibly, reasonNone},
		{"fixed line is the nearest boundary over last_affected", "3.0.0", "", affectedPackageWithRanges(semverLastAffectedLine("1.0.0", "1.2.0"), semverLine("2.0.0", "2.5.0")), fixedByLine, reasonLine},
		{"fixed line below a last_affected line", "0.95.0", "", mixedLastAffected, fixedByLine, reasonLine},
		{"limit range is skipped", "2.0.0", "", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "1.5.0"}, {Limit: "3.0.0"}}}), possibly, reasonNone},
		{"malformed observed version", "latest", "", jsYAML, possibly, "installed_version_malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := evaluateSupplyChainVersionMatch("npm", tc.observed, "", tc.scalar, tc.pkgs)
			if string(got.Status) != tc.wantStatus || got.Reason != tc.wantReason {
				t.Fatalf("status=%q reason=%q, want %q / %q", got.Status, got.Reason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

// TestNPMPerLineKnownFixedStaysNPMOnly pins that the ecosystems sharing the
// npm semver code shape keep their prior fail-closed answer in this change:
// the per-line branch is npm-only until each ecosystem proves the same defect.
func TestNPMPerLineKnownFixedStaysNPMOnly(t *testing.T) {
	t.Parallel()
	pkgs := affectedPackageWithRanges(semverLine("0", "3.14.2"), semverLine("4.0.0", "4.1.1"))
	for _, ecosystem := range []string{"cargo", "hex", "swift", "pub", "go"} {
		t.Run(ecosystem, func(t *testing.T) {
			t.Parallel()
			got := evaluateSupplyChainVersionMatch(ecosystem, "3.15.0", "", "", pkgs)
			if got.Status != SupplyChainImpactPossiblyAffected || got.Reason != "version_not_in_advisory_range" {
				t.Fatalf("%s: status=%q reason=%q, want possibly_affected / version_not_in_advisory_range", ecosystem, got.Status, got.Reason)
			}
		})
	}
}

// TestNPMOutsideEveryRangeIsKnownFixedWhenFixedScalarBlank drives the #9854
// js-yaml repro through the production finding builder.
func TestNPMOutsideEveryRangeIsKnownFixedWhenFixedScalarBlank(t *testing.T) {
	t.Parallel()
	rng := func(intro, fixed string) map[string]any {
		return map[string]any{"type": "SEMVER", "events": []any{
			map[string]any{"introduced": intro}, map[string]any{"fixed": fixed},
		}}
	}
	for _, tc := range []struct{ name, observed, want, wantReason string }{
		{"line1 patched", "3.15.0", "not_affected_known_fixed", "npm_semver_known_fixed"},
		{"line2 exactly fixed", "4.1.1", "not_affected_known_fixed", "npm_semver_known_fixed"},
		{"between lines", "3.99.0", "not_affected_known_fixed", "npm_semver_known_fixed"},
		{"inside line2 stays affected", "4.0.5", "affected_exact", "npm_semver_affected_range"},
		{"inside line1 stays affected", "3.0.0", "affected_exact", "npm_semver_affected_range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cve := vulnerabilityCVEFactWithProvenance("cve", "CVE-2025-64718", "osv", "GHSA-mh29-5h37-fv8m", 7.5,
				"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N", "HIGH", "2026-05-20T12:00:00Z")
			pkg := facts.Envelope{FactID: "aff", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
				"cve_id": "CVE-2025-64718", "advisory_id": "GHSA-mh29-5h37-fv8m", "source": "osv",
				"package_id": "pkg:npm/js-yaml", "ecosystem": "npm", "package_name": "js-yaml",
				"fixed_versions":  []any{"3.14.2", "4.1.1"},
				"affected_ranges": []any{rng("0", "3.14.2"), rng("4.0.0", "4.1.1")},
			}}
			consume := facts.Envelope{FactID: "consume", FactKind: correlation.PackageConsumptionFactKind, Payload: map[string]any{
				"package_id": "pkg:npm/js-yaml", "repository_id": testImpactRepositoryID,
				"dependency_range": tc.observed, "observed_version": tc.observed,
				"dependency_path": []any{"js-yaml"}, "dependency_depth": 1,
			}}
			got := BuildSupplyChainImpactFindings([]facts.Envelope{cve, pkg, consume})
			if len(got) != 1 {
				t.Fatalf("findings = %d, want 1", len(got))
			}
			if string(got[0].Status) != tc.want || got[0].MatchReason != tc.wantReason {
				t.Fatalf("status=%v reason=%q fixed=%q, want %s / %s", got[0].Status, got[0].MatchReason, got[0].FixedVersion, tc.want, tc.wantReason)
			}
		})
	}
}

// TestNPMPerLineKnownFixedFailsClosedOnUnevaluatedRanges pins the review
// findings on #9854. A range the containment check never evaluates (ECOSYSTEM,
// GIT, unknown kind) may contain the observed version, so it keeps the row
// possibly_affected even when another line's Fixed is at or below it. A range
// whose events are listed out of version order stays conservative too: since
// #9865 containment sorts the events, so the order rows below use a version
// outside every range, and the guard now only withholds a flip.
func TestNPMPerLineKnownFixedFailsClosedOnUnevaluatedRanges(t *testing.T) {
	t.Parallel()
	const possibly = "possibly_affected"
	ecosystemLine := func(introduced, fixed string) supplychainmodel.AffectedRange {
		r := semverLine(introduced, fixed)
		r.Kind = "ECOSYSTEM"
		return r
	}
	cases := []struct {
		name     string
		observed string
		pkgs     []supplychainmodel.AffectedPackage
	}{
		{"ecosystem range mixed into one entry", "2.1.0", affectedPackageWithRanges(semverLine("0", "1.5.0"), ecosystemLine("2.0.0", "2.3.0"))},
		{"ecosystem-only entry from a second source", "2.1.0", append(
			affectedPackageWithRanges(semverLine("0", "1.5.0"), semverLine("3.0.0", "3.1.0")),
			affectedPackageWithRanges(ecosystemLine("2.0.0", "2.3.0"))...)},
		{"git range beside a semver line", "2.0.0", affectedPackageWithRanges(semverLine("0", "1.5.0"), supplychainmodel.AffectedRange{Kind: "GIT", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "abc123def"}}})},
		{"unknown range kind beside a semver line", "2.0.0", affectedPackageWithRanges(semverLine("0", "1.5.0"), supplychainmodel.AffectedRange{Kind: "NPM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "1.9.0"}, {Fixed: "2.3.0"}}})},
		{"events listed out of version order stay conservative", "1.7.0", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "2.0.0"}, {Fixed: "2.5.0"}, {Introduced: "0"}, {Fixed: "1.5.0"}}})},
		{"empty same-package entry with nil versions", "2.1.0", append(
			affectedPackageWithRanges(semverLine("0", "1.5.0")),
			supplychainmodel.AffectedPackage{PackageID: "pkg:npm/example", Ecosystem: "npm", Name: "example"})},
		{"empty same-package entry with empty versions slice", "2.1.0", append(
			affectedPackageWithRanges(semverLine("0", "1.5.0")),
			supplychainmodel.AffectedPackage{PackageID: "pkg:npm/example", Ecosystem: "npm", Name: "example", AffectedVersions: []string{}})},
		{"empty entry carrying only blank versions", "2.1.0", append(
			affectedPackageWithRanges(semverLine("0", "1.5.0")),
			supplychainmodel.AffectedPackage{PackageID: "pkg:npm/example", Ecosystem: "npm", Name: "example", AffectedVersions: []string{"", "  "}})},
		{"introduced before a fixed at the same version", "1.5.0", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Introduced: "1.5.0"}, {Fixed: "1.5.0"}, {Fixed: "2.0.0"}}})},
		{"fixed before an introduced at the same version", "2.5.0", affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "1.5.0"}, {Introduced: "1.5.0"}, {Fixed: "2.0.0"}}})},
		{"unsorted range beside a sorted line stays conservative", "1.7.0", affectedPackageWithRanges(semverLine("0", "1.0.0"), supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "2.0.0"}, {Fixed: "2.5.0"}, {Introduced: "0"}, {Fixed: "1.5.0"}}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := evaluateSupplyChainVersionMatch("npm", tc.observed, "", "", tc.pkgs)
			if string(got.Status) != possibly || got.Reason != "version_not_in_advisory_range" {
				t.Fatalf("status=%q reason=%q, want possibly_affected / version_not_in_advisory_range", got.Status, got.Reason)
			}
		})
	}
}

// TestNPMPerLineKnownFixedTieOrderStillReadsContainment pins that the
// same-version introduced/terminator guard only blocks the per-line verdict:
// the containment walk still decides the rows it can read.
func TestNPMPerLineKnownFixedTieOrderStillReadsContainment(t *testing.T) {
	t.Parallel()
	pkgs := affectedPackageWithRanges(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "1.5.0"}, {Introduced: "1.5.0"}, {Fixed: "2.0.0"}}})
	got := evaluateSupplyChainVersionMatch("npm", "1.5.0", "", "", pkgs)
	if string(got.Status) != "affected_exact" {
		t.Fatalf("status=%q reason=%q, want affected_exact", got.Status, got.Reason)
	}
}

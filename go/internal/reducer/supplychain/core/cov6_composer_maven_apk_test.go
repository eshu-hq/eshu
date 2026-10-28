// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func TestCov6ComposerWildcardContains(t *testing.T) {
	cases := []struct {
		name     string
		token    string
		observed string
		affected bool
		valid    bool
	}{
		{name: "star matches all", token: "*", observed: "9.9.9", affected: true, valid: true},
		{name: "minor wildcard hit", token: "1.2.*", observed: "1.2.5", affected: true, valid: true},
		{name: "minor wildcard miss", token: "1.2.*", observed: "1.3.0", affected: false, valid: true},
		{name: "major wildcard hit", token: "1.*", observed: "1.9.9", affected: true, valid: true},
		{name: "x spelling", token: "1.2.x", observed: "1.2.0", affected: true, valid: true},
		{name: "stability suffix", token: "1.2.*@stable", observed: "1.2.3", affected: true, valid: true},
		{name: "non-numeric", token: "a.*", observed: "1.0.0", affected: false, valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := composerWildcardContains(tc.token, tc.observed)
			if ok != tc.valid || (ok && got != tc.affected) {
				t.Fatalf("composerWildcardContains(%q,%q) = (%v,%v), want (%v,%v)",
					tc.token, tc.observed, got, ok, tc.affected, tc.valid)
			}
		})
	}
}

func TestCov6ComposerConstraintEdges(t *testing.T) {
	if affected, valid := composerConstraintContains("", "1.0.0"); valid || affected {
		t.Fatalf("empty constraint = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := composerConstraintContains("1.0 as 2.0", "1.0.0"); valid || affected {
		t.Fatalf("alias constraint = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := composerConstraintContains("^1.0", "dev-main"); valid || affected {
		t.Fatalf("dev observed = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := composerConstraintContains("dev-master", "1.0.0"); valid || affected {
		t.Fatalf("dev branch = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := composerConstraintContains("^1.2", "1.5.0"); !valid || !affected {
		t.Fatalf("caret hit = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := composerConstraintContains("~1.2", "1.2.5"); !valid || !affected {
		t.Fatalf("tilde hit = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := composerConstraintContains("^!!!", "1.0.0"); valid || affected {
		t.Fatalf("broken caret = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := composerConstraintBranchContains("", "1.0.0"); valid || affected {
		t.Fatalf("empty branch = (%v,%v), want (false,false)", affected, valid)
	}
	if got := stripComposerStability("1.2.3@stable"); got != "1.2.3" {
		t.Fatalf("stripComposerStability(1.2.3@stable) = %q, want %q", got, "1.2.3")
	}
	if got := stripComposerStability("  1.2.3  "); got != "1.2.3" {
		t.Fatalf("stripComposerStability(spaces) = %q, want %q", got, "1.2.3")
	}
}

func TestCov6ComposerCaretTildeUpperBounds(t *testing.T) {
	if got, ok := composerCaretUpperBound("1.2.3"); !ok || got != "2.0.0" {
		t.Fatalf("composerCaretUpperBound(1.2.3) = (%q,%v), want (2.0.0,true)", got, ok)
	}
	if got, ok := composerCaretUpperBound("0.2.3"); !ok || got != "0.3.0" {
		t.Fatalf("composerCaretUpperBound(0.2.3) = (%q,%v), want (0.3.0,true)", got, ok)
	}
	if got, ok := composerCaretUpperBound("0.0.5"); !ok || got != "0.0.6" {
		t.Fatalf("composerCaretUpperBound(0.0.5) = (%q,%v), want (0.0.6,true)", got, ok)
	}
	if _, ok := composerCaretUpperBound("!!!"); ok {
		t.Fatal("composerCaretUpperBound(invalid) = true, want false")
	}
	if got, ok := composerTildeUpperBound("1.2.3"); !ok || got != "1.3.0" {
		t.Fatalf("composerTildeUpperBound(1.2.3) = (%q,%v), want (1.3.0,true)", got, ok)
	}
	if got, ok := composerTildeUpperBound("1.2"); !ok || got != "2.0.0" {
		t.Fatalf("composerTildeUpperBound(1.2) = (%q,%v), want (2.0.0,true)", got, ok)
	}
	if _, ok := composerTildeUpperBound("!!!"); ok {
		t.Fatal("composerTildeUpperBound(invalid) = true, want false")
	}
	if _, ok := composerNumericParts("1.2.3.4"); ok {
		t.Fatal("composerNumericParts(4 parts) = true, want false")
	}
	if _, ok := composerNumericParts("a.b"); ok {
		t.Fatal("composerNumericParts(alpha) = true, want false")
	}
	if parts, ok := composerNumericParts("1.2"); !ok || len(parts) != 3 || parts[2] != 0 {
		t.Fatalf("composerNumericParts(1.2) = (%v,%v), want ([1 2 0],true)", parts, ok)
	}
}

func TestCov6ComposerAffectedByPackage(t *testing.T) {
	hit := supplychainmodel.AffectedPackage{AffectedVersions: []string{"", "1.2.3"}}
	if affected, valid := composerAffectedByPackage("1.2.3", hit); !valid || !affected {
		t.Fatalf("exact hit = (%v,%v), want (true,true)", affected, valid)
	}
	broken := supplychainmodel.AffectedPackage{AffectedVersions: []string{"!!!"}}
	if affected, valid := composerAffectedByPackage("1.2.3", broken); valid || affected {
		t.Fatalf("broken candidate = (%v,%v), want (false,false)", affected, valid)
	}
	skipped := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "1.0.0"}}},
	}}
	if affected, valid := composerAffectedByPackage("1.2.3", skipped); !valid || affected {
		t.Fatalf("skipped kind = (%v,%v), want (false,true)", affected, valid)
	}
	ranged := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: "1.0.0"},
			{Fixed: "2.0.0"},
		}},
	}}
	if affected, valid := composerAffectedByPackage("1.5.0", ranged); !valid || !affected {
		t.Fatalf("ranged hit = (%v,%v), want (true,true)", affected, valid)
	}
	raw := supplychainmodel.AffectedPackage{AffectedRangeRaw: "^1.0"}
	if affected, valid := composerAffectedByPackage("1.5.0", raw); !valid || !affected {
		t.Fatalf("raw hit = (%v,%v), want (true,true)", affected, valid)
	}
	rawBroken := supplychainmodel.AffectedPackage{AffectedRangeRaw: "as"}
	if affected, valid := composerAffectedByPackage("1.5.0", rawBroken); valid || affected {
		t.Fatalf("raw broken = (%v,%v), want (false,false)", affected, valid)
	}
}

func TestCov6MavenBracketBranchContains(t *testing.T) {
	if affected, valid := mavenBracketBranchContains("x", "1.0"); valid || affected {
		t.Fatalf("short branch = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("x1.0]", "1.0"); valid || affected {
		t.Fatalf("bad open = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("[1.0x", "1.0"); valid || affected {
		t.Fatalf("bad close = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("[1.0]", "1.0"); !valid || !affected {
		t.Fatalf("exact pin = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("(1.0)", "1.0"); !valid || affected {
		t.Fatalf("open exact = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("[1.0,2.0]", "1.5"); !valid || !affected {
		t.Fatalf("closed window = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("(1.0,2.0)", "1.0"); !valid || affected {
		t.Fatalf("open lower edge = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("(,)", "1.0"); !valid || !affected {
		t.Fatalf("open both = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("[1.0,2.0]", "has space"); valid || affected {
		t.Fatalf("invalid observed = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := mavenBracketBranchContains("(1.0,has space)", "1.5"); valid || affected {
		t.Fatalf("invalid upper = (%v,%v), want (false,false)", affected, valid)
	}
}

func TestCov6MavenQualifierCanonicalAndRank(t *testing.T) {
	canonical := map[string]string{
		"a": "alpha", "alpha": "alpha",
		"b": "beta", "beta": "beta",
		"m": "milestone", "milestone": "milestone",
		"cr": "rc", "rc": "rc",
		"snapshot": "snapshot",
		"final":    "", "ga": "", "release": "",
		"sp": "sp",
	}
	for raw, want := range canonical {
		if got := mavenQualifierCanonical(raw); got != want {
			t.Fatalf("mavenQualifierCanonical(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := mavenQualifierCanonical("xyz"); got != "xyz" {
		t.Fatalf("mavenQualifierCanonical(xyz) = %q, want passthrough", got)
	}
	ranks := map[string]int{
		"alpha": 1, "beta": 2, "milestone": 3, "rc": 4,
		"snapshot": 5, "final": 6, "sp": 7,
	}
	for qualifier, want := range ranks {
		got, ok := mavenQualifierRank(qualifier)
		if !ok || got != want {
			t.Fatalf("mavenQualifierRank(%q) = (%d,%v), want (%d,true)", qualifier, got, ok, want)
		}
	}
	if got, ok := mavenQualifierRank("xyz"); ok || got != 8 {
		t.Fatalf("mavenQualifierRank(xyz) = (%d,%v), want (8,false)", got, ok)
	}
}

func TestCov6APKVersionEdges(t *testing.T) {
	for suffix, want := range map[string]int{
		"alpha": -40, "beta": -30, "pre": -20, "rc": -10, "": 0,
		"cvs": 10, "svn": 20, "git": 30, "hg": 40, "p": 50,
	} {
		if got := apkSuffixRank(suffix); got != want {
			t.Fatalf("apkSuffixRank(%q) = %d, want %d", suffix, got, want)
		}
	}
	if got := apkSuffixRank("nope"); got != apkUnknownSuffixRank {
		t.Fatalf("apkSuffixRank(nope) = %d, want %d", got, apkUnknownSuffixRank)
	}
	for value, want := range map[string]bool{
		"": false, "a1": false, "1.0_beta!": false,
		"1.0": true, "1a2b": true,
	} {
		if got := apkMainVersionValid(value); got != want {
			t.Fatalf("apkMainVersionValid(%q) = %v, want %v", value, got, want)
		}
	}
	for _, raw := range []string{
		"", "1.0 beta", "1.0-r", "1.0-rX", "1.0-2", "_alpha", "x_y", "1.0_foo", "1.0_1abc", "1.0_rcX",
	} {
		if validAPKVersion(raw) {
			t.Fatalf("validAPKVersion(%q) = true, want false", raw)
		}
	}
	if !validAPKVersion("1.0_alpha1-r2") {
		t.Fatal("validAPKVersion(1.0_alpha1-r2) = false, want true")
	}
	if got, ok := compareAPKVersion("1.0", "1.0"); !ok || got != 0 {
		t.Fatalf("compareAPKVersion(equal) = (%d,%v), want (0,true)", got, ok)
	}
	if got, ok := compareAPKVersion("1.0_alpha1", "1.0_beta1"); !ok || got != -1 {
		t.Fatalf("compareAPKVersion(alpha,beta) = (%d,%v), want (-1,true)", got, ok)
	}
	if got, ok := compareAPKVersion("1.0_rc1", "1.0_rc2"); !ok || got != -1 {
		t.Fatalf("compareAPKVersion(rc1,rc2) = (%d,%v), want (-1,true)", got, ok)
	}
	if got, ok := compareAPKVersion("1.0-r1", "1.0-r2"); !ok || got != -1 {
		t.Fatalf("compareAPKVersion(release) = (%d,%v), want (-1,true)", got, ok)
	}
	if _, ok := compareAPKVersion("!!! has space", "1.0"); ok {
		t.Fatal("compareAPKVersion(invalid left) = true, want false")
	}
	if _, ok := compareAPKVersion("1.0", "!!! has space"); ok {
		t.Fatal("compareAPKVersion(invalid right) = true, want false")
	}
}

func TestCov6RPMVersionMatchEdges(t *testing.T) {
	hit := supplychainmodel.AffectedPackage{AffectedVersions: []string{"1.0-1"}}
	decision := evaluateRPMVersionMatch("1.0-1", "", []supplychainmodel.AffectedPackage{hit})
	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("rpm exact hit status = %q, want affected-exact", decision.Status)
	}
	broken := supplychainmodel.AffectedPackage{AffectedVersions: []string{""}}
	decision = evaluateRPMVersionMatch("1.0-1", "", []supplychainmodel.AffectedPackage{broken})
	if decision.Status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("rpm broken candidate status = %q, want possibly-affected", decision.Status)
	}
	decision = evaluateRPMVersionMatch("has space", "", nil)
	if decision.Reason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("rpm invalid observed reason = %q, want malformed-installed", decision.Reason)
	}
	decision = evaluateRPMVersionMatch("1.0-2", "1.0-1", nil)
	if decision.Status != SupplyChainImpactNotAffectedKnownFixed {
		t.Fatalf("rpm known-fixed status = %q, want known-fixed", decision.Status)
	}
	// A blank fixed version parses to an empty EVR version, which the
	// comparator reports invalid so the decision fails closed.
	decision = evaluateRPMVersionMatch("1.0-1", "   ", nil)
	if decision.Reason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("rpm invalid fixed reason = %q, want malformed-range", decision.Reason)
	}
	if got, ok := compareRPMEVR("1:1.0", "2:1.0"); !ok || got != -1 {
		t.Fatalf("compareRPMEVR(epoch) = (%d,%v), want (-1,true)", got, ok)
	}
	if _, ok := compareRPMEVR("", "1.0"); ok {
		t.Fatal("compareRPMEVR(empty left) = true, want false")
	}
	for value, want := range map[string]bool{"": false, "12a": false, "12": true} {
		if got := isDecimalString(value); got != want {
			t.Fatalf("isDecimalString(%q) = %v, want %v", value, got, want)
		}
	}
	if got := parseRPMEpoch(""); got != 0 {
		t.Fatalf("parseRPMEpoch(empty) = %d, want 0", got)
	}
	if got := parseRPMEpoch("abc"); got != 0 {
		t.Fatalf("parseRPMEpoch(alpha) = %d, want 0", got)
	}
	if got := parseRPMEpoch("5"); got != 5 {
		t.Fatalf("parseRPMEpoch(5) = %d, want 5", got)
	}
}

func TestCov6ExpandNPMComparator(t *testing.T) {
	if _, ok := expandNPMComparator(""); ok {
		t.Fatal("expandNPMComparator(empty) = true, want false")
	}
	got, ok := expandNPMComparator("^1.2.3")
	if !ok || len(got) != 2 || got[0] != ">=1.2.3" || got[1] != "<2.0.0" {
		t.Fatalf("expandNPMComparator(^1.2.3) = (%v,%v), want ([>=1.2.3 <2.0.0],true)", got, ok)
	}
	got, ok = expandNPMComparator("~1.2.3")
	if !ok || len(got) != 2 || got[0] != ">=1.2.3" || got[1] != "<1.3.0" {
		t.Fatalf("expandNPMComparator(~1.2.3) = (%v,%v), want ([>=1.2.3 <1.3.0],true)", got, ok)
	}
	// Two-char operators route through normalizeComparator.
	got, ok = expandNPMComparator(">=  1.2.3")
	if !ok || len(got) != 1 || got[0] != ">=1.2.3" {
		t.Fatalf("expandNPMComparator(>=  1.2.3) = (%v,%v), want ([>=1.2.3],true)", got, ok)
	}
	// One-char operators route through normalizeComparator with width 1.
	got, ok = expandNPMComparator(">  1.2.3")
	if !ok || len(got) != 1 || got[0] != ">1.2.3" {
		t.Fatalf("expandNPMComparator(>  1.2.3) = (%v,%v), want ([>1.2.3],true)", got, ok)
	}
	got, ok = expandNPMComparator("1.2.3")
	if !ok || len(got) != 1 || got[0] != "=1.2.3" {
		t.Fatalf("expandNPMComparator(bare) = (%v,%v), want ([=1.2.3],true)", got, ok)
	}
	if _, ok := expandNPMComparator("!!!"); ok {
		t.Fatal("expandNPMComparator(bare invalid) = true, want false")
	}
	if _, ok := expandNPMComparator("^!!!"); ok {
		t.Fatal("expandNPMComparator(^invalid) = true, want false")
	}
}

func TestCov6ExpandCaretAndSemverParts(t *testing.T) {
	got, ok := expandCaret("0.2.3")
	if !ok || len(got) != 2 || got[1] != "<0.3.0" {
		t.Fatalf("expandCaret(0.2.3) = (%v,%v), want ([>=0.2.3 <0.3.0],true)", got, ok)
	}
	got, ok = expandCaret("0.0.5")
	if !ok || len(got) != 2 || got[1] != "<0.0.6" {
		t.Fatalf("expandCaret(0.0.5) = (%v,%v), want ([>=0.0.5 <0.0.6],true)", got, ok)
	}
	if _, ok := expandCaret("!!!"); ok {
		t.Fatal("expandCaret(invalid) = true, want false")
	}
	major, minor, patch, ok := parseSemverParts("1.0-beta+x")
	if !ok || major != 1 || minor != 0 || patch != 0 {
		t.Fatalf("parseSemverParts(suffixed) = (%d,%d,%d,%v), want (1,0,0,true)", major, minor, patch, ok)
	}
	if _, _, _, ok := parseSemverParts(""); ok {
		t.Fatal("parseSemverParts(empty) = true, want false")
	}
	if _, _, _, ok := parseSemverParts("1.2.3.4"); ok {
		t.Fatal("parseSemverParts(4 parts) = true, want false")
	}
	if _, _, _, ok := parseSemverParts("a.b"); ok {
		t.Fatal("parseSemverParts(alpha) = true, want false")
	}
	for token, want := range map[string]bool{"": false, "-1": false, "x": false, "3": true} {
		_, ok := atoiNonNegative(token)
		if ok != want {
			t.Fatalf("atoiNonNegative(%q) = %v, want %v", token, ok, want)
		}
	}
}

func TestCov6NPMManifestAllowsFix(t *testing.T) {
	verdict, missing := evaluateNPMManifestAllowsFix("^1.2.3", "")
	if verdict != SupplyChainRemediationManifestUnknown || missing != SupplyChainRemediationMissingFixedVersion {
		t.Fatalf("empty candidate = (%q,%q), want unknown+missing-fixed", verdict, missing)
	}
	verdict, missing = evaluateNPMManifestAllowsFix("", "1.2.3")
	if verdict != SupplyChainRemediationManifestUnknown || missing != SupplyChainRemediationMissingManifestRange {
		t.Fatalf("empty range = (%q,%q), want unknown+missing-range", verdict, missing)
	}
	verdict, missing = evaluateNPMManifestAllowsFix("1.0 - 2.0", "1.5.0")
	if verdict != SupplyChainRemediationManifestUnknown || missing != SupplyChainRemediationMissingManifestRangeMalformed {
		t.Fatalf("hyphen range = (%q,%q), want unknown+malformed", verdict, missing)
	}
	if verdict, _ := evaluateNPMManifestAllowsFix("^1.2.3", "1.5.0"); verdict != SupplyChainRemediationManifestAllowed {
		t.Fatalf("caret allows = %q, want allowed", verdict)
	}
	if verdict, _ := evaluateNPMManifestAllowsFix("^1.2.3", "2.0.0"); verdict != SupplyChainRemediationManifestBlocked {
		t.Fatalf("caret blocks = %q, want blocked", verdict)
	}
	verdict, missing = evaluateNPMManifestAllowsFix("^1.2.3", "!!!")
	if verdict != SupplyChainRemediationManifestUnknown || missing != SupplyChainRemediationMissingManifestRangeMalformed {
		t.Fatalf("invalid candidate = (%q,%q), want unknown+malformed", verdict, missing)
	}
}

func TestCov6CargoComparatorAndManifest(t *testing.T) {
	if _, ok := expandCargoComparator(""); ok {
		t.Fatal("expandCargoComparator(empty) = true, want false")
	}
	if _, ok := expandCargoComparator("!!!"); ok {
		t.Fatal("expandCargoComparator(invalid) = true, want false")
	}
	got, ok := expandCargoComparator("^1.0")
	if !ok || len(got) != 2 {
		t.Fatalf("expandCargoComparator(^1.0) = (%v,%v), want 2 comparators", got, ok)
	}
	got, ok = expandCargoComparator("~1.0")
	if !ok || len(got) != 2 {
		t.Fatalf("expandCargoComparator(~1.0) = (%v,%v), want 2 comparators", got, ok)
	}
	got, ok = expandCargoComparator(">=1.0")
	if !ok || len(got) != 1 || got[0] != ">=1.0" {
		t.Fatalf("expandCargoComparator(>=1.0) = (%v,%v), want ([>=1.0],true)", got, ok)
	}
	got, ok = expandCargoComparator(">1.0")
	if !ok || len(got) != 1 || got[0] != ">1.0" {
		t.Fatalf("expandCargoComparator(>1.0) = (%v,%v), want ([>1.0],true)", got, ok)
	}
	got, ok = expandCargoComparator("=1.0")
	if !ok || len(got) != 1 || got[0] != "=1.0" {
		t.Fatalf("expandCargoComparator(=1.0) = (%v,%v), want ([=1.0],true)", got, ok)
	}
	got, ok = expandCargoComparator("1.0")
	if !ok || len(got) != 2 {
		t.Fatalf("expandCargoComparator(bare) = (%v,%v), want caret expansion", got, ok)
	}
	if verdict, _ := evaluateCargoManifestAllowsFix("^1.0", "1.5.0"); verdict != SupplyChainRemediationManifestAllowed {
		t.Fatalf("cargo allows = %q, want allowed", verdict)
	}
	verdict, missing := evaluateCargoManifestAllowsFix("latest", "1.0.0")
	if verdict != SupplyChainRemediationManifestUnknown || missing != SupplyChainRemediationMissingManifestRangeMalformed {
		t.Fatalf("cargo latest = (%q,%q), want unknown+malformed", verdict, missing)
	}
	verdict, missing = evaluateCargoManifestAllowsFix("^1.0", "")
	if verdict != SupplyChainRemediationManifestUnknown || missing != SupplyChainRemediationMissingFixedVersion {
		t.Fatalf("cargo empty candidate = (%q,%q), want unknown+missing-fixed", verdict, missing)
	}
}

func TestCov6OSRemediationFamilyFromSource(t *testing.T) {
	for source, want := range map[string]string{
		"RedHat": "rpm", "Fedora": "rpm", "Ubuntu": "dpkg",
		"Alpine": "apk", "something-else": "",
	} {
		if got := osRemediationFamilyFromSource(source); got != want {
			t.Fatalf("osRemediationFamilyFromSource(%q) = %q, want %q", source, got, want)
		}
	}
}

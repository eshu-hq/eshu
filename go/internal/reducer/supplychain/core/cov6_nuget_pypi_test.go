// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func TestCov6NuGetPrereleaseNumber(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  int
		valid bool
	}{
		{name: "empty", raw: "", want: 0, valid: false},
		{name: "alpha", raw: "alpha", want: 0, valid: false},
		{name: "negative", raw: "-1", want: 0, valid: false},
		{name: "zero", raw: "0", want: 0, valid: true},
		{name: "number", raw: "12", want: 12, valid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nugetPrereleaseNumber(tc.raw)
			if ok != tc.valid || (ok && got != tc.want) {
				t.Fatalf("nugetPrereleaseNumber(%q) = (%d,%v), want (%d,%v)", tc.raw, got, ok, tc.want, tc.valid)
			}
		})
	}
}

func TestCov6CompareNuGetPrerelease(t *testing.T) {
	cases := []struct {
		name  string
		left  []string
		right []string
		want  int
	}{
		{name: "both release", left: nil, right: nil, want: 0},
		{name: "left release wins", left: nil, right: []string{"alpha"}, want: 1},
		{name: "right release wins", left: []string{"alpha"}, right: nil, want: -1},
		{name: "equal", left: []string{"alpha"}, right: []string{"alpha"}, want: 0},
		{name: "numeric order", left: []string{"2"}, right: []string{"10"}, want: -1},
		{name: "numeric beats text", left: []string{"1"}, right: []string{"alpha"}, want: -1},
		{name: "text beats numeric", left: []string{"alpha"}, right: []string{"1"}, want: 1},
		{name: "text order", left: []string{"alpha"}, right: []string{"beta"}, want: -1},
		{name: "left longer", left: []string{"alpha", "1"}, right: []string{"alpha"}, want: 1},
		{name: "left shorter", left: []string{"alpha"}, right: []string{"alpha", "1"}, want: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareNuGetPrerelease(tc.left, tc.right); got != tc.want {
				t.Fatalf("compareNuGetPrerelease(%v,%v) = %d, want %d", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

func TestCov6NuGetVersionOrderingHelpers(t *testing.T) {
	if ok, valid := nugetVersionGreaterThan("1.2.0", "1.0.0"); !valid || !ok {
		t.Fatalf("nugetVersionGreaterThan(1.2.0,1.0.0) = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := nugetVersionGreaterThan("!!!", "1.0.0"); valid || ok {
		t.Fatalf("nugetVersionGreaterThan(invalid,1.0.0) = (%v,%v), want (false,false)", ok, valid)
	}
	if ok, valid := nugetVersionLessThan("1.0.0", "1.2.0"); !valid || !ok {
		t.Fatalf("nugetVersionLessThan(1.0.0,1.2.0) = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := nugetVersionLessThan("1.0.0", "!!!"); valid || ok {
		t.Fatalf("nugetVersionLessThan(1.0.0,invalid) = (%v,%v), want (false,false)", ok, valid)
	}
	if nugetRangeBranchUsesBounds("") {
		t.Fatal("nugetRangeBranchUsesBounds(empty) = true, want false")
	}
	if !nugetRangeBranchUsesBounds("[1.0]") {
		t.Fatal("nugetRangeBranchUsesBounds([1.0]) = false, want true")
	}
	if nugetRangeBranchUsesBounds(">=1.0") {
		t.Fatal("nugetRangeBranchUsesBounds(>=1.0) = true, want false")
	}
}

func TestCov6NuGetBeforeLimitsDecision(t *testing.T) {
	if ok, valid := nugetVersionBeforeLimitsDecision("1.0.0", nil); !valid || !ok {
		t.Fatalf("no limits = (%v,%v), want (true,true)", ok, valid)
	}
	star := []supplychainmodel.AffectedRangeEvent{{Limit: "*"}}
	if ok, valid := nugetVersionBeforeLimitsDecision("9.9.9", star); !valid || !ok {
		t.Fatalf("star limit = (%v,%v), want (true,true)", ok, valid)
	}
	below := []supplychainmodel.AffectedRangeEvent{{Limit: "2.0.0"}}
	if ok, valid := nugetVersionBeforeLimitsDecision("1.0.0", below); !valid || !ok {
		t.Fatalf("below limit = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := nugetVersionBeforeLimitsDecision("3.0.0", below); !valid || ok {
		t.Fatalf("above limit = (%v,%v), want (false,true)", ok, valid)
	}
	broken := []supplychainmodel.AffectedRangeEvent{{Limit: "!!!"}}
	if ok, valid := nugetVersionBeforeLimitsDecision("1.0.0", broken); valid || ok {
		t.Fatalf("broken limit = (%v,%v), want (false,false)", ok, valid)
	}
}

func TestCov6NuGetSemverRangeContainsDecision(t *testing.T) {
	window := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "1.0.0"},
		{Fixed: "2.0.0"},
	}}
	if affected, valid := nugetSemverRangeContainsDecision(window, "1.5.0"); !valid || !affected {
		t.Fatalf("inside window = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetSemverRangeContainsDecision(window, "2.0.0"); !valid || affected {
		t.Fatalf("at fixed = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := nugetSemverRangeContainsDecision(window, "0.5.0"); !valid || affected {
		t.Fatalf("before introduced = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := nugetSemverRangeContainsDecision(window, "!!!"); valid || affected {
		t.Fatalf("invalid observed = (%v,%v), want (false,false)", affected, valid)
	}
	capped := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "1.0.0"},
		{LastAffected: "1.5.0"},
	}}
	if affected, valid := nugetSemverRangeContainsDecision(capped, "1.6.0"); !valid || affected {
		t.Fatalf("past last-affected = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := nugetSemverRangeContainsDecision(capped, "1.2.0"); !valid || !affected {
		t.Fatalf("within last-affected = (%v,%v), want (true,true)", affected, valid)
	}
	beforeLimited := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Limit: "1.0.0"},
		{Introduced: "0.1.0"},
	}}
	if affected, valid := nugetSemverRangeContainsDecision(beforeLimited, "2.0.0"); !valid || affected {
		t.Fatalf("past limit = (%v,%v), want (false,true)", affected, valid)
	}
}

func TestCov6NuGetRangeBranchContains(t *testing.T) {
	if affected, valid := nugetRangeBranchContains("[1.2.3]", "1.2.3"); !valid || !affected {
		t.Fatalf("exact pin = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("[1.2.3]", "1.2.4"); !valid || affected {
		t.Fatalf("exact pin miss = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("(1.2.3]", "1.2.3"); valid || affected {
		t.Fatalf("mixed exact = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("[1.0.0,2.0.0]", "1.5.0"); !valid || !affected {
		t.Fatalf("closed window = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("(1.0.0,2.0.0)", "1.0.0"); !valid || affected {
		t.Fatalf("open lower edge = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("[,2.0.0]", "1.0.0"); !valid || !affected {
		t.Fatalf("open lower = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("[1.0.0,]", "9.0.0"); !valid || !affected {
		t.Fatalf("open upper = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("[,]", "1.0.0"); valid || affected {
		t.Fatalf("empty window = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("x", "1.0.0"); valid || affected {
		t.Fatalf("short branch = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := nugetRangeBranchContains("[1.0.0,2.0.0]", "!!!"); valid || affected {
		t.Fatalf("invalid observed = (%v,%v), want (false,false)", affected, valid)
	}
}

func TestCov6NuGetAffectedRangeRawContains(t *testing.T) {
	if affected, valid := nugetAffectedRangeRawContains("", "1.0.0"); !valid || affected {
		t.Fatalf("empty raw = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := nugetAffectedRangeRawContains(" || ", "1.0.0"); valid || affected {
		t.Fatalf("blank branches = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := nugetAffectedRangeRawContains("[1.0.0]", "1.0.0"); !valid || !affected {
		t.Fatalf("bracket hit = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetAffectedRangeRawContains(">=1.0.0", "2.0.0"); !valid || !affected {
		t.Fatalf("comparator hit = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := nugetAffectedRangeRawContains(">=9.0.0", "1.0.0"); !valid || affected {
		t.Fatalf("comparator miss = (%v,%v), want (false,true)", affected, valid)
	}
}

func TestCov6PyPIGreaterThan(t *testing.T) {
	if ok, valid := pypiGreaterThan("2.0", "1.0"); !valid || !ok {
		t.Fatalf("pypiGreaterThan(2.0,1.0) = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := pypiGreaterThan("1.0", "2.0"); !valid || ok {
		t.Fatalf("pypiGreaterThan(1.0,2.0) = (%v,%v), want (false,true)", ok, valid)
	}
	if ok, valid := pypiGreaterThan("!!!", "1.0"); valid || ok {
		t.Fatalf("pypiGreaterThan(invalid,1.0) = (%v,%v), want (false,false)", ok, valid)
	}
}

func TestCov6ComparePyPIVersion(t *testing.T) {
	cases := []struct {
		name  string
		left  string
		right string
		want  int
		valid bool
	}{
		{name: "equal", left: "1.0", right: "1.0.0", want: 0, valid: true},
		{name: "epoch wins", left: "1!1.0", right: "9.0", want: 1, valid: true},
		{name: "epoch loses", left: "1.0", right: "1!0.1", want: -1, valid: true},
		{name: "release order", left: "1.2", right: "1.10", want: -1, valid: true},
		{name: "prerelease below final", left: "1.0a1", right: "1.0", want: -1, valid: true},
		{name: "post above final", left: "1.0.post1", right: "1.0", want: 1, valid: true},
		{name: "dev below alpha", left: "1.0.dev1", right: "1.0a1", want: -1, valid: true},
		{name: "stage number", left: "1.0a2", right: "1.0a1", want: 1, valid: true},
		{name: "invalid left", left: "!!!", right: "1.0", want: 0, valid: false},
		{name: "invalid right", left: "1.0", right: "!!!", want: 0, valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := comparePyPIVersion(tc.left, tc.right)
			if ok != tc.valid || (ok && got != tc.want) {
				t.Fatalf("comparePyPIVersion(%q,%q) = (%d,%v), want (%d,%v)",
					tc.left, tc.right, got, ok, tc.want, tc.valid)
			}
		})
	}
}

func TestCov6PyPIBeforeLimitsDecision(t *testing.T) {
	if ok, valid := pypiBeforeLimitsDecision("1.0", nil); !valid || !ok {
		t.Fatalf("no limits = (%v,%v), want (true,true)", ok, valid)
	}
	star := []supplychainmodel.AffectedRangeEvent{{Limit: "*"}}
	if ok, valid := pypiBeforeLimitsDecision("9.0", star); !valid || !ok {
		t.Fatalf("star limit = (%v,%v), want (true,true)", ok, valid)
	}
	below := []supplychainmodel.AffectedRangeEvent{{Limit: "2.0"}}
	if ok, valid := pypiBeforeLimitsDecision("1.0", below); !valid || !ok {
		t.Fatalf("below limit = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := pypiBeforeLimitsDecision("3.0", below); !valid || ok {
		t.Fatalf("above limit = (%v,%v), want (false,true)", ok, valid)
	}
	broken := []supplychainmodel.AffectedRangeEvent{{Limit: "!!!"}}
	if ok, valid := pypiBeforeLimitsDecision("1.0", broken); valid || ok {
		t.Fatalf("broken limit = (%v,%v), want (false,false)", ok, valid)
	}
}

func TestCov6PyPIRangeContainsDecision(t *testing.T) {
	window := supplychainmodel.AffectedRange{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "1.0"},
		{Fixed: "2.0"},
	}}
	if affected, valid := pypiRangeContainsDecision(window, "1.5"); !valid || !affected {
		t.Fatalf("inside window = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := pypiRangeContainsDecision(window, "2.0"); !valid || affected {
		t.Fatalf("at fixed = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := pypiRangeContainsDecision(window, "!!!"); valid || affected {
		t.Fatalf("invalid observed = (%v,%v), want (false,false)", affected, valid)
	}
	capped := supplychainmodel.AffectedRange{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "1.0"},
		{LastAffected: "1.5"},
	}}
	if affected, valid := pypiRangeContainsDecision(capped, "1.6"); !valid || affected {
		t.Fatalf("past last-affected = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := pypiRangeContainsDecision(capped, "1.2"); !valid || !affected {
		t.Fatalf("within last-affected = (%v,%v), want (true,true)", affected, valid)
	}
}

func TestCov6PyPIAffectedByPackage(t *testing.T) {
	hit := supplychainmodel.AffectedPackage{AffectedVersions: []string{"", "1.0"}}
	if affected, valid := pypiAffectedByPackage("1.0", hit); !valid || !affected {
		t.Fatalf("exact hit = (%v,%v), want (true,true)", affected, valid)
	}
	broken := supplychainmodel.AffectedPackage{AffectedVersions: []string{"!!!"}}
	if affected, valid := pypiAffectedByPackage("1.0", broken); valid || affected {
		t.Fatalf("broken candidate = (%v,%v), want (false,false)", affected, valid)
	}
	skipped := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "GIT", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}}},
	}}
	if affected, valid := pypiAffectedByPackage("1.0", skipped); !valid || affected {
		t.Fatalf("skipped kind = (%v,%v), want (false,true)", affected, valid)
	}
	ranged := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: "1.0"},
			{Fixed: "2.0"},
		}},
	}}
	if affected, valid := pypiAffectedByPackage("1.5", ranged); !valid || !affected {
		t.Fatalf("ranged hit = (%v,%v), want (true,true)", affected, valid)
	}
	raw := supplychainmodel.AffectedPackage{AffectedRangeRaw: ">=1.0"}
	if affected, valid := pypiAffectedByPackage("2.0", raw); !valid || !affected {
		t.Fatalf("raw hit = (%v,%v), want (true,true)", affected, valid)
	}
}

func TestCov6VersionRangeGenericHelpers(t *testing.T) {
	if ok, valid := versionGreaterThan("1.0.1", "1.0.0", compareOSVSemver); !valid || !ok {
		t.Fatalf("versionGreaterThan = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := versionGreaterThan("!!!", "1.0.0", compareOSVSemver); valid || ok {
		t.Fatalf("versionGreaterThan invalid = (%v,%v), want (false,false)", ok, valid)
	}
	if ok, valid := versionLessThan("1.0.0", "1.0.1", compareOSVSemver); !valid || !ok {
		t.Fatalf("versionLessThan = (%v,%v), want (true,true)", ok, valid)
	}
	if ok, valid := versionLessThan("1.0.0", "!!!", compareOSVSemver); valid || ok {
		t.Fatalf("versionLessThan invalid = (%v,%v), want (false,false)", ok, valid)
	}
	star := supplychainmodel.AffectedRange{Events: []supplychainmodel.AffectedRangeEvent{{Limit: "*"}}}
	if affected, valid := versionRangeContainsDecision(star, "1.2.3", compareOSVSemver); !valid || affected {
		t.Fatalf("limit-only range = (%v,%v), want (false,true)", affected, valid)
	}
	// A star limit short-circuits before the observed version is validated,
	// so an invalid version under a boundless range is (false,true).
	if affected, valid := versionRangeContainsDecision(star, "!!!", compareOSVSemver); !valid || affected {
		t.Fatalf("limit-only invalid = (%v,%v), want (false,true)", affected, valid)
	}
}

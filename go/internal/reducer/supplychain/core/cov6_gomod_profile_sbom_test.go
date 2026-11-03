// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func TestCov6GoModuleAffectedByPackage(t *testing.T) {
	hit := supplychainmodel.AffectedPackage{AffectedVersions: []string{"", "1.2.3"}}
	if affected, valid := goModuleAffectedByPackage("1.2.3", hit); !valid || !affected {
		t.Fatalf("exact hit = (%v,%v), want (true,true)", affected, valid)
	}
	broken := supplychainmodel.AffectedPackage{AffectedVersions: []string{"!!!"}}
	if affected, valid := goModuleAffectedByPackage("1.2.3", broken); valid || affected {
		t.Fatalf("broken candidate = (%v,%v), want (false,false)", affected, valid)
	}
	skipped := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "GIT", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0.0.0"}}},
	}}
	if affected, valid := goModuleAffectedByPackage("1.2.3", skipped); !valid || affected {
		t.Fatalf("skipped kind = (%v,%v), want (false,true)", affected, valid)
	}
	floored := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0.0.0"}}},
	}}
	if affected, valid := goModuleAffectedByPackage("1.2.3", floored); !valid || !affected {
		t.Fatalf("zero floor = (%v,%v), want (true,true)", affected, valid)
	}
	raw := supplychainmodel.AffectedPackage{AffectedRangeRaw: ">=1.0.0"}
	if affected, valid := goModuleAffectedByPackage("1.5.0", raw); !valid || !affected {
		t.Fatalf("raw hit = (%v,%v), want (true,true)", affected, valid)
	}
	rawBroken := supplychainmodel.AffectedPackage{AffectedRangeRaw: "||"}
	if affected, valid := goModuleAffectedByPackage("1.5.0", rawBroken); valid || affected {
		t.Fatalf("raw broken = (%v,%v), want (false,false)", affected, valid)
	}
}

func TestCov6GoModuleRangeContainsDecision(t *testing.T) {
	floored := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "0.0.0"},
	}}
	if affected, valid := goModuleRangeContainsDecision(floored, "1.2.3"); !valid || !affected {
		t.Fatalf("zero floor = (%v,%v), want (true,true)", affected, valid)
	}
	window := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "1.0.0"},
		{Fixed: "2.0.0"},
	}}
	if affected, valid := goModuleRangeContainsDecision(window, "1.5.0"); !valid || !affected {
		t.Fatalf("inside window = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := goModuleRangeContainsDecision(window, "2.0.0"); !valid || affected {
		t.Fatalf("at fixed = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := goModuleRangeContainsDecision(window, "!!!"); valid || affected {
		t.Fatalf("invalid observed = (%v,%v), want (false,false)", affected, valid)
	}
	capped := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Introduced: "1.0.0"},
		{LastAffected: "1.5.0"},
	}}
	if affected, valid := goModuleRangeContainsDecision(capped, "1.6.0"); !valid || affected {
		t.Fatalf("past last-affected = (%v,%v), want (false,true)", affected, valid)
	}
	beforeLimited := supplychainmodel.AffectedRange{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
		{Limit: "1.0.0"},
		{Introduced: "0.0.0"},
	}}
	if affected, valid := goModuleRangeContainsDecision(beforeLimited, "2.0.0"); !valid || affected {
		t.Fatalf("past limit = (%v,%v), want (false,true)", affected, valid)
	}
}

func TestCov6GoModuleComparatorConstraint(t *testing.T) {
	if affected, valid := goModuleComparatorConstraintContains(">=", "1.2.3"); valid || affected {
		t.Fatalf("bare operator = (%v,%v), want (false,false)", affected, valid)
	}
	// A >= v0.0.0 floor accepts any valid semver without a range check.
	if affected, valid := goModuleComparatorConstraintContains(">=v0.0.0", "1.2.3"); !valid || !affected {
		t.Fatalf("zero floor = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := goModuleComparatorRangeContains("", "1.2.3"); !valid || affected {
		t.Fatalf("empty raw = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := goModuleComparatorRangeContains("||", "1.2.3"); valid || affected {
		t.Fatalf("blank branches = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := goModuleComparatorRangeContains(">=1.0.0", "1.5.0"); !valid || !affected {
		t.Fatalf("comparator hit = (%v,%v), want (true,true)", affected, valid)
	}
	if !goModuleVersionFloor("0.0.0") {
		t.Fatal("goModuleVersionFloor(0.0.0) = false, want true")
	}
	if goModuleVersionFloor("1.0.0") {
		t.Fatal("goModuleVersionFloor(1.0.0) = true, want false")
	}
	if goModuleVersionFloor("!!!") {
		t.Fatal("goModuleVersionFloor(invalid) = true, want false")
	}
	decision := evaluateGoSemverMatch("!!!", "", nil)
	if decision.Reason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("go invalid observed reason = %q, want malformed-installed", decision.Reason)
	}
	decision = evaluateGoSemverMatch("1.5.0", "2.0.0", nil)
	if decision.Status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("go no match status = %q, want possibly-affected", decision.Status)
	}
}

func TestCov6NuGetSemverMatchDecisions(t *testing.T) {
	decision := evaluateNuGetSemverMatch("!!!", "", nil)
	if decision.Reason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("nuget invalid observed reason = %q, want malformed-installed", decision.Reason)
	}
	window := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "SEMVER", Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: "1.0.0"},
			{Fixed: "2.0.0"},
		}},
	}}
	decision = evaluateNuGetSemverMatch("1.5.0", "", []supplychainmodel.AffectedPackage{window})
	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("nuget window hit status = %q, want affected-exact", decision.Status)
	}
	decision = evaluateNuGetSemverMatch("1.5.0", "!!!", nil)
	if decision.Reason != supplyChainVersionReasonMalformedRange {
		t.Fatalf("nuget invalid fixed reason = %q, want malformed-range", decision.Reason)
	}
	decision = evaluateNuGetSemverMatch("2.5.0", "2.0.0", nil)
	if decision.Status != SupplyChainImpactNotAffectedKnownFixed {
		t.Fatalf("nuget known-fixed status = %q, want known-fixed", decision.Status)
	}
	if affected, valid := nugetAffectedByPackage("1.0.0", supplychainmodel.AffectedPackage{
		AffectedRangeRaw: ">=9.0.0",
	}); !valid || affected {
		t.Fatalf("nuget raw miss = (%v,%v), want (false,true)", affected, valid)
	}
}

func TestCov6RubyGemsRequirementEdges(t *testing.T) {
	if affected, valid := rubyGemsRequirementContains("", "1.0"); !valid || affected {
		t.Fatalf("empty requirement = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := rubyGemsRequirementContains("~>", "1.0"); valid || affected {
		t.Fatalf("dangling pessimistic = (%v,%v), want (false,false)", affected, valid)
	}
	if affected, valid := rubyGemsRequirementContains("~> 1.2", "1.5"); !valid || !affected {
		t.Fatalf("pessimistic hit = (%v,%v), want (true,true)", affected, valid)
	}
	if affected, valid := rubyGemsRequirementContains("~> 1.2", "2.0"); !valid || affected {
		t.Fatalf("pessimistic miss = (%v,%v), want (false,true)", affected, valid)
	}
	if affected, valid := rubyGemsRequirementContains("~> !!!", "1.0"); valid || affected {
		t.Fatalf("pessimistic invalid = (%v,%v), want (false,false)", affected, valid)
	}
	if got, ok := rubyGemsPessimisticUpperBound("1.2.3"); !ok || got != "1.3.0" {
		t.Fatalf("rubyGemsPessimisticUpperBound(1.2.3) = (%q,%v), want (1.3.0,true)", got, ok)
	}
	if got, ok := rubyGemsPessimisticUpperBound("1.2"); !ok || got != "2.0" {
		t.Fatalf("rubyGemsPessimisticUpperBound(1.2) = (%q,%v), want (2.0,true)", got, ok)
	}
	if _, ok := rubyGemsPessimisticUpperBound(""); ok {
		t.Fatal("rubyGemsPessimisticUpperBound(empty) = true, want false")
	}
	if _, ok := rubyGemsPessimisticUpperBound("..."); ok {
		t.Fatal("rubyGemsPessimisticUpperBound(dots) = true, want false")
	}
	hit := supplychainmodel.AffectedPackage{AffectedRangeRaw: ">=1.0"}
	if affected, valid := rubyGemsAffectedByPackage("2.0", hit); !valid || !affected {
		t.Fatalf("rubygems raw hit = (%v,%v), want (true,true)", affected, valid)
	}
	skipped := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "GIT", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "1.0"}}},
	}}
	if affected, valid := rubyGemsAffectedByPackage("2.0", skipped); !valid || affected {
		t.Fatalf("rubygems skipped kind = (%v,%v), want (false,true)", affected, valid)
	}
	if !rubyGemsAffectedRangeKind("semver") || !rubyGemsAffectedRangeKind("ECOSYSTEM") || rubyGemsAffectedRangeKind("GIT") {
		t.Fatal("rubyGemsAffectedRangeKind mismatch: want semver/ecosystem true, git false")
	}
}

func TestCov6ValidDetectionProfile(t *testing.T) {
	for value, want := range map[string]bool{
		"": true, "precise": true, "comprehensive": true, "bogus": false,
	} {
		if got := ValidDetectionProfile(value); got != want {
			t.Fatalf("ValidDetectionProfile(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestCov6ConsumptionProvesObservedVersion(t *testing.T) {
	proven := supplychainmodel.PackageConsumption{ObservedVersion: "1.0"}
	if !consumptionProvesObservedVersion("npm", "1.0.0", proven) {
		t.Fatal("npm 1.0 vs 1.0.0 proves = false, want true")
	}
	if consumptionProvesObservedVersion("npm", "2.0.0", proven) {
		t.Fatal("npm 1.0 vs 2.0.0 proves = true, want false")
	}
	// Range-only manifests cannot borrow an SBOM exact version.
	rangeOnly := supplychainmodel.PackageConsumption{DependencyRange: ">=1.0"}
	if consumptionProvesObservedVersion("npm", "1.5.0", rangeOnly) {
		t.Fatal("range-only manifest proves = true, want false")
	}
	if !equivalentSupplyChainVersions("nonsuch-eco", "1.0", "1.0") {
		t.Fatal("identical versions across unknown ecosystem = false, want true")
	}
	if equivalentSupplyChainVersions("nonsuch-eco", "1.0", "2.0") {
		t.Fatal("distinct versions across unknown ecosystem = true, want false")
	}
	if !equivalentSupplyChainVersions("nuget", "1.0", "1.0.0") {
		t.Fatal("nuget 1.0 vs 1.0.0 equivalent = false, want true")
	}
	if equivalentSupplyChainVersions("pypi", "1.0", "!!!") {
		t.Fatal("pypi 1.0 vs invalid equivalent = true, want false")
	}
	conflict := supplychainmodel.SBOMComponent{PayloadVersion: "1.0", PURLVersion: "2.0"}
	if !sbomVersionEvidenceConflicts("npm", conflict, supplychainmodel.PackageConsumption{}) {
		t.Fatal("conflicting SBOM evidence conflicts = false, want true")
	}
	calm := supplychainmodel.SBOMComponent{PayloadVersion: "1.0", PURLVersion: "1.0.0"}
	if sbomVersionEvidenceConflicts("npm", calm, supplychainmodel.PackageConsumption{}) {
		t.Fatal("agreeing SBOM evidence conflicts = true, want false")
	}
}

func TestCov6GoReachabilityLevelPriority(t *testing.T) {
	cases := map[string]int{
		"symbol": 0, "import": 1, "not_called": 2,
		"module": 3, "unknown": 4, "anything-else": 100,
	}
	for level, want := range cases {
		if got := goReachabilityLevelPriority(level); got != want {
			t.Fatalf("goReachabilityLevelPriority(%q) = %d, want %d", level, got, want)
		}
	}
}

func TestCov6PythonPackageNameHelpers(t *testing.T) {
	for raw, want := range map[string]string{
		"": "", "pkg:pypi/requests": "requests", "myorg/mypkg": "mypkg", "noseparator": "",
	} {
		if got := pythonPackageNameFromPackageID(raw); got != want {
			t.Fatalf("pythonPackageNameFromPackageID(%q) = %q, want %q", raw, got, want)
		}
	}
	for raw, want := range map[string]string{
		"": "", "pkg:pypi/requests@2.0": "requests", "pkg:pypi/requests": "requests", "bare": "",
	} {
		if got := pythonPackageNameFromPURL(raw); got != want {
			t.Fatalf("pythonPackageNameFromPURL(%q) = %q, want %q", raw, got, want)
		}
	}
	evidence := pythonReachabilityRepositoryEvidence{
		importedModules:  map[string][]string{"os": {"f1", "f1"}},
		calledModules:    map[string][]string{"os": {"f2"}},
		decoratedModules: map[string][]string{"flask": {"f3"}},
	}
	if got := evidence.importFactIDs("os"); len(got) != 1 || got[0] != "f1" {
		t.Fatalf("importFactIDs(os) = %v, want [f1] deduplicated", got)
	}
	if got := evidence.callFactIDs("missing"); len(got) != 0 {
		t.Fatalf("callFactIDs(missing) = %v, want empty", got)
	}
	if got := evidence.decoratorFactIDs("flask"); len(got) != 1 || got[0] != "f3" {
		t.Fatalf("decoratorFactIDs(flask) = %v, want [f3]", got)
	}
	if evidence.hasParserEvidence() == (pythonReachabilityRepositoryEvidence{}).hasParserEvidence() {
		t.Fatal("populated evidence must report parser evidence while empty must not")
	}
	scopes := sortedPythonReachabilityRepositoryScopes(map[pythonReachabilityRepositoryScope][]string{
		{scopeID: "b", generationID: "2"}: {"r2"},
		{scopeID: "a", generationID: "1"}: {"r1"},
	})
	if len(scopes) != 2 || scopes[0].scopeID != "a" || scopes[1].scopeID != "b" {
		t.Fatalf("sorted scopes = %+v, want [a b]", scopes)
	}
}

func TestCov6NpmPackageRootHelpers(t *testing.T) {
	if _, ok := npmPackageRootFromSCIPValue(""); ok {
		t.Fatal("npmPackageRootFromSCIPValue(empty) = true, want false")
	}
	if got, ok := npmPackageRootFromSCIPValue("npm lodash"); !ok || got != "lodash" {
		t.Fatalf("npmPackageRootFromSCIPValue(npm lodash) = (%q,%v), want (lodash,true)", got, ok)
	}
	if got, ok := npmPackageRootFromSCIPValue("lodash/fp"); !ok || got != "lodash" {
		t.Fatalf("npmPackageRootFromSCIPValue(fallback) = (%q,%v), want (lodash,true)", got, ok)
	}
	for raw := range map[string]struct{}{"": {}, "./rel": {}, "/abs": {}, "node:fs": {}, "@scope": {}} {
		if _, ok := npmPackageRootFromImportSource(raw); ok {
			t.Fatalf("npmPackageRootFromImportSource(%q) = true, want false", raw)
		}
	}
	if got, ok := npmPackageRootFromImportSource("@scope/pkg/sub"); !ok || got != "@scope/pkg" {
		t.Fatalf("scoped root = (%q,%v), want (@scope/pkg,true)", got, ok)
	}
	if !jsTSCallReferencesImportAlias("foo.bar", "foo") {
		t.Fatal("jsTSCallReferencesImportAlias(foo.bar,foo) = false, want true")
	}
	if jsTSCallReferencesImportAlias("foobar", "foo") {
		t.Fatal("jsTSCallReferencesImportAlias(foobar,foo) = true, want false")
	}
	if jsTSCallReferencesImportAlias("foo", "default") {
		t.Fatal("jsTSCallReferencesImportAlias(foo,default) = true, want false")
	}
	if got := jsTSPackageAPIEvidencePriority(jsTSPackageAPICallEvidence); got != 0 {
		t.Fatalf("call priority = %d, want 0", got)
	}
	if got := jsTSPackageAPIEvidencePriority(jsTSPackageAPISCIPCallEvidence); got != 0 {
		t.Fatalf("scip call priority = %d, want 0", got)
	}
	if got := jsTSPackageAPIEvidencePriority(jsTSPackageAPIReExportEvidence); got != 1 {
		t.Fatalf("reexport priority = %d, want 1", got)
	}
	if got := jsTSPackageAPIEvidencePriority(jsTSPackageAPIImportEvidence); got != 2 {
		t.Fatalf("import priority = %d, want 2", got)
	}
	if got := jsTSPackageAPIEvidencePriority("bogus"); got != 3 {
		t.Fatalf("unknown priority = %d, want 3", got)
	}
}

func TestCov6EvaluateOSPackageVersionMatch(t *testing.T) {
	decision := evaluateOSPackageVersionMatch(
		"!!! has space", "", nil,
		"apk_exact_affected_version", "apk_exact_known_fixed",
		"apk_affected_range", "apk_known_fixed",
		compareAPKVersion,
	)
	if decision.Reason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("apk invalid observed reason = %q, want malformed-installed", decision.Reason)
	}
	ranged := supplychainmodel.AffectedPackage{AffectedRanges: []supplychainmodel.AffectedRange{
		{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: "1.0"},
			{Fixed: "2.0"},
		}},
	}}
	decision = evaluateOSPackageVersionMatch(
		"1.5", "", []supplychainmodel.AffectedPackage{ranged},
		"apk_exact_affected_version", "apk_exact_known_fixed",
		"apk_affected_range", "apk_known_fixed",
		compareAPKVersion,
	)
	if decision.Reason != supplyChainVersionReasonAPKAffectedRange {
		t.Fatalf("apk range hit reason = %q, want apk_affected_range", decision.Reason)
	}
	decision = evaluateOSPackageVersionMatch(
		"1.0", "1.0", nil,
		"apk_exact_affected_version", "apk_exact_known_fixed",
		"apk_affected_range", "apk_known_fixed",
		compareAPKVersion,
	)
	if decision.Reason != supplyChainVersionReasonAPKExactKnownFixed {
		t.Fatalf("apk pinned fixed reason = %q, want apk_exact_known_fixed", decision.Reason)
	}
	decision = evaluateOSPackageVersionMatch(
		"2.0", "1.0", nil,
		"apk_exact_affected_version", "apk_exact_known_fixed",
		"apk_affected_range", "apk_known_fixed",
		compareAPKVersion,
	)
	if decision.Reason != supplyChainVersionReasonAPKKnownFixed {
		t.Fatalf("apk newer reason = %q, want apk_known_fixed", decision.Reason)
	}
}

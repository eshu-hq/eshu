// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsProvesComposerLockfileExactVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-composer-runtime", "CVE-2026-101201", 8.2),
		vulnerabilityAffectedPackageRangeFact(
			"affected-composer-runtime",
			"CVE-2026-101201",
			"pkg:composer/symfony/http-kernel",
			"packagist",
			"symfony/http-kernel",
			"6.4.2",
		),
		composerLockContentEntityFact(
			"composer-runtime",
			"repo-php",
			"symfony/http-kernel",
			"6.4.1",
			"packages",
			"runtime",
			[]string{"symfony/http-kernel"},
			1,
			boolPointer(true),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-101201"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "6.4.1" {
		t.Fatalf("ObservedVersion = %q, want Composer lockfile version 6.4.1", got.ObservedVersion)
	}
	if got.MatchReason != supplyChainVersionReasonComposerSemverAffectedRange {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonComposerSemverAffectedRange)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise", got.DetectionProfile)
	}
	if got.DependencyScope != "runtime" {
		t.Fatalf("DependencyScope = %q, want runtime", got.DependencyScope)
	}
	if got.DirectDependency == nil || !*got.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true", got.DirectDependency)
	}
	if !reflect.DeepEqual(got.DependencyPath, []string{"symfony/http-kernel"}) {
		t.Fatalf("DependencyPath = %#v, want direct Composer path", got.DependencyPath)
	}
	assertSupplyChainReachability(
		t,
		got,
		SupplyChainReachabilityReachable,
		"composer",
		"composer_dependency_path",
	)
}

func TestBuildSupplyChainImpactFindingsKeepsComposerDevScopeVisible(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-composer-dev", "CVE-2026-101202", 5.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-composer-dev",
			"CVE-2026-101202",
			"pkg:composer/phpunit/phpunit",
			"composer",
			"phpunit/phpunit",
			"10.5.1",
		),
		composerLockContentEntityFact(
			"composer-dev",
			"repo-php",
			"phpunit/phpunit",
			"10.5.0",
			"packages-dev",
			"dev",
			[]string{"phpunit/phpunit"},
			1,
			boolPointer(true),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-101202"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.DependencyScope != "dev" {
		t.Fatalf("DependencyScope = %q, want dev", got.DependencyScope)
	}
	if got.RuntimeReachability != "package_manifest" {
		t.Fatalf("RuntimeReachability = %q, want package_manifest", got.RuntimeReachability)
	}
	assertSupplyChainReachability(
		t,
		got,
		SupplyChainReachabilityReachable,
		"composer",
		"composer_dependency_path",
	)
}

func TestBuildSupplyChainImpactFindingsLeavesComposerManifestRangeIncomplete(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-composer-range", "CVE-2026-101203", 7.0),
		vulnerabilityAffectedPackageRangeFact(
			"affected-composer-range",
			"CVE-2026-101203",
			"pkg:composer/monolog/monolog",
			"composer",
			"monolog/monolog",
			"3.7.0",
		),
		composerManifestContentEntityFact("composer-range", "repo-php", "monolog/monolog", "^3.0", "require", "runtime"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-101203"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank for Composer manifest-only range", got.ObservedVersion)
	}
	if got.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
	if !containsString(got.MissingEvidence, supplyChainMissingInstalledVersion) {
		t.Fatalf("MissingEvidence = %#v, want installed-version reason", got.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactFindingsMarksComposerKnownFixed(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-composer-fixed", "CVE-2026-101204", 6.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-composer-fixed",
			"CVE-2026-101204",
			"pkg:composer/symfony/http-kernel",
			"composer",
			"symfony/http-kernel",
			"6.4.2",
		),
		composerLockContentEntityFact(
			"composer-fixed",
			"repo-php",
			"symfony/http-kernel",
			"6.4.2",
			"packages",
			"runtime",
			[]string{"symfony/http-kernel"},
			1,
			boolPointer(true),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-101204"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.MatchReason != supplyChainVersionReasonComposerSemverKnownFixed {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonComposerSemverKnownFixed)
	}
}

func TestBuildSupplyChainImpactFindingsReportsMalformedComposerInstalledVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-composer-malformed", "CVE-2026-101205", 6.4),
		vulnerabilityAffectedPackageRangeFact(
			"affected-composer-malformed",
			"CVE-2026-101205",
			"pkg:composer/symfony/http-kernel",
			"composer",
			"symfony/http-kernel",
			"6.4.2",
		),
		composerLockContentEntityFact(
			"composer-malformed",
			"repo-php",
			"symfony/http-kernel",
			"6.x-dev",
			"packages",
			"runtime",
			[]string{"symfony/http-kernel"},
			1,
			boolPointer(true),
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-101205"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != supplyChainVersionReasonMalformedInstalled {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonMalformedInstalled)
	}
	if !containsString(got.MissingEvidence, supplyChainMissingMalformedInstalled) {
		t.Fatalf("MissingEvidence = %#v, want malformed installed-version reason", got.MissingEvidence)
	}
}

func composerLockContentEntityFact(
	factID string,
	repositoryID string,
	name string,
	version string,
	section string,
	scope string,
	dependencyPath []string,
	dependencyDepth int,
	directDependency *bool,
) facts.Envelope {
	payload := composerManifestContentEntityFact(factID, repositoryID, name, version, section, scope)
	payload.Payload["relative_path"] = "composer.lock"
	metadata := payload.Payload["entity_metadata"].(map[string]any)
	metadata["lockfile"] = true
	pathValues := make([]any, 0, len(dependencyPath))
	for _, item := range dependencyPath {
		pathValues = append(pathValues, item)
	}
	metadata["dependency_path"] = pathValues
	metadata["dependency_depth"] = dependencyDepth
	if directDependency != nil {
		metadata["direct_dependency"] = *directDependency
	}
	return payload
}

func composerManifestContentEntityFact(
	factID string,
	repositoryID string,
	name string,
	value string,
	section string,
	scope string,
) facts.Envelope {
	observedAt := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	return facts.Envelope{
		FactID:      factID,
		FactKind:    factKindContentEntity,
		ObservedAt:  observedAt,
		SourceRef:   facts.Ref{SourceSystem: "git"},
		IsTombstone: false,
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": "composer.json",
			"entity_type":   "Variable",
			"entity_name":   name,
			"repo_name":     "php-app",
			"entity_metadata": map[string]any{
				"config_kind":      "dependency",
				"package_manager":  "composer",
				"section":          section,
				"value":            value,
				"dependency_scope": scope,
			},
		},
	}
}

func boolPointer(value bool) *bool {
	return &value
}

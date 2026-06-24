// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageConsumptionDecisionsSeparatesNuGetLockfileRequestedRange(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:nuget/newtonsoft.json", "nuget", "Newtonsoft.Json", "", observedAt),
		packageSourceRepositoryFact("repo-dotnet", "dotnet-worker", "https://github.com/acme/dotnet-worker", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-dotnet",
			"dotnet-worker",
			"packages.lock.json",
			"Newtonsoft.Json",
			"nuget",
			"13.0.3",
			observedAt,
			map[string]any{
				"section":           "packages.lock.json:net8.0",
				"lockfile":          true,
				"requested_range":   "[13.0.0, 14.0.0)",
				"dependency_path":   []any{"Newtonsoft.Json"},
				"dependency_depth":  1,
				"direct_dependency": true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.ObservedVersion, "13.0.3"; got != want {
		t.Fatalf("ObservedVersion = %q, want exact lockfile version %q", got, want)
	}
	if got, want := decision.RequestedRange, "[13.0.0, 14.0.0)"; got != want {
		t.Fatalf("RequestedRange = %q, want lockfile requested range %q", got, want)
	}
	if got, want := decision.DependencyRange, "13.0.3"; got != want {
		t.Fatalf("DependencyRange = %q, want compatibility field to keep exact lockfile value %q", got, want)
	}
}

func TestBuildPackageConsumptionDecisionsPreservesNuGetPartialMSBuildEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 5, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:nuget/newtonsoft.json", "nuget", "Newtonsoft.Json", "", observedAt),
		packageSourceRepositoryFact("repo-dotnet", "dotnet-worker", "https://github.com/acme/dotnet-worker", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-dotnet",
			"dotnet-worker",
			"Worker.csproj",
			"Newtonsoft.Json",
			"nuget",
			"$(NewtonsoftJsonVersion)",
			observedAt,
			map[string]any{
				"section":                     "PackageReference",
				"requested_version":           "$(NewtonsoftJsonVersion)",
				"version_evidence":            "unresolved_msbuild_property",
				"unresolved_msbuild_property": "NewtonsoftJsonVersion",
				"partial_evidence":            true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if decision.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want empty for unresolved MSBuild property", decision.ObservedVersion)
	}
	if got, want := decision.RequestedRange, "$(NewtonsoftJsonVersion)"; got != want {
		t.Fatalf("RequestedRange = %q, want raw unresolved property %q", got, want)
	}
	if got, want := decision.VersionEvidence, "unresolved_msbuild_property"; got != want {
		t.Fatalf("VersionEvidence = %q, want %q", got, want)
	}
	if got, want := decision.UnresolvedMSBuildProperty, "NewtonsoftJsonVersion"; got != want {
		t.Fatalf("UnresolvedMSBuildProperty = %q, want %q", got, want)
	}
	if !decision.PartialEvidence {
		t.Fatal("PartialEvidence = false, want true for unresolved MSBuild property")
	}
}

func TestPackageConsumptionPayloadPersistsNuGetVersionEvidence(t *testing.T) {
	t.Parallel()

	payload := packageConsumptionPayload(PackageCorrelationWrite{
		IntentID:     "intent-nuget",
		ScopeID:      "scope-nuget",
		GenerationID: "generation-nuget",
		SourceSystem: "package_registry",
		Cause:        "package registry observed",
	}, PackageConsumptionDecision{
		PackageID:                 "pkg:nuget/newtonsoft.json",
		Ecosystem:                 "nuget",
		PackageName:               "Newtonsoft.Json",
		RepositoryID:              "repo-dotnet",
		RelativePath:              "packages.lock.json",
		ManifestSection:           "packages.lock.json:net8.0",
		DependencyRange:           "13.0.3",
		ObservedVersion:           "13.0.3",
		RequestedRange:            "[13.0.0, 14.0.0)",
		VersionEvidence:           "unresolved_msbuild_property",
		UnresolvedMSBuildProperty: "NewtonsoftJsonVersion",
		PartialEvidence:           true,
		Outcome:                   PackageConsumptionManifestDeclared,
		Reason:                    "git manifest dependency matches package registry identity",
		CanonicalWrites:           1,
	})

	if got, want := payload["observed_version"], "13.0.3"; got != want {
		t.Fatalf("observed_version = %#v, want %#v", got, want)
	}
	if got, want := payload["requested_range"], "[13.0.0, 14.0.0)"; got != want {
		t.Fatalf("requested_range = %#v, want %#v", got, want)
	}
	if got, want := payload["unresolved_msbuild_property"], "NewtonsoftJsonVersion"; got != want {
		t.Fatalf("unresolved_msbuild_property = %#v, want %#v", got, want)
	}
	if got, want := payload["partial_evidence"], true; got != want {
		t.Fatalf("partial_evidence = %#v, want %#v", got, want)
	}
}

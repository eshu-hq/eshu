// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildPackageConsumptionDecisionsAdmitsComposerLockfileExactVersion
// proves the Composer lockfile coverage acceptance criterion from
// issue #647: a composer.lock fact (section "packages", lockfile flag,
// exact installed version) must produce a package-consumption decision
// against a registry identity, while the lockfile shape implies that
// directness is not yet known so the reducer must surface that as nil
// instead of inventing a direct-dependency claim.
func TestBuildPackageConsumptionDecisionsAdmitsComposerLockfileExactVersion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pkg:composer/monolog/monolog",
			"composer",
			"monolog",
			"monolog",
			observedAt,
		),
		packageSourceRepositoryFact("repo-php", "php-app", "https://github.com/acme/php-app", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-php",
			"php-app",
			"composer.lock",
			"monolog/monolog",
			"composer",
			"2.9.1",
			observedAt,
			map[string]any{
				"section":  "packages",
				"lockfile": true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.PackageID, "pkg:composer/monolog/monolog"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.Ecosystem, "composer"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	if got, want := decision.RepositoryID, "repo-php"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := decision.RelativePath, "composer.lock"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := decision.ManifestSection, "packages"; got != want {
		t.Fatalf("ManifestSection = %q, want %q", got, want)
	}
	if got, want := decision.DependencyRange, "2.9.1"; got != want {
		t.Fatalf("DependencyRange = %q, want %q", got, want)
	}
	if decision.DirectDependency != nil {
		t.Fatalf("DirectDependency = %#v, want nil for composer.lock entry without explicit chain", decision.DirectDependency)
	}
	if got, want := decision.DependencyDepth, 0; got != want {
		t.Fatalf("DependencyDepth = %d, want %d for lockfile entry without explicit chain", got, want)
	}
	if got, want := decision.Outcome, PackageConsumptionManifestDeclared; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decision.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
}

// TestBuildPackageConsumptionDecisionsKeepsComposerDevLockfileSeparate
// guards the dev/runtime split for composer.lock evidence. Both
// `packages` and `packages-dev` rows must reach the reducer as distinct
// consumption decisions so impact reporting can bound dev-only
// vulnerabilities away from production code.
func TestBuildPackageConsumptionDecisionsKeepsComposerDevLockfileSeparate(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"pkg:composer/phpunit/phpunit",
			"composer",
			"phpunit",
			"phpunit",
			observedAt,
		),
		packageRegistryPackageFact(
			"pkg:composer/monolog/monolog",
			"composer",
			"monolog",
			"monolog",
			observedAt,
		),
		packageSourceRepositoryFact("repo-php", "php-app", "https://github.com/acme/php-app", false, observedAt),
		composerLockManifestDependencyFact(
			"repo-php",
			"php-app",
			"monolog/monolog",
			"2.9.1",
			"packages",
			"dep-runtime",
			observedAt,
		),
		composerLockManifestDependencyFact(
			"repo-php",
			"php-app",
			"phpunit/phpunit",
			"9.6.13",
			"packages-dev",
			"dep-dev",
			observedAt,
		),
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}

	bySection := map[string]PackageConsumptionDecision{}
	for _, decision := range decisions {
		bySection[decision.ManifestSection] = decision
	}
	runtime, ok := bySection["packages"]
	if !ok {
		t.Fatalf("missing packages decision: %#v", decisions)
	}
	if got, want := runtime.PackageID, "pkg:composer/monolog/monolog"; got != want {
		t.Fatalf("runtime PackageID = %q, want %q", got, want)
	}
	dev, ok := bySection["packages-dev"]
	if !ok {
		t.Fatalf("missing packages-dev decision: %#v", decisions)
	}
	if got, want := dev.PackageID, "pkg:composer/phpunit/phpunit"; got != want {
		t.Fatalf("dev PackageID = %q, want %q", got, want)
	}
	if got, want := dev.DependencyRange, "9.6.13"; got != want {
		t.Fatalf("dev exact version = %q, want %q", got, want)
	}
	if got, want := runtime.DependencyRange, "2.9.1"; got != want {
		t.Fatalf("runtime exact version = %q, want %q", got, want)
	}
}

func composerLockManifestDependencyFact(
	repositoryID string,
	repositoryName string,
	dependencyName string,
	exactVersion string,
	section string,
	factSuffix string,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "composer-lock-dep:" + repositoryID + ":" + factSuffix,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repositoryID + ":composer.lock:" + dependencyName,
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": "composer.lock",
			"entity_type":   "Variable",
			"entity_name":   dependencyName,
			"entity_metadata": map[string]any{
				"config_kind":     "dependency",
				"package_manager": "composer",
				"section":         section,
				"value":           exactVersion,
				"lockfile":        true,
			},
			"repo_name": repositoryName,
		},
	}
}

func TestBuildPackageConsumptionDecisionsMatchesNuGetLockfileDependencies(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
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
	if got, want := decision.PackageID, "pkg:nuget/newtonsoft.json"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.Ecosystem, "nuget"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	if got, want := decision.PackageName, "Newtonsoft.Json"; got != want {
		t.Fatalf("PackageName = %q, want source manifest spelling %q", got, want)
	}
	if !reflect.DeepEqual(decision.DependencyPath, []string{"Newtonsoft.Json"}) {
		t.Fatalf("DependencyPath = %#v, want direct NuGet dependency path", decision.DependencyPath)
	}
	if decision.DirectDependency == nil || !*decision.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true for NuGet direct lockfile dependency", decision.DirectDependency)
	}
}

func TestBuildPackageConsumptionDecisionsPreservesNuGetProjectSignals(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 10, 35, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:nuget/xunit", "nuget", "xunit", "", observedAt),
		packageSourceRepositoryFact("repo-dotnet", "dotnet-worker", "https://github.com/acme/dotnet-worker", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-dotnet",
			"dotnet-worker",
			"Worker.csproj",
			"xunit",
			"nuget",
			"2.7.0",
			observedAt,
			map[string]any{
				"section":                "PackageReference",
				"dependency_scope":       "test",
				"private_assets":         "all",
				"include_assets":         "runtime; build; native; contentfiles; analyzers; buildtransitive",
				"development_dependency": true,
				"test_dependency":        true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.DependencyScope, "test"; got != want {
		t.Fatalf("DependencyScope = %q, want %q", got, want)
	}
	if got, want := decision.PrivateAssets, "all"; got != want {
		t.Fatalf("PrivateAssets = %q, want %q", got, want)
	}
	if !decision.DevelopmentOnly || !decision.TestDependency {
		t.Fatalf("DevelopmentOnly/TestDependency = %v/%v, want true/true", decision.DevelopmentOnly, decision.TestDependency)
	}
}

func TestBuildPackageConsumptionDecisionsKeepsCargoLockfileWithoutProofUnchained(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("cargo://crates.io/ambiguous", "cargo", "ambiguous", "", observedAt),
		packageSourceRepositoryFact("repo-rust", "rust-api", "https://github.com/example/rust-api", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-rust",
			"rust-api",
			"Cargo.lock",
			"ambiguous",
			"cargo",
			"0.1.0",
			observedAt,
			map[string]any{
				"section":  "cargo-lock",
				"lockfile": true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if len(decision.DependencyPath) != 0 {
		t.Fatalf("DependencyPath = %#v, want empty without Cargo.lock reachability proof", decision.DependencyPath)
	}
	if decision.DependencyDepth != 0 {
		t.Fatalf("DependencyDepth = %d, want 0 without Cargo.lock reachability proof", decision.DependencyDepth)
	}
	if decision.DirectDependency != nil {
		t.Fatalf("DirectDependency = %#v, want nil without Cargo.lock reachability proof", decision.DirectDependency)
	}
}

func TestPackageCorrelationWriterPersistsCargoLockfileEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 18, 15, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("cargo://crates.io/serde_json", "cargo", "serde_json", "", observedAt),
		packageSourceRepositoryFact("repo-rust", "rust-api", "https://github.com/example/rust-api", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-rust",
			"rust-api",
			"Cargo.lock",
			"serde_json",
			"cargo",
			"1.0.116",
			observedAt,
			map[string]any{
				"section":  "cargo-lock",
				"lockfile": true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresPackageCorrelationWriter{DB: db, Now: func() time.Time { return observedAt }}
	_, err := writer.WritePackageCorrelations(context.Background(), PackageCorrelationWrite{
		ScopeID:              "scope-package",
		GenerationID:         "generation-package",
		ConsumptionDecisions: decisions,
	})
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	payload := unmarshalPackageCorrelationPayload(t, db.execs[0].args[15])
	if got, want := payload["lockfile"], true; got != want {
		t.Fatalf("lockfile = %#v, want %#v so Cargo.lock exact-version evidence survives reducer handoff", got, want)
	}
}

func TestBuildPackageConsumptionDecisionsMatchesCargoRenamedPackage(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("cargo://crates.io/serde_json", "cargo", "serde_json", "", observedAt),
		packageSourceRepositoryFact("repo-rust", "rust-api", "https://github.com/example/rust-api", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-rust",
			"rust-api",
			"Cargo.toml",
			"serde_json",
			"cargo",
			"1.0",
			observedAt,
			map[string]any{
				"section":          "dependencies",
				"dependency_scope": "runtime",
				"dependency_alias": "json",
				"manifest_name":    "json",
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.PackageID, "cargo://crates.io/serde_json"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.Ecosystem, "cargo"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	if got, want := decision.PackageName, "serde_json"; got != want {
		t.Fatalf("PackageName = %q, want canonical Cargo package identity %q", got, want)
	}
	if got, want := decision.DependencyRange, "1.0"; got != want {
		t.Fatalf("DependencyRange = %q, want %q", got, want)
	}
}

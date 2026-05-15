package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCanonicalMaterializationExtractsPackageRegistryRows(t *testing.T) {
	t.Parallel()

	result := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		packageRegistryFacts(),
	)

	if got, want := len(result.PackageRegistryPackages), 1; got != want {
		t.Fatalf("len(PackageRegistryPackages) = %d, want %d", got, want)
	}
	pkg := result.PackageRegistryPackages[0]
	if got, want := pkg.UID, packageRegistryPackageID(); got != want {
		t.Fatalf("package UID = %q, want %q", got, want)
	}
	if got, want := pkg.Ecosystem, "npm"; got != want {
		t.Fatalf("package Ecosystem = %q, want %q", got, want)
	}
	if got, want := pkg.NormalizedName, "@scope/pkg"; got != want {
		t.Fatalf("package NormalizedName = %q, want %q", got, want)
	}
	if got, want := pkg.Visibility, "public"; got != want {
		t.Fatalf("package Visibility = %q, want %q", got, want)
	}

	if got, want := len(result.PackageRegistryVersions), 1; got != want {
		t.Fatalf("len(PackageRegistryVersions) = %d, want %d", got, want)
	}
	version := result.PackageRegistryVersions[0]
	if got, want := version.UID, packageRegistryVersionID(); got != want {
		t.Fatalf("version UID = %q, want %q", got, want)
	}
	if got, want := version.PackageID, packageRegistryPackageID(); got != want {
		t.Fatalf("version PackageID = %q, want %q", got, want)
	}
	if got, want := version.Version, "1.2.3"; got != want {
		t.Fatalf("version Version = %q, want %q", got, want)
	}
	if !version.PublishedAt.Equal(packageRegistryPublishedAt()) {
		t.Fatalf("version PublishedAt = %s, want %s", version.PublishedAt, packageRegistryPublishedAt())
	}
}

func TestBuildCanonicalMaterializationExtractsPackageRegistryDependencies(t *testing.T) {
	t.Parallel()

	result := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), packageRegistryDependencyFact()),
	)

	if got, want := len(result.PackageRegistryDependencies), 1; got != want {
		t.Fatalf("len(PackageRegistryDependencies) = %d, want %d", got, want)
	}
	dependency := result.PackageRegistryDependencies[0]
	if got, want := dependency.UID, "package-registry-dependency-1"; got != want {
		t.Fatalf("dependency UID = %q, want %q", got, want)
	}
	if got, want := dependency.VersionID, packageRegistryVersionID(); got != want {
		t.Fatalf("dependency VersionID = %q, want %q", got, want)
	}
	if got, want := dependency.DependencyPackageID, "package://npm/registry.npmjs.org/left-pad"; got != want {
		t.Fatalf("dependency DependencyPackageID = %q, want %q", got, want)
	}
	if got, want := dependency.DependencyType, "runtime"; got != want {
		t.Fatalf("dependency DependencyType = %q, want %q", got, want)
	}
	if !dependency.Optional {
		t.Fatal("dependency Optional = false, want true")
	}
}

func TestBuildCanonicalMaterializationSkipsUnstablePackageRegistryDependency(t *testing.T) {
	t.Parallel()

	dependencyFact := packageRegistryDependencyFact()
	dependencyFact.StableFactKey = ""
	dependencyFact.FactID = "ephemeral-package-registry-dependency-1"
	result := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), dependencyFact),
	)

	if got := len(result.PackageRegistryDependencies); got != 0 {
		t.Fatalf("len(PackageRegistryDependencies) = %d, want 0 for missing stable fact key", got)
	}
}

func TestBuildCanonicalMaterializationKeepsPackageSourceHintsProvenanceOnly(t *testing.T) {
	t.Parallel()

	result := buildCanonicalMaterialization(
		packageRegistryScope(),
		packageRegistryGeneration(),
		append(packageRegistryFacts(), packageRegistrySourceHintFact()),
	)

	if got, want := len(result.PackageRegistryPackages), 1; got != want {
		t.Fatalf("len(PackageRegistryPackages) = %d, want %d", got, want)
	}
	if got, want := len(result.PackageRegistryVersions), 1; got != want {
		t.Fatalf("len(PackageRegistryVersions) = %d, want %d", got, want)
	}
	if result.Repository != nil {
		t.Fatalf("Repository = %#v, want nil because source hints are not ownership truth", result.Repository)
	}
}

func TestRuntimeProjectRejectsUnknownPackageRegistrySchemaVersion(t *testing.T) {
	t.Parallel()

	runtime := Runtime{
		CanonicalWriter: &recordingCanonicalWriter{},
		ContentWriter:   &recordingContentWriter{},
	}

	_, err := runtime.Project(
		context.Background(),
		packageRegistryScope(),
		packageRegistryGeneration(),
		[]facts.Envelope{{
			FactID:        "package-registry-package-1",
			ScopeID:       "package-registry-scope-1",
			GenerationID:  "package-registry-generation-1",
			FactKind:      facts.PackageRegistryPackageFactKind,
			SchemaVersion: "2.0.0",
			Payload: map[string]any{
				"package_id": packageRegistryPackageID(),
			},
		}},
	)
	if err == nil {
		t.Fatal("Project() error = nil, want non-nil")
	}
}

func packageRegistryScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "package-registry-scope-1",
		SourceSystem:  "package_registry",
		ScopeKind:     scope.KindPackageRegistry,
		CollectorKind: scope.CollectorPackageRegistry,
		PartitionKey:  packageRegistryPackageID(),
	}
}

func packageRegistryGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "package-registry-generation-1",
		ScopeID:      "package-registry-scope-1",
		ObservedAt:   time.Date(2026, time.May, 13, 14, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.May, 13, 14, 1, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func packageRegistryFacts() []facts.Envelope {
	observedAt := time.Date(2026, time.May, 13, 14, 0, 0, 0, time.UTC)
	return []facts.Envelope{
		{
			FactID:           "package-registry-package-1",
			ScopeID:          "package-registry-scope-1",
			GenerationID:     "package-registry-generation-1",
			FactKind:         facts.PackageRegistryPackageFactKind,
			StableFactKey:    packageRegistryPackageID(),
			SchemaVersion:    facts.PackageRegistryPackageSchemaVersion,
			CollectorKind:    "package_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"collector_instance_id": "package-registry-collector-1",
				"ecosystem":             "npm",
				"registry":              "https://registry.npmjs.org",
				"raw_name":              "@scope/pkg",
				"normalized_name":       "@scope/pkg",
				"namespace":             "scope",
				"classifier":            "library",
				"package_id":            packageRegistryPackageID(),
				"visibility":            "public",
				"correlation_anchors": []any{
					packageRegistryPackageID(),
				},
			},
			SourceRef: facts.Ref{
				SourceSystem:   "package_registry",
				ScopeID:        "package-registry-scope-1",
				GenerationID:   "package-registry-generation-1",
				SourceRecordID: packageRegistryPackageID(),
			},
		},
		{
			FactID:           "package-registry-version-1",
			ScopeID:          "package-registry-scope-1",
			GenerationID:     "package-registry-generation-1",
			FactKind:         facts.PackageRegistryPackageVersionFactKind,
			StableFactKey:    packageRegistryVersionID(),
			SchemaVersion:    facts.PackageRegistryPackageVersionSchemaVersion,
			CollectorKind:    "package_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"collector_instance_id": "package-registry-collector-1",
				"ecosystem":             "npm",
				"registry":              "https://registry.npmjs.org",
				"package_id":            packageRegistryPackageID(),
				"version_id":            packageRegistryVersionID(),
				"version":               "1.2.3",
				"published_at":          packageRegistryPublishedAt().Format(time.RFC3339),
				"is_yanked":             false,
				"is_unlisted":           false,
				"is_deprecated":         false,
				"is_retracted":          false,
				"artifact_urls": []any{
					"https://registry.npmjs.org/@scope/pkg/-/pkg-1.2.3.tgz",
				},
				"checksums": map[string]any{
					"sha512": "sha512-test",
				},
				"correlation_anchors": []any{
					packageRegistryPackageID(),
					packageRegistryVersionID(),
				},
			},
			SourceRef: facts.Ref{
				SourceSystem:   "package_registry",
				ScopeID:        "package-registry-scope-1",
				GenerationID:   "package-registry-generation-1",
				SourceRecordID: packageRegistryVersionID(),
			},
		},
	}
}

func packageRegistrySourceHintFact() facts.Envelope {
	return facts.Envelope{
		FactID:           "package-registry-source-hint-1",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistrySourceHintFactKind,
		StableFactKey:    "source-hint-1",
		SchemaVersion:    facts.PackageRegistrySourceHintSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 13, 14, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"collector_instance_id": "package-registry-collector-1",
			"ecosystem":             "npm",
			"registry":              "https://registry.npmjs.org",
			"package_id":            packageRegistryPackageID(),
			"version_id":            packageRegistryVersionID(),
			"version":               "1.2.3",
			"hint_kind":             "repository",
			"raw_url":               "https://github.com/example/pkg",
			"normalized_url":        "https://github.com/example/pkg",
			"confidence_reason":     "package metadata repository field",
		},
	}
}

func packageRegistryDependencyFact() facts.Envelope {
	return facts.Envelope{
		FactID:           "package-registry-dependency-1",
		ScopeID:          "package-registry-scope-1",
		GenerationID:     "package-registry-generation-1",
		FactKind:         facts.PackageRegistryPackageDependencyFactKind,
		StableFactKey:    "package-registry-dependency-1",
		SchemaVersion:    facts.PackageRegistryPackageDependencySchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 13, 14, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"collector_instance_id": "package-registry-collector-1",
			"ecosystem":             "npm",
			"registry":              "https://registry.npmjs.org",
			"package_id":            packageRegistryPackageID(),
			"version_id":            packageRegistryVersionID(),
			"version":               "1.2.3",
			"dependency_package_id": "package://npm/registry.npmjs.org/left-pad",
			"dependency_ecosystem":  "npm",
			"dependency_registry":   "https://registry.npmjs.org",
			"dependency_namespace":  "",
			"dependency_normalized": "left-pad",
			"dependency_range":      "^1.3.0",
			"dependency_type":       "runtime",
			"target_framework":      "node18",
			"marker":                "optional peer fallback",
			"optional":              true,
			"excluded":              false,
			"correlation_anchors": []any{
				packageRegistryPackageID(),
				packageRegistryVersionID(),
				"package://npm/registry.npmjs.org/left-pad",
			},
		},
		SourceRef: facts.Ref{
			SourceSystem:   "package_registry",
			ScopeID:        "package-registry-scope-1",
			GenerationID:   "package-registry-generation-1",
			SourceRecordID: packageRegistryVersionID() + "->package://npm/registry.npmjs.org/left-pad",
		},
	}
}

func packageRegistryPackageID() string {
	return "package://npm/registry.npmjs.org/@scope/pkg"
}

func packageRegistryVersionID() string {
	return packageRegistryPackageID() + "@1.2.3"
}

func packageRegistryPublishedAt() time.Time {
	return time.Date(2026, time.May, 13, 13, 0, 0, 0, time.UTC)
}

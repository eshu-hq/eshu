package cypher

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterBuildsPackageRegistryStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 2, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{{
			UID:                 "package://npm/registry.npmjs.org/@scope/pkg",
			Ecosystem:           "npm",
			Registry:            "https://registry.npmjs.org",
			RawName:             "@scope/pkg",
			NormalizedName:      "@scope/pkg",
			Namespace:           "scope",
			Classifier:          "library",
			Visibility:          "public",
			SourceFactID:        "package-registry-package-1",
			StableFactKey:       "package://npm/registry.npmjs.org/@scope/pkg",
			SourceSystem:        "package_registry",
			SourceRecordID:      "package://npm/registry.npmjs.org/@scope/pkg",
			SourceConfidence:    facts.SourceConfidenceReported,
			CollectorKind:       "package_registry",
			CorrelationAnchors:  []string{"package://npm/registry.npmjs.org/@scope/pkg"},
			CollectorInstanceID: "package-registry-collector-1",
		}},
		PackageRegistryVersions: []projector.PackageRegistryVersionRow{{
			UID:                 "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			PackageID:           "package://npm/registry.npmjs.org/@scope/pkg",
			Ecosystem:           "npm",
			Registry:            "https://registry.npmjs.org",
			Version:             "1.2.3",
			PublishedAt:         time.Date(2026, time.May, 13, 13, 0, 0, 0, time.UTC),
			ArtifactURLs:        []string{"https://registry.npmjs.org/@scope/pkg/-/pkg-1.2.3.tgz"},
			Checksums:           map[string]string{"sha512": "sha512-test"},
			SourceFactID:        "package-registry-version-1",
			StableFactKey:       "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			SourceSystem:        "package_registry",
			SourceRecordID:      "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			SourceConfidence:    facts.SourceConfidenceReported,
			CollectorKind:       "package_registry",
			CorrelationAnchors:  []string{"package://npm/registry.npmjs.org/@scope/pkg"},
			CollectorInstanceID: "package-registry-collector-1",
		}},
		PackageRegistryDependencies: []projector.PackageRegistryDependencyRow{{
			UID:                  "package-registry-dependency-1",
			PackageID:            "package://npm/registry.npmjs.org/@scope/pkg",
			VersionID:            "package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
			Version:              "1.2.3",
			DependencyPackageID:  "package://npm/registry.npmjs.org/left-pad",
			DependencyEcosystem:  "npm",
			DependencyRegistry:   "https://registry.npmjs.org",
			DependencyNormalized: "left-pad",
			DependencyRange:      "^1.3.0",
			DependencyType:       "runtime",
			TargetFramework:      "node18",
			Marker:               "optional peer fallback",
			Optional:             true,
			SourceFactID:         "package-registry-dependency-1",
			StableFactKey:        "package-registry-dependency-1",
			SourceSystem:         "package_registry",
			SourceRecordID:       "package://npm/registry.npmjs.org/@scope/pkg@1.2.3->package://npm/registry.npmjs.org/left-pad",
			SourceConfidence:     facts.SourceConfidenceReported,
			CollectorKind:        "package_registry",
			CorrelationAnchors: []string{
				"package://npm/registry.npmjs.org/@scope/pkg@1.2.3",
				"package://npm/registry.npmjs.org/left-pad",
			},
			CollectorInstanceID: "package-registry-collector-1",
		}},
	}

	statements := writer.buildPackageRegistryStatements(mat)
	if got, want := len(statements), 3; got != want {
		t.Fatalf("buildPackageRegistryStatements() count = %d, want %d", got, want)
	}

	pkg := statements[0]
	if !strings.Contains(pkg.Cypher, "MERGE (p:Package:PackageRegistryPackage {uid: row.uid})") {
		t.Fatalf("package Cypher = %q, want Package uid merge", pkg.Cypher)
	}
	if strings.Contains(pkg.Cypher, "Repository") {
		t.Fatalf("package Cypher = %q, must not infer repository ownership", pkg.Cypher)
	}
	if got, want := pkg.Parameters[StatementMetadataPhaseKey], canonicalPhasePackageRegistryPackages; got != want {
		t.Fatalf("package phase = %#v, want %#v", got, want)
	}

	version := statements[1]
	if !strings.Contains(version.Cypher, "MATCH (p:Package {uid: row.package_id})") {
		t.Fatalf("version Cypher = %q, want package uid anchor", version.Cypher)
	}
	if !strings.Contains(version.Cypher, "MERGE (v:PackageVersion:PackageRegistryPackageVersion {uid: row.uid})") {
		t.Fatalf("version Cypher = %q, want PackageVersion uid merge", version.Cypher)
	}
	if !strings.Contains(version.Cypher, "MERGE (p)-[rel:HAS_VERSION]->(v)") {
		t.Fatalf("version Cypher = %q, want package version edge", version.Cypher)
	}
	if strings.Contains(version.Cypher, "SourceHint") || strings.Contains(version.Cypher, "Repository") {
		t.Fatalf("version Cypher = %q, must not promote source hints to ownership", version.Cypher)
	}

	dependency := statements[2]
	for _, fragment := range []string{
		"ON CREATE SET target.id = row.dependency_package_id",
		"target.scope_id = row.scope_id",
		"target.generation_id = row.generation_id",
		"target.evidence_source = 'projector/package_registry'",
		"MERGE (d:PackageDependency:PackageRegistryPackageDependency {uid: row.uid})",
		"MERGE (v)-[declares:DECLARES_DEPENDENCY]->(d)",
		"MERGE (d)-[depends:DEPENDS_ON_PACKAGE]->(target)",
		"dependency_type = row.dependency_type",
		"target_framework = row.target_framework",
		"marker = row.marker",
	} {
		if !strings.Contains(dependency.Cypher, fragment) {
			t.Fatalf("dependency Cypher = %q, want fragment %q", dependency.Cypher, fragment)
		}
	}
	if strings.Contains(dependency.Cypher, "\nSET target.") {
		t.Fatalf("dependency Cypher = %q, must not overwrite observed target package properties", dependency.Cypher)
	}
	if strings.Contains(dependency.Cypher, "Repository") {
		t.Fatalf("dependency Cypher = %q, must not infer repository ownership", dependency.Cypher)
	}
}

func TestCanonicalNodeWriterSeparatesPackageRegistryPhaseGroups(t *testing.T) {
	t.Parallel()

	exec := &mockPhaseGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{{
			UID:              "npm://registry.npmjs.org/lodash",
			Ecosystem:        "npm",
			Registry:         "registry.npmjs.org",
			NormalizedName:   "lodash",
			SourceFactID:     "package-registry-package-1",
			StableFactKey:    "npm://registry.npmjs.org/lodash",
			SourceSystem:     "package_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			CollectorKind:    "package_registry",
		}},
		PackageRegistryVersions: []projector.PackageRegistryVersionRow{{
			UID:              "npm://registry.npmjs.org/lodash@1.0.0",
			PackageID:        "npm://registry.npmjs.org/lodash",
			Ecosystem:        "npm",
			Registry:         "registry.npmjs.org",
			Version:          "1.0.0",
			SourceFactID:     "package-registry-version-1",
			StableFactKey:    "npm://registry.npmjs.org/lodash@1.0.0",
			SourceSystem:     "package_registry",
			SourceConfidence: facts.SourceConfidenceReported,
			CollectorKind:    "package_registry",
		}},
		PackageRegistryDependencies: []projector.PackageRegistryDependencyRow{{
			UID:                  "package-registry-dependency-1",
			PackageID:            "npm://registry.npmjs.org/lodash",
			VersionID:            "npm://registry.npmjs.org/lodash@1.0.0",
			Version:              "1.0.0",
			DependencyPackageID:  "npm://registry.npmjs.org/left-pad",
			DependencyEcosystem:  "npm",
			DependencyRegistry:   "registry.npmjs.org",
			DependencyNormalized: "left-pad",
			SourceFactID:         "package-registry-dependency-1",
			StableFactKey:        "package-registry-dependency-1",
			SourceSystem:         "package_registry",
			SourceConfidence:     facts.SourceConfidenceReported,
			CollectorKind:        "package_registry",
		}},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	packageGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryPackage")
	versionGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryPackageVersion")
	dependencyGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryPackageDependency")
	if packageGroup >= versionGroup || versionGroup >= dependencyGroup {
		t.Fatalf(
			"package registry phase groups = package:%d version:%d dependency:%d, want package before version before dependency",
			packageGroup,
			versionGroup,
			dependencyGroup,
		)
	}
}

func packageRegistryPhaseGroupIndex(t *testing.T, groups [][]Statement, label string) int {
	t.Helper()

	for groupIndex, group := range groups {
		for _, stmt := range group {
			if got, _ := stmt.Parameters[StatementMetadataEntityLabelKey].(string); got == label {
				return groupIndex
			}
		}
	}
	t.Fatalf("missing package registry phase group for %s", label)
	return -1
}

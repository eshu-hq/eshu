// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
			PURL:                "pkg:npm/%40scope/pkg",
			BOMRef:              "pkg:npm/%40scope/pkg",
			PackageManager:      "npm",
			SourcePath:          "package.json",
			SourceSpecificID:    "npm:@scope/pkg",
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
			PURL:                "pkg:npm/%40scope/pkg@1.2.3",
			BOMRef:              "pkg:npm/%40scope/pkg@1.2.3",
			PackageManager:      "npm",
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
			DependencyPURL:       "pkg:npm/left-pad",
			DependencyBOMRef:     "pkg:npm/left-pad",
			DependencyManager:    "npm",
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
	if got, want := len(statements), 4; got != want {
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
	packageRows := pkg.Parameters["rows"].([]map[string]any)
	if got, want := packageRows[0]["purl"], "pkg:npm/%40scope/pkg"; got != want {
		t.Fatalf("package row purl = %#v, want %#v", got, want)
	}
	for _, fragment := range []string{
		"p.purl = row.purl",
		"p.bom_ref = row.bom_ref",
		"p.package_manager = row.package_manager",
		"p.source_path = row.source_path",
		"p.source_specific_id = row.source_specific_id",
	} {
		if !strings.Contains(pkg.Cypher, fragment) {
			t.Fatalf("package Cypher = %q, want fragment %q", pkg.Cypher, fragment)
		}
	}

	version := statements[1]
	// The version NODE statement creates the multi-label PackageVersion node only.
	// It must NOT MATCH the Package node nor MERGE the HAS_VERSION edge in the same
	// statement: NornicDB does not make the multi-label Package visible to a
	// same-transaction UNWIND-driven MATCH, so the edge is deferred to a second group.
	if strings.Contains(version.Cypher, "MATCH (p:Package {uid: row.package_id})") {
		t.Fatalf("version NODE Cypher = %q, must not anchor on Package (deferred to edge phase)", version.Cypher)
	}
	if strings.Contains(version.Cypher, "HAS_VERSION") {
		t.Fatalf("version NODE Cypher = %q, must not MERGE HAS_VERSION (deferred to edge phase)", version.Cypher)
	}
	if !strings.Contains(version.Cypher, "MERGE (v:PackageVersion:PackageRegistryPackageVersion {uid: row.uid})") {
		t.Fatalf("version Cypher = %q, want PackageVersion uid merge", version.Cypher)
	}
	if strings.Contains(version.Cypher, "SourceHint") || strings.Contains(version.Cypher, "Repository") {
		t.Fatalf("version Cypher = %q, must not promote source hints to ownership", version.Cypher)
	}
	versionRows := version.Parameters["rows"].([]map[string]any)
	if got, want := versionRows[0]["purl"], "pkg:npm/%40scope/pkg@1.2.3"; got != want {
		t.Fatalf("version row purl = %#v, want %#v", got, want)
	}
	for _, fragment := range []string{
		"v.purl = row.purl",
		"v.bom_ref = row.bom_ref",
		"v.package_manager = row.package_manager",
	} {
		if !strings.Contains(version.Cypher, fragment) {
			t.Fatalf("version Cypher = %q, want fragment %q", version.Cypher, fragment)
		}
	}

	dependencyTargets := statements[2]
	if !strings.Contains(dependencyTargets.Cypher, "MERGE (target:Package:PackageRegistryPackage {uid: row.dependency_package_id})") {
		t.Fatalf("dependency target Cypher = %q, want target Package uid merge", dependencyTargets.Cypher)
	}
	if strings.Contains(dependencyTargets.Cypher, "\nSET target.") {
		t.Fatalf("dependency target Cypher = %q, must not overwrite observed target package properties", dependencyTargets.Cypher)
	}
	targetRows := dependencyTargets.Parameters["rows"].([]map[string]any)
	if got, want := targetRows[0]["dependency_purl"], "pkg:npm/left-pad"; got != want {
		t.Fatalf("dependency target row purl = %#v, want %#v", got, want)
	}

	dependency := statements[3]
	// The dependency NODE statement creates the multi-label PackageDependency node
	// only. The leading MATCHes and both edge MERGEs are deferred to the edge phase.
	for _, fragment := range []string{
		"MERGE (d:PackageDependency:PackageRegistryPackageDependency {uid: row.uid})",
		"dependency_type = row.dependency_type",
		"dependency_purl = row.dependency_purl",
		"dependency_bom_ref = row.dependency_bom_ref",
		"dependency_manager = row.dependency_manager",
		"target_framework = row.target_framework",
		"marker = row.marker",
	} {
		if !strings.Contains(dependency.Cypher, fragment) {
			t.Fatalf("dependency NODE Cypher = %q, want fragment %q", dependency.Cypher, fragment)
		}
	}
	for _, forbidden := range []string{
		"MATCH (",
		"DECLARES_DEPENDENCY",
		"DEPENDS_ON_PACKAGE",
	} {
		if strings.Contains(dependency.Cypher, forbidden) {
			t.Fatalf("dependency NODE Cypher = %q, must not contain %q (deferred to edge phase)", dependency.Cypher, forbidden)
		}
	}
	if strings.Contains(dependency.Cypher, "Repository") {
		t.Fatalf("dependency Cypher = %q, must not infer repository ownership", dependency.Cypher)
	}

	// Edge cypher: the deferred version edge statement MUST anchor on both nodes and
	// MERGE the HAS_VERSION edge.
	versionEdges := writer.buildPackageRegistryVersionEdgeStatements(mat)
	if got, want := len(versionEdges), 1; got != want {
		t.Fatalf("buildPackageRegistryVersionEdgeStatements() count = %d, want %d", got, want)
	}
	for _, fragment := range []string{
		"MATCH (p:Package {uid: row.package_id})",
		"MATCH (v:PackageVersion {uid: row.uid})",
		"MERGE (p)-[rel:HAS_VERSION]->(v)",
	} {
		if !strings.Contains(versionEdges[0].Cypher, fragment) {
			t.Fatalf("version EDGE Cypher = %q, want fragment %q", versionEdges[0].Cypher, fragment)
		}
	}
	if got, want := versionEdges[0].Parameters[StatementMetadataPhaseKey], canonicalPhasePackageRegistryVersionEdges; got != want {
		t.Fatalf("version edge phase = %#v, want %#v", got, want)
	}

	// Edge cypher: the deferred dependency edge statement MUST MATCH all three nodes
	// and MERGE both DECLARES_DEPENDENCY and DEPENDS_ON_PACKAGE.
	dependencyEdges := writer.buildPackageRegistryDependencyEdgeStatements(mat)
	if got, want := len(dependencyEdges), 1; got != want {
		t.Fatalf("buildPackageRegistryDependencyEdgeStatements() count = %d, want %d", got, want)
	}
	for _, fragment := range []string{
		"MATCH (v:PackageVersion {uid: row.version_id})",
		"MATCH (target:Package {uid: row.dependency_package_id})",
		"MATCH (d:PackageDependency {uid: row.uid})",
		"MERGE (v)-[declares:DECLARES_DEPENDENCY]->(d)",
		"MERGE (d)-[depends:DEPENDS_ON_PACKAGE]->(target)",
	} {
		if !strings.Contains(dependencyEdges[0].Cypher, fragment) {
			t.Fatalf("dependency EDGE Cypher = %q, want fragment %q", dependencyEdges[0].Cypher, fragment)
		}
	}
	if got, want := dependencyEdges[0].Parameters[StatementMetadataPhaseKey], canonicalPhasePackageRegistryDependencyEdges; got != want {
		t.Fatalf("dependency edge phase = %#v, want %#v", got, want)
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
	dependencyPackageGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryDependencyPackage")
	dependencyGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryPackageDependency")
	versionEdgeGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryVersionEdge")
	dependencyEdgeGroup := packageRegistryPhaseGroupIndex(t, exec.phaseGroups, "PackageRegistryDependencyEdge")

	// Edge phases must run AFTER every package_registry node phase on the
	// PhaseGroupExecutor (and sequential) path too: the edges MATCH the
	// multi-label nodes the node phases create, so they must commit last. This
	// guards the non-atomic paths, not just the atomic two-group split.
	nodeGroups := []int{packageGroup, versionGroup, dependencyPackageGroup, dependencyGroup}
	for _, nodeGroup := range nodeGroups {
		if versionEdgeGroup <= nodeGroup || dependencyEdgeGroup <= nodeGroup {
			t.Fatalf(
				"package registry edge phases must run after all node phases: node groups=%v version_edge=%d dependency_edge=%d",
				nodeGroups,
				versionEdgeGroup,
				dependencyEdgeGroup,
			)
		}
	}

	if packageGroup >= versionGroup || versionGroup >= dependencyPackageGroup || dependencyPackageGroup >= dependencyGroup {
		t.Fatalf(
			"package registry phase groups = package:%d version:%d dependency_package:%d dependency:%d, want package before version before dependency targets before dependency",
			packageGroup,
			versionGroup,
			dependencyPackageGroup,
			dependencyGroup,
		)
	}
}

func TestCanonicalNodeWriterDeduplicatesPackageRegistryDependencyTargets(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	statements := writer.buildPackageRegistryDependencyPackageStatements(projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryDependencies: []projector.PackageRegistryDependencyRow{
			{
				UID:                  "dependency-1",
				DependencyPackageID:  "npm://registry.npmjs.org/graphql16",
				DependencyEcosystem:  "npm",
				DependencyRegistry:   "https://registry.npmjs.org",
				DependencyNormalized: "graphql",
				SourceFactID:         "fact-1",
				StableFactKey:        "fact-1",
				SourceSystem:         "package_registry",
				SourceConfidence:     facts.SourceConfidenceReported,
				CollectorKind:        "package_registry",
			},
			{
				UID:                  "dependency-2",
				DependencyPackageID:  "npm://registry.npmjs.org/graphql16",
				DependencyEcosystem:  "npm",
				DependencyRegistry:   "https://registry.npmjs.org",
				DependencyNormalized: "graphql",
				SourceFactID:         "fact-2",
				StableFactKey:        "fact-2",
				SourceSystem:         "package_registry",
				SourceConfidence:     facts.SourceConfidenceReported,
				CollectorKind:        "package_registry",
			},
		},
	})
	if got, want := len(statements), 1; got != want {
		t.Fatalf("buildPackageRegistryDependencyPackageStatements() count = %d, want %d", got, want)
	}
	rows, ok := statements[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", statements[0].Parameters["rows"])
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("dependency target rows = %d, want %d", got, want)
	}
	if got, want := rows[0]["dependency_package_id"], "npm://registry.npmjs.org/graphql16"; got != want {
		t.Fatalf("dependency target uid = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterSkipsDependencyTargetsCoveredByPackageRows(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	statements := writer.buildPackageRegistryDependencyPackageStatements(projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{
			{
				UID:              "npm://registry.npmjs.org/eslint-plugin-es-x",
				Ecosystem:        "npm",
				Registry:         "https://registry.npmjs.org",
				RawName:          "eslint-plugin-es-x",
				NormalizedName:   "eslint-plugin-es-x",
				SourceFactID:     "package-registry-package-1",
				StableFactKey:    "package-registry-package-1",
				SourceSystem:     "package_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				CollectorKind:    "package_registry",
			},
		},
		PackageRegistryDependencies: []projector.PackageRegistryDependencyRow{
			{
				UID:                  "dependency-1",
				DependencyPackageID:  "npm://registry.npmjs.org/eslint-plugin-es-x",
				DependencyEcosystem:  "npm",
				DependencyRegistry:   "https://registry.npmjs.org",
				DependencyNormalized: "eslint-plugin-es-x",
				SourceFactID:         "package-registry-dependency-1",
				StableFactKey:        "package-registry-dependency-1",
				SourceSystem:         "package_registry",
				SourceConfidence:     facts.SourceConfidenceReported,
				CollectorKind:        "package_registry",
			},
		},
	})
	if got := len(statements); got != 0 {
		t.Fatalf("dependency target statements = %d, want 0 because package row already upserts the UID", got)
	}
}

func TestCanonicalNodeWriterDeduplicatesPackageRegistryPackages(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	statements := writer.buildPackageRegistryPackageStatements(projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{
			{
				UID:              "npm://registry.npmjs.org/graphql",
				Ecosystem:        "npm",
				Registry:         "https://registry.npmjs.org",
				RawName:          "graphql-old",
				NormalizedName:   "graphql",
				SourceFactID:     "package-registry-package-1",
				StableFactKey:    "package-registry-package-1",
				SourceSystem:     "package_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				CollectorKind:    "package_registry",
				ObservedAt:       time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
			},
			{
				UID:              "npm://registry.npmjs.org/graphql",
				Ecosystem:        "npm",
				Registry:         "https://registry.npmjs.org",
				RawName:          "graphql-new",
				NormalizedName:   "graphql",
				SourceFactID:     "package-registry-package-2",
				StableFactKey:    "package-registry-package-2",
				SourceSystem:     "package_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				CollectorKind:    "package_registry",
				ObservedAt:       time.Date(2026, time.June, 1, 12, 5, 0, 0, time.UTC),
			},
		},
	})
	if got, want := len(statements), 1; got != want {
		t.Fatalf("buildPackageRegistryPackageStatements() count = %d, want %d", got, want)
	}
	rows, ok := statements[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", statements[0].Parameters["rows"])
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("package rows = %d, want %d", got, want)
	}
	if got, want := rows[0]["uid"], "npm://registry.npmjs.org/graphql"; got != want {
		t.Fatalf("package uid = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["raw_name"], "graphql-new"; got != want {
		t.Fatalf("package raw_name = %#v, want newest duplicate row %#v", got, want)
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

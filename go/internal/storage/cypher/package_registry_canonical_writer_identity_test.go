// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// This file holds the package_registry canonical writer's identity/dedup
// tests and the packageRegistryPhaseGroupIndex helper, split out of
// package_registry_canonical_writer_test.go to stay under the package's
// 500-line-per-file convention. package_registry_canonical_writer_test.go
// keeps the statement-shape and phase-ordering tests
// (TestCanonicalNodeWriterBuildsPackageRegistryStatements,
// TestCanonicalNodeWriterSeparatesPackageRegistryPhaseGroups) and calls
// packageRegistryPhaseGroupIndex, defined here, since both files are part of
// the same cypher package.

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

// TestCanonicalNodeWriterArtifactEdgeCypherPinsPackageIDOnVersionMatch is the
// shaped regression for the #5820 P2 review finding: a malformed-but-schema-
// valid artifact fact whose package_id names package A while its version_id
// resolves to a PackageVersion node genuinely owned by package B must not
// attach that artifact to B's version. Anchoring the deferred HAS_ARTIFACT
// edge's PackageVersion MATCH on uid alone would still find B's version and
// MERGE an edge to an artifact node that itself claims package_id A --
// internally contradictory package graph truth. Pinning package_id:
// row.package_id alongside uid: row.version_id on the same MATCH means a
// mismatched pair finds no PackageVersion row, so no edge is written. This
// also proves the edge row's package_id parameter is the artifact's OWN
// claimed package_id (packageRegistryArtifactRows), not derived from
// version_id, which is what makes the predicate meaningful rather than
// tautological.
func TestCanonicalNodeWriterArtifactEdgeCypherPinsPackageIDOnVersionMatch(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryArtifacts: []projector.PackageRegistryArtifactRow{{
			UID:           "artifact-1",
			PackageID:     "package-a",
			VersionID:     "version-owned-by-b",
			ArtifactKey:   "pkg-1.0.0.tgz",
			StableFactKey: "artifact-1",
		}},
	}

	edges := writer.buildPackageRegistryArtifactEdgeStatements(mat)
	if got, want := len(edges), 1; got != want {
		t.Fatalf("buildPackageRegistryArtifactEdgeStatements() count = %d, want %d", got, want)
	}
	if !strings.Contains(edges[0].Cypher, "MATCH (v:PackageVersion {uid: row.version_id, package_id: row.package_id})") {
		t.Fatalf("artifact edge Cypher = %q, want the PackageVersion MATCH to pin package_id alongside uid so a cross-package artifact/version pair cannot bind", edges[0].Cypher)
	}
	rows, ok := edges[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows parameter = %#v, want one row", edges[0].Parameters["rows"])
	}
	if got, want := rows[0]["package_id"], "package-a"; got != want {
		t.Fatalf("edge row package_id = %#v, want %#v (the artifact's OWN claimed package_id, not derived from version_id)", got, want)
	}
	if got, want := rows[0]["version_id"], "version-owned-by-b"; got != want {
		t.Fatalf("edge row version_id = %#v, want %#v", got, want)
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

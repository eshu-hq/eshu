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

// capturingGroupExecutor records every ExecuteGroup invocation's statements so a
// test can assert how the atomic write path partitions node phases from the
// deferred package_registry edge phases.
type capturingGroupExecutor struct {
	groups [][]Statement
}

func (e *capturingGroupExecutor) Execute(context.Context, Statement) error { return nil }

func (e *capturingGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	e.groups = append(e.groups, append([]Statement(nil), stmts...))
	return nil
}

func packageRegistryEdgeFixture() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
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
	}
}

// TestCanonicalNodeWriterDefersPackageRegistryEdgesToSecondGroup proves the
// atomic GroupExecutor path runs node MERGEs in the first transaction and the
// HAS_VERSION / DEPENDS_ON_PACKAGE / DECLARES_DEPENDENCY edge MERGEs in a
// deferred second transaction, so the edge MATCHes resolve against committed,
// per-label-indexed nodes on NornicDB.
func TestCanonicalNodeWriterDefersPackageRegistryEdgesToSecondGroup(t *testing.T) {
	t.Parallel()

	exec := &capturingGroupExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	if err := writer.Write(context.Background(), packageRegistryEdgeFixture()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got, want := len(exec.groups), 2; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d (node group + deferred edge group)", got, want)
	}

	firstGroup := groupCypher(exec.groups[0])
	secondGroup := groupCypher(exec.groups[1])

	// Node MERGEs belong to the first group.
	for _, fragment := range []string{
		"MERGE (p:Package:PackageRegistryPackage {uid: row.uid})",
		"MERGE (v:PackageVersion:PackageRegistryPackageVersion {uid: row.uid})",
		"MERGE (d:PackageDependency:PackageRegistryPackageDependency {uid: row.uid})",
	} {
		if !strings.Contains(firstGroup, fragment) {
			t.Fatalf("first group missing node fragment %q\nfirst group:\n%s", fragment, firstGroup)
		}
	}

	// Edge MERGEs MUST NOT appear in the first group.
	for _, forbidden := range []string{"HAS_VERSION", "DEPENDS_ON_PACKAGE", "DECLARES_DEPENDENCY"} {
		if strings.Contains(firstGroup, forbidden) {
			t.Fatalf("first group must not contain edge %q\nfirst group:\n%s", forbidden, firstGroup)
		}
	}

	// Edge MERGEs MUST appear in the deferred second group.
	for _, fragment := range []string{
		"MERGE (p)-[rel:HAS_VERSION]->(v)",
		"MERGE (v)-[declares:DECLARES_DEPENDENCY]->(d)",
		"MERGE (d)-[depends:DEPENDS_ON_PACKAGE]->(target)",
	} {
		if !strings.Contains(secondGroup, fragment) {
			t.Fatalf("second group missing edge fragment %q\nsecond group:\n%s", fragment, secondGroup)
		}
	}

	// The second group must contain only the deferred edge phases.
	for _, stmt := range exec.groups[1] {
		phase, _ := stmt.Parameters[StatementMetadataPhaseKey].(string)
		if !isDeferredPackageRegistryEdgePhase(phase) {
			t.Fatalf("second group statement phase = %q, want a deferred package_registry edge phase", phase)
		}
	}
}

func TestCanonicalNodeWriterDeduplicatesPackageRegistryPackagesWithDeterministicTieBreaker(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	statements := writer.buildPackageRegistryPackageStatements(projector.CanonicalMaterialization{
		ScopeID:      "package-registry-scope-1",
		GenerationID: "package-registry-generation-1",
		PackageRegistryPackages: []projector.PackageRegistryPackageRow{
			{
				UID:              "npm://registry.npmjs.org/graphql",
				Ecosystem:        "npm",
				Registry:         "https://registry.npmjs.org",
				RawName:          "graphql-low-fact",
				NormalizedName:   "graphql",
				SourceFactID:     "package-registry-package-1",
				StableFactKey:    "package-registry-package-z",
				SourceSystem:     "package_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				CollectorKind:    "package_registry",
				ObservedAt:       observedAt,
			},
			{
				UID:              "npm://registry.npmjs.org/graphql",
				Ecosystem:        "npm",
				Registry:         "https://registry.npmjs.org",
				RawName:          "graphql-high-fact",
				NormalizedName:   "graphql",
				SourceFactID:     "package-registry-package-2",
				StableFactKey:    "package-registry-package-a",
				SourceSystem:     "package_registry",
				SourceConfidence: facts.SourceConfidenceReported,
				CollectorKind:    "package_registry",
				ObservedAt:       observedAt,
			},
		},
	})
	rows := statements[0].Parameters["rows"].([]map[string]any)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("package rows = %d, want %d", got, want)
	}
	if got, want := rows[0]["raw_name"], "graphql-high-fact"; got != want {
		t.Fatalf("package raw_name = %#v, want source fact tie-break row %#v", got, want)
	}
}

func groupCypher(stmts []Statement) string {
	parts := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		parts = append(parts, stmt.Cypher)
	}
	return strings.Join(parts, "\n---\n")
}

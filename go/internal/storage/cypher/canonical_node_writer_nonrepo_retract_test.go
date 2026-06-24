// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterSkipsRepositoryRetractForNonRepositoryProjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mat  projector.CanonicalMaterialization
	}{
		{
			name: "oci registry",
			mat: projector.CanonicalMaterialization{
				ScopeID:      "scope-oci",
				GenerationID: "gen-2",
				OCIRegistryRepository: &projector.OCIRegistryRepositoryRow{
					UID:        "oci-repo:example",
					Provider:   "ghcr",
					Registry:   "ghcr.io",
					Repository: "example/app",
				},
			},
		},
		{
			name: "package registry",
			mat: projector.CanonicalMaterialization{
				ScopeID:      "scope-package",
				GenerationID: "gen-2",
				PackageRegistryPackages: []projector.PackageRegistryPackageRow{{
					UID:            "package:npm:left-pad",
					Ecosystem:      "npm",
					Registry:       "registry.npmjs.org",
					RawName:        "left-pad",
					NormalizedName: "left-pad",
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
			for _, phase := range writer.buildPhases(tt.mat) {
				switch phase.name {
				case "retract", "entity_retract":
					if len(phase.statements) != 0 {
						t.Fatalf("%s statements = %d, want 0 for non-repository projection", phase.name, len(phase.statements))
					}
				}
				for _, stmt := range phase.statements {
					if strings.Contains(stmt.Cypher, "MATCH (f:File)") ||
						strings.Contains(stmt.Cypher, "MATCH (d:Directory)") ||
						strings.Contains(stmt.Cypher, "WHERE n.repo_id = $repo_id") {
						t.Fatalf("non-repository projection emitted repository-scoped cleanup in phase %s: %s", phase.name, stmt.Cypher)
					}
				}
			}
		})
	}
}

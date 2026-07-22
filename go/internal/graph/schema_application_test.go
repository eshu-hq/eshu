// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"slices"
	"testing"
)

func TestSchemaApplicationsDeclareCompatibilityDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		backend     SchemaBackend
		fingerprint string
		compatible  []string
	}{
		{
			name:        "neo4j",
			backend:     SchemaBackendNeo4j,
			fingerprint: graphSchemaNeo4jFingerprint,
			// The shell_exec and inheritance child lookup-index bumps add only
			// repo_id/path indexes, so their immediately preceding schemas stay
			// compatible too.
			// The Helm template-value schema bump only adds
			// HelmValueDefinition/HelmTemplateValueUsage constraints + uid
			// constraints; an older writer can safely write against it. The
			// additive chain pre-GitLab -> GitLab -> Helm -> SqlMigration ->
			// Flux is cumulative, so every predecessor stays compatible. Both the
			// Flux typed-entity bump (issue #5360 PR A: uid constraints for the
			// four new Flux labels, immediate predecessor = the SqlMigration tip)
			// and the SqlMigration bump (#5346: SqlMigration uid constraint,
			// immediate predecessor = the Helm tip) are additive — an older
			// writer creates no nodes of the new labels, so the new constraints
			// never apply to it. The FluxHelmRelease/FluxHelmRepository bump
			// (issue #5483 C1: uid constraints for two new Flux labels,
			// immediate predecessor = the Flux typed-entity tip) is additive
			// the same way.
			compatible: []string{
				graphSchemaNeo4jPreCodeownersOwnershipFingerprint,
				graphSchemaNeo4jPreFluxHelmEntitiesFingerprint,
				graphSchemaNeo4jPreFluxTypedEntitiesFingerprint,
				graphSchemaNeo4jPreSqlMigrationFingerprint,
				graphSchemaNeo4jPreShellExecRetractIndexesFingerprint,
				graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint,
				graphSchemaNeo4jPreFunctionRetractIndexesFingerprint,
				graphSchemaNeo4jPreHelmTemplateValuesFingerprint,
				graphSchemaNeo4jPreGitlabFingerprint,
			},
		},
		{
			name:        "nornicdb",
			backend:     SchemaBackendNornicDB,
			fingerprint: graphSchemaNornicDBFingerprint,
			compatible: []string{
				graphSchemaNornicDBPreCodeownersOwnershipFingerprint,
				graphSchemaNornicDBPreKubernetesWorkloadIDLookupFingerprint,
				graphSchemaNornicDBPreFluxHelmEntitiesFingerprint,
				graphSchemaNornicDBPreFluxTypedEntitiesFingerprint,
				graphSchemaNornicDBPreSqlMigrationFingerprint,
				graphSchemaNornicDBPreFunctionLegacyIDLookupFingerprint,
				graphSchemaNornicDBPreShellExecRetractIndexesFingerprint,
				graphSchemaNornicDBPreInheritanceRetractIndexesFingerprint,
				graphSchemaNornicDBPreFunctionRetractIndexesFingerprint,
				graphSchemaNornicDBPreHelmTemplateValuesFingerprint,
				graphSchemaNornicDBPreGitlabFingerprint,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app, err := SchemaApplicationForBackend(tt.backend)
			if err != nil {
				t.Fatalf("SchemaApplicationForBackend(%q) error = %v, want nil", tt.backend, err)
			}
			if app.Fingerprint != tt.fingerprint {
				t.Fatalf("Fingerprint = %q, want %q; update the schema compatibility decision before accepting the new schema fingerprint", app.Fingerprint, tt.fingerprint)
			}
			if !slices.Equal(app.CompatibleFingerprints, tt.compatible) {
				t.Fatalf("CompatibleFingerprints = %#v, want %#v", app.CompatibleFingerprints, tt.compatible)
			}
		})
	}
}

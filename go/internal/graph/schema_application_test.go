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
			compatible:  []string{},
		},
		{
			name:        "nornicdb",
			backend:     SchemaBackendNornicDB,
			fingerprint: graphSchemaNornicDBFingerprint,
			compatible:  []string{},
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

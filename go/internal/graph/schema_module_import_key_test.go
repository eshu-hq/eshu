// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"slices"
	"testing"
)

// moduleImportKeyConstraint is the #8168 uniqueness constraint behind the
// canonical Module MERGE. Semantic Module nodes carry no import_key, so the
// constraint never sees them.
const moduleImportKeyConstraint = "CREATE CONSTRAINT module_import_key IF NOT EXISTS FOR (m:Module) REQUIRE m.import_key IS UNIQUE"

// TestModuleImportKeyConstraintLandsOnBothBackends proves both dialects apply
// the single-property constraint. NornicDB drops composite IS UNIQUE forms, so
// a composite key would silently leave NornicDB without one.
func TestModuleImportKeyConstraintLandsOnBothBackends(t *testing.T) {
	t.Parallel()

	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		statements, err := SchemaStatementsForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%q) error = %v", backend, err)
		}
		count := 0
		for _, statement := range statements {
			if statement == moduleImportKeyConstraint {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%s schema applies %q %d times, want exactly once", backend, moduleImportKeyConstraint, count)
		}
	}
}

// TestModuleImportKeyIsAWriteIdentityCutover proves the #8168 identity change
// fences every older writer. A writer on the previous release MERGEs Module on
// (name, lang) and never sets import_key, so after the migration it would
// mint a second node beside each canonical one. It must be refused, so the
// current fingerprint lists no compatible predecessor at all.
func TestModuleImportKeyIsAWriteIdentityCutover(t *testing.T) {
	t.Parallel()

	if !slices.Contains(graphWriteIdentityContract, "Module=import_key") {
		t.Fatalf("graphWriteIdentityContract = %v, want the Module=import_key line", graphWriteIdentityContract)
	}
	if slices.Contains(graphWriteIdentityContract, "Module=name,lang") {
		t.Fatalf("graphWriteIdentityContract = %v still records the retired Module=name,lang identity", graphWriteIdentityContract)
	}
	for backend, previous := range map[SchemaBackend]string{
		SchemaBackendNeo4j:    graphSchemaNeo4jPreModuleImportKeyFingerprint,
		SchemaBackendNornicDB: graphSchemaNornicDBPreModuleImportKeyFingerprint,
	} {
		app, err := SchemaApplicationForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaApplicationForBackend(%q) error = %v", backend, err)
		}
		if app.Fingerprint == previous {
			t.Fatalf("%s fingerprint did not move for the import_key cutover", backend)
		}
		if len(app.CompatibleFingerprints) != 0 {
			t.Fatalf("%s CompatibleFingerprints = %v, want none: every older writer MERGEs Module on (name, lang)",
				backend, app.CompatibleFingerprints)
		}
	}
}

// TestPreModuleImportKeyApplicationsRetainTheirAdditiveChain keeps the record
// of what the schema immediately before #8168 admitted: the additive index
// bumps since the #6102 cutover. The map entry is keyed by that schema's own
// fingerprint, so the current lookup can never reach it.
func TestPreModuleImportKeyApplicationsRetainTheirAdditiveChain(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		backend     SchemaBackend
		fingerprint string
		compatible  []string
	}{
		{
			backend:     SchemaBackendNeo4j,
			fingerprint: graphSchemaNeo4jPreModuleImportKeyFingerprint,
			compatible: []string{
				graphSchemaNeo4jPreInfraSearchKeyIndexesFingerprint,
				graphSchemaNeo4jPreFulltextModernFormFingerprint,
				graphSchemaNeo4jPreRetiredNarrowConstraintsFingerprint,
				graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint,
				graphSchemaNeo4jPreInfraEvidenceSourceIndexFingerprint,
				graphSchemaNeo4jPreDirectoryRepoIDIndexFingerprint,
			},
		},
		{
			backend:     SchemaBackendNornicDB,
			fingerprint: graphSchemaNornicDBPreModuleImportKeyFingerprint,
			compatible: []string{
				graphSchemaNornicDBPreRetiredNarrowConstraintsFingerprint,
				graphSchemaNornicDBPreInfraEvidenceSourceIndexFingerprint,
				graphSchemaNornicDBPreDirectoryRepoIDIndexFingerprint,
			},
		},
	} {
		got := graphSchemaCompatibleFingerprints[tt.backend][tt.fingerprint]
		if !slices.Equal(got, tt.compatible) {
			t.Fatalf("%s pre-import_key compatible list = %v, want %v", tt.backend, got, tt.compatible)
		}
		current := MustSchemaApplicationForBackend(tt.backend)
		if slices.Contains(current.CompatibleFingerprints, tt.fingerprint) {
			t.Fatalf("%s current schema admits the pre-import_key writer %s", tt.backend, tt.fingerprint)
		}
	}
}

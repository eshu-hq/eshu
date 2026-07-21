// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestRelationshipFamilyCandidateIndexMigration(t *testing.T) {
	t.Parallel()

	var migration Definition
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == "relationship_family_candidate_index" {
			migration = definition
			break
		}
	}
	if migration.Name == "" {
		t.Fatal("relationship-family candidate index migration missing")
	}
	if migration.Path != "go/internal/storage/postgres/migrations/059_relationship_family_candidate_index.sql" {
		t.Fatalf("relationship-family migration path = %q", migration.Path)
	}
	normalize := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	wantPredicate := normalize(strings.ReplaceAll(deferredRelationshipFamilyCandidatePredicateSQL, "fact.", ""))
	got := normalize(migration.SQL)
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + relationshipFamilyProofIndexName,
		"ON fact_records (scope_id, generation_id, observed_at, fact_id)",
		"WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship') AND " + wantPredicate,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("relationship-family index migration missing %q", want)
		}
	}
	// CONCURRENTLY DDL must be one statement per migration file: a
	// multi-statement string forms an implicit transaction, which
	// CREATE/DROP INDEX CONCURRENTLY cannot run inside.
	if terminators := strings.Count(migration.SQL, ";"); terminators != 1 {
		t.Fatalf("059 migration has %d SQL terminators, want 1 (CONCURRENTLY must stay isolated)", terminators)
	}
}

// TestRelationshipFamilyCandidateIndexRenameDropsLegacy pins the #5483 C2
// re-execute-model idempotency fix: the candidate partial index was RENAMED to
// the _v2 name (so an existing deployment actually rebuilds with the Flux-arm
// WHERE, instead of `CREATE ... IF NOT EXISTS <same-name>` no-opping and keeping
// the old predicate forever), and the ORIGINAL name is dropped by a sibling
// migration. Both are isolated one-statement CONCURRENTLY files.
func TestRelationshipFamilyCandidateIndexRenameDropsLegacy(t *testing.T) {
	t.Parallel()

	const (
		legacyIndexName = "fact_records_relationship_family_scope_generation_idx"
		dropMigration   = "drop_relationship_family_candidate_index_legacy"
	)

	// The active index must be the renamed v2 name, never the legacy name, and
	// the legacy name must not linger in the create migration.
	if relationshipFamilyProofIndexName == legacyIndexName {
		t.Fatalf("relationshipFamilyProofIndexName is still the legacy name %q; the C2 rename must bump it", legacyIndexName)
	}
	if !strings.HasSuffix(relationshipFamilyProofIndexName, "_v2") {
		t.Fatalf("relationshipFamilyProofIndexName = %q, want the renamed _v2 index", relationshipFamilyProofIndexName)
	}

	var drop Definition
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == dropMigration {
			drop = definition
			break
		}
	}
	if drop.Name == "" {
		t.Fatalf("missing legacy-index drop migration %q", dropMigration)
	}
	if !strings.Contains(drop.SQL, "DROP INDEX CONCURRENTLY IF EXISTS "+legacyIndexName) {
		t.Fatalf("drop migration does not drop the legacy index %q:\n%s", legacyIndexName, drop.SQL)
	}
	if terminators := strings.Count(drop.SQL, ";"); terminators != 1 {
		t.Fatalf("drop migration has %d SQL terminators, want 1 (CONCURRENTLY must stay isolated)", terminators)
	}
	// The drop migration (068) must sort AFTER the create migration (059) so the
	// v2 index exists before the legacy one is removed — no window without a
	// covering index.
	if drop.Path <= "go/internal/storage/postgres/migrations/059_relationship_family_candidate_index.sql" {
		t.Fatalf("drop migration path %q must sort after 059 so the v2 index is created first", drop.Path)
	}
}

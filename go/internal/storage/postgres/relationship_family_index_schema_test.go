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
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
)

// TestRetirementTableCensusClassifiesEveryScopeKeyedTable joins the closed
// retirement table against information_schema on a fully migrated schema:
// every foreign key to a retirement parent and every scope, repository, or
// partition-key column must resolve to a recorded disposition. A table the
// join cannot see fails here until someone records whether the purge
// deletes it or why it stays.
func TestRetirementTableCensusClassifiesEveryScopeKeyedTable(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_census",
		"set ESHU_POSTGRES_DSN to run the retirement census Postgres proof")

	unclassified, stale, err := RetirementCensusUnclassified(ctx, database)
	if err != nil {
		t.Fatalf("RetirementCensusUnclassified: %v", err)
	}
	for _, hit := range unclassified {
		t.Errorf("table %q is not in the retirement census (columns %v, via_fk=%v); record its purge or census-test disposition", hit.Table, hit.Columns, hit.ViaForeignKey)
	}
	for _, table := range stale {
		t.Errorf("table %q is in the retirement census but the schema scan cannot see it; drop the stale row or record why it is invisible", table)
	}
}

// TestRetirementTableCensusSeededViolationProvesSensitivity is the
// RED/GREEN pair for the join above: a planted table with a source_repo_id
// column fails the census, and dropping it passes again. A guard satisfied
// by its own data is not proof; this pair proves the join sees new tables.
func TestRetirementTableCensusSeededViolationProvesSensitivity(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_census_seed",
		"set ESHU_POSTGRES_DSN to run the retirement census sensitivity Postgres proof")

	if _, err := database.ExecContext(ctx, `CREATE TABLE census_probe_source_repo (
		id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		source_repo_id TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("plant violation table: %v", err)
	}
	unclassified, _, err := RetirementCensusUnclassified(ctx, database)
	if err != nil {
		t.Fatalf("RetirementCensusUnclassified with violation: %v", err)
	}
	found := false
	for _, hit := range unclassified {
		if hit.Table == "census_probe_source_repo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeded violation table was not reported unclassified (got %v); the join is blind to new tables", unclassified)
	}
	if _, err := database.ExecContext(ctx, `DROP TABLE census_probe_source_repo`); err != nil {
		t.Fatalf("drop violation table: %v", err)
	}
	unclassified, stale, err := RetirementCensusUnclassified(ctx, database)
	if err != nil {
		t.Fatalf("RetirementCensusUnclassified after drop: %v", err)
	}
	if len(unclassified) != 0 || len(stale) != 0 {
		t.Fatalf("clean tree still fails after dropping the violation (unclassified=%v stale=%v)", unclassified, stale)
	}
}

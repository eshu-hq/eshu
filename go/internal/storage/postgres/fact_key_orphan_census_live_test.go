// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"

	factkeys "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/keys"
)

// factKeyCensusRow is one row of factkeys.OrphanCensusSQL.
type factKeyCensusRow struct {
	table      string
	scope      string
	generation string
	orphans    int64
}

// factKeyOrphanCensus runs the shipped operator census.
func factKeyOrphanCensus(t *testing.T, ctx context.Context, database *sql.DB) []factKeyCensusRow {
	t.Helper()
	rows, err := database.QueryContext(ctx, factkeys.OrphanCensusSQL)
	if err != nil {
		t.Fatalf("run key sidecar orphan census: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var census []factKeyCensusRow
	for rows.Next() {
		var row factKeyCensusRow
		if err := rows.Scan(&row.table, &row.scope, &row.generation, &row.orphans); err != nil {
			t.Fatalf("scan key sidecar orphan census: %v", err)
		}
		census = append(census, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read key sidecar orphan census: %v", err)
	}
	return census
}

// TestFactKeyOrphanCensusCountsPlantedOrphansLive pins the #7420 operator
// census on the migrated schema, where the per-fact foreign keys are gone:
// keys written by the production writers next to their facts read zero, and
// each planted orphan is counted under its own table, scope and generation.
// The registry orphan names a live fact under another generation, the case
// a fact_id-only anti-join misses but the readers' (fact_id, scope_id,
// generation_id) join never sees.
func TestFactKeyOrphanCensusCountsPlantedOrphansLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	requireFactKeyForeignKeysDropped(t, ctx, database)
	const scope = "keys-census"
	active := scope + "-active"
	testfixtures.SeedScope(t, ctx, database, scope)
	envelopes := factKeyEnvelopes(scope, active, 20)
	writeFactsWithKeys(t, ctx, SQLDB{DB: database}, envelopes)
	for _, table := range factKeyTables {
		if factKeyRowsFor(t, ctx, database, table, scope, active) == 0 {
			t.Fatalf("fixture wrote no %s rows", table)
		}
	}
	if got := factKeyOrphanCensus(t, ctx, database); len(got) != 0 {
		t.Fatalf("census on a clean schema = %+v, want no rows", got)
	}

	for _, plant := range []struct {
		statement string
		args      []any
	}{
		{`INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
VALUES ('census-gone', $1, $2, 'repo-keys', '|gone|')`, []any{scope, active}},
		{`INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
VALUES ('census-gone', $1, $2, 'repo-keys', 'npm', 'pkg-a'), ('census-gone', $1, $2, 'repo-keys', 'npm', 'pkg-b')`, []any{scope, active}},
		{`INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
VALUES ($1, $2, $3, 'npm', 'pkg-census', 'pid-census')`, []any{envelopes[0].FactID, scope, scope + "-other"}},
	} {
		if _, err := database.ExecContext(ctx, plant.statement, plant.args...); err != nil {
			t.Fatalf("plant orphan: %v", err)
		}
	}
	want := []factKeyCensusRow{
		{factkeys.ManifestConsumptionTable, scope, active, 2},
		{factkeys.RegistryIdentityTable, scope, scope + "-other", 1},
		{factkeys.RelationshipReferenceTable, scope, active, 1},
	}
	if got := factKeyOrphanCensus(t, ctx, database); !slices.Equal(got, want) {
		t.Fatalf("census with planted orphans = %+v, want %+v", got, want)
	}

	// The shipped cleanup, run per reported generation, clears what the
	// census names and keeps the live keys of the same generation.
	for _, generation := range []string{active, scope + "-other"} {
		var relationship, manifest, registry int64
		if err := database.QueryRowContext(ctx, factkeys.OrphanCleanupSQL, scope, generation).Scan(&relationship, &manifest, &registry); err != nil {
			t.Fatalf("orphan cleanup for %s: %v", generation, err)
		}
		wantDeleted := [3]int64{1, 2, 0}
		if generation != active {
			wantDeleted = [3]int64{0, 0, 1}
		}
		if got := [3]int64{relationship, manifest, registry}; got != wantDeleted {
			t.Errorf("orphan cleanup for %s deleted %v, want %v", generation, got, wantDeleted)
		}
	}
	if got := factKeyOrphanCensus(t, ctx, database); len(got) != 0 {
		t.Fatalf("census after clearing the orphans = %+v, want no rows", got)
	}
	for _, table := range factKeyTables {
		if factKeyRowsFor(t, ctx, database, table, scope, active) == 0 {
			t.Fatalf("cleanup removed the live %s rows", table)
		}
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// factKeyForeignKeys are the three per-fact foreign keys #7420 retires. The
// live tests drop them in their isolated schema to run the retention code in
// the end state of the follow-up FK drop, where only the explicit deletes
// keep the key sidecars clean.
var factKeyForeignKeys = map[string]string{
	"relationship_reference_candidate_keys": "relationship_reference_candidate_keys_fact_id_fkey",
	"package_manifest_consumption_keys":     "package_manifest_consumption_keys_fact_id_fkey",
	"package_registry_identity_keys":        "package_registry_identity_keys_fact_id_fkey",
}

// dropFactKeyForeignKeys drops the three per-fact foreign keys in the test's
// isolated schema only.
func dropFactKeyForeignKeys(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for table, constraint := range factKeyForeignKeys {
		if _, err := database.ExecContext(ctx, "ALTER TABLE "+table+" DROP CONSTRAINT "+constraint); err != nil {
			t.Fatalf("drop %s: %v", constraint, err)
		}
	}
}

// seedFactKeysForGeneration writes key sidecar rows for the generation's
// facts, each copying the fact's scope and generation as the key writers do:
// a relationship key on about a quarter of the facts, two manifest keys on
// about a tenth and a registry key on about a twentieth, sparse enough that
// the chunk fixture's 500-fact remainder still fits its BatchRowLimit.
func seedFactKeysForGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
SELECT fact_id, scope_id, generation_id, 'repo-keys', '|ref|' || fact_id || '|'
FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND hashtext(fact_id || 'r') % 4 = 0`,
		`INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
SELECT fact.fact_id, fact.scope_id, fact.generation_id, 'repo-keys', 'npm', pkg.name
FROM fact_records AS fact CROSS JOIN (VALUES ('pkg-a'), ('pkg-b')) AS pkg(name)
WHERE fact.scope_id = $1 AND fact.generation_id = $2 AND hashtext(fact.fact_id || 'm') % 10 = 0`,
		`INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
SELECT fact_id, scope_id, generation_id, 'npm', 'pkg-a', 'pid-' || fact_id
FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND hashtext(fact_id || 'p') % 20 = 0`,
	} {
		if _, err := database.ExecContext(ctx, statement, scopeID, generationID); err != nil {
			t.Fatalf("seed fact keys for %s: %v", generationID, err)
		}
	}
}

// orphanFactKeys counts key sidecar rows whose fact or generation is gone,
// by table: the anti-join the dropped foreign key used to make impossible.
func orphanFactKeys(t *testing.T, ctx context.Context, database *sql.DB) map[string]int64 {
	t.Helper()
	orphans := make(map[string]int64, len(factKeyForeignKeys))
	for table := range factKeyForeignKeys {
		var count int64
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM `+table+` AS keys
WHERE NOT EXISTS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_id = keys.fact_id)
   OR NOT EXISTS (SELECT 1 FROM scope_generations AS generation
                  WHERE generation.scope_id = keys.scope_id AND generation.generation_id = keys.generation_id)`).Scan(&count); err != nil {
			t.Fatalf("count %s orphans: %v", table, err)
		}
		orphans[table] = count
	}
	return orphans
}

func factKeyRowsFor(t *testing.T, ctx context.Context, database *sql.DB, table, scopeID, generationID string) int64 {
	t.Helper()
	var count int64
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func assertNoOrphanFactKeys(t *testing.T, ctx context.Context, database *sql.DB, when string) {
	t.Helper()
	for table, count := range orphanFactKeys(t, ctx, database) {
		if count != 0 {
			t.Errorf("%s: %s has %d orphan key rows, want 0", when, table, count)
		}
	}
}

// TestGenerationRetentionNormalPruneLeavesNoOrphanFactKeysLive proves site A
// (#7420): with the per-fact foreign keys dropped, a whole-generation prune
// deletes every key sidecar row of the pruned generation, reports the counts
// in RowsPruned, and keeps the active generation's keys.
func TestGenerationRetentionNormalPruneLeavesNoOrphanFactKeysLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	dropFactKeyForeignKeys(t, ctx, database)
	now := time.Now().UTC()
	const scope = "keys-normal"
	const oldGen, activeGen = "keys-normal-old", "keys-normal-active"
	testfixtures.SeedScope(t, ctx, database, scope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, oldGen, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT md5(gen || '-' || n), $1, gen, 'content_entity', 'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 400) AS n CROSS JOIN (VALUES ($2::text), ($3::text)) AS g(gen)`, scope, oldGen, activeGen); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	seedFactKeysForGeneration(t, ctx, database, scope, oldGen)
	seedFactKeysForGeneration(t, ctx, database, scope, activeGen)
	want := map[string]int64{}
	keptBefore := map[string]int64{}
	for table := range factKeyForeignKeys {
		want[table] = factKeyRowsFor(t, ctx, database, table, scope, oldGen)
		keptBefore[table] = factKeyRowsFor(t, ctx, database, table, scope, activeGen)
		if want[table] == 0 {
			t.Fatalf("fixture seeded no %s rows for the pruned generation", table)
		}
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	assertNoOrphanFactKeys(t, ctx, database, "after the whole-generation prune")
	for table, rows := range want {
		if got := result.RowsPruned[table]; got != rows {
			t.Errorf("RowsPruned[%s] = %d, want %d", table, got, rows)
		}
		if got := factKeyRowsFor(t, ctx, database, table, scope, activeGen); got != keptBefore[table] {
			t.Errorf("active generation %s rows = %d, want %d kept", table, got, keptBefore[table])
		}
	}
}

// TestGenerationRetentionChunkDrainLeavesNoOrphanFactKeysLive proves site B
// (#7420): with the per-fact foreign keys dropped, every over-limit chunk
// cycle deletes exactly its page's key sidecar rows in the page's
// transaction, so no cycle commits an orphan, and the final whole prune
// leaves none either while the neighbor scope's keys survive.
func TestGenerationRetentionChunkDrainLeavesNoOrphanFactKeysLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	dropFactKeyForeignKeys(t, ctx, database)
	now := time.Now().UTC()
	seedChunkDrainFixture(t, ctx, database, now)
	seedFactKeysForGeneration(t, ctx, database, chunkDrainScope, chunkDrainGen)
	seedFactKeysForGeneration(t, ctx, database, chunkNeighbor, chunkNeighbor+"-active")
	neighborBefore := map[string]int64{}
	for table := range factKeyForeignKeys {
		neighborBefore[table] = factKeyRowsFor(t, ctx, database, table, chunkNeighbor, chunkNeighbor+"-active")
	}
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }

	for cycle := 1; cycle <= 3; cycle++ {
		before := map[string]int64{}
		for table := range factKeyForeignKeys {
			before[table] = factKeyRowsFor(t, ctx, database, table, chunkDrainScope, chunkDrainGen)
		}
		result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
		if err != nil {
			t.Fatalf("cycle %d PruneSupersededGenerations() error = %v", cycle, err)
		}
		assertNoOrphanFactKeys(t, ctx, database, "after chunk drain cycle")
		for table := range factKeyForeignKeys {
			after := factKeyRowsFor(t, ctx, database, table, chunkDrainScope, chunkDrainGen)
			if got := result.RowsPruned[table]; got != before[table]-after {
				t.Errorf("cycle %d RowsPruned[%s] = %d, want %d deleted", cycle, table, got, before[table]-after)
			}
		}
	}
	if facts := countScopeGenerationFacts(t, ctx, database, chunkDrainScope, chunkDrainGen); facts != 0 {
		t.Fatalf("final facts = %d, want 0", facts)
	}
	for table, rows := range neighborBefore {
		if got := factKeyRowsFor(t, ctx, database, table, chunkNeighbor, chunkNeighbor+"-active"); got != rows {
			t.Errorf("neighbor %s rows = %d, want %d kept", table, got, rows)
		}
	}
}

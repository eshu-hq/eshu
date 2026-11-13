// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10512 disposable PostgreSQL proof that the reshaped retention row-count arms and the intents DELETE stay on their generation indexes (custom and forced-generic plans, seeded drop-index REDs, executed buffers tracking the batch), that the direct package-key counts equal the old fact-join counts on clean keys and diverge exactly by planted orphans, and that a missing, INVALID or partial-only intents generation index refuses the cycle; bridges ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN.
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// seedRetention10512Intents writes batchPerGen intents on each batch
// generation and otherPerGen on each remaining plan generation, half of
// them completed so the migration 108 partial pending index cannot serve
// the generation probe.
func seedRetention10512Intents(t *testing.T, ctx context.Context, database *sql.DB, batch []string, batchPerGen, otherPerGen int) {
	t.Helper()
	insert := `INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
SELECT generation.generation_id || '-intent-' || n, 'domain', 'p', generation.scope_id,
    'unit', 'repo', 'run', generation.generation_id, '{}'::jsonb, now(),
    CASE WHEN n % 2 = 0 THEN now() END
FROM scope_generations AS generation CROSS JOIN generate_series(1, $1::int) AS n
WHERE generation.generation_id = $2`
	for _, generationID := range batch {
		if _, err := database.ExecContext(ctx, insert, batchPerGen, generationID); err != nil {
			t.Fatalf("seed batch intents %s: %v", generationID, err)
		}
	}
	rows, err := database.QueryContext(ctx,
		`SELECT generation_id FROM scope_generations WHERE scope_id LIKE 'plan-scope-%' AND generation_id <> ALL($1::text[])`, batch)
	if err != nil {
		t.Fatalf("list non-batch generations: %v", err)
	}
	var others []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan generation: %v", err)
		}
		others = append(others, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("list non-batch generations: %v", err)
	}
	for _, generationID := range others {
		if _, err := database.ExecContext(ctx, insert, otherPerGen, generationID); err != nil {
			t.Fatalf("seed non-batch intents %s: %v", generationID, err)
		}
	}
}

// seedRetention10512FillerGenerations adds 200 generations outside the
// batch (4 scopes x 50), each with facts, one manifest and one registry
// key per fact, and intents. Without them the 12 batch generations are
// half the fixture's generations and a quarter of its intents, so the
// planner legitimately prefers a hash join over a sequential scan even
// though the production statements probe an index: the measured
// production batch is 100 generations over a far larger generation
// population (docs/internal/evidence/10512-retention-row-counts.md).
// With the filler the batch is 12 of 224 generations and each guarded
// table holds roughly 50k rows, so the generation indexes win on cost
// under cold, analyzed and forced-generic plans alike, with no planner
// settings forced.
func seedRetention10512FillerGenerations(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	const (
		fillerScopes      = 4
		fillerGenerations = 50
		fillerFacts       = 250
	)
	statements := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'plan-fill-scope-' || s, 'repository', 'git', 'plan-fill-' || s, 'git', 'plan-fill-' || s, now(), now(), 'active'
FROM generate_series(1, $1::int) AS s`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
SELECT 'plan-fill-scope-' || s || '-gen-' || g, 'plan-fill-scope-' || s, 'snapshot', now(), now(), 'superseded', now()
FROM generate_series(1, $1::int) AS s CROSS JOIN generate_series(1, $2::int) AS g`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT md5(generation.generation_id || '-fill-' || n), generation.scope_id, generation.generation_id, 'content_entity',
    'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM scope_generations AS generation CROSS JOIN generate_series(1, $1::int) AS n
WHERE generation.scope_id LIKE 'plan-fill-scope-%'`,
		`INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
SELECT fact_id, scope_id, generation_id, 'repo-plan-fill', 'npm', 'pkg-' || (hashtext(fact_id) & 255)
FROM fact_records WHERE scope_id LIKE 'plan-fill-scope-%'`,
		`INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
SELECT fact_id, scope_id, generation_id, 'npm', 'pkg-' || (hashtext(fact_id) & 255), 'pid-' || fact_id
FROM fact_records WHERE scope_id LIKE 'plan-fill-scope-%'`,
		`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
SELECT generation.generation_id || '-fill-intent-' || n, 'domain', 'p', generation.scope_id,
    'unit', 'repo', 'run', generation.generation_id, '{}'::jsonb, now(),
    CASE WHEN n % 2 = 0 THEN now() END
FROM scope_generations AS generation CROSS JOIN generate_series(1, $1::int) AS n
WHERE generation.scope_id LIKE 'plan-fill-scope-%'`,
	}
	args := [][]any{{fillerScopes}, {fillerScopes, fillerGenerations}, {fillerFacts}, nil, nil, {fillerFacts}}
	for i, statement := range statements {
		if _, err := database.ExecContext(ctx, statement, args[i]...); err != nil {
			t.Fatalf("seed filler generations step %d: %v", i, err)
		}
	}
}

// TestGenerationRetentionRowCountPlansStayOnGenerationIndexesLive fails when
// the full row-count query or the intents DELETE reads a guarded table by a
// path whose cost grows with the table, under custom and forced-generic
// plans. Seeded drop-index REDs prove the guard sees a lost probe, and the
// executed intents DELETE must touch buffers that track the batch while
// the filler generations hold far more rows outside the batch.
func TestGenerationRetentionRowCountPlansStayOnGenerationIndexesLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	for _, table := range []string{"shared_projection_intents", "package_manifest_consumption_keys", "package_registry_identity_keys"} {
		if _, err := database.ExecContext(ctx, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)"); err != nil {
			t.Fatalf("disable autovacuum on %s: %v", table, err)
		}
	}
	seedFactKeyPlanCorpus(t, ctx, database)
	seedRetention10512FillerGenerations(t, ctx, database)
	var candidates []string
	for s := 1; s <= factKeyPlanScopes; s++ {
		for g := 1; g <= 2; g++ {
			candidates = append(candidates, fmt.Sprintf("plan-scope-%d-gen-%d", s, g))
		}
	}
	const batchPerGen = 25
	seedRetention10512Intents(t, ctx, database, candidates, batchPerGen, 3*batchPerGen)
	// Plain VACUUM, never ANALYZE, before the cold checks: the cold
	// condition under test is missing column statistics, but a table old
	// enough to hold superseded generations has been vacuumed, so its
	// visibility map is set. Without it the planner charges the intents
	// arm's index-only probe a heap fetch per estimated row at the
	// default 0.5% selectivity and a hash join over a sequential scan
	// legitimately wins at any fixture scale, cold-stats or not.
	for _, table := range []string{"shared_projection_intents", "package_manifest_consumption_keys", "package_registry_identity_keys"} {
		if _, err := database.ExecContext(ctx, "VACUUM "+table); err != nil {
			t.Fatalf("vacuum %s: %v", table, err)
		}
	}
	// The cold condition assumes the VACUUM above settled the visibility
	// map, but VACUUM cannot mark pages all-visible while a same-database
	// snapshot from before the seeds is still open (#10593): the fraction
	// then stays near 0 and the intents arm's index-only probe prices
	// above a sequential scan plus hash join. Poll until
	// relallvisible=relpages so a pinned horizon fails here with the
	// holder attached instead of as a plan-guard flip.
	requireGuardedTablesVisible10512(t, ctx, database, retentionGuardedVisibilityTables10512)

	rowCountsRequired := []string{
		"shared_projection_intents_generation_idx",
		"package_manifest_consumption_keys_partition_idx",
		"package_registry_identity_keys_partition_idx",
	}
	statements := []retentionStatement{
		{"row_counts", generationRetentionRowCountsQuery, rowCountsRequired},
		{"delete_intents", deleteSharedProjectionIntentsForGenerationsQuery, []string{"shared_projection_intents_generation_idx"}},
	}
	check := func(t *testing.T, condition string, explain func(string) []byte) {
		t.Helper()
		for _, statement := range statements {
			plan := explain(statement.sql)
			if violations := retentionGuardedPlanViolations(t, plan, statement.required); len(violations) > 0 {
				visibility, err := queryGuardedTableVisibility10512(ctx, database, retentionGuardedVisibilityTables10512)
				if err != nil {
					t.Errorf("%s %s plan leaves its generation index: %s (pg_class query failed: %v)\n%s",
						condition, statement.name, strings.Join(violations, "; "), err, plan)
					continue
				}
				t.Errorf("%s\n%s", retentionGuardedPlanFailure10512(condition, statement.name, violations, visibility, retentionGuardedVisibilityTables10512), plan)
			}
		}
	}
	t.Run("cold-custom", func(t *testing.T) {
		check(t, "cold custom", func(statement string) []byte {
			return explainRetentionCustom(t, ctx, database, statement, candidates)
		})
	})
	t.Run("cold-generic", func(t *testing.T) {
		check(t, "cold generic", func(statement string) []byte {
			return explainRetentionGeneric(t, ctx, database, statement, candidates)
		})
	})
	for _, table := range []string{"shared_projection_intents", "package_manifest_consumption_keys", "package_registry_identity_keys", "fact_records", "scope_generations"} {
		if _, err := database.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	t.Run("analyzed-generic", func(t *testing.T) {
		check(t, "analyzed generic", func(statement string) []byte {
			return explainRetentionGeneric(t, ctx, database, statement, candidates)
		})
	})
	t.Run("executed-intents-delete-buffers-track-the-batch", func(t *testing.T) {
		rows, buffers := executeFactKeyDelete(t, ctx, database,
			deleteSharedProjectionIntentsForGenerationsQuery, []any{candidates})
		wantRows := int64(batchPerGen * len(candidates))
		if rows != wantRows || buffers > 4*rows+64 {
			t.Errorf("intents delete removed %d rows (want %d) with %d buffers, want buffers <= 4*rows+64", rows, wantRows, buffers)
		}
		t.Logf("intents delete: %d rows, %d buffers", rows, buffers)
	})
	for _, index := range rowCountsRequired {
		t.Run("seeded-red-missing-"+index, func(t *testing.T) {
			conn, err := database.Conn(ctx)
			if err != nil {
				t.Fatalf("open connection: %v", err)
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.ExecContext(ctx, "BEGIN; DROP INDEX "+index); err != nil {
				t.Fatalf("drop %s: %v", index, err)
			}
			defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
			var plan []byte
			if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+generationRetentionRowCountsQuery, candidates).Scan(&plan); err != nil {
				t.Fatalf("explain without %s: %v", index, err)
			}
			violations := retentionGuardedPlanViolations(t, plan, rowCountsRequired)
			if joined := strings.Join(violations, "; "); !strings.Contains(joined, "never probes "+index) {
				t.Fatalf("plan guard accepted a row count with no %s (violations: %s):\n%s", index, joined, plan)
			}
		})
	}
}

// legacyPackageKeyCounts10512 is the pre-#10512 fact-join form of the two
// package key arms, kept as the differential oracle: on clean keys it must
// agree with the shipped direct arms row for row.
const legacyPackageKeyCounts10512 = `
WITH candidates AS (SELECT unnest($1::text[]) AS generation_id)
SELECT candidate.generation_id, 'package_manifest_consumption_keys' AS table_name,
  (SELECT COUNT(keys.fact_id)
   FROM fact_records AS fact
   JOIN package_manifest_consumption_keys AS keys
     ON keys.fact_id = fact.fact_id
   WHERE fact.scope_id = generation.scope_id
     AND fact.generation_id = candidate.generation_id) AS row_count
FROM candidates AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'package_registry_identity_keys' AS table_name,
  (SELECT COUNT(keys.fact_id)
   FROM fact_records AS fact
   JOIN package_registry_identity_keys AS keys
     ON keys.fact_id = fact.fact_id
   WHERE fact.scope_id = generation.scope_id
     AND fact.generation_id = candidate.generation_id) AS row_count
FROM candidates AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
`

// TestGenerationRetentionPackageKeyCountsMatchFactJoinOnCleanKeysLive proves
// the #10512 exactness contract: with clean keys the shipped direct arms
// equal the old fact-join arms per generation, and planted orphan keys (no
// fact, deleted by the (scope_id, generation_id) envelope) move the direct
// count by exactly the planted rows and nothing else.
func TestGenerationRetentionPackageKeyCountsMatchFactJoinOnCleanKeysLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scope = "keys-10512"
	const oldGen, activeGen = "keys-10512-old", "keys-10512-active"
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
	candidates := []string{oldGen, activeGen}
	tables := []string{"package_manifest_consumption_keys", "package_registry_identity_keys"}

	direct := retentionCountsWith(t, ctx, database, generationRetentionRowCountsQuery, candidates)
	legacy := retentionCountsWith(t, ctx, database, legacyPackageKeyCounts10512, candidates)
	for _, gen := range candidates {
		for _, table := range tables {
			key := gen + "|" + table
			if legacy[key] == 0 {
				t.Fatalf("fixture seeded no clean %s rows for %s", table, gen)
			}
			if direct[key] != legacy[key] {
				t.Errorf("clean %s: direct = %d, fact-join = %d, want equal", key, direct[key], legacy[key])
			}
		}
	}

	planted := map[string]int64{"package_manifest_consumption_keys": 2, "package_registry_identity_keys": 1}
	if _, err := database.ExecContext(ctx, `INSERT INTO package_manifest_consumption_keys
    (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name) VALUES
('orphan-10512-m1', $1, $2, 'repo-keys', 'npm', 'pkg-orphan-1'),
('orphan-10512-m2', $1, $2, 'repo-keys', 'npm', 'pkg-orphan-2')`, scope, oldGen); err != nil {
		t.Fatalf("plant orphan manifest keys: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO package_registry_identity_keys
    (fact_id, scope_id, generation_id, ecosystem, package_name, package_id) VALUES
('orphan-10512-r1', $1, $2, 'npm', 'pkg-orphan-1', 'pid-orphan-1')`, scope, oldGen); err != nil {
		t.Fatalf("plant orphan registry key: %v", err)
	}
	direct = retentionCountsWith(t, ctx, database, generationRetentionRowCountsQuery, candidates)
	legacy = retentionCountsWith(t, ctx, database, legacyPackageKeyCounts10512, candidates)
	for _, table := range tables {
		key := oldGen + "|" + table
		if got := direct[key] - legacy[key]; got != planted[table] {
			t.Errorf("planted %s: direct - fact-join = %d, want exactly the %d planted orphans", key, got, planted[table])
		}
		kept := activeGen + "|" + table
		if direct[kept] != legacy[kept] {
			t.Errorf("unaffected %s: direct = %d, fact-join = %d, want equal", kept, direct[kept], legacy[kept])
		}
	}
}

// TestGenerationRetentionRefusesWithoutValidIntentsGenerationIndexLive
// extends the #7279 refusal to migration 292 (#10512): the intents count
// arm and DELETE probe shared_projection_intents by generation_id, so a
// dropped index, an INVALID one (what a failed concurrent build leaves)
// and a partial index of the right name (the migration 108 pending shape,
// which cannot see completed intents) each refuse the cycle, name the
// index, and prune nothing. Restoring the index lets the next cycle run.
func TestGenerationRetentionRefusesWithoutValidIntentsGenerationIndexLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scope, oldGen = "intents-refuse", "intents-refuse-old"
	testfixtures.SeedScope(t, ctx, database, scope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scope, oldGen, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT md5($2 || '-' || n), $1, $2, 'content_entity', 'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 50) AS n`, scope, oldGen); err != nil {
		t.Fatalf("seed facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
SELECT $2 || '-intent-' || n, 'domain', 'p', $1, 'unit', 'repo', 'run', $2, '{}'::jsonb, now(), now()
FROM generate_series(1, 5) AS n`, scope, oldGen); err != nil {
		t.Fatalf("seed intents: %v", err)
	}
	const index = "shared_projection_intents_generation_idx"
	restore := `CREATE INDEX CONCURRENTLY ` + index + ` ON shared_projection_intents (generation_id)`
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	for _, tc := range []struct {
		name  string
		setup []string
	}{
		{"intents-dropped", []string{"DROP INDEX " + index}},
		{"intents-invalid", []string{
			// Duplicate generation ids make the unique concurrent build
			// fail after it created the catalog entry, leaving
			// indisvalid = false.
			"DROP INDEX " + index,
			`CREATE UNIQUE INDEX CONCURRENTLY ` + index + ` ON shared_projection_intents (generation_id)`,
		}},
		{"intents-partial-only", []string{
			"DROP INDEX " + index,
			`CREATE INDEX ` + index + ` ON shared_projection_intents (generation_id) WHERE completed_at IS NULL`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, statement := range tc.setup {
				_, err := database.ExecContext(ctx, statement)
				expectedFailure := tc.name == "intents-invalid" && i == 1
				if err != nil && !expectedFailure {
					t.Fatalf("setup %q: %v", statement, err)
				}
			}
			_, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
			if !errors.Is(err, ErrGenerationRetentionKeyIndexUnavailable) || !strings.Contains(err.Error(), index) {
				t.Fatalf("PruneSupersededGenerations() error = %v, want ErrGenerationRetentionKeyIndexUnavailable naming %s", err, index)
			}
			if remaining := remainingScopeGenerations(t, ctx, database, []string{oldGen}); !remaining[oldGen] {
				t.Fatal("refused cycle pruned the superseded generation")
			}
			for _, statement := range []string{"DROP INDEX IF EXISTS " + index, restore} {
				if _, err := database.ExecContext(ctx, statement); err != nil {
					t.Fatalf("restore %s: %v", index, err)
				}
			}
		})
	}
	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	if err != nil || result.GenerationsPruned != 1 {
		t.Fatalf("PruneSupersededGenerations() after restoring the index = %d pruned, %v; want 1, nil", result.GenerationsPruned, err)
	}
}

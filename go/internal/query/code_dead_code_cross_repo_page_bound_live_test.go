// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCrossRepoDeadCodeConsumerEvidencePageBoundLive proves what the
// consumer-evidence page reads to produce its answer (#6527, #7249).
//
// buildCrossRepoDeadCodeConsumerEvidenceQuery ranks each producer entity's
// consumers (confidence DESC, depth, repository_id, root_entity_id, scope_id,
// generation_id) inside a per-entity LATERAL capped at
// maxCrossRepoDeadCodeConsumerEvidenceRows+1, and the page stops at the same
// sentinel across entities. The LIMITs bound what comes back; the guards here
// bound what is READ, which is the property a busy symbol or a wide page can
// silently lose with every answer unchanged.
//
// Three fixtures, all writer-conformant: the reducer replaces one (scope,
// generation, repository) snapshot at a time and keeps one row per entity per
// snapshot, so a producer entity's fan-in is spread over many consumer
// repositories, one row each, never stacked under one repository.
//
//   - plain: one ingestion scope, one active generation, and a busy entity
//     consumed by 1,500 repositories. The ranking scan walks the cap, not the
//     fan-in.
//   - retained: the same busy population with every position also kept under
//     three superseded generations. The liveness test is a per-entry filter, so
//     the walk is the cap times (1 + retained) -- and must be MORE than the
//     plain arm's, or the fixture stopped carrying generations.
//   - wide: replica-shaped statistics -- 1,500 ingestion scopes with 24 retained
//     generations each, 40,000 producer entities, a 250-entity page with the
//     busy entity on it. This is where the join form this replaced flipped to
//     probing the primary key once per active ingestion scope, and where the
//     rows and their order are compared against that shipped statement.
//
// Every work guard reads the page as an unscoped caller, the read that takes the
// lateral. A grant-bound read keeps the flat statement, which neither shape
// bounds; the wide arm proves only that the two would return the same rows.
//
// The guards are in code_dead_code_cross_repo_page_bound_live_guards_test.go,
// the wide arm in code_dead_code_cross_repo_page_bound_live_wide_test.go, and
// the EXPLAIN plumbing in code_dead_code_cross_repo_page_bound_live_plan_test.go.
//
// Run with:
//
//	ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 \
//	ESHU_POSTGRES_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/query -run TestCrossRepoDeadCodeConsumerEvidencePageBoundLive -count=1
func TestCrossRepoDeadCodeConsumerEvidencePageBoundLive(t *testing.T) {
	if os.Getenv("ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE") != "1" {
		t.Skip("set ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close Postgres: %v", err)
		}
	})
	// One connection for the whole test, so the proof schema's search_path is
	// the one every statement runs under -- the reader's included.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "SET jit = off"); err != nil {
		t.Fatalf("turn jit off: %v", err)
	}

	// One schema per arm, so each arm's statistics -- and therefore its plan --
	// are its own. A shared schema moved one arm's plan with another's rows.
	stamp := time.Now().UnixNano()
	schemas := map[string]string{}
	for _, arm := range []string{"plain", "retained", "wide"} {
		schema := fmt.Sprintf("cross_repo_dead_code_page_%d_%s", stamp, arm)
		schemas[arm] = schema
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
			t.Fatalf("create proof schema %s: %v", schema, err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cleanupCancel()
			if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
				t.Errorf("drop proof schema %s: %v", schema, err)
			}
		})
		useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, schema)
		seedCrossRepoDeadCodeConsumerPageSchema(ctx, t, db)
	}

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, schemas["plain"])
	seedCrossRepoDeadCodeConsumerPageRows(ctx, t, db, "ent-hot", 0)
	page := crossRepoDeadCodeConsumerPageEntities("ent-hot")
	runCrossRepoDeadCodeConsumerPageIndexGuard(ctx, t, db)
	runCrossRepoDeadCodeConsumerPageAnswerGuard(ctx, t, db, page)
	t.Run("the page walks its cap, not the busy entity's fan-in", func(t *testing.T) {
		runCrossRepoDeadCodeConsumerPageWorkGuard(ctx, t, db, page,
			nil, crossRepoDeadCodeConsumerPageWorkBudget{
				ceiling: crossRepoDeadCodeConsumerPageScanRowBudget,
			})
	})

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, schemas["retained"])
	seedCrossRepoDeadCodeConsumerPageRows(ctx, t, db, "ent-hot-retained", crossRepoDeadCodeConsumerPageRetainedGenerations)
	t.Run("retained generations multiply the walk, and nothing else does", func(t *testing.T) {
		runCrossRepoDeadCodeConsumerPageWorkGuard(ctx, t, db,
			crossRepoDeadCodeConsumerPageEntities("ent-hot-retained"),
			nil, crossRepoDeadCodeConsumerPageWorkBudget{
				ceiling: crossRepoDeadCodeConsumerPageRetainedScanRowBudget,
				floor:   crossRepoDeadCodeConsumerPageScanRowBudget,
			})
	})

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, schemas["wide"])
	runCrossRepoDeadCodeConsumerPageWideArm(ctx, t, db)
}

// useCrossRepoDeadCodeConsumerPageSchema points the pinned connection at one of
// the proof schemas. The pool is one connection, so this is the schema every
// following statement runs under, the reader's included.
func useCrossRepoDeadCodeConsumerPageSchema(ctx context.Context, t *testing.T, db *sql.DB, schema string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set proof search path to %s: %v", schema, err)
	}
}

// crossRepoDeadCodeConsumerPageMigrations are the shipped definitions this
// proof applies, in the order a deployment applies them. Reading the DDL from
// the files is what stops the proof passing against a schema no deployment
// builds -- the plan this test asserts on depends on the table's other indexes
// and on the real ingestion_scopes and scope_generations columns, so a
// hand-written fixture schema plans differently.
var crossRepoDeadCodeConsumerPageMigrations = []string{
	"001_ingestion_scopes.sql",
	"002_scope_generations.sql",
	"027_code_reachability.sql",
	"101_code_reachability_entity_repository_scope_generation_idx.sql",
	"102_drop_code_reachability_entity_repository_idx.sql",
	"103_code_reachability_entity_confidence_rank_idx.sql",
}

// crossRepoDeadCodeConsumerPageRankIndex is the index migration 103 builds: the
// consumer-evidence ranking order with entity_id pinned per page entity, so the
// per-entity scan is answered in output order instead of ranking the group
// first.
const crossRepoDeadCodeConsumerPageRankIndex = "code_reachability_entity_confidence_rank_idx"

// crossRepoDeadCodeConsumerPageHotConsumers is how many consumer repositories
// the busy producer entity has, one active row each -- the shape the reducer
// writes, since it keeps one row per entity per (scope, generation,
// repository) snapshot. 1,500 is past the 1,001-row cap, so the busy entity is
// the one the page truncates.
const crossRepoDeadCodeConsumerPageHotConsumers = 1500

// crossRepoDeadCodeConsumerPageOrdinaryConsumers is how many consumer
// repositories each ordinary producer entity has.
const crossRepoDeadCodeConsumerPageOrdinaryConsumers = 3

// crossRepoDeadCodeConsumerPageOrdinaryEntities is how many ordinary producer
// entities share the plain and retained pages with the busy one. They sort
// BEFORE it, so the read returns all of their rows and then spends the rest of
// its cap inside the busy entity -- which is what makes the truncation marker
// land on exactly one entity.
const crossRepoDeadCodeConsumerPageOrdinaryEntities = 5

// crossRepoDeadCodeConsumerPageRetainedGenerations is how many superseded
// generations the retention arm keeps per position.
// DefaultGenerationRetentionPolicy keeps at least 24; three is enough to make
// the multiplication visible and cheap enough to seed.
const crossRepoDeadCodeConsumerPageRetainedGenerations = 3

// crossRepoDeadCodeConsumerPageScanRowBudget bounds the entries the ranking
// scan may walk on the arm with NO retained generations: the busy entity's
// cap+1 plus the ordinary entities' rows, with slack for rows the producer
// exclusion or the depth filter discards after reading them.
const crossRepoDeadCodeConsumerPageScanRowBudget = maxCrossRepoDeadCodeConsumerEvidenceRows + 200

// crossRepoDeadCodeConsumerPageRetainedScanRowBudget bounds the same walk on
// the retention arm, and the arithmetic is the point rather than the number.
// The liveness test discards a superseded entry only after the scan has read
// it, so for an answer of N rows drawn from positions holding 1 + R generations
// each, the scan walks up to N x (1 + R). A budget of N alone would be a bound
// this route does NOT have, and every real install retains generations.
const crossRepoDeadCodeConsumerPageRetainedScanRowBudget = (maxCrossRepoDeadCodeConsumerEvidenceRows+1)*
	(1+crossRepoDeadCodeConsumerPageRetainedGenerations) + 200

// crossRepoDeadCodeConsumerPageEntities is a plain or retained page: the
// ordinary entities first in entity_id order, then the busy one.
func crossRepoDeadCodeConsumerPageEntities(busy string) []string {
	entities := make([]string, 0, crossRepoDeadCodeConsumerPageOrdinaryEntities+1)
	for i := 1; i <= crossRepoDeadCodeConsumerPageOrdinaryEntities; i++ {
		entities = append(entities, fmt.Sprintf("ent-%03d", i))
	}
	return append(entities, busy)
}

// seedCrossRepoDeadCodeConsumerPageSchema applies the shipped table and index
// definitions into the proof schema. CONCURRENTLY is stripped because this
// schema has no concurrent writer and CREATE INDEX CONCURRENTLY cannot run
// inside the test's statement batch.
func seedCrossRepoDeadCodeConsumerPageSchema(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	for _, name := range crossRepoDeadCodeConsumerPageMigrations {
		migration, err := os.ReadFile("../storage/postgres/migrations/" + name)
		if err != nil {
			t.Fatalf("read the shipped migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, strings.ReplaceAll(string(migration), "CONCURRENTLY ", "")); err != nil {
			t.Fatalf("apply the shipped migration %s: %v", name, err)
		}
	}
}

// seedCrossRepoDeadCodeConsumerPageRows seeds one ingestion scope whose active
// generation holds one busy producer entity consumed by every hot repository
// and a handful of ordinary entities, plus `retained` superseded generations
// that each hold the busy entity's same positions again. Every (scope,
// generation, repository) snapshot carries at most one row per entity, as the
// reducer writes it.
func seedCrossRepoDeadCodeConsumerPageRows(ctx context.Context, t *testing.T, db *sql.DB, busy string, retained int) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-1', 'repository', 'git', 'key-1', 'code', 'partition-1', now(), now(), 'active', 'gen-active');
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-active', 'scope-1', 'sync', now(), now(), 'active', now());
`); err != nil {
		t.Fatalf("seed proof scope and generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen-old-' || lpad(g::text, 3, '0'), 'scope-1', 'sync', now(), now(), 'superseded', now()
FROM generate_series(1, $1) AS g`, retained); err != nil {
		t.Fatalf("seed retained generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', gen.generation_id, 'repo-' || lpad(r::text, 4, '0'),
       'repo-' || lpad(r::text, 4, '0') || '#main', $1, 1 + (r % 3), 'reachable', 0.95,
       'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $2) AS r
CROSS JOIN (
  SELECT 'gen-active' AS generation_id
  UNION ALL
  SELECT 'gen-old-' || lpad(g::text, 3, '0') FROM generate_series(1, $3) AS g
) AS gen`, busy, crossRepoDeadCodeConsumerPageHotConsumers, retained); err != nil {
		t.Fatalf("seed busy-entity rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-active', 'repo-' || lpad(r::text, 4, '0'),
       'repo-' || lpad(r::text, 4, '0') || '#ordinary', 'ent-' || lpad(i::text, 3, '0'),
       1, 'reachable', 0.9, 'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $1) AS i
CROSS JOIN generate_series(1, $2) AS r`,
		crossRepoDeadCodeConsumerPageOrdinaryEntities, crossRepoDeadCodeConsumerPageOrdinaryConsumers); err != nil {
		t.Fatalf("seed ordinary-entity rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("vacuum the proof fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE ingestion_scopes; ANALYZE scope_generations"); err != nil {
		t.Fatalf("analyze the proof fixture: %v", err)
	}
}

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
// consumer-evidence page's LIMIT is actually worth (#6527).
//
// buildCrossRepoDeadCodeConsumerEvidenceQuery orders a page of producer
// entities' consumers (entity_id, confidence DESC, depth, repository_id,
// root_entity_id) and stops at maxCrossRepoDeadCodeConsumerEvidenceRows+1. The
// LIMIT bounds what comes back, not what is read: without an index in that
// order Postgres has to rank a producer entity's whole fan-in group before it
// can emit the group's first row, so one busy symbol costs the page its entire
// consumer set. Migration 103 builds that index.
//
// Three guards, because they fail to different mutations and none of them sees
// another's:
//
//   - the index guard reads the shipped migrations' end state and requires the
//     ordering index to exist with its five key columns in the statement's own
//     order. The same columns in another order still answer correctly.
//   - the answer guard reads the page through the shipped reader and requires
//     the rows and the per-entity truncation marker the route contracts for.
//     It is what an index that changed the ordering would break, and it is
//     unmoved by an index that is merely absent.
//   - the work guard counts the rows the plan's reachability scan actually
//     read. Dropping migration 103 leaves every row and every marker in the
//     answer guard identical and turns 1,001 rows read into 30,015, which is
//     the defect this issue is about.
//
// The work guard runs under both plan modes, matching the sibling probe proof.
// The three guards and the EXPLAIN plumbing they share live in
// code_dead_code_cross_repo_page_bound_live_guards_test.go.
// Note what the fixture can and cannot show: at fixture scale the planner picks
// the index under a custom plan AND under force_generic_plan, while on the
// 2.2M-row corpus in docs/internal/evidence/5167-cross-repo-consumer-page-bound.md
// a 251-entity page keeps the pre-index plan under a forced generic one. The
// corpus measurement is where that is recorded, together with the
// pg_prepared_statements reading that shows Postgres's plan cache never
// promotes this statement to a generic plan in the first place.
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	// Two schemas, not one. The retention arm's rows are a large fraction of
	// the table, and sharing a schema moved the no-retention arm's plan onto a
	// bitmap scan -- one arm's fixture silently changed the other's statistics.
	// Each arm gets its own table so each one's plan is its own.
	stamp := time.Now().UnixNano()
	plain := fmt.Sprintf("cross_repo_dead_code_page_%d_plain", stamp)
	retained := fmt.Sprintf("cross_repo_dead_code_page_%d_retained", stamp)
	for _, schema := range []string{plain, retained} {
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
			t.Fatalf("create proof schema %s: %v", schema, err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
				t.Errorf("drop proof schema %s: %v", schema, err)
			}
		})
	}

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, plain)
	seedCrossRepoDeadCodeConsumerPageSchema(ctx, t, db)
	seedCrossRepoDeadCodeConsumerPageRows(ctx, t, db)

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, retained)
	seedCrossRepoDeadCodeConsumerPageSchema(ctx, t, db)
	seedCrossRepoDeadCodeConsumerPageRows(ctx, t, db)
	seedCrossRepoDeadCodeConsumerPageRetainedRows(ctx, t, db)

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, plain)
	page := crossRepoDeadCodeConsumerPageEntities()
	runCrossRepoDeadCodeConsumerPageIndexGuard(ctx, t, db)
	runCrossRepoDeadCodeConsumerPageAnswerGuard(ctx, t, db, page)
	runCrossRepoDeadCodeConsumerPageWorkGuard(ctx, t, db, page)

	useCrossRepoDeadCodeConsumerPageSchema(ctx, t, db, retained)
	runCrossRepoDeadCodeConsumerPageRetainedWorkGuard(
		ctx, t, db, crossRepoDeadCodeConsumerPageRetainedEntities())
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
// consumer-evidence page's ORDER BY, with entity_id pinned by the statement's
// IN list, so the scan is already in output order and the LIMIT stops it.
const crossRepoDeadCodeConsumerPageRankIndex = "code_reachability_entity_confidence_rank_idx"

// crossRepoDeadCodeConsumerPageHotRepositories are the consumer repositories
// the busy producer entity's rows spread across. All three are granted, so the
// grant never trims the group and the work guard measures the ordering alone.
var crossRepoDeadCodeConsumerPageHotRepositories = []string{"repo-a", "repo-b", "repo-c"}

// crossRepoDeadCodeConsumerPageHotRowsPerRepository is how many
// active-generation consumer rows the busy entity gets in each repository.
// 10,000 across three repositories is 30,015 rows on the page against a 1,001
// row cap: far enough above it that a read bounded by the cap and a read
// bounded by the group cannot be confused, and small enough to seed in about a
// second.
const crossRepoDeadCodeConsumerPageHotRowsPerRepository = 10000

// crossRepoDeadCodeConsumerPageOrdinaryEntities is how many ordinary producer
// entities share the page with the busy one. They sort BEFORE it, so the read
// returns all of their rows and then spends the rest of its cap inside the busy
// entity -- which is what makes the truncation marker land on exactly one
// entity and makes the answer guard's counts deterministic.
const crossRepoDeadCodeConsumerPageOrdinaryEntities = 5

// crossRepoDeadCodeConsumerPageRetainedGenerations is how many superseded
// generations the retention arm keeps per position. The reducer's reachability
// delete is keyed (scope_id, generation_id, repository_id), so a new generation
// ADDS a row set and leaves the previous one for the retention runner, and
// DefaultGenerationRetentionPolicy keeps at least 24. Three is enough to make
// the multiplication visible and cheap enough to seed.
const crossRepoDeadCodeConsumerPageRetainedGenerations = 3

// crossRepoDeadCodeConsumerPageScanRowBudget bounds the rows the plan's
// reachability scan may read on the arm with NO retained generations. With
// migration 103 the scan reads exactly the statement's LIMIT, 1,001; without it
// the same page reads 30,015. The budget sits just above the LIMIT rather than
// at it, because rows the grant or the depth filter discards are read before
// they are discarded and this fixture is free to grow one.
const crossRepoDeadCodeConsumerPageScanRowBudget = maxCrossRepoDeadCodeConsumerEvidenceRows + 200

// crossRepoDeadCodeConsumerPageRetainedScanRowBudget bounds the same scan on
// the retention arm, and the arithmetic is the point rather than the number.
//
// The LIMIT bounds rows RETURNED. The active-generation test is a join above
// the scan and migration 103's key carries no way to reach the scope's active
// generation, so the scan emits one entry per RETAINED generation per position
// and the join discards the superseded ones. For an answer of N rows drawn from
// positions holding 1 + R generations each, the scan walks up to N x (1 + R).
// Here N is the sentinel-inclusive cap and R is the constant above, so the
// budget is that product plus the same slack the arm above uses.
//
// A budget of N alone would be the bound this route does NOT have, and it would
// fail as soon as anything retained a second generation -- which is the state
// every real install is in.
const crossRepoDeadCodeConsumerPageRetainedScanRowBudget = (maxCrossRepoDeadCodeConsumerEvidenceRows+1)*
	(1+crossRepoDeadCodeConsumerPageRetainedGenerations) + 200

// crossRepoDeadCodeConsumerPageEntities is the producer page: the ordinary
// entities first in entity_id order, then the busy one.
func crossRepoDeadCodeConsumerPageEntities() []string {
	entities := make([]string, 0, crossRepoDeadCodeConsumerPageOrdinaryEntities+1)
	for i := 1; i <= crossRepoDeadCodeConsumerPageOrdinaryEntities; i++ {
		entities = append(entities, fmt.Sprintf("ent-%03d", i))
	}
	return append(entities, "ent-hot")
}

// crossRepoDeadCodeConsumerPageRetainedEntities is the retention arm's page: the
// same ordinary entities, and a busy entity whose every position also exists
// under each retained superseded generation.
func crossRepoDeadCodeConsumerPageRetainedEntities() []string {
	entities := make([]string, 0, crossRepoDeadCodeConsumerPageOrdinaryEntities+1)
	for i := 1; i <= crossRepoDeadCodeConsumerPageOrdinaryEntities; i++ {
		entities = append(entities, fmt.Sprintf("ent-%03d", i))
	}
	return append(entities, "ent-hot-retained")
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

// seedCrossRepoDeadCodeConsumerPageRows seeds one busy producer entity and a
// handful of ordinary ones, all under one active generation.
func seedCrossRepoDeadCodeConsumerPageRows(ctx context.Context, t *testing.T, db *sql.DB) {
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

	for _, repositoryID := range crossRepoDeadCodeConsumerPageHotRepositories {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-active', $1, $1 || '#caller-' || lpad(i::text, 6, '0'), 'ent-hot',
       1 + (i % 3), 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $2) AS i`, repositoryID, crossRepoDeadCodeConsumerPageHotRowsPerRepository); err != nil {
			t.Fatalf("seed busy-entity rows in %s: %v", repositoryID, err)
		}
	}
	for _, repositoryID := range crossRepoDeadCodeConsumerPageHotRepositories {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-active', $1, $1 || '#ordinary-' || lpad(i::text, 3, '0'),
       'ent-' || lpad(i::text, 3, '0'), 1, 'reachable', 0.9, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $2) AS i`, repositoryID, crossRepoDeadCodeConsumerPageOrdinaryEntities); err != nil {
			t.Fatalf("seed ordinary-entity rows in %s: %v", repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows; ANALYZE ingestion_scopes; ANALYZE scope_generations"); err != nil {
		t.Fatalf("analyze the proof fixture: %v", err)
	}
}

// seedCrossRepoDeadCodeConsumerPageRetainedRows gives one busy producer entity
// the same active population as ent-hot AND a copy of every position under each
// retained superseded generation, which is the state the retention runner
// leaves behind and the state the one-generation arm cannot show.
func seedCrossRepoDeadCodeConsumerPageRetainedRows(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen-old-' || lpad(g::text, 3, '0'), 'scope-1', 'sync', now(), now(), 'superseded', now()
FROM generate_series(1, $1) AS g`, crossRepoDeadCodeConsumerPageRetainedGenerations); err != nil {
		t.Fatalf("seed retained generations: %v", err)
	}
	for _, repositoryID := range crossRepoDeadCodeConsumerPageHotRepositories {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', gen.generation_id, $1, $1 || '#retained-' || lpad(i::text, 6, '0'),
       'ent-hot-retained', 1 + (i % 3), 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $2) AS i
CROSS JOIN (
  SELECT 'gen-active' AS generation_id
  UNION ALL
  SELECT 'gen-old-' || lpad(g::text, 3, '0') FROM generate_series(1, $3) AS g
) AS gen`, repositoryID, crossRepoDeadCodeConsumerPageHotRowsPerRepository,
			crossRepoDeadCodeConsumerPageRetainedGenerations); err != nil {
			t.Fatalf("seed retained-entity rows in %s: %v", repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows; ANALYZE scope_generations"); err != nil {
		t.Fatalf("analyze the retention fixture: %v", err)
	}
}

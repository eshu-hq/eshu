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
)

// The fixtures TestCrossRepoDeadCodeUngrantedConsumerProbeLive seeds, and the
// schema they live in. Every one of them exists for a cost axis the walk has to
// be insensitive to -- row fan-in, distinct consumer repositories, retained
// generations, ingestion scopes per repository -- so the guards in
// code_dead_code_cross_repo_ungranted_probe_live_work_test.go have something to
// measure that a verdict assertion cannot see.

// crossRepoDeadCodeProbeFanInRepositories are the consumer repositories the
// fan-in fixture spreads its rows across.
var crossRepoDeadCodeProbeFanInRepositories = []string{"repo-a", "repo-c", "repo-e", "repo-g", "repo-i"}

// crossRepoDeadCodeProbeFanOutRepositories are 200 distinct consumer
// repositories for one producer entity. They sort after every repository the
// grants in this test name, so a grant can leave all of them out.
var crossRepoDeadCodeProbeFanOutRepositories = func() []string {
	repositories := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		repositories = append(repositories, fmt.Sprintf("repo-x%03d", i))
	}
	return repositories
}()

// seedCrossRepoDeadCodeProbeFanIn gives one producer entity perRepositoryRows
// active-generation consumer rows in each of the named repositories. Row fan-in
// is the axis the walk must NOT be sensitive to: it visits each distinct
// consumer repository once and never looks at a second row of any of them.
func seedCrossRepoDeadCodeProbeFanIn(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryIDs []string,
	perRepositoryRows int,
) {
	t.Helper()

	for _, repositoryID := range repositoryIDs {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-active', $1, $1 || '#caller-' || lpad(i::text, 6, '0'), $2, 1,
       'reachable', 0.95, 'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $3) AS i`, repositoryID, entityID, perRepositoryRows); err != nil {
			t.Fatalf("seed fan-in rows for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze fan-in rows: %v", err)
	}
}

// crossRepoDeadCodeProbeScopesPerRepository is how many ingestion scopes cover
// one repository in the scope fan-out fixtures. A repository ingested by more
// than one scope is ordinary -- a monorepo path scope beside a whole-repository
// scope, or a re-onboard that left the old scope in place -- and 50 is a
// deliberately unremarkable point on that range, chosen to be far enough above
// the walk's real step count that a shape which pays per scope cannot hide
// inside the noise.
const crossRepoDeadCodeProbeScopesPerRepository = 50

// seedCrossRepoDeadCodeProbeGrantedScopeFanOut gives one producer entity a
// consumer in a GRANTED repository covered by many ingestion scopes -- each
// with its own active generation and its own live row -- and one live consumer
// in an ungranted repository sorting after it.
//
// Every pair in the granted repository is granted, so none of them can ever be
// hidden and the entity's verdict does not depend on how many there are. What
// depends on it is the number of steps: a walk that leaves a granted repository
// by seeking the next repository takes one step for it, and one that seeks the
// next pair takes one per scope.
func seedCrossRepoDeadCodeProbeGrantedScopeFanOut(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	grantedRepositoryID string,
	hiddenRepositoryID string,
	scopes int,
) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT 'scope-fanout-g-' || lpad(i::text, 4, '0'), 'gen-fanout-g-' || lpad(i::text, 4, '0')
FROM generate_series(1, $1) AS i
ON CONFLICT (scope_id) DO NOTHING`, scopes); err != nil {
		t.Fatalf("seed granted fan-out scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT 'gen-fanout-g-' || lpad(i::text, 4, '0'), 'scope-fanout-g-' || lpad(i::text, 4, '0'), 'active'
FROM generate_series(1, $1) AS i
ON CONFLICT (generation_id) DO NOTHING`, scopes); err != nil {
		t.Fatalf("seed granted fan-out generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-fanout-g-' || lpad(i::text, 4, '0'), 'gen-fanout-g-' || lpad(i::text, 4, '0'),
       $1, $1 || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $3) AS i`, grantedRepositoryID, entityID, scopes); err != nil {
		t.Fatalf("seed granted fan-out rows for %s in %s: %v", entityID, grantedRepositoryID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', 'gen-active', $1, $1 || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
        '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
		hiddenRepositoryID, entityID); err != nil {
		t.Fatalf("seed hidden consumer for %s in %s: %v", entityID, hiddenRepositoryID, err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze granted fan-out rows: %v", err)
	}
}

// seedCrossRepoDeadCodeProbeUngrantedScopeFanOut gives one producer entity a
// consumer in a single repository covered by many ingestion scopes whose rows
// are all superseded, optionally followed by one scope whose row is live.
//
// This is the case the granted skip must NOT be applied to. Any one of an
// ungranted repository's scopes can hold the live row, and here the live one is
// last, so a walk that left the repository after its first pair would answer
// "nothing hidden" for an entity that has a hidden consumer.
func seedCrossRepoDeadCodeProbeUngrantedScopeFanOut(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryID string,
	scopePrefix string,
	staleScopes int,
	live bool,
) {
	t.Helper()

	scopeName := "scope-fanout-" + scopePrefix + "-"
	activeGeneration := "gen-fanout-" + scopePrefix + "-active-"
	staleGeneration := "gen-fanout-" + scopePrefix + "-stale-"
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT $1 || lpad(i::text, 4, '0'), $2 || lpad(i::text, 4, '0')
FROM generate_series(1, $3) AS i
ON CONFLICT (scope_id) DO NOTHING`, scopeName, activeGeneration, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT $2 || lpad(i::text, 4, '0'), $1 || lpad(i::text, 4, '0'), 'active'
FROM generate_series(1, $3) AS i
ON CONFLICT (generation_id) DO NOTHING`, scopeName, activeGeneration, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out active generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT $2 || lpad(i::text, 4, '0'), $1 || lpad(i::text, 4, '0'), 'superseded'
FROM generate_series(1, $3) AS i
ON CONFLICT (generation_id) DO NOTHING`, scopeName, staleGeneration, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out superseded generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT $1 || lpad(i::text, 4, '0'), $2 || lpad(i::text, 4, '0'),
       $3, $3 || '#caller', $4, 1, 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $5) AS i`,
		scopeName, staleGeneration, repositoryID, entityID, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out superseded rows for %s in %s: %v", entityID, repositoryID, err)
	}
	if live {
		// 9999 so this scope sorts after every stale one: the walk has to reach
		// the LAST scope of the repository to find the live row.
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
VALUES ($1 || '9999', $2 || '9999')
ON CONFLICT (scope_id) DO NOTHING`, scopeName, activeGeneration); err != nil {
			t.Fatalf("seed ungranted fan-out live scope: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
VALUES ($2 || '9999', $1 || '9999', 'active')
ON CONFLICT (generation_id) DO NOTHING`, scopeName, activeGeneration); err != nil {
			t.Fatalf("seed ungranted fan-out live generation: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ($1 || '9999', $2 || '9999', $3, $3 || '#caller', $4, 1, 'reachable', 0.95,
        'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
			scopeName, activeGeneration, repositoryID, entityID); err != nil {
			t.Fatalf("seed ungranted fan-out live row for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze ungranted fan-out rows: %v", err)
	}
}

// crossRepoDeadCodeProbeRetainedGenerations is how many superseded generations
// the retained-generation fixture keeps per consumer repository. The default
// retention policy keeps the 24 most recent superseded generations per scope
// plus everything superseded inside the last seven days
// (postgres.DefaultGenerationRetentionPolicy), so a scope resynced every few
// minutes holds far more than 24; 200 is a deliberately ordinary point on that
// range rather than a worst case.
const crossRepoDeadCodeProbeRetainedGenerations = 200

// seedCrossRepoDeadCodeProbeRetainedGenerations gives one producer entity a row
// in each named repository for each of retained superseded generations, and
// then its one active-generation row.
//
// The superseded rows go in first on purpose. That is the order a real install
// writes them -- the active generation is the newest -- so the active row is the
// last heap tuple in its (entity_id, repository_id) group and the worst case for
// a step that stops on the first row it can use.
func seedCrossRepoDeadCodeProbeRetainedGenerations(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryIDs []string,
	retained int,
) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT 'gen-retained-' || lpad(i::text, 4, '0'), 'scope-1', 'superseded'
FROM generate_series(1, $1) AS i
ON CONFLICT (generation_id) DO NOTHING`, retained); err != nil {
		t.Fatalf("seed retained generations: %v", err)
	}
	for _, repositoryID := range repositoryIDs {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-retained-' || lpad(i::text, 4, '0'), $1, $1 || '#caller', $2, 1,
       'reachable', 0.95, 'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $3) AS i`, repositoryID, entityID, retained); err != nil {
			t.Fatalf("seed retained-generation rows for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	for _, repositoryID := range repositoryIDs {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', 'gen-active', $1, $1 || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
        '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`, repositoryID, entityID); err != nil {
			t.Fatalf("seed active row for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze retained-generation rows: %v", err)
	}
}

// crossRepoDeadCodeProbeRow is one seeded reachability row.
type crossRepoDeadCodeProbeRow struct {
	entityID     string
	repositoryID string
	depth        int
	generationID string
}

// seedCrossRepoDeadCodeProbeSchema creates the three tables the probe joins and
// the index it depends on. The index DDL is read from the shipped migration so
// a test cannot pass against an index the deployment does not build;
// CONCURRENTLY is stripped because this schema has no concurrent writer and
// CREATE INDEX CONCURRENTLY cannot run inside the test's statement batch.
func seedCrossRepoDeadCodeProbeSchema(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
  scope_id text PRIMARY KEY,
  active_generation_id text NULL
);
CREATE TABLE scope_generations (
  generation_id text PRIMARY KEY,
  scope_id text NOT NULL,
  status text NOT NULL
);
CREATE TABLE code_reachability_rows (
  scope_id text NOT NULL,
  generation_id text NOT NULL,
  repository_id text NOT NULL,
  root_entity_id text NOT NULL,
  entity_id text NOT NULL,
  depth integer NOT NULL,
  state text NOT NULL,
  confidence double precision NOT NULL,
  min_resolution_method text NOT NULL,
  evidence jsonb NOT NULL,
  root_kinds jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (scope_id, generation_id, repository_id, root_entity_id, entity_id)
);
INSERT INTO ingestion_scopes VALUES ('scope-1', 'gen-active');
INSERT INTO scope_generations VALUES ('gen-active', 'scope-1', 'active'), ('gen-stale', 'scope-1', 'superseded');
`); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}

	// Both index migrations, in the order a deployment applies them, so the
	// fixture ends where a real install ends: 101's four-column key built, and
	// 102 dropping the two-column index an earlier release built. Reading the
	// DDL from the shipped files is what stops a proof passing against an index
	// no deployment builds.
	for _, name := range crossRepoDeadCodeProbeIndexMigrations {
		migration, err := os.ReadFile("../storage/postgres/migrations/" + name)
		if err != nil {
			t.Fatalf("read the shipped index migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, strings.ReplaceAll(string(migration), "CONCURRENTLY ", "")); err != nil {
			t.Fatalf("apply the shipped index migration %s: %v", name, err)
		}
	}
}

// crossRepoDeadCodeProbeIndexMigrations are the index migrations the probe
// depends on, in migration order.
var crossRepoDeadCodeProbeIndexMigrations = []string{
	"101_code_reachability_entity_repository_scope_generation_idx.sql",
	"102_drop_code_reachability_entity_repository_idx.sql",
}

func seedCrossRepoDeadCodeProbeRows(ctx context.Context, t *testing.T, db *sql.DB, rows []crossRepoDeadCodeProbeRow) {
	t.Helper()

	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', $1, $2, $3, $4, $5, 'reachable', 0.95, 'symbol_exact',
        '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
			row.generationID,
			row.repositoryID,
			row.repositoryID+"#caller",
			row.entityID,
			row.depth,
		); err != nil {
			t.Fatalf("seed reachability row %+v: %v", row, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze proof rows: %v", err)
	}
}

// crossRepoDeadCodeProbeStaleConsumerRepositories is how many ungranted
// consumer repositories the stale-consumer fixture gives one producer entity.
// Every one of them holds only superseded rows, so none is hidden and the walk
// passes all of them before it reaches the one that is.
const crossRepoDeadCodeProbeStaleConsumerRepositories = 300

// seedCrossRepoDeadCodeProbeStaleConsumerFanOut gives one producer entity a
// consumer row in each of many ungranted repositories under a SUPERSEDED
// generation, and one live consumer in an ungranted repository sorting after
// all of them.
//
// This is the axis the walk's stop condition does not bound.
// ReplaceCodeReachabilityRepositoryRows deletes by
// (scope_id, generation_id, repository_id) before it writes, so a generation
// that no longer names an entity leaves the previous generation's rows on disk
// until the retention runner prunes them -- and the default policy keeps the 24
// most recent superseded generations per scope plus everything superseded
// inside the last seven days. A repository that stopped calling the symbol is
// therefore still a (repository, scope) pair the walk visits: ungranted, so the
// granted skip does not apply, and not live, so it is not hidden and the walk
// steps past it rather than stopping.
//
// No answer depends on how many there are -- the entity is hidden either way,
// because of the live consumer at the end -- so only a work measurement can see
// the cost, which is why the guard that ships counts recursive rows.
func seedCrossRepoDeadCodeProbeStaleConsumerFanOut(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryPrefix string,
	staleRepositories int,
) {
	t.Helper()

	// gen-stale is the superseded generation seedCrossRepoDeadCodeProbeSchema
	// already declares for scope-1, whose active generation is gen-active.
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-stale', $1 || lpad(i::text, 3, '0'),
       $1 || lpad(i::text, 3, '0') || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(0, $3 - 1) AS i`,
		repositoryPrefix, entityID, staleRepositories); err != nil {
		t.Fatalf("seed stale consumer rows for %s: %v", entityID, err)
	}
	// 'zzz' so this repository sorts after every stale one: the walk has to pass
	// all of them before it reaches the consumer that hides the entity.
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', 'gen-active', $1 || 'zzz', $1 || 'zzz#caller', $2, 1, 'reachable', 0.95,
        'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
		repositoryPrefix, entityID); err != nil {
		t.Fatalf("seed live hidden consumer for %s: %v", entityID, err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze stale consumer rows: %v", err)
	}
}

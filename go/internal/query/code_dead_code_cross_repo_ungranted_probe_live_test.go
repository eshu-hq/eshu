// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCrossRepoDeadCodeUngrantedConsumerProbeLive proves the one thing no fake
// driver can: that the probe's repository_id ranges really are the complement
// of the caller's grant, in Postgres, under Postgres's own collation.
//
// The probe replaces `NOT (repository_id = ANY($grant))` with three range
// families around the sorted grant -- below the first granted id, between two
// consecutive ones, above the last -- because a range is seekable and a NOT IN
// is not. That rewrite is only correct if the union of those ranges is exactly
// the set complement, which depends on the bounds being ordered the way the
// index and the comparisons order them. This test drives the shipped statement
// against real rows for eight grant shapes and requires it to return the same
// producer entities as the NOT IN it replaced, every time.
//
// Run with:
//
//	ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 \
//	ESHU_POSTGRES_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1
func TestCrossRepoDeadCodeUngrantedConsumerProbeLive(t *testing.T) {
	if os.Getenv("ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE") != "1" {
		t.Skip("set ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	// One connection for the whole test so the proof schema's search_path is
	// the one every statement, including the reader's, actually runs under.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema := fmt.Sprintf("cross_repo_dead_code_probe_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set proof search path: %v", err)
	}
	seedCrossRepoDeadCodeProbeSchema(ctx, t, db)

	// One producer entity per fan-in shape the probe has to get right. The
	// consumer repository names are spaced so a hidden one can sit below the
	// smallest granted id, between two granted ids, or above the largest.
	seedCrossRepoDeadCodeProbeRows(ctx, t, db, []crossRepoDeadCodeProbeRow{
		{entityID: "ent-spread", repositoryID: "repo-a", depth: 1, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-c", depth: 2, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-e", depth: 1, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-g", depth: 3, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-i", depth: 1, generationID: "gen-active"},
		{entityID: "ent-middle", repositoryID: "repo-e", depth: 1, generationID: "gen-active"},
		// Only the producer's own repository consumes this one, and the
		// statement excludes it, so no grant can make it hidden.
		{entityID: "ent-self", repositoryID: "repo-producer", depth: 1, generationID: "gen-active"},
		// Depth 0 is the root's own row, not a consumer edge.
		{entityID: "ent-depth-zero", repositoryID: "repo-z", depth: 0, generationID: "gen-active"},
		// A superseded generation is not evidence of anything.
		{entityID: "ent-stale", repositoryID: "repo-z", depth: 1, generationID: "gen-stale"},
	})
	// ent-busy is the shape the probe exists for: a producer entity whose
	// fan-in is far too large to read per request. Its consumers are the same
	// five repositories as ent-spread's, so it answers identically -- but only
	// a plan that seeks can answer it without reading the group, which is what
	// the plan subtest below checks.
	seedCrossRepoDeadCodeProbeFanIn(ctx, t, db, "ent-busy", crossRepoDeadCodeProbeFanInRepositories, 40000)
	// ent-fanout is the axis the walk's stop condition governs: 200 DISTINCT
	// consumer repositories, one row each. Its smallest is repo-x000, which no
	// grant below names, so a walk that stops at the first ungranted repository
	// takes one step for it and a walk that does not takes 200.
	seedCrossRepoDeadCodeProbeFanIn(ctx, t, db, "ent-fanout", crossRepoDeadCodeProbeFanOutRepositories, 1)

	page := []string{
		"ent-spread", "ent-middle", "ent-self", "ent-depth-zero",
		"ent-stale", "ent-absent", "ent-busy",
	}
	reader := NewContentReader(db)

	cases := []struct {
		name  string
		grant []string
		want  []string
	}{
		{name: "every consumer granted", grant: crossRepoDeadCodeProbeFanInRepositories},
		{
			name:  "hidden consumer below the smallest granted id",
			grant: []string{"repo-c", "repo-e", "repo-g", "repo-i"},
			want:  []string{"ent-busy", "ent-spread"},
		},
		{
			name:  "hidden consumer between two granted ids",
			grant: []string{"repo-a", "repo-c", "repo-g", "repo-i"},
			want:  []string{"ent-busy", "ent-middle", "ent-spread"},
		},
		{
			name:  "hidden consumer above the largest granted id",
			grant: []string{"repo-a", "repo-c", "repo-e", "repo-g"},
			want:  []string{"ent-busy", "ent-spread"},
		},
		{
			// One granted id makes both outer ranges and no interior one, and
			// ent-middle -- whose only consumer is that id -- must stay unflagged
			// while ent-spread, which has consumers on both sides of it, does not.
			name:  "single-element grant",
			grant: []string{"repo-e"},
			want:  []string{"ent-busy", "ent-spread"},
		},
		{
			name:  "grant wider than the corpus",
			grant: []string{"repo-a", "repo-c", "repo-e", "repo-g", "repo-i", "repo-producer", "repo-z"},
		},
		{
			name:  "grant disjoint from every consumer",
			grant: []string{"repo-b", "repo-d"},
			want:  []string{"ent-busy", "ent-middle", "ent-spread"},
		},
		{
			name:  "grant naming only the producer repository",
			grant: []string{"repo-producer"},
			want:  []string{"ent-busy", "ent-middle", "ent-spread"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(ctx, "repo-producer", page, testCase.grant)
			if err != nil {
				t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
			}
			got := make([]string, 0, len(hidden))
			for entityID := range hidden {
				got = append(got, entityID)
			}
			sort.Strings(got)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("hidden = %#v, want %#v", got, testCase.want)
			}
			// The range rewrite is only worth anything if it agrees with the
			// NOT IN it replaced. Ask the same question the slow way and
			// require an identical answer, so a bound that drifts by one
			// operator fails here rather than in a tenant's results.
			reference := crossRepoDeadCodeProbeReference(ctx, t, db, "repo-producer", page, testCase.grant)
			if !slices.Equal(got, reference) {
				t.Fatalf("probe = %#v, NOT IN reference = %#v; the ranges are not the complement of the grant", got, reference)
			}
		})
	}

	t.Run("the walk seeks and never scans a group", func(t *testing.T) {
		// This is the property the shape rests on, and no behavioural
		// assertion above can see it: a walk whose per-step lookup lands in a
		// filter instead of an index condition still returns the right
		// entities, and reads the producer entity's whole fan-in to do it.
		//
		// Which index the planner picks is left alone deliberately: this
		// fixture has one scope and one generation, so its primary key serves
		// the same seek that code_reachability_entity_repository_idx serves in
		// a real deployment. The index's presence is asserted separately, and
		// the plan it produces at corpus scale is measured in
		// docs/internal/evidence/5167-code-family-batch-1.md.
		assertCrossRepoDeadCodeProbeIndexExists(ctx, t, db)
		// Both plan modes, because they are not the same question. pgx caches
		// server-side prepared statements, so these reads run on a GENERIC
		// plan in production -- built once with no parameter values -- and the
		// shape this walk replaced planned identically to it under a custom
		// plan and then lost its bounds from the Index Cond under a generic
		// one. A guard that only ever asks the planner with the values in hand
		// cannot see that class of regression at all.
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeProbePlan(
					ctx, t, db, mode, "EXPLAIN ",
					"repo-producer", page, crossRepoDeadCodeProbeFanInRepositories,
				)
				if strings.Contains(plan, "Seq Scan on code_reachability_rows") {
					t.Fatalf("probe fell back to a sequential scan over code_reachability_rows:\n%s", plan)
				}
				stepped := false
				for _, line := range strings.Split(plan, "\n") {
					if !strings.Contains(line, "repository_id > walk") {
						continue
					}
					// A bitmap path splits the same qual across Index Cond and
					// Recheck Cond; both mean the bound reached the index. A
					// Filter does not.
					if !strings.Contains(line, "Index Cond:") && !strings.Contains(line, "Recheck Cond:") {
						t.Fatalf("the walk's step is applied as %q rather than an index condition:\n%s", strings.TrimSpace(line), plan)
					}
					stepped = true
				}
				if !stepped {
					t.Fatalf("no plan node carries the walk's per-step seek; the probe shape has drifted:\n%s", plan)
				}
			})
		}
	})

	t.Run("the walk stops at the first ungranted repository", func(t *testing.T) {
		// The stop condition is a bound on work, not on the answer: dropping it
		// leaves every entity's verdict identical and turns each walk into a
		// full enumeration of that entity's distinct consumer repositories. No
		// assertion on the result can see that, so this one counts the rows the
		// recursive term actually produced.
		//
		// ent-fanout has 200 distinct consumer repositories and its smallest is
		// ungranted, so its walk must be one step. With 250 page entities whose
		// walks are a handful of steps each, the recursive CTE stays in the low
		// hundreds of rows; without the stop condition ent-fanout alone adds
		// about 200.
		// Both plan modes again, for the same reason: the work a generic plan
		// does is the work production does.
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeProbePlan(
					ctx, t, db, mode, "EXPLAIN (ANALYZE) ",
					"repo-producer",
					append(append([]string(nil), page...), "ent-fanout"),
					crossRepoDeadCodeProbeFanInRepositories,
				)
				walkRows := crossRepoDeadCodeProbeWalkRows(t, plan)
				if walkRows > crossRepoDeadCodeProbeWalkRowBudget {
					t.Fatalf("the recursive walk produced %d rows, want at most %d; it is no longer stopping at the first ungranted repository:\n%s",
						walkRows, crossRepoDeadCodeProbeWalkRowBudget, plan)
				}
			})
		}
	})

	// The read this walk replaced cost one index probe per granted repository
	// per producer entity, so a caller with a broad grant paid for the grant
	// rather than for the answer. These grants are the sizes that exposed it:
	// at 500 granted repositories the old shape took 633 ms on the corpus-scale
	// seed against 5.0 ms for this one. Correctness at those sizes is what is
	// asserted here; the timings are in the evidence doc.
	t.Run("a broad grant changes the answer for no entity", func(t *testing.T) {
		broad := make([]string, 0, 500)
		broad = append(broad, crossRepoDeadCodeProbeFanInRepositories...)
		for i := 0; len(broad) < 500; i++ {
			candidate := fmt.Sprintf("repo-pad%04d", i)
			if !slices.Contains(crossRepoDeadCodeProbeFanInRepositories, candidate) {
				broad = append(broad, candidate)
			}
		}
		for _, testCase := range []struct {
			name  string
			grant []string
			want  []string
		}{
			{name: "500 granted, every consumer among them", grant: broad},
			{
				name:  "500 granted, one consumer left out",
				grant: append(append([]string(nil), broad[:2]...), broad[3:]...),
				want:  []string{"ent-busy", "ent-middle", "ent-spread"},
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(ctx, "repo-producer", page, testCase.grant)
				if err != nil {
					t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
				}
				got := make([]string, 0, len(hidden))
				for entityID := range hidden {
					got = append(got, entityID)
				}
				sort.Strings(got)
				if !slices.Equal(got, testCase.want) {
					t.Fatalf("hidden = %#v, want %#v", got, testCase.want)
				}
				reference := crossRepoDeadCodeProbeReference(ctx, t, db, "repo-producer", page, testCase.grant)
				if !slices.Equal(got, reference) {
					t.Fatalf("probe = %#v, NOT IN reference = %#v", got, reference)
				}
			})
		}
	})
}

// assertCrossRepoDeadCodeProbeIndexExists fails when the shipped migration did
// not create the index the probe's walk seeks on at corpus scale.
func assertCrossRepoDeadCodeProbeIndexExists(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	var count int
	if err := db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1",
		"code_reachability_entity_repository_idx",
	).Scan(&count); err != nil {
		t.Fatalf("look up the probe index: %v", err)
	}
	if count != 1 {
		t.Fatalf("code_reachability_entity_repository_idx count = %d, want 1", count)
	}
}

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

	migration, err := os.ReadFile("../storage/postgres/migrations/100_code_reachability_entity_repository_idx.sql")
	if err != nil {
		t.Fatalf("read the shipped index migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, strings.ReplaceAll(string(migration), "CONCURRENTLY ", "")); err != nil {
		t.Fatalf("apply the shipped index migration: %v", err)
	}
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

// crossRepoDeadCodeProbeReference answers the probe's question with the
// unseekable predicate the probe replaced.
func crossRepoDeadCodeProbeReference(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT DISTINCT row.entity_id
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.entity_id = ANY($2)
  AND row.repository_id <> $1
  AND row.depth > 0
  AND NOT (row.repository_id = ANY($3))
ORDER BY row.entity_id`,
		producerRepoID,
		crossRepoDeadCodeProbeTextArray(entityIDs),
		crossRepoDeadCodeProbeTextArray(grantRepositoryIDs),
	)
	if err != nil {
		t.Fatalf("reference query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entities := make([]string, 0, len(entityIDs))
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			t.Fatalf("scan reference row: %v", err)
		}
		entities = append(entities, entityID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reference rows: %v", err)
	}
	return entities
}

// crossRepoDeadCodeProbeWalkRowBudget is the most rows the probe's recursive
// term may produce for this fixture's page plus ent-fanout. The shipped walk
// produces 15; with the stop condition removed it produces 215, because
// ent-fanout's 200 distinct consumer repositories are then all walked. 60 sits
// far enough above the real count to survive fixture edits and far enough
// below the broken one to fail on it.
//
// Measured, not guessed: both numbers came from running this guard with the
// budget temporarily set to 1, once against the shipped statement and once
// against the mutation, which is also proof row 38 needed -- an earlier budget
// of 900 sat above BOTH and would have passed the mutation it exists to catch.
const crossRepoDeadCodeProbeWalkRowBudget = 60

// crossRepoDeadCodeProbeWalkRows reads the row count the recursive walk term
// actually produced out of an EXPLAIN ANALYZE plan.
func crossRepoDeadCodeProbeWalkRows(t *testing.T, plan string) int {
	t.Helper()

	for _, line := range strings.Split(plan, "\n") {
		if !strings.Contains(line, "Recursive Union") {
			continue
		}
		match := crossRepoDeadCodeProbeActualRows.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		rows, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse walk row count from %q: %v", strings.TrimSpace(line), err)
		}
		return rows
	}
	t.Fatalf("no Recursive Union node in the plan; the probe shape has drifted:\n%s", plan)
	return 0
}

var crossRepoDeadCodeProbeActualRows = regexp.MustCompile(`actual time=[0-9.]+\.\.[0-9.]+ rows=(\d+) loops=`)

// crossRepoDeadCodeProbePlanMode is one of the two ways the planner can be
// asked about the probe.
//
// custom passes the values with the statement, which is what a one-shot
// EXPLAIN does. generic prepares the statement and forces a plan built without
// them, which is where pgx's statement cache puts these reads in production.
// The two disagree: the shape this walk replaced planned identically under
// custom and lost its Index Cond under generic, so a plan assertion that only
// runs the first mode proves nothing about what production executes.
type crossRepoDeadCodeProbePlanMode struct {
	name    string
	generic bool
}

var crossRepoDeadCodeProbePlanModes = []crossRepoDeadCodeProbePlanMode{
	{name: "custom plan"},
	{name: "generic plan", generic: true},
}

// crossRepoDeadCodeProbePlan runs the shipped probe under the given EXPLAIN
// prefix and plan mode, and returns the plan as text.
func crossRepoDeadCodeProbePlan(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	mode crossRepoDeadCodeProbePlanMode,
	prefix string,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) string {
	t.Helper()

	statement := prefix + crossRepoDeadCodeUngrantedConsumerProbeQuery
	args := []any{
		producerRepoID,
		crossRepoDeadCodeProbeTextArray(entityIDs),
		crossRepoDeadCodeProbeTextArray(grantRepositoryIDs),
		len(entityIDs),
	}
	if mode.generic {
		statement, args = crossRepoDeadCodeProbeGenericStatement(
			ctx, t, db, prefix, producerRepoID, entityIDs, grantRepositoryIDs,
		)
	}

	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		t.Fatalf("explain probe (%s): %v", mode.name, err)
	}
	defer func() { _ = rows.Close() }()

	plan := strings.Builder{}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	// A generic plan is built without the parameter values, so the producer
	// repository stays a parameter marker in the plan where a custom plan
	// inlines it as a literal. Checking that is what keeps this mode honest: a
	// refactor that quietly stopped forcing the mode would otherwise leave two
	// subtests asking the planner the same question twice.
	if mode.generic && !strings.Contains(plan.String(), "repository_id <> $1") {
		t.Fatalf("plan was not built generically -- the producer repository is not a parameter in it:\n%s", plan.String())
	}
	if !mode.generic && !strings.Contains(plan.String(), "repository_id <> 'repo-producer'::text") {
		t.Fatalf("plan was not built with the values in hand:\n%s", plan.String())
	}
	return plan.String()
}

// crossRepoDeadCodeProbeGenericStatement prepares the probe on the connection
// and forces a generic plan for it, returning an EXPLAIN of the EXECUTE with no
// bind parameters left.
//
// The values are rendered into the EXECUTE rather than bound, because under
// force_generic_plan the plan is already built without them -- which is the
// point. The test pool is pinned to one connection, so the SET, the PREPARE and
// the EXPLAIN all land on the same session; the cleanup puts both back.
func crossRepoDeadCodeProbeGenericStatement(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	prefix string,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) (string, []any) {
	t.Helper()

	name := fmt.Sprintf("cross_repo_dead_code_probe_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force a generic plan: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		"PREPARE "+name+"(text, text[], text[], int) AS "+crossRepoDeadCodeUngrantedConsumerProbeQuery,
	); err != nil {
		t.Fatalf("prepare the probe: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "DEALLOCATE "+name); err != nil {
			t.Errorf("deallocate the probe: %v", err)
		}
		if _, err := db.ExecContext(cleanupCtx, "RESET plan_cache_mode"); err != nil {
			t.Errorf("reset plan_cache_mode: %v", err)
		}
	})
	return fmt.Sprintf(
		"%sEXECUTE %s(%s, %s, %s, %d)",
		prefix,
		name,
		quoteLiteral(producerRepoID),
		quoteLiteral(crossRepoDeadCodeProbeTextArray(entityIDs)),
		quoteLiteral(crossRepoDeadCodeProbeTextArray(grantRepositoryIDs)),
		len(entityIDs),
	), nil
}

// quoteLiteral renders a SQL string literal for the EXECUTE above. The values
// are test-owned entity and repository ids, never caller input.
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// crossRepoDeadCodeProbeTextArray renders a Postgres text[] literal for the
// helper statements above. The probe itself binds pgarray.Array; this exists so
// the reference query and the EXPLAIN take the same values without depending on
// the encoder under test.
func crossRepoDeadCodeProbeTextArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, `"`+strings.ReplaceAll(value, `"`, `\"`)+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

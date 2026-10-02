// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// The wide arm of TestCrossRepoDeadCodeConsumerEvidencePageBoundLive: a page of
// 250 producer entities against statistics shaped like the QA replica's -- many
// ingestion scopes, many retained generations, tens of thousands of producer
// entities with a handful of rows each -- plus one busy entity consumed by every
// scope. #7249 measured the join form this statement replaced flipping, at 250
// page entities on those statistics, to probing the primary key once per active
// ingestion scope. This arm holds the unscoped read's lateral to the bounded
// shape there, under both plan modes and the plan cache's own choice, compares
// its rows and their order against the retired statement, and shows that with a
// grant bound the two shapes would still agree row for row.

const (
	// crossRepoDeadCodeConsumerWideScopes is the arm's ingestion scope count,
	// one consumer repository each. The QA replica had 819 active scopes when
	// #7249 measured it; 1,500 also puts the busy entity past the page cap.
	crossRepoDeadCodeConsumerWideScopes = 1500
	// crossRepoDeadCodeConsumerWideRetained is how many superseded generations
	// each scope keeps -- DefaultGenerationRetentionPolicy's floor.
	crossRepoDeadCodeConsumerWideRetained = 24
	// crossRepoDeadCodeConsumerWideEntities is the producer entity population.
	// Entity n is consumed by one repository under 1 + n%9 generations, so the
	// average entity has about five rows, as on the replica.
	crossRepoDeadCodeConsumerWideEntities = 40000
	// crossRepoDeadCodeConsumerWideOrdinaryRetained is the most superseded
	// generations an ordinary entity's position carries (n%9).
	crossRepoDeadCodeConsumerWideOrdinaryRetained = 8
	// crossRepoDeadCodeConsumerWidePageOrdinary is how many ordinary entities
	// share the 250-entity page with the busy one.
	crossRepoDeadCodeConsumerWidePageOrdinary = 249
)

// crossRepoDeadCodeConsumerWideBudget bounds the ranking walk on the wide page:
// the busy entity's cap+1 active rows times the generations each position
// keeps, plus every ordinary entity's full row set, plus slack. The floor says
// the walk did pass retained generations, so the arm is measuring them.
var crossRepoDeadCodeConsumerWideBudget = crossRepoDeadCodeConsumerPageWorkBudget{
	ceiling: float64((maxCrossRepoDeadCodeConsumerEvidenceRows+1)*(1+crossRepoDeadCodeConsumerWideRetained) +
		crossRepoDeadCodeConsumerWidePageOrdinary*(1+crossRepoDeadCodeConsumerWideOrdinaryRetained) + 200),
	floor: float64((maxCrossRepoDeadCodeConsumerEvidenceRows + 1) * 2),
}

// runCrossRepoDeadCodeConsumerPageWideArm seeds the wide fixture into the
// current schema and runs its guards.
func runCrossRepoDeadCodeConsumerPageWideArm(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	seedCrossRepoDeadCodeConsumerWideRows(ctx, t, db)
	ordinary := make([]string, 0, crossRepoDeadCodeConsumerWidePageOrdinary)
	for n := 1000; n < 1000+crossRepoDeadCodeConsumerWidePageOrdinary; n++ {
		ordinary = append(ordinary, fmt.Sprintf("ent-%06d", n))
	}
	page := append(append([]string{}, ordinary...), "ent-hot")
	grants := map[string][]string{}
	for r := 0; r < crossRepoDeadCodeConsumerWideScopes; r++ {
		repository := fmt.Sprintf("repo-%04d", r)
		grants["every repository"] = append(grants["every repository"], repository)
		if r < 3 {
			grants["three repositories"] = append(grants["three repositories"], repository)
		}
	}

	t.Run("a 250-entity unscoped page on replica-shaped statistics stays page-bound", func(t *testing.T) {
		runCrossRepoDeadCodeConsumerPageWorkGuard(ctx, t, db, page, nil, crossRepoDeadCodeConsumerWideBudget)
		runCrossRepoDeadCodeConsumerPageCachedPlanGuard(ctx, t, db, page, nil, crossRepoDeadCodeConsumerWideBudget)
	})
	t.Run("unscoped rows and order match the retired join statement", func(t *testing.T) {
		for _, read := range []struct {
			name string
			page []string
			want int
		}{
			{name: "busy entity, past the cap", page: page, want: maxCrossRepoDeadCodeConsumerEvidenceRows + 1},
			{name: "ordinary entities only, nothing truncated", page: ordinary, want: len(ordinary)},
		} {
			t.Run(read.name, func(t *testing.T) {
				query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery("repo-producer", read.page, nil)
				oracle, oracleArgs := crossRepoDeadCodeConsumerRetiredJoinQuery("repo-producer", read.page, nil)
				crossRepoDeadCodeConsumerWideCompare(ctx, t, db, query, args, oracle, oracleArgs, read.want)
			})
		}
	})
	// A grant-bound read does not take the lateral -- the builder keeps the flat
	// statement for it, pinned by TestCrossRepoDeadCodeGrantBoundPageKeepsTheShippedStatement.
	// This proves the two shapes would still return the same rows in the same
	// order with the grant bound, so routing grant-bound reads through the
	// lateral later is a cost question, never a correctness one.
	t.Run("grant-bound rows and order match between the two shapes", func(t *testing.T) {
		for _, name := range []string{"three repositories", "every repository"} {
			t.Run(name, func(t *testing.T) {
				want := 0
				if name == "every repository" {
					want = maxCrossRepoDeadCodeConsumerEvidenceRows + 1
				}
				query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery("repo-producer", page, grants[name])
				oracle, _ := crossRepoDeadCodeConsumerRetiredJoinQuery("repo-producer", page, grants[name])
				if query != oracle {
					t.Fatalf("the grant-bound page is no longer the retired join statement:\n%s", query)
				}
				lateral := strings.Replace(crossRepoDeadCodeConsumerEvidenceLateralQuery,
					"\n    AND row.depth > 0\n", "\n    AND row.depth > 0\n    AND row.repository_id = ANY($3)\n", 1)
				if lateral == crossRepoDeadCodeConsumerEvidenceLateralQuery {
					t.Fatalf("could not bind the grant into the lateral; its depth predicate moved:\n%s", lateral)
				}
				lateralArgs := []any{"repo-producer", array.Of(page), array.Of(grants[name])}
				crossRepoDeadCodeConsumerWideCompare(ctx, t, db, lateral, lateralArgs, query, args, want)
			})
		}
	})
}

// crossRepoDeadCodeConsumerWideCompare runs a statement and its oracle and
// requires the same rows in the same positions. want, when non-zero, pins the
// oracle's row count, so a fixture that drifted cannot pass by returning
// nothing from both.
func crossRepoDeadCodeConsumerWideCompare(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	query string,
	args []any,
	oracle string,
	oracleArgs []any,
	want int,
) {
	t.Helper()

	got := crossRepoDeadCodeConsumerWideRows(ctx, t, db, query, args)
	expected := crossRepoDeadCodeConsumerWideRows(ctx, t, db, oracle, oracleArgs)
	if len(expected) == 0 {
		t.Fatalf("the oracle returned no rows; the comparison proves nothing")
	}
	if want > 0 && len(expected) != want {
		t.Fatalf("the oracle returned %d rows, want %d; the fixture drifted", len(expected), want)
	}
	if len(got) != len(expected) {
		t.Fatalf("the statement returned %d rows, the oracle %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("row %d differs:\n got  %s\n want %s", i+1, got[i], expected[i])
		}
	}
}

// crossRepoDeadCodeConsumerWideRows runs one statement and renders each row as
// one comparable string, in the order the statement returned them.
func crossRepoDeadCodeConsumerWideRows(ctx context.Context, t *testing.T, db *sql.DB, query string, args []any) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("run the page statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("read the page statement's columns: %v", err)
	}
	var rendered []string
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatalf("scan a page row: %v", err)
		}
		parts := make([]string, 0, len(values))
		for _, value := range values {
			if raw, ok := value.([]byte); ok {
				value = string(raw)
			}
			parts = append(parts, fmt.Sprint(value))
		}
		rendered = append(rendered, strings.Join(parts, " | "))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("page rows: %v", err)
	}
	return rendered
}

// crossRepoDeadCodeConsumerRetiredJoinQuery is the RETIRED evidence page -- the
// statement every read used before #7249 -- frozen here as an oracle and
// baseline only. The unscoped read must agree with it row for row and position
// for position; the grant-bound read still IS it.
func crossRepoDeadCodeConsumerRetiredJoinQuery(producerRepoID string, entityIDs []string, grant []string) (string, []any) {
	args := []any{producerRepoID}
	placeholders := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	grantFilter := ""
	if len(grant) > 0 {
		args = append(args, array.Of(grant))
		grantFilter = fmt.Sprintf("\n  AND row.repository_id = ANY($%d)", len(args))
	}
	return `
SELECT row.entity_id,
       row.repository_id,
       '' AS consumer_repo_name,
       row.root_entity_id,
       row.depth,
       row.state,
       row.confidence,
       row.min_resolution_method,
       row.evidence,
       row.root_kinds,
       row.generation_id,
       generation.status AS generation_status,
       row.observed_at,
       row.updated_at
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.repository_id <> $1
  AND row.entity_id IN (` + strings.Join(placeholders, ", ") + `)
  AND row.depth > 0` + grantFilter + `
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC,
         row.repository_id ASC, row.root_entity_id ASC,
         row.scope_id ASC, row.generation_id ASC
LIMIT ` + fmt.Sprint(maxCrossRepoDeadCodeConsumerEvidenceRows+1) + "\n", args
}

// seedCrossRepoDeadCodeConsumerWideRows seeds the wide fixture. Rows go in
// generation by generation, oldest first, and scope by scope inside each -- the
// order the reducer's per-snapshot replacements write them -- because the
// planner reads entity_id's physical correlation, and a table seeded in
// entity order plans as no install's table does.
func seedCrossRepoDeadCodeConsumerWideRows(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	for _, statement := range []string{
		`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
SELECT 'scope-' || lpad(r::text, 4, '0'), 'repository', 'git', 'key-' || r, 'code', 'p-' || r,
       now(), now(), 'active', 'gen-' || lpad(r::text, 4, '0') || '-00'
FROM generate_series(0, $1 - 1) AS r`,
		`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen-' || lpad(r::text, 4, '0') || '-' || lpad(g::text, 2, '0'), 'scope-' || lpad(r::text, 4, '0'),
       'sync', now(), now(), CASE WHEN g = 0 THEN 'active' ELSE 'superseded' END, now()
FROM generate_series(0, $1 - 1) AS r CROSS JOIN generate_series(0, $2) AS g`,
		`INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-' || lpad(r::text, 4, '0'), 'gen-' || lpad(r::text, 4, '0') || '-' || lpad(g::text, 2, '0'),
       'repo-' || lpad(r::text, 4, '0'), root, entity, depth, 'reachable', confidence,
       'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM (
  SELECT n % $1 AS r, g, 'repo-' || lpad((n % $1)::text, 4, '0') || '#root-' || (n % 97) AS root,
         'ent-' || lpad(n::text, 6, '0') AS entity, 1 + (n % 3) AS depth,
         (ARRAY[0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0])[1 + (n % 7)] AS confidence
  FROM generate_series(1, $3) AS n CROSS JOIN LATERAL generate_series(0, n % 9) AS g
  UNION ALL
  SELECT r, g, 'repo-' || lpad(r::text, 4, '0') || '#main', 'ent-hot', 1 + (r % 3), 0.95
  FROM generate_series(0, $1 - 1) AS r CROSS JOIN generate_series(0, $2) AS g
) AS seed
ORDER BY g DESC, r, depth, entity`,
	} {
		args := []any{crossRepoDeadCodeConsumerWideScopes}
		if strings.Contains(statement, "$2") {
			args = append(args, crossRepoDeadCodeConsumerWideRetained)
		}
		if strings.Contains(statement, "$3") {
			args = append(args, crossRepoDeadCodeConsumerWideEntities)
		}
		if _, err := db.ExecContext(ctx, statement, args...); err != nil {
			t.Fatalf("seed the wide fixture: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, "VACUUM ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("vacuum the wide fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE ingestion_scopes; ANALYZE scope_generations"); err != nil {
		t.Fatalf("analyze the wide fixture: %v", err)
	}
}

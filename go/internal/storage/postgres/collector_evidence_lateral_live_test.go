// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10601 differential proof that the lateral collector-instance lookup returns rows identical to the pre-change DISTINCT ON join over blank-latest fallback, updated_at tiebreak, empty-instance, and superseded-generation branches; one focused test passed in about 2s using an isolated schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// preChange10601ScopedInstanceBody is the pre-#10601 instance-identity leg of
// collectorFactEvidenceQuery, kept verbatim as the differential oracle for the
// #10601 rewrite. The rewrite swaps only this leg (DISTINCT ON over the joined
// set, re-scanned per summary row) for the scoped lateral lookup; the
// active_scopes CTE, the summary join, and the GROUP/ORDER/LIMIT aggregate are
// derived from the shipped query constant, so they can never drift from it.
const preChange10601ScopedInstanceBody = `SELECT DISTINCT ON (
    workflow_item.collector_kind,
    workflow_item.scope_id,
    workflow_item.generation_id
)
    workflow_item.collector_kind,
    workflow_item.scope_id,
    workflow_item.generation_id,
    workflow_item.collector_instance_id
FROM workflow_work_items AS workflow_item
JOIN active_scopes AS scope
  ON scope.collector_kind = workflow_item.collector_kind
 AND scope.scope_id = workflow_item.scope_id
 AND scope.generation_id = workflow_item.generation_id
WHERE NULLIF(BTRIM(workflow_item.collector_instance_id), '') IS NOT NULL
ORDER BY
    workflow_item.collector_kind,
    workflow_item.scope_id,
    workflow_item.generation_id,
    workflow_item.updated_at DESC,
    workflow_item.work_item_id ASC
`

// preChange10601Query returns collectorFactEvidenceQuery with the #10601
// inline lateral leg swapped back to the pre-change DISTINCT ON leg (the
// workflow_instances MATERIALIZED CTE plus its LEFT JOIN). It fails the
// test when a swap anchor does not appear exactly once, so the oracle can
// never silently equal the query it is checking.
func preChange10601Query(t *testing.T, query string) string {
	t.Helper()
	const lateralHead = "LEFT JOIN LATERAL ("
	const lateralTail = ") AS item ON TRUE"
	if strings.Count(query, lateralHead) != 1 {
		t.Fatalf("lateral leg head appears %d times, want 1", strings.Count(query, lateralHead))
	}
	if strings.Count(query, lateralTail) != 1 {
		t.Fatalf("lateral leg tail appears %d times, want 1", strings.Count(query, lateralTail))
	}
	start := strings.Index(query, lateralHead)
	end := strings.Index(query, lateralTail) + len(lateralTail)
	const join = `LEFT JOIN workflow_instances AS item
  ON item.collector_kind = scope.collector_kind
 AND item.scope_id = scope.scope_id
 AND item.generation_id = scope.generation_id`
	old := query[:start] + join + query[end:]
	const cteAnchor = "AND generation.status = 'active'\n)"
	if strings.Count(old, cteAnchor) != 1 {
		t.Fatalf("active scopes CTE tail appears %d times, want 1", strings.Count(old, cteAnchor))
	}
	return strings.Replace(old, cteAnchor,
		cteAnchor+",\nworkflow_instances AS MATERIALIZED (\n"+preChange10601ScopedInstanceBody+"\n)", 1)
}

// TestCollectorFactEvidenceLateralMatchesPreChangeJoinLive is the #10601
// differential: over a fixture covering every branch of the instance-identity
// leg, the rewritten query must return exactly the rows the pre-change join
// returned.
//
// Fixture, by construction (all under collector kinds the active_scopes CTE
// admits):
//   - c-git/g1: latest work item has a blank instance id, so the leg must fall
//     back to the newest non-blank row (WHERE applies before DISTINCT ON);
//     two rows tie on updated_at, so work_item_id ASC breaks the tie.
//   - c-git-blank/gb1: every work item has a blank instance id -> empty
//     instance group.
//   - c-git-empty/ge1: no work items at all -> empty instance group.
//   - c-git-stale/gs1: work items exist only on the superseded generation ->
//     excluded from the active set -> empty instance group.
//   - c-aws/a1: a second collector kind with two evidence sources and two
//     source systems, exercising the summary fanout the old plan multiplied
//     the CTE scan by.
func TestCollectorFactEvidenceLateralMatchesPreChangeJoinLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #10601 collector evidence differential")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedCollectorEvidenceDifferentialFixture(ctx, t, conn)

	run := func(query string) []string {
		t.Helper()
		rows, err := conn.QueryContext(ctx, query)
		if err != nil {
			t.Fatalf("collector evidence query: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var kind, inst, source, systems string
			var count int64
			var observed, ingested time.Time
			if err := rows.Scan(&kind, &inst, &source, &systems, &count, &observed, &ingested); err != nil {
				t.Fatalf("scan collector evidence row: %v", err)
			}
			out = append(out, fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s",
				kind, inst, source, systems,
				count, observed.UTC().Format(time.RFC3339), ingested.UTC().Format(time.RFC3339)))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate collector evidence rows: %v", err)
		}
		return out
	}

	got := run(collectorFactEvidenceQuery)
	want := run(preChange10601Query(t, collectorFactEvidenceQuery))
	if !slices.Equal(got, want) {
		t.Fatalf("lateral rows differ from the pre-change join:\n got:  %v\n want: %v", got, want)
	}
	// The fixture must actually exercise the leg: the blank-latest fallback,
	// the updated_at tiebreak, and the empty-instance groups all appear.
	for _, row := range []string{
		"git|inst-b|source_facts|{git}|",
		"git|inst-x|source_facts|{git}|",
		"git||source_facts|{git}|",
		"aws|inst-aws|reducer_facts|{aws}|",
	} {
		found := false
		for _, actual := range got {
			if strings.HasPrefix(actual, row) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("lateral rows %v missing %q", got, row)
		}
	}
	if len(got) == 0 {
		t.Fatal("lateral query returned no rows; the fixture seeds none of the summary")
	}
}

// seedCollectorEvidenceDifferentialFixture seeds the scopes, generations,
// summary rows, and work items
// TestCollectorFactEvidenceLateralMatchesPreChangeJoinLive covers.
func seedCollectorEvidenceDifferentialFixture(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	at := func(offset time.Duration) time.Time { return statusSemanticsAsOf.Add(offset) }
	exec(`INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ('run-10601', 'schedule', 'running', $1, $1)`, at(-time.Hour))
	for _, scope := range []struct{ id, kind, active string }{
		{"c-git", "git", "g1"},
		{"c-git-tie", "git", "gt1"},
		{"c-git-blank", "git", "gb1"},
		{"c-git-empty", "git", "ge1"},
		{"c-git-stale", "git", "gs-active"},
		{"c-aws", "aws", "a1"},
	} {
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, $2, $1, $3, $3, 'active', $4, '{}'::jsonb)`,
			scope.id, scope.kind, at(-3*time.Hour), scope.active)
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
  ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'active', '{}'::jsonb)`, scope.active, scope.id, at(-3*time.Hour))
	}
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
  ingested_at, status, payload)
VALUES ('gs-stale', 'c-git-stale', 'snapshot', $1, $1, 'superseded', '{}'::jsonb)`, at(-4*time.Hour))
	for _, summary := range []struct {
		scope, gen, kind, source, system string
		count                            int64
	}{
		{"c-git", "g1", "git", "source_facts", "git", 5},
		{"c-git-tie", "gt1", "git", "source_facts", "git", 3},
		{"c-git-blank", "gb1", "git", "source_facts", "git", 2},
		{"c-git-empty", "ge1", "git", "source_facts", "git", 1},
		{"c-git-stale", "gs-active", "git", "source_facts", "git", 4},
		{"c-aws", "a1", "aws", "source_facts", "aws", 7},
		{"c-aws", "a1", "aws", "reducer_facts", "aws", 9},
	} {
		exec(`INSERT INTO collector_evidence_summary (scope_id, generation_id, collector_kind,
  evidence_source, source_system, observation_count, last_observed_at, last_ingested_at, materialized_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $7)`,
			summary.scope, summary.gen, summary.kind, summary.source, summary.system,
			summary.count, at(-time.Hour))
	}
	workItem := func(id, kind, scope, gen, inst string, updated time.Duration) {
		t.Helper()
		exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind,
  collector_instance_id, source_system, scope_id, acceptance_unit_id, source_run_id,
  generation_id, status, created_at, updated_at)
VALUES ($1, 'run-10601', $2, $3, 'git', $4, 'au', 'run-10601', $5, 'completed', $6, $6)`,
			id, kind, inst, scope, gen, at(updated))
	}
	// c-git/g1: blank latest falls back to inst-b; inst-a is older.
	workItem("w-10601-old", "git", "c-git", "g1", "inst-a", -3*time.Hour)
	workItem("w-10601-mid", "git", "c-git", "g1", "inst-b", -2*time.Hour)
	workItem("w-10601-blank", "git", "c-git", "g1", "  ", -time.Hour)
	// c-git-tie/gt1: updated_at tie broken by work_item_id ASC.
	workItem("w-10601-tie-1", "git", "c-git-tie", "gt1", "inst-x", -2*time.Hour)
	workItem("w-10601-tie-2", "git", "c-git-tie", "gt1", "inst-y", -2*time.Hour)
	// c-git-blank/gb1: only blank instance ids.
	workItem("w-10601-blank-1", "git", "c-git-blank", "gb1", "", -2*time.Hour)
	workItem("w-10601-blank-2", "git", "c-git-blank", "gb1", "   ", -time.Hour)
	// c-git-stale: items only on the superseded generation.
	workItem("w-10601-stale-1", "git", "c-git-stale", "gs-stale", "inst-stale", -time.Hour)
	// c-aws/a1: ordinary non-blank instance.
	workItem("w-10601-aws-1", "aws", "c-aws", "a1", "inst-aws", -2*time.Hour)
}

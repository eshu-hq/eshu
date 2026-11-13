// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10601 (reopened) differential proof that the leg1 summary read returns rows identical to the per-scope lateral instance lookup over blank-latest fallback, updated_at tiebreak, empty-instance, superseded-generation, tombstone, and blank-source-system branches; the summary rows are built by the shipped resweep from fact_records, so the proof covers the maintainer path too; one focused test passed in about 2s using an isolated schema
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

// summaryRead10601InstanceLegBody is the per-scope collector-instance leg the
// #10601 leg1 summary read (reopened) moved from the snapshot read into the
// resweep. It is frozen here as the differential oracle leg: the test rebuilds
// the pre-summary-read query by deriving it from the shipped
// collectorFactEvidenceQuery constant (see lateral10601SummaryReadQuery),
// so the oracle can never silently equal the query it checks.
const summaryRead10601InstanceLegBody = `SELECT workflow_item.collector_instance_id
FROM workflow_work_items AS workflow_item
WHERE workflow_item.collector_kind = scope.collector_kind
  AND workflow_item.scope_id = scope.scope_id
  AND workflow_item.generation_id = scope.generation_id
  AND NULLIF(BTRIM(workflow_item.collector_instance_id), '') IS NOT NULL
ORDER BY workflow_item.updated_at DESC, workflow_item.work_item_id ASC
LIMIT 1`

// lateral10601SummaryReadQuery returns collectorFactEvidenceQuery with the
// resweep-maintained summary.collector_instance_id leg swapped back to the
// per-scope lateral lookup it replaced. Every anchor must appear exactly
// once, so the oracle can never silently equal the query it checks.
func lateral10601SummaryReadQuery(t *testing.T, query string) string {
	t.Helper()
	const selectAnchor = "    summary.collector_instance_id,\n"
	if strings.Count(query, selectAnchor) != 1 {
		t.Fatalf("summary instance select appears %d times, want 1", strings.Count(query, selectAnchor))
	}
	const joinAnchor = "JOIN collector_evidence_summary AS summary"
	if strings.Count(query, joinAnchor) != 1 {
		t.Fatalf("summary join appears %d times, want 1", strings.Count(query, joinAnchor))
	}
	const groupAnchor = "GROUP BY summary.collector_kind, summary.collector_instance_id, summary.evidence_source"
	if strings.Count(query, groupAnchor) != 1 {
		t.Fatalf("summary group-by appears %d times, want 1", strings.Count(query, groupAnchor))
	}
	const orderAnchor = "ORDER BY summary.collector_kind ASC, summary.collector_instance_id ASC, summary.evidence_source ASC"
	if strings.Count(query, orderAnchor) != 1 {
		t.Fatalf("summary order-by appears %d times, want 1", strings.Count(query, orderAnchor))
	}
	// GROUP BY/ORDER BY repeat the normalized lateral expression instead of the
	// bare output alias the pre-summary query used: the alias is ambiguous now
	// that the summary table owns a collector_instance_id column of its own,
	// and grouping by the raw item value would split NULL/blank identities
	// the SELECT normalizes to the empty string.
	out := strings.Replace(query, selectAnchor,
		"    COALESCE(NULLIF(BTRIM(item.collector_instance_id), ''), '') AS collector_instance_id,\n", 1)
	out = strings.Replace(out, joinAnchor,
		"LEFT JOIN LATERAL (\n"+summaryRead10601InstanceLegBody+"\n) AS item ON TRUE\n"+joinAnchor, 1)
	out = strings.Replace(out, groupAnchor,
		"GROUP BY summary.collector_kind, COALESCE(NULLIF(BTRIM(item.collector_instance_id), ''), ''), summary.evidence_source", 1)
	out = strings.Replace(out, orderAnchor,
		"ORDER BY summary.collector_kind ASC, COALESCE(NULLIF(BTRIM(item.collector_instance_id), ''), '') ASC, summary.evidence_source ASC", 1)
	return out
}

// TestCollectorFactEvidenceSummaryReadMatchesLateralLive is the #10601
// (reopened) differential: over a fixture covering every branch of the
// instance-identity leg and the resweep aggregate, the summary read (after
// the shipped resweep materializes the summary from fact_records) must
// return exactly the rows the per-scope lateral lookup returned.
//
// Fixture, by construction (all under collector kinds the active_scopes CTE
// admits):
//   - c-git/g1: latest work item has a blank instance id, so the leg must
//     fall back to the newest non-blank row; one fact is tombstoned (excluded
//     from the count) and one reducer fact carries a blank source system.
//   - c-git-tie/gt1: two work items tie on updated_at, so work_item_id ASC
//     breaks the tie.
//   - c-git-blank/gb1: every work item has a blank instance id -> empty
//     instance group.
//   - c-git-empty/ge1: no work items at all -> empty instance group.
//   - c-git-stale/gs-active: work items exist only on the superseded
//     generation, and a fact with a marker source system exists only on the
//     superseded generation -> both excluded.
//   - c-aws/a1: a second collector kind with two evidence sources,
//     exercising the summary fanout the read aggregates.
func TestCollectorFactEvidenceSummaryReadMatchesLateralLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #10601 leg1 summary-read differential")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedCollectorEvidenceSummaryReadFixture(ctx, t, conn)
	if _, err := conn.ExecContext(ctx, rebuildCollectorEvidenceSummarySQL, statusSemanticsAsOf); err != nil {
		t.Fatalf("resweep collector evidence summary: %v", err)
	}

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

	if strings.Contains(collectorFactEvidenceQuery, "workflow_work_items") {
		t.Fatalf("shipped collector evidence query still probes workflow_work_items:\n%s", collectorFactEvidenceQuery)
	}
	oracle := lateral10601SummaryReadQuery(t, collectorFactEvidenceQuery)
	if !strings.Contains(oracle, "workflow_work_items") {
		t.Fatal("derived lateral oracle lost the workflow_work_items probe; the derivation is broken")
	}
	got := run(collectorFactEvidenceQuery)
	want := run(oracle)
	if !slices.Equal(got, want) {
		t.Fatalf("summary rows differ from the lateral lookup:\n got:  %v\n want: %v", got, want)
	}
	// The fixture must actually exercise the leg: the blank-latest fallback,
	// the updated_at tiebreak, the empty-instance groups, and the
	// superseded-generation exclusion all appear.
	for _, row := range []string{
		"git|inst-edge-b|source_facts|{git}|2|",
		"git|inst-edge-b|reducer_facts|{}|1|",
		"git|inst-tie-x|source_facts|{git}|1|",
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
			t.Fatalf("summary rows %v missing %q", got, row)
		}
	}
	for _, forbidden := range []string{"inst-stale", "stale-sys"} {
		for _, actual := range got {
			if strings.Contains(actual, forbidden) {
				t.Fatalf("summary rows %v contain superseded-generation %q", got, forbidden)
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("summary query returned no rows; the resweep seeded none of the summary")
	}
}

// seedCollectorEvidenceSummaryReadFixture seeds the scopes, generations,
// facts, and work items
// TestCollectorFactEvidenceSummaryReadMatchesLateralLive covers. Summary
// rows are NOT seeded: the test runs the shipped resweep to build them.
func seedCollectorEvidenceSummaryReadFixture(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	at := func(offset time.Duration) time.Time { return statusSemanticsAsOf.Add(offset) }
	exec(`INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ('run-10601-sum', 'schedule', 'running', $1, $1)`, at(-time.Hour))
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
	fact := func(id, scope, gen, kind, stableKey, system, factKey string, tombstone bool, observed, ingested time.Duration) {
		t.Helper()
		exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind,
  stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, scope, gen, kind, stableKey, system, factKey, at(observed), at(ingested), tombstone)
	}
	// c-git/g1: two live source facts, one tombstoned source fact (excluded),
	// one reducer fact with a blank source system.
	fact("f-sum-g1-s1", "c-git", "g1", "source_observed", "sk-g1-s1", "git", "fk-g1-s1", false, -2*time.Hour, -90*time.Minute)
	fact("f-sum-g1-s2", "c-git", "g1", "source_observed", "sk-g1-s2", "git", "fk-g1-s2", false, -2*time.Hour, -80*time.Minute)
	fact("f-sum-g1-tomb", "c-git", "g1", "source_observed", "sk-g1-tomb", "git", "fk-g1-tomb", true, -time.Hour, -70*time.Minute)
	fact("f-sum-g1-r1", "c-git", "g1", "reducer_derived", "rk-g1-r1", "", "fk-g1-r1", false, -2*time.Hour, -60*time.Minute)
	fact("f-sum-tie-1", "c-git-tie", "gt1", "source_observed", "sk-tie-1", "git", "fk-tie-1", false, -2*time.Hour, -90*time.Minute)
	fact("f-sum-blank-1", "c-git-blank", "gb1", "source_observed", "sk-blank-1", "git", "fk-blank-1", false, -2*time.Hour, -90*time.Minute)
	fact("f-sum-blank-2", "c-git-blank", "gb1", "source_observed", "sk-blank-2", "git", "fk-blank-2", false, -2*time.Hour, -80*time.Minute)
	fact("f-sum-empty-1", "c-git-empty", "ge1", "source_observed", "sk-empty-1", "git", "fk-empty-1", false, -2*time.Hour, -90*time.Minute)
	fact("f-sum-stale-active-1", "c-git-stale", "gs-active", "source_observed", "sk-stale-a1", "git", "fk-stale-a1", false, -2*time.Hour, -90*time.Minute)
	fact("f-sum-stale-old-1", "c-git-stale", "gs-stale", "source_observed", "sk-stale-o1", "stale-sys", "fk-stale-o1", false, -5*time.Hour, -5*time.Hour)
	fact("f-sum-aws-s1", "c-aws", "a1", "source_observed", "sk-aws-s1", "aws", "fk-aws-s1", false, -2*time.Hour, -90*time.Minute)
	fact("f-sum-aws-r1", "c-aws", "a1", "reducer_derived", "rk-aws-r1", "aws", "fk-aws-r1", false, -2*time.Hour, -60*time.Minute)
	workItem := func(id, kind, scope, gen, inst string, updated time.Duration) {
		t.Helper()
		exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind,
  collector_instance_id, source_system, scope_id, acceptance_unit_id, source_run_id,
  generation_id, status, created_at, updated_at)
VALUES ($1, 'run-10601-sum', $2, $3, 'git', $4, 'au', 'run-10601-sum', $5, 'completed', $6, $6)`,
			id, kind, inst, scope, gen, at(updated))
	}
	// c-git/g1: blank latest falls back to inst-edge-b; inst-edge-a is older.
	workItem("w-sum-old", "git", "c-git", "g1", "inst-edge-a", -3*time.Hour)
	workItem("w-sum-mid", "git", "c-git", "g1", "inst-edge-b", -2*time.Hour)
	workItem("w-sum-blank", "git", "c-git", "g1", "  ", -time.Hour)
	// c-git-tie/gt1: updated_at tie broken by work_item_id ASC.
	workItem("w-sum-tie-1", "git", "c-git-tie", "gt1", "inst-tie-x", -2*time.Hour)
	workItem("w-sum-tie-2", "git", "c-git-tie", "gt1", "inst-tie-y", -2*time.Hour)
	// c-git-blank/gb1: only blank instance ids.
	workItem("w-sum-blank-1", "git", "c-git-blank", "gb1", "", -2*time.Hour)
	workItem("w-sum-blank-2", "git", "c-git-blank", "gb1", "   ", -time.Hour)
	// c-git-stale: items only on the superseded generation.
	workItem("w-sum-stale-1", "git", "c-git-stale", "gs-stale", "inst-stale", -time.Hour)
	// c-aws/a1: ordinary non-blank instance.
	workItem("w-sum-aws-1", "aws", "c-aws", "a1", "inst-aws", -2*time.Hour)
}

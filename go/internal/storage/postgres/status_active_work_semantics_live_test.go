// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// statusSemanticsAsOf is the fixed status clock for the #6794 semantics
// fixture; every seeded timestamp is relative to it.
var statusSemanticsAsOf = time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

// TestStatusActiveWorkQueriesPreserveSemantics is the #6794 output-parity
// proof for the status queries that embed activeFactWorkItemsCTE and for the
// shared-projection half of domainBacklogQuery. The expected rows are derived
// by hand from the fixture intent below (not from a frozen copy of any SQL),
// and apart from the lease-only phantom count below it passes unmodified on
// the pre-#6794 query shapes, so it pins the observable contract across the
// rewrite:
//
//   - unleased reducer rows on an older generation than the scope's active
//     generation are hidden (older ingested_at, or equal ingested_at with a
//     smaller generation_id); claimed, succeeded, and non-reducer rows on that
//     older generation stay visible;
//   - scopes with no active pointer, a pointer to a missing generation, or a
//     pointer to another scope's generation, and generations newer than the
//     active one keep their rows;
//   - shared projection domains appear only with pending intents or a live
//     lease; a live-lease domain with no intent rows at all reports zero
//     outstanding intents. The pre-#6794 LEFT JOIN counted its null-extended
//     row as one phantom pending intent; #6794 fixes that on purpose.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestStatusActiveWorkQueriesPreserveSemantics(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 status semantics proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)

	cases := []struct {
		name  string
		query string
		args  []any
		want  []string
	}{
		{
			name:  "stage counts",
			query: stageCountsQuery,
			// reducer|succeeded counts 2: the fixture's w-old-succeeded plus
			// the standing eshu:global code_value_flow_refresh singleton
			// migration 115 seeds on every migrated database.
			want: []string{
				"projector|pending|1",
				"reducer|claimed|2",
				"reducer|failed|1",
				"reducer|pending|4",
				"reducer|retrying|1",
				"reducer|succeeded|2",
				"reducer|superseded|1",
			},
		},
		{
			name:  "queue snapshot",
			query: queueSnapshotQuery,
			args:  []any{statusSemanticsAsOf},
			// total counts every row, hidden stale rows included. The
			// bootstrap records the 096 provenance upgrade marker, and
			// migration 115 adds the one standing succeeded refresh row; this fixture also adds one superseded
			// row (total 15, succeeded 2).
			want: []string{"15|8|5|2|1|2|0|1|true|0|1800|1"},
		},
		{
			name:  "domain backlog",
			query: domainBacklogQuery,
			args:  []any{statusSemanticsAsOf},
			want: []string{
				"done|3|1|0|0|0|10800",
				"shared|2|0|0|0|0|7200",
				"expired|1|0|0|0|0|14400",
				"d2|1|0|1|0|0|1800",
				"d3|1|0|0|0|1|1800",
				"d4|1|0|0|0|0|1800",
				"d5|1|1|0|0|0|1800",
				"dforeign|1|0|0|0|0|1800",
				"dproj|1|0|0|0|0|1800",
				"completedlease|0|1|0|0|0|0",
				"leaseonly|0|1|0|0|0|0",
			},
		},
		{
			name:  "latest queue failure",
			query: latestQueueFailureQuery,
			want:  []string{"w-tie-c"},
		},
		{
			name:  "conflict blockages",
			query: reducerConflictBlockageQuery,
			args:  []any{statusSemanticsAsOf},
			// w-old-pending shares the fenced conflict key but is a hidden
			// stale row, so domain "done" must not appear.
			want: []string{"reducer|d4|cd|k|1|1800"},
		},
	}
	for _, tc := range cases {
		got := statusSemanticsRows(ctx, t, conn, tc.query, tc.args...)
		if tc.name == "latest queue failure" {
			got = statusSemanticsWorkItemIDs(got)
		}
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s rows:\n got: %q\nwant: %q", tc.name, got, tc.want)
		}
	}
}

// TestActiveWorkSummaryMatchesStandaloneReads is the #6794 dedupe
// differential: on the semantics fixture, the single activeWorkSummaryQuery
// round trip must decode to exactly what the five pre-#6794 standalone reads
// (kept as a test oracle) return.
func TestActiveWorkSummaryMatchesStandaloneReads(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 active work summary differential")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)
	seedActiveWorkDifferentialExtras(ctx, t, conn)
	queryer := sqlConnQueryer{conn: conn}

	summary, err := readActiveWorkSummary(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("readActiveWorkSummary() error = %v", err)
	}
	stageCounts, err := listStageCounts(ctx, queryer)
	if err != nil {
		t.Fatalf("standalone stage counts: %v", err)
	}
	backlogs, err := listDomainBacklogs(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone domain backlog: %v", err)
	}
	queue, err := readQueueSnapshot(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone queue snapshot: %v", err)
	}
	blockages, err := listReducerConflictBlockages(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone blockages: %v", err)
	}
	failure, err := readLatestQueueFailure(ctx, queryer)
	if err != nil {
		t.Fatalf("standalone latest failure: %v", err)
	}
	for name, pair := range map[string][2]any{
		"stage counts":   {summary.StageCounts, stageCounts},
		"domain backlog": {summary.DomainBacklogs, backlogs},
		"queue snapshot": {summary.Queue, queue},
		"blockages":      {summary.Blockages, blockages},
		"latest failure": {summary.LatestFailure, failure},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s:\n summary:    %#v\n standalone: %#v", name, pair[0], pair[1])
		}
	}
	if len(summary.DomainBacklogs) == 0 || summary.LatestFailure == nil {
		t.Fatalf("fixture must exercise every section: %#v", summary)
	}
	// The extras seed more fenced conflict keys than the blockage limit, with
	// age ties, and several failure candidates with an updated_at tie, so a
	// changed limit or ordering in the summary cannot pass by coincidence.
	if got, want := len(summary.Blockages), reducerConflictBlockageLimit; got != want {
		t.Fatalf("blockage rows = %d, want the %d-row limit to truncate", got, want)
	}
	if got, want := summary.LatestFailure.WorkItemID, "w-fail-tie-a"; got != want {
		t.Fatalf("latest failure = %q, want %q (updated_at tie broken by work_item_id)", got, want)
	}
}

// TestActiveFactWorkItemsFormsSelectTheSameRows keeps the materialized
// activeFactWorkItemsCTE and the per-row activeFactWorkItemsPerRowCTE (used by
// the drain EXISTS) in step: on the semantics fixture, which covers every
// stale-generation branch, both must select exactly the same work items.
func TestActiveFactWorkItemsFormsSelectTheSameRows(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 active work form parity proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)
	seedActiveWorkDifferentialExtras(ctx, t, conn)

	selectIDs := func(cte string) []string {
		return statusSemanticsRows(ctx, t, conn,
			"WITH "+cte+" SELECT work_item_id FROM active_fact_work_items ORDER BY work_item_id")
	}
	materialized := selectIDs(activeFactWorkItemsCTE)
	perRow := selectIDs(activeFactWorkItemsPerRowCTE)
	if strings.Join(materialized, ",") != strings.Join(perRow, ",") {
		t.Fatalf("active work forms disagree:\n materialized: %v\n per-row:      %v", materialized, perRow)
	}
	if len(materialized) == 0 {
		t.Fatal("fixture selected no active work items")
	}
	for _, hidden := range []string{"w-old-pending", "w-old-dead", "w-tie-a"} {
		for _, id := range materialized {
			if id == hidden {
				t.Fatalf("stale unleased row %s must be hidden by both forms", hidden)
			}
		}
	}
}

// TestActiveWorkSummaryDropsTerminalTextFromMaterializedRows exercises the
// production summary CTE against the status fixture. Terminal rows still
// contribute to counts, but do not carry text used only by active consumers.
func TestActiveWorkSummaryDropsTerminalTextFromMaterializedRows(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		if os.Getenv("ESHU_REQUIRE_ACTIVE_WORK_PROJECTION_PROOF") == "1" {
			t.Fatal("ESHU_POSTGRES_DSN is required for the active work projection proof")
		}
		t.Skip("set ESHU_POSTGRES_DSN to run the active work projection proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)

	prefix, _, ok := strings.Cut(activeWorkSummaryQuery, "\n),\nfact_domain_backlogs AS (")
	if !ok || !strings.HasPrefix(prefix, "\nWITH "+activeFactWorkItemsScopeStateCTE) {
		t.Fatal("could not derive active work CTE from production summary query")
	}
	tail := `
)
SELECT COUNT(*) FILTER (WHERE status = 'succeeded'),
       COUNT(*) FILTER (WHERE status = 'superseded'),
       COUNT(*) FILTER (WHERE status IN ('succeeded', 'superseded')
         AND (work_item_id IS NOT NULL OR scope_id IS NOT NULL
           OR generation_id IS NOT NULL OR domain IS NOT NULL
           OR conflict_domain IS NOT NULL OR conflict_key IS NOT NULL))
FROM active_fact_work_items`
	rows := statusSemanticsRows(ctx, t, conn, prefix+tail)
	if len(rows) != 1 || rows[0] != "2|1|0" {
		t.Fatalf("succeeded, superseded, and wide terminal rows = %v, want [2|1|0]", rows)
	}
	conditionalID := "CASE WHEN work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') THEN work.work_item_id END AS work_item_id"
	unconditional := strings.Replace(prefix, conditionalID, "work.work_item_id AS work_item_id", 1)
	if unconditional == prefix {
		t.Fatal("seeded unconditional identity projection did not change production CTE")
	}
	violatingRows := statusSemanticsRows(ctx, t, conn, unconditional+tail)
	if len(violatingRows) != 1 || violatingRows[0] != "2|1|3" {
		t.Fatalf("seeded unconditional identity projection = %v, want [2|1|3]", violatingRows)
	}
}

// TestActiveWorkSummaryMatchesPreHistoryGroupsOracle is the #7009 grouped
// history differential: on every edge case ported from the C3 shim fixture,
// the busy 20%-live shape, and the #6794 semantics fixture, the shipped
// summary query and the pre-change oracle (rendered from origin/main
// 9bcca588f) run in one statement and must return identical section rows.
// Each edge case also checks hand-derived values on the shipped query.
func TestActiveWorkSummaryMatchesPreHistoryGroupsOracle(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #7009 grouped history differential")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	for _, tc := range append(historyEdgeCases(), historyFixtureCases()...) {
		t.Run(tc.name, func(t *testing.T) {
			resetHistoryCase(ctx, t, conn)
			tc.seed(ctx, t, conn)
			diff := diffAgainstOracle(ctx, t, conn, activeWorkSummaryQuery, tc.asOf)
			if !diff.equal() || diff.oracleRows == 0 {
				t.Fatalf("summary differs from the pre-change oracle: %+v", diff)
			}
			t.Logf("%d equal section rows", diff.oracleRows)
			checkHistoryExpectations(ctx, t, conn, tc.asOf, tc.expect)
		})
	}
}

// TestActiveWorkSummaryOracleCatchesSeededHistoryMutations is the positive
// control for that differential: each seeded violation of a grouped-history
// guard must differ from the oracle on at least one case.
func TestActiveWorkSummaryOracleCatchesSeededHistoryMutations(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #7009 grouped history mutation controls")
	}
	mutants := historySummaryMutants()
	mutated := make([]string, len(mutants))
	for i, m := range mutants {
		mutated[i] = m.apply(t, activeWorkSummaryQuery)
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	caught := make(map[string][]string, len(mutants))
	for _, tc := range append(historyEdgeCases(), historyFixtureCases()...) {
		resetHistoryCase(ctx, t, conn)
		tc.seed(ctx, t, conn)
		for i, m := range mutants {
			if diff := diffAgainstOracle(ctx, t, conn, mutated[i], tc.asOf); !diff.equal() {
				caught[m.name] = append(caught[m.name], tc.name)
			}
		}
	}
	for _, m := range mutants {
		if len(caught[m.name]) == 0 {
			t.Errorf("seeded mutation %q matched the oracle on every case", m.name)
			continue
		}
		t.Logf("%s: caught by %s", m.name, strings.Join(caught[m.name], ", "))
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// historyDiff is the same-snapshot comparison of the oracle and a candidate
// summary query: their row counts and the bidirectional EXCEPT ALL row
// counts over (section, ordinal, section_json text).
type historyDiff struct {
	oracleRows, candidateRows int64
	oracleOnly, candidateOnly int64
}

func (d historyDiff) equal() bool {
	return d.oracleRows == d.candidateRows && d.oracleOnly == 0 && d.candidateOnly == 0
}

// diffAgainstOracle runs the oracle and candidate as two CTEs of one
// statement, so both read the same snapshot, and compares their rows.
func diffAgainstOracle(ctx context.Context, t *testing.T, conn *sql.Conn, candidate string, asOf time.Time) historyDiff {
	t.Helper()
	query := `WITH oracle AS MATERIALIZED (` + activeWorkSummaryPreHistoryGroupsOracle + `),
candidate AS MATERIALIZED (` + candidate + `)
SELECT (SELECT COUNT(*) FROM oracle),
       (SELECT COUNT(*) FROM candidate),
       (SELECT COUNT(*) FROM (SELECT * FROM oracle EXCEPT ALL SELECT * FROM candidate) AS oracle_only),
       (SELECT COUNT(*) FROM (SELECT * FROM candidate EXCEPT ALL SELECT * FROM oracle) AS candidate_only)`
	var d historyDiff
	if err := conn.QueryRowContext(ctx, query, asOf).Scan(
		&d.oracleRows, &d.candidateRows, &d.oracleOnly, &d.candidateOnly,
	); err != nil {
		t.Fatalf("diff against oracle: %v", err)
	}
	return d
}

// checkHistoryExpectations reads the shipped query's section rows and checks
// each hand-derived expectation against them.
func checkHistoryExpectations(ctx context.Context, t *testing.T, conn *sql.Conn, asOf time.Time, expect []historyExpectation) {
	t.Helper()
	rows, err := conn.QueryContext(ctx, activeWorkSummaryQuery, asOf)
	if err != nil {
		t.Fatalf("summary query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	sections := map[string]map[int64]map[string]json.RawMessage{}
	for rows.Next() {
		var section, raw string
		var ordinal int64
		if err := rows.Scan(&section, &ordinal, &raw); err != nil {
			t.Fatalf("scan summary row: %v", err)
		}
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatalf("decode %s row %d: %v", section, ordinal, err)
		}
		if sections[section] == nil {
			sections[section] = map[int64]map[string]json.RawMessage{}
		}
		sections[section][ordinal] = fields
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("summary rows: %v", err)
	}
	for _, e := range expect {
		got := "<missing>"
		if e.ordinal == 0 {
			got = strconv.Itoa(len(sections[e.section]))
		} else if value, ok := sections[e.section][e.ordinal][e.field]; ok {
			got = string(value)
		}
		if got != e.want {
			t.Errorf("%s[%d].%s = %s, want %s", e.section, e.ordinal, e.field, got, e.want)
		}
	}
}

// historySummaryMutant is a seeded violation of one grouped-history guard.
// Each replacement must match the shipped query exactly once, so a mutant can
// never silently equal the query it perturbs.
type historySummaryMutant struct {
	name         string
	replacements [][2]string
}

const (
	historyDetailSix       = "status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter') OFFSET 0"
	historyHistorySix      = "status_group.status NOT IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter')"
	historyGroupsTotal     = "(SELECT COALESCE(SUM(row_count), 0)::BIGINT FROM fact_work_status_groups) AS total_count"
	historySucceededSource = "FROM fact_work_history_counts WHERE status = 'succeeded'"
	historyJoinKey         = "    ON stale_generation.scope_id = status_group.scope_id\n   AND stale_generation.generation_id = status_group.generation_id"
)

// historySummaryMutants are the guards the oracle differential must catch.
func historySummaryMutants() []historySummaryMutant {
	without := func(list, status string) string {
		return strings.Replace(list, "'"+status+"', ", "", 1)
	}
	partition := func(status string) [][2]string {
		return [][2]string{
			{historyDetailSix, strings.Replace(without(historyDetailSix, status), ", '"+status+"'", "", 1)},
			{historyHistorySix, strings.Replace(without(historyHistorySix, status), ", '"+status+"'", "", 1)},
		}
	}
	return []historySummaryMutant{
		{name: "drop failed from the status partition", replacements: partition("failed")},
		{name: "drop claimed from the status partition", replacements: partition("claimed")},
		{name: "drop retrying from the detail statuses only", replacements: [][2]string{
			{historyDetailSix, without(historyDetailSix, "retrying")},
		}},
		{name: "drop the stale-generation predicate", replacements: [][2]string{
			{"    AND work.generation_id <> scope_state.active_generation_id\n", "    AND FALSE\n"},
		}},
		{name: "join history generations without the scope key", replacements: [][2]string{
			{historyJoinKey, "    ON stale_generation.generation_id = status_group.generation_id"},
		}},
		{name: "count total_count from the joined rows", replacements: [][2]string{
			{historyGroupsTotal, "((SELECT COUNT(*) FROM active_fact_work_items) + (SELECT COALESCE(SUM(row_count), 0)::BIGINT FROM fact_work_history_counts)) AS total_count"},
		}},
		{name: "count succeeded from the unjoined groups", replacements: [][2]string{
			{historySucceededSource, "FROM fact_work_status_groups WHERE status = 'succeeded'"},
		}},
	}
}

// apply returns the mutated query, failing the test when an anchor does not
// match the shipped query exactly once or the mutation changes nothing.
func (m historySummaryMutant) apply(t *testing.T, query string) string {
	t.Helper()
	mutated := query
	for _, pair := range m.replacements {
		if n := strings.Count(mutated, pair[0]); n != 1 {
			t.Fatalf("%s: anchor matched %d times, want 1: %q", m.name, n, pair[0])
		}
		mutated = strings.Replace(mutated, pair[0], pair[1], 1)
	}
	if mutated == query {
		t.Fatalf("%s: mutation did not change the query", m.name)
	}
	return mutated
}

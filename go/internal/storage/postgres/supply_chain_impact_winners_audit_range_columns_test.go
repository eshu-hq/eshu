// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The range statement (#11131) feeds the shipped winner body from its own
// narrow fact CTEs instead of `SELECT fact.*`, so a column the shared body
// reads off `fact` but the range CTEs do not project fails every range with
// SQLSTATE 42703 and the sweep defers batch_abort on every tick. #10880
// slice 1 added fact.source_confidence to the body and nothing here noticed
// until a live run. These tests derive the column set from the shipped body
// constant and check every arm that feeds it.

// winnerBodyFactColumns lists every `fact.<column>` the shared winner body
// reads, sorted, from the shipped constant (never a hand copy).
func winnerBodyFactColumns() []string {
	found := map[string]bool{}
	for _, match := range regexp.MustCompile(`\bfact\.([a-z_][a-z0-9_]*)\b`).FindAllStringSubmatch(supplyChainImpactWinnerBodySQL, -1) {
		found[match[1]] = true
	}
	columns := make([]string, 0, len(found))
	for column := range found {
		columns = append(columns, column)
	}
	slices.Sort(columns)
	return columns
}

// cteArms returns the SELECT list of every UNION ALL arm of the named CTE.
func cteArms(t *testing.T, sql, name string) []string {
	t.Helper()
	start := regexp.MustCompile(`(?m)\b` + name + ` AS (?:NOT )?(?:MATERIALIZED )?\(`).FindStringIndex(sql)
	if start == nil {
		t.Fatalf("CTE %s not found in the range statement", name)
	}
	depth, end := 1, -1
	for i := start[1]; i < len(sql) && end < 0; i++ {
		switch sql[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
	}
	if end < 0 {
		t.Fatalf("CTE %s has no closing parenthesis", name)
	}
	var arms []string
	for _, arm := range strings.Split(sql[start[1]:end], "UNION ALL") {
		from := regexp.MustCompile(`\n\s*FROM `).FindStringIndex(arm)
		if from == nil {
			t.Fatalf("CTE %s arm has no top-level FROM: %q", name, arm)
		}
		arms = append(arms, arm[:from[0]])
	}
	return arms
}

// projectedColumns returns the columns a SELECT list projects as a bare
// qualified reference (`f.payload`, `r.payload`, `fact.payload`). It splits the
// list at top-level commas, so a column that only appears inside another item,
// such as the `payload` of `(f.payload->>'package_id') AS i_package_id`, does
// not count as projected.
func projectedColumns(selectList string) map[string]bool {
	projected := map[string]bool{}
	bare := regexp.MustCompile(`^(?:f|r|fact)\.([a-z_][a-z0-9_]*)$`)
	depth, start := 0, strings.Index(selectList, "SELECT ")+len("SELECT ")
	flush := func(end int) {
		if match := bare.FindStringSubmatch(strings.TrimSpace(selectList[start:end])); match != nil {
			projected[match[1]] = true
		}
		start = end + 1
	}
	for i := start; i < len(selectList); i++ {
		switch selectList[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				flush(i)
			}
		}
	}
	flush(len(selectList))
	return projected
}

// rangeFeedCTEs are the CTEs whose rows reach the winner body's `fact` alias,
// with the arm count each must have.
var rangeFeedCTEs = []struct {
	name string
	arms int
}{{"range_raw", 1}, {"range_facts", 2}, {"active_facts", 2}}

// missingRangeColumns reports every (CTE, arm, column) the range statement
// does not project although the winner body reads it.
func missingRangeColumns(t *testing.T, sql string, columns []string) []string {
	t.Helper()
	var missing []string
	for _, cte := range rangeFeedCTEs {
		arms := cteArms(t, sql, cte.name)
		if len(arms) != cte.arms {
			t.Fatalf("CTE %s has %d arms, want %d; update the guard with the statement", cte.name, len(arms), cte.arms)
		}
		for i, selectList := range arms {
			projected := projectedColumns(selectList)
			for _, column := range columns {
				if !projected[column] {
					missing = append(missing, fmt.Sprintf("%s arm %d: %s", cte.name, i+1, column))
				}
			}
		}
	}
	return missing
}

func TestWinnersAuditRangeProjectsEveryColumnTheWinnerBodyReads(t *testing.T) {
	t.Parallel()
	columns := winnerBodyFactColumns()
	for _, want := range []string{"fact_id", "scope_id", "payload", "source_confidence"} {
		if !slices.Contains(columns, want) {
			t.Fatalf("derived winner body columns %v miss %q: the derivation went blind", columns, want)
		}
	}
	for name, sql := range map[string]string{"first": selectWinnersAuditFirstRangeSQL, "next": selectWinnersAuditNextRangeSQL} {
		if missing := missingRangeColumns(t, sql, columns); len(missing) > 0 {
			t.Errorf("%s range statement does not project columns the winner body reads (SQLSTATE 42703 on every range): %v", name, missing)
		}
	}
}

// TestWinnersAuditRangeColumnGuardSeesAMissingColumn is the seeded pair: for
// source_confidence and for payload (whose text also appears inside the
// identifier expressions of the same select lists) the guard names exactly the
// arm that lost the bare column, and all five arms when none carries it.
func TestWinnersAuditRangeColumnGuardSeesAMissingColumn(t *testing.T) {
	t.Parallel()
	columns := winnerBodyFactColumns()
	arms := []string{"range_raw arm 1", "range_facts arm 1", "range_facts arm 2", "active_facts arm 1", "active_facts arm 2"}
	sql := selectWinnersAuditNextRangeSQL
	// The feed CTEs all sit before the winner body's first CTE.
	feedEnd := strings.Index(sql, "source_candidates AS")
	if feedEnd < 0 {
		t.Fatal("winner body CTE source_candidates not found in the range statement")
	}
	for _, column := range []string{"source_confidence", "payload"} {
		// A bare reference is a qualified column not followed by a JSON arrow.
		var at [][]int
		for _, loc := range regexp.MustCompile(`\b(?:f|r|fact)\.`+column+`\b`).FindAllStringIndex(sql[:feedEnd], -1) {
			if !strings.HasPrefix(sql[loc[1]:], "->") {
				at = append(at, loc)
			}
		}
		if len(at) != len(arms) {
			t.Fatalf("range feed CTEs project bare %s %d times, want %d (one per arm); update the guard", column, len(at), len(arms))
		}
		for i, arm := range arms {
			mutated := sql[:at[i][0]] + "NULL" + sql[at[i][1]:]
			missing := missingRangeColumns(t, mutated, columns)
			if want := arm + ": " + column; len(missing) != 1 || missing[0] != want {
				t.Fatalf("dropping %s from %s reported %v, want exactly [%s]", column, arm, missing, want)
			}
		}
		all := sql
		for i := len(at) - 1; i >= 0; i-- {
			all = all[:at[i][0]] + "NULL" + all[at[i][1]:]
		}
		if missing := missingRangeColumns(t, all, columns); len(missing) != len(arms) {
			t.Fatalf("dropping %s everywhere reported %v, want %d arms", column, missing, len(arms))
		}
	}
}

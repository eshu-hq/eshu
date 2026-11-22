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
			for _, column := range columns {
				if !regexp.MustCompile(`\b` + column + `\b`).MatchString(selectList) {
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

// TestWinnersAuditRangeColumnGuardSeesAMissingColumn is the seeded pair: the
// guard names exactly the arm whose select list lost source_confidence, and
// all five arms when none carries it (the pre-fix shape).
func TestWinnersAuditRangeColumnGuardSeesAMissingColumn(t *testing.T) {
	t.Parallel()
	columns := winnerBodyFactColumns()
	arms := []string{"range_raw arm 1", "range_facts arm 1", "range_facts arm 2", "active_facts arm 1", "active_facts arm 2"}
	const column = "source_confidence"
	sql := selectWinnersAuditNextRangeSQL
	// The feed CTEs all sit before the winner body's first CTE.
	feedEnd := strings.Index(sql, "source_candidates AS")
	if feedEnd < 0 {
		t.Fatal("winner body CTE source_candidates not found in the range statement")
	}
	feed := sql[:feedEnd]
	var at []int
	for offset := 0; ; {
		found := strings.Index(feed[offset:], column)
		if found < 0 {
			break
		}
		at = append(at, offset+found)
		offset += found + len(column)
	}
	if len(at) != len(arms) {
		t.Fatalf("range feed CTEs mention %s %d times, want %d (one per arm); update the guard", column, len(at), len(arms))
	}
	for i, arm := range arms {
		mutated := sql[:at[i]] + "src_conf_dropped" + sql[at[i]+len(column):]
		missing := missingRangeColumns(t, mutated, columns)
		if want := arm + ": " + column; len(missing) != 1 || missing[0] != want {
			t.Fatalf("dropping %s from %s reported %v, want exactly [%s]", column, arm, missing, want)
		}
	}
	if missing := missingRangeColumns(t, strings.ReplaceAll(sql, column, "src_conf_dropped"), columns); len(missing) != len(arms) {
		t.Fatalf("dropping %s everywhere reported %v, want %d arms", column, missing, len(arms))
	}
}

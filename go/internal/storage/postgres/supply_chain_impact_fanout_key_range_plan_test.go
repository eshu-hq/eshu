// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"
)

// keyRangeArmSegments returns the text of each child of the plan's Append
// node: one segment per key-range arm (cve_id, advisory_id). A child is a
// line at the Append line's indent plus six, with its deeper lines.
func keyRangeArmSegments(plan string) []string {
	lines := strings.Split(plan, "\n")
	indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	var segments []string
	appendIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case appendIndent < 0:
			if strings.HasPrefix(trimmed, "->  Append") {
				appendIndent = indent(line)
			}
		case trimmed != "" && indent(line) <= appendIndent:
			return segments
		case strings.HasPrefix(trimmed, "->") && indent(line) == appendIndent+6:
			segments = append(segments, line)
		case len(segments) > 0:
			segments[len(segments)-1] += "\n" + line
		}
	}
	return segments
}

// keyRangeArms are the two arms of the key-range statement, in plan order,
// with the lookup expression each reads and the one partial index that keys
// it. Naming the index is the point: fact_records_supply_chain_impact_status_
// lookup_idx has no cve_id column, so a plan that reads the cve arm through
// it carries only the fact_id bounds in its Index Cond and filters cve_id on
// the heap: a 1/16 slice of every finding row per statement, the harm the
// plan pin exists to prevent.
var keyRangeArms = []struct{ name, expr, index string }{
	{"cve_id", "payload ->> 'cve_id'", "fact_records_supply_chain_impact_lookup_idx"},
	{"advisory_id", "payload ->> 'advisory_id'", "fact_records_supply_chain_impact_advisory_lookup_idx"},
}

// keyRangePlanViolation returns why plan is not the bounded key-range shape,
// or "" when it is. Under the pinned custom plan, with the hot id a minority
// of the table (keyRangeSeed), each arm must read its own lookup index with
// the arm's lookup expression and both fact_id bounds in one Index Cond
// line, and no Filter line may carry the lookup expression or fact_id. The
// plan must read no sequential scan of fact_records and no primary-key range.
func keyRangePlanViolation(plan string) string {
	for _, banned := range []string{"Seq Scan on fact_records", "fact_records_pkey"} {
		if strings.Contains(plan, banned) {
			return fmt.Sprintf("plan reads %q", banned)
		}
	}
	segments := keyRangeArmSegments(plan)
	if len(segments) != len(keyRangeArms) {
		return fmt.Sprintf("plan has %d key-range arms under Append, want %d (cve_id, advisory_id)", len(segments), len(keyRangeArms))
	}
	for i, segment := range segments {
		arm := keyRangeArms[i]
		if !strings.Contains(segment, arm.index) {
			return fmt.Sprintf("%s arm does not read %s", arm.name, arm.index)
		}
		keyed := false
		for _, line := range strings.Split(segment, "\n") {
			cond := strings.TrimSpace(line)
			if strings.HasPrefix(cond, "Filter:") && (strings.Contains(cond, "fact_id") || strings.Contains(cond, arm.expr)) {
				return fmt.Sprintf("%s arm: the lookup expression or fact_id bounds are a heap filter, not an index condition", arm.name)
			}
			if strings.HasPrefix(cond, "Index Cond:") && strings.Contains(cond, arm.expr) &&
				strings.Contains(cond, "fact_id >=") && strings.Contains(cond, "fact_id <") {
				keyed = true
			}
		}
		if !keyed {
			return fmt.Sprintf("%s arm: no Index Cond line holds %s together with both fact_id bounds", arm.name, arm.expr)
		}
	}
	return ""
}

const keyRangePlanHead = `Unique
  CTE candidates
    ->  HashAggregate
          ->  Append
`

// TestKeyRangePlanViolationShapes seeds the plan-shape assertion with the
// good plans (green) and the bad ones (red), so the live plan test cannot
// pass by copying one planner choice or by matching a comment. The status
// lookup plan is red: it is what PostgreSQL 18.6 picks when the hot id is
// nearly the whole table, and it reads a 1/16 slice of every finding row.
func TestKeyRangePlanViolationShapes(t *testing.T) {
	const bounds = "(fact_id >= 'a'::text) AND (fact_id < 'b'::text)"
	cveLookup := `                ->  Index Scan using fact_records_supply_chain_impact_lookup_idx on fact_records finding
                      Index Cond: (((payload ->> 'cve_id'::text) = ANY ('{X}'::text[])) AND ` + bounds + `)
`
	cveBitmapLookup := `                ->  Bitmap Heap Scan on fact_records finding
                      Recheck Cond: (((payload ->> 'cve_id'::text) = ANY ('{X}'::text[])) AND ` + bounds + `)
                      ->  Bitmap Index Scan on fact_records_supply_chain_impact_lookup_idx
                            Index Cond: (((payload ->> 'cve_id'::text) = ANY ('{X}'::text[])) AND ` + bounds + `)
`
	cveStatusLookup := `                ->  Bitmap Heap Scan on fact_records finding
                      Recheck Cond: (` + bounds + ` AND (fact_kind = 'k'::text))
                      Filter: ((payload ->> 'cve_id'::text) = ANY ('{X}'::text[]))
                      ->  Bitmap Index Scan on fact_records_supply_chain_impact_status_lookup_idx
                            Index Cond: (` + bounds + `)
`
	advisory := `                ->  Index Scan using fact_records_supply_chain_impact_advisory_lookup_idx on fact_records finding_1
                      Index Cond: (((payload ->> 'advisory_id'::text) = ANY ('{X}'::text[])) AND ` + bounds + `)
`
	tail := `    ->  Sort
          ->  Seq Scan on ingestion_scopes scope
`
	tests := []struct {
		name string
		plan string
		want string // substring of the violation; "" is green
	}{
		{"cve lookup_idx index scan, advisory_lookup", keyRangePlanHead + cveLookup + advisory + tail, ""},
		{"cve lookup_idx bitmap scan, advisory_lookup", keyRangePlanHead + cveBitmapLookup + advisory + tail, ""},
		{"cve status_lookup_idx: bounds only, cve_id a heap filter", keyRangePlanHead + cveStatusLookup + advisory + tail, "does not read fact_records_supply_chain_impact_lookup_idx"},
		{"cve arm on the status index even with cve_id in the Index Cond", keyRangePlanHead + strings.ReplaceAll(cveLookup, "impact_lookup_idx", "impact_status_lookup_idx") + advisory + tail, "does not read fact_records_supply_chain_impact_lookup_idx"},
		{"primary-key range (the generic plan)", keyRangePlanHead + strings.ReplaceAll(cveLookup, "fact_records_supply_chain_impact_lookup_idx", "fact_records_pkey") + advisory + tail, "fact_records_pkey"},
		{"sequential scan of fact_records", keyRangePlanHead + "                ->  Seq Scan on fact_records finding\n" + advisory + tail, "Seq Scan on fact_records"},
		{"cve_id lookup expression is a heap filter on the right index", keyRangePlanHead + cveLookup + "                      Filter: ((payload ->> 'cve_id'::text) = ANY ('{X}'::text[]))\n" + advisory + tail, "heap filter"},
		{"fact_id bounds are a heap filter", keyRangePlanHead + cveLookup + advisory + "                      Filter: (fact_id >= 'a'::text)\n" + tail, "heap filter"},
		{"cve Index Cond lacks the upper bound", keyRangePlanHead + strings.ReplaceAll(cveLookup, " AND (fact_id < 'b'::text)", "") + advisory + tail, "no Index Cond line holds payload ->> 'cve_id'"},
		{"advisory arm reads the cve index", keyRangePlanHead + cveLookup + strings.ReplaceAll(advisory, "impact_advisory_lookup_idx", "impact_lookup_idx") + tail, "advisory_id arm does not read"},
		{"only one arm", keyRangePlanHead + cveLookup + tail, "want 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keyRangePlanViolation(tt.plan)
			if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
				t.Fatalf("keyRangePlanViolation() = %q, want containing %q", got, tt.want)
			}
		})
	}
}

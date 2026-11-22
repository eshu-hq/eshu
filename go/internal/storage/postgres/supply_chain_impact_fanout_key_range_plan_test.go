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

// keyRangePlanViolation returns why plan is not the bounded key-range shape,
// or "" when it is. The shape does not name one index, because the planner
// picks per arm by selectivity and fixture size: the cve arm may read
// fact_records_supply_chain_impact_lookup_idx or ..._status_lookup_idx (the
// bound lives in the index, cve_id is a recheck filter), the advisory arm
// ..._advisory_lookup_idx. What must hold under the pinned custom plan: no
// sequential scan of fact_records, no primary-key range, and every arm reads
// a fact_records_supply_chain_impact_ index with both fact_id bounds in an
// index or recheck condition, never in a post-filter.
func keyRangePlanViolation(plan string) string {
	for _, banned := range []string{"Seq Scan on fact_records", "fact_records_pkey"} {
		if strings.Contains(plan, banned) {
			return fmt.Sprintf("plan reads %q", banned)
		}
	}
	segments := keyRangeArmSegments(plan)
	if len(segments) != 2 {
		return fmt.Sprintf("plan has %d key-range arms under Append, want 2 (cve_id, advisory_id)", len(segments))
	}
	for i, segment := range segments {
		arm := []string{"cve_id", "advisory_id"}[i]
		if !strings.Contains(segment, "fact_records_supply_chain_impact_") {
			return fmt.Sprintf("%s arm reads no fact_records_supply_chain_impact_ index", arm)
		}
		bounded := false
		for _, line := range strings.Split(segment, "\n") {
			cond := strings.TrimSpace(line)
			if strings.HasPrefix(cond, "Filter:") && strings.Contains(cond, "fact_id") {
				return fmt.Sprintf("%s arm: fact_id bounds are a filter, not an index condition", arm)
			}
			if (strings.HasPrefix(cond, "Index Cond:") || strings.HasPrefix(cond, "Recheck Cond:")) &&
				strings.Contains(cond, "fact_id >=") && strings.Contains(cond, "fact_id <") {
				bounded = true
			}
		}
		if !bounded {
			return fmt.Sprintf("%s arm: fact_id bounds are in no index or recheck condition", arm)
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
// planner choices PostgreSQL 18.6 makes at fixture scale (green) and with
// planted violations (red), so the live plan test cannot pass by copying one
// planner choice or by matching a comment.
func TestKeyRangePlanViolationShapes(t *testing.T) {
	const bounds = "(fact_id >= 'a'::text) AND (fact_id < 'b'::text)"
	cveBitmap := `                ->  Bitmap Heap Scan on fact_records finding
                      Recheck Cond: (` + bounds + ` AND (fact_kind = 'k'::text))
                      Filter: ((payload ->> 'cve_id'::text) = ANY ('{X}'::text[]))
                      ->  Bitmap Index Scan on fact_records_supply_chain_impact_status_lookup_idx
                            Index Cond: (` + bounds + `)
`
	cveLookup := `                ->  Index Scan using fact_records_supply_chain_impact_lookup_idx on fact_records finding
                      Index Cond: (((payload ->> 'cve_id'::text) = ANY ('{X}'::text[])) AND ` + bounds + `)
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
		{"cve status_lookup bitmap, advisory_lookup (the observed fixture plan)", keyRangePlanHead + cveBitmap + advisory + tail, ""},
		{"cve lookup_idx, advisory_lookup (the QA-scale plan)", keyRangePlanHead + cveLookup + advisory + tail, ""},
		{"primary-key range (the generic plan)", keyRangePlanHead + strings.ReplaceAll(cveBitmap, "fact_records_supply_chain_impact_status_lookup_idx", "fact_records_pkey") + advisory + tail, "fact_records_pkey"},
		{"sequential scan of fact_records", keyRangePlanHead + "                ->  Seq Scan on fact_records finding\n" + advisory + tail, "Seq Scan on fact_records"},
		{"cve arm bound is a post-filter", keyRangePlanHead + strings.ReplaceAll(strings.ReplaceAll(cveBitmap, "Recheck Cond", "Recheck Cond-gone"), "Index Cond", "Filter") + advisory + tail, "filter"},
		{"cve arm index lacks the bounds", keyRangePlanHead + strings.ReplaceAll(cveBitmap, bounds, "(x = 1)") + advisory + tail, "no index or recheck condition"},
		{"advisory arm reads no impact index", keyRangePlanHead + cveLookup + "                ->  Index Scan using other_idx on fact_records finding_1\n                      Index Cond: (" + bounds + ")\n" + tail, "advisory_id arm reads no"},
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

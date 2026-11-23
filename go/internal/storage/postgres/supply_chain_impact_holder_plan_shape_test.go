// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"
)

// holderArmSegments returns the text of each child of the plan's Append node:
// one segment per key-range arm. A child is a line at the Append line's indent
// plus six, with its deeper lines.
func holderArmSegments(plan string) []string {
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

// holderKeyRangeArms are the three arms of the key-range statements on the
// supports table, in plan order: the lookup expression each reads and the index
// that keys it. The cve and advisory arms carry both fact_id bounds in their
// Index Cond (lower only on the open statement); the foreign-member token arm
// reads the token GIN with the && overlap and no fact_id bound, because the
// token's bucket, not the key range, partitions the foreign rows over the
// statements (#10643).
var holderKeyRangeArms = []struct {
	name, expr, index string
	bounded           bool
}{
	{"cve_id", "cve_id = ANY", holderCveIdx, true},
	{"advisory_id", "advisory_id = ANY", holderAdvisoryIdx, true},
	{"token", "eshu_sci_foreign_member_tokens_v4(fact_id, payload) &&", holderTokensIdx, false},
}

// holderKeyRangePlanViolation returns why plan is not the key-range shape of the
// supports table, or "" when it is: no sequential or primary-key scan of the
// table, each arm on its named index with its lookup expression and both
// fact_id bounds in one Index Cond line (lower only when open), no bound or
// lookup expression pushed into a Filter, and no fact_id bound on the token arm.
func holderKeyRangePlanViolation(plan string, open bool) string {
	for _, banned := range []string{"Seq Scan on " + holderSupportsTbl, holderSupportsTbl + "_pkey"} {
		if strings.Contains(plan, banned) {
			return fmt.Sprintf("plan reads %q", banned)
		}
	}
	segments := holderArmSegments(plan)
	if len(segments) != len(holderKeyRangeArms) {
		return fmt.Sprintf("plan has %d key-range arms under Append, want %d (cve_id, advisory_id, token)", len(segments), len(holderKeyRangeArms))
	}
	for i, segment := range segments {
		arm := holderKeyRangeArms[i]
		if !strings.Contains(segment, arm.index) {
			return fmt.Sprintf("%s arm does not read %s", arm.name, arm.index)
		}
		keyed := false
		for _, line := range strings.Split(segment, "\n") {
			cond := strings.TrimSpace(line)
			if strings.HasPrefix(cond, "Filter:") && (strings.Contains(cond, "fact_id") || strings.Contains(cond, arm.expr)) {
				return fmt.Sprintf("%s arm: the lookup expression or fact_id bounds are a heap filter, not an index condition", arm.name)
			}
			if !strings.HasPrefix(cond, "Index Cond:") || !strings.Contains(cond, arm.expr) {
				continue
			}
			bounds := strings.Contains(cond, "fact_id >=") && strings.Contains(cond, "fact_id <")
			if open {
				if strings.Contains(cond, "fact_id <") {
					return fmt.Sprintf("%s arm: the open statement's Index Cond carries an upper fact_id bound", arm.name)
				}
				bounds = strings.Contains(cond, "fact_id >=")
			}
			if !arm.bounded && strings.Contains(cond, "fact_id >=") {
				return fmt.Sprintf("%s arm: the Index Cond carries a fact_id bound the token arm must not have", arm.name)
			}
			if bounds || !arm.bounded {
				keyed = true
			}
		}
		if !keyed && arm.bounded {
			want := "both fact_id bounds"
			if open {
				want = "the lower fact_id bound"
			}
			return fmt.Sprintf("%s arm: no Index Cond line holds %s together with %s", arm.name, arm.expr, want)
		}
		if !keyed {
			return fmt.Sprintf("%s arm: no Index Cond line holds %s", arm.name, arm.expr)
		}
	}
	return ""
}

const holderPlanHead = `Unique
  CTE candidates
    ->  HashAggregate
          ->  Append
`

// holderPlanArm renders one Append child in the shape PostgreSQL prints for an
// Index Scan, with the given Index Cond (and Filter when set).
func holderPlanArm(index, cond, filter string) string {
	out := "                ->  Index Scan using " + index + " on supply_chain_impact_finding_supports support\n" +
		"                      Index Cond: (" + cond + ")\n"
	if filter != "" {
		out += "                      Filter: (" + filter + ")\n"
	}
	return out
}

const (
	holderCveBounded = "cve_id = ANY ('{x}'::text[])) AND (fact_id >= 'a'::text) AND (fact_id < 'b'::text"
	holderAdvBounded = "advisory_id = ANY ('{x}'::text[])) AND (fact_id >= 'a'::text) AND (fact_id < 'b'::text"
	holderCveOpen    = "cve_id = ANY ('{x}'::text[])) AND (fact_id >= 'a'::text"
	holderAdvOpen    = "advisory_id = ANY ('{x}'::text[])) AND (fact_id >= 'a'::text"
	holderTokenCond  = "eshu_sci_foreign_member_tokens_v4(fact_id, payload) && '{1:x}'::text[]"
)

// TestHolderKeyRangePlanViolationShapes seeds the plan-shape assertion with the
// good plans (green) and the bad ones (red), so the live plan test cannot pass
// by copying one planner choice or by matching a comment.
func TestHolderKeyRangePlanViolationShapes(t *testing.T) {
	t.Parallel()

	good := holderPlanHead +
		holderPlanArm(holderCveIdx, holderCveBounded, "") +
		holderPlanArm(holderAdvisoryIdx, holderAdvBounded, "") +
		holderPlanArm(holderTokensIdx, holderTokenCond, "")
	goodOpen := holderPlanHead +
		holderPlanArm(holderCveIdx, holderCveOpen, "") +
		holderPlanArm(holderAdvisoryIdx, holderAdvOpen, "") +
		holderPlanArm(holderTokensIdx, holderTokenCond, "")

	tests := []struct {
		name string
		plan string
		open bool
		want string
	}{
		{"bounded green", good, false, ""},
		{"open green", goodOpen, true, ""},
		{"upper bound in a filter", holderPlanHead +
			holderPlanArm(holderCveIdx, "cve_id = ANY ('{x}'::text[])) AND (fact_id >= 'a'::text", "fact_id < 'b'::text") +
			holderPlanArm(holderAdvisoryIdx, holderAdvBounded, "") +
			holderPlanArm(holderTokensIdx, holderTokenCond, ""), false, "heap filter"},
		{"lookup expression in a filter", holderPlanHead +
			holderPlanArm(holderCveIdx, "fact_id >= 'a'::text) AND (fact_id < 'b'::text", "cve_id = ANY ('{x}'::text[])") +
			holderPlanArm(holderAdvisoryIdx, holderAdvBounded, "") +
			holderPlanArm(holderTokensIdx, holderTokenCond, ""), false, "heap filter"},
		{"open statement with an upper bound", good, true, "upper fact_id bound"},
		{"bounded statement with only a lower bound", goodOpen, false, "no Index Cond line holds"},
		{"fact_id bound on the token arm", holderPlanHead +
			holderPlanArm(holderCveIdx, holderCveBounded, "") +
			holderPlanArm(holderAdvisoryIdx, holderAdvBounded, "") +
			holderPlanArm(holderTokensIdx, holderTokenCond+") AND (fact_id >= 'a'::text", ""), false, "token arm must not have"},
		{"cve arm on the wrong index", holderPlanHead +
			holderPlanArm(holderAdvisoryIdx, holderCveBounded, "") +
			holderPlanArm(holderAdvisoryIdx, holderAdvBounded, "") +
			holderPlanArm(holderTokensIdx, holderTokenCond, ""), false, "cve_id arm does not read"},
		{"primary-key scan", holderPlanHead +
			holderPlanArm(holderSupportsTbl+"_pkey", holderCveBounded, "") +
			holderPlanArm(holderAdvisoryIdx, holderAdvBounded, "") +
			holderPlanArm(holderTokensIdx, holderTokenCond, ""), false, "_pkey"},
		{"sequential scan", good + "                ->  Seq Scan on " + holderSupportsTbl + " support\n", false, "Seq Scan"},
		{"missing token arm", holderPlanHead +
			holderPlanArm(holderCveIdx, holderCveBounded, "") +
			holderPlanArm(holderAdvisoryIdx, holderAdvBounded, ""), false, "2 key-range arms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := holderKeyRangePlanViolation(tt.plan, tt.open)
			if tt.want == "" {
				if got != "" {
					t.Fatalf("holderKeyRangePlanViolation() = %q, want a green plan", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("holderKeyRangePlanViolation() = %q, want containing %q", got, tt.want)
			}
		})
	}
}

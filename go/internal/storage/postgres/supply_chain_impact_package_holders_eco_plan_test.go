// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"
)

// packageHolderEcoIndex is the #11376 ecosystem-anchored partial index the
// package-keyed holder read must plan on. It leads with the folded ecosystem
// expression byte-identical to FindPackageHolderCandidatesQuery's predicate,
// then the active-generation join columns, over active
// vulnerability.affected_package rows only.
const packageHolderEcoIndex = "fact_records_vuln_pkg_eco_scope_gen_active_idx"

// packageHolderEcoPlanViolation returns why plan is not the bounded
// ecosystem-anchored shape, or "" when it is. The plan must read
// packageHolderEcoIndex with the ecosystem lookup expression and the ANY
// list in one Index Cond line, keep the active-generation join to
// ingestion_scopes, and carry no ecosystem heap filter: without the anchor
// the planner bitmap-scans the whole affected-package partial index
// (superseded rows included) and filters the ecosystem on the heap, the
// #11376 breach shape. No sequential scan of fact_records and no primary-key
// range may appear.
func packageHolderEcoPlanViolation(plan string) string {
	for _, banned := range []string{"Seq Scan on fact_records", "fact_records_pkey"} {
		if strings.Contains(plan, banned) {
			return fmt.Sprintf("plan reads %q", banned)
		}
	}
	if !strings.Contains(plan, "ingestion_scopes") {
		return "plan drops the active-generation join to ingestion_scopes"
	}
	if !strings.Contains(plan, packageHolderEcoIndex) {
		return fmt.Sprintf("plan does not read %s", packageHolderEcoIndex)
	}
	keyed := false
	for _, line := range strings.Split(plan, "\n") {
		cond := strings.TrimSpace(line)
		if strings.HasPrefix(cond, "Filter:") && strings.Contains(cond, "payload ->> 'ecosystem'") {
			return "the ecosystem lookup is a heap filter, not an index condition"
		}
		if strings.HasPrefix(cond, "Index Cond:") && strings.Contains(cond, "payload ->> 'ecosystem'") &&
			strings.Contains(cond, "ANY") {
			keyed = true
		}
	}
	if !keyed {
		return "no Index Cond line holds the ecosystem expression with ANY"
	}
	return ""
}

// TestPackageHolderEcoPlanViolationShapes seeds the plan-shape assertion
// with the good plans (green) and the bad ones (red), so the live plan test
// cannot pass by copying one planner choice. The red whole-index bitmap is
// the #11376 breach shape: every retained affected-package row is bitmap
// heap-fetched and the ecosystem is a heap residual.
func TestPackageHolderEcoPlanViolationShapes(t *testing.T) {
	t.Parallel()

	ecoCond := "(((lower((btrim((payload ->> 'ecosystem'::text)))::text))::text = ANY ('{npm,node}'::text[]))"
	ecoIndexScan := `                ->  Index Scan using fact_records_vuln_pkg_eco_scope_gen_active_idx on fact_records fact
                      Index Cond: (` + ecoCond + ` AND (scope_id = scope.scope_id) AND (generation_id = scope.active_generation_id))
`
	ecoBitmap := `                ->  Bitmap Heap Scan on fact_records fact
                      Recheck Cond: (` + ecoCond + `)
                      ->  Bitmap Index Scan on fact_records_vuln_pkg_eco_scope_gen_active_idx
                            Index Cond: (` + ecoCond + `)
`
	breachBitmap := `                ->  Bitmap Heap Scan on fact_records fact
                      Recheck Cond: ((fact_kind = 'vulnerability.affected_package'::text) AND (NOT is_tombstone))
                      Filter: (((lower((btrim((payload ->> 'ecosystem'::text)))::text))::text = ANY ('{npm,node}'::text[]))
                      ->  Bitmap Index Scan on fact_records_vulnerability_affected_package_active_scan_idx
                            Index Cond: ((scope_id = scope.scope_id) AND (generation_id = scope.active_generation_id))
`
	join := `    ->  Seq Scan on ingestion_scopes scope
`
	tests := []struct {
		name string
		plan string
		want string // substring of the violation; "" is green
	}{
		{"eco index scan with ANY in the Index Cond", ecoIndexScan + join, ""},
		{"eco bitmap with ANY in the Index Cond", ecoBitmap + join, ""},
		{"whole-index bitmap with the ecosystem as a heap filter", breachBitmap + join, "does not read fact_records_vuln_pkg_eco_scope_gen_active_idx"},
		{"right index but the ecosystem is a heap filter", ecoIndexScan + "                      Filter: ((payload ->> 'ecosystem'::text) = 'npm'::text)\n" + join, "heap filter"},
		{"Index Cond without the ANY list", `                ->  Index Scan using fact_records_vuln_pkg_eco_scope_gen_active_idx on fact_records fact
                      Index Cond: ((scope_id = scope.scope_id) AND (generation_id = scope.active_generation_id))
` + join, "no Index Cond line holds the ecosystem expression"},
		{"sequential scan of fact_records", "                ->  Seq Scan on fact_records fact\n" + join, "Seq Scan on fact_records"},
		{"primary-key range", `                ->  Index Scan using fact_records_pkey on fact_records fact
                      Index Cond: (fact_id = 'x'::text)
` + join, "fact_records_pkey"},
		{"active-generation join dropped", ecoIndexScan, "drops the active-generation join"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := packageHolderEcoPlanViolation(tt.plan)
			if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
				t.Fatalf("packageHolderEcoPlanViolation() = %q, want containing %q", got, tt.want)
			}
		})
	}
}

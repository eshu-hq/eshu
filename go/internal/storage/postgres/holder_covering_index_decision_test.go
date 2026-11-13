// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// TestHolderCoveringPairIndexesStayUnbuilt pins the H-3 verdict for #10643:
// the covering pair-arm indexes fact_records_sci_cve_scope_gen_idx
// (((payload->>'cve_id'), scope_id, generation_id)) and the advisory twin
// fact_records_sci_adv_scope_gen_idx stay unbuilt. The #10564 gate priced
// them as no-build on economics (no read need at the 25-id design chunk
// against a ~6.5-11 GB permanent pair cost). The #10643 lane re-fired the
// gate on real guard pressure (hot-CVE holder reads near the 10 s guard)
// and proved the prescribed shape cannot deliver the required plan:
// on PostgreSQL 18.6 the planner generates no index-only path for a query
// that filters on an indexed expression ((payload->>'cve_id') = ANY(...)),
// so the arms keep their heap fetches with or without the pair —
// TestHolderCoveringExpressionBlocksIndexOnlyLive guards the mechanism, and
// evidence-holder-covering-index-h3.md records the proof. A follow-up that
// changes the mechanism (a generated column plus query rewrite, or a
// planner behavior change) revises this test with its migration in the
// same change.
func TestHolderCoveringPairIndexesStayUnbuilt(t *testing.T) {
	t.Parallel()

	const (
		cveCovering = "fact_records_sci_cve_scope_gen_idx"
		advCovering = "fact_records_sci_adv_scope_gen_idx"
	)

	corpus := []string{factRecordSchemaSQL}
	for _, definition := range BootstrapDefinitions() {
		corpus = append(corpus, definition.SQL)
	}

	// No-build pin: neither covering index may appear in bootstrap or any
	// migration. A follow-up that proves a buildable shape revises this
	// assertion with its migration.
	for _, sql := range corpus {
		for _, wantAbsent := range []string{cveCovering, advCovering} {
			if strings.Contains(sql, wantAbsent) {
				t.Fatalf("covering index %s present in schema corpus; H-3 verdict is no-build "+
					"(update evidence-holder-covering-index-h3.md and this test with the new proof):\n%.200s",
					wantAbsent, sql)
			}
		}
	}

	// Premise pin: the disproof assumes the existing payload-leading
	// lookup pair stays as measured (payload key + impact_status + fact_id +
	// generation_id under the finding partial predicate). A shape change
	// re-opens the mechanism question, so it must fail here first.
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"cve", []string{
			"fact_records_supply_chain_impact_lookup_idx",
			"(payload->>'cve_id')",
			"(payload->>'impact_status')",
		}},
		{"advisory", []string{
			"fact_records_supply_chain_impact_advisory_lookup_idx",
			"(payload->>'advisory_id')",
			"(payload->>'impact_status')",
		}},
	} {
		joined := strings.Join(corpus, "\n")
		for _, want := range tc.want {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s lookup premise changed: corpus missing %q; re-price H-3", tc.name, want)
			}
		}
	}

	// Decision-record pin: the no-build verdict must stay documented with
	// its mechanism proof and re-trigger where reviewers look for it.
	raw, err := os.ReadFile("evidence-holder-covering-index-h3.md")
	if err != nil {
		t.Fatalf("H-3 decision record missing: %v", err)
	}
	for _, marker := range []string{
		"H-3", "no-build", "re-trigger",
		cveCovering, advCovering, "#10564", "#10643",
		"expression", "Heap Fetches",
	} {
		if !strings.Contains(string(raw), marker) {
			t.Fatalf("decision record missing marker %q", marker)
		}
	}
}

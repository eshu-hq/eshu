// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// reopenTargetBoundary is the stable text that ends the reopen statement's
// target CTE and starts its write. The differential reads everything before
// it, so the prefilter is compared with the classification the shipped reopen
// computes, not with a hand-copied one.
const reopenTargetBoundary = "), moved AS ("

// deriveReopenClassification derives a read-only statement from the shipped
// reopen text: its target CTE up to the boundary, then a tail that returns the
// prior status of every matched row. It reports false when the boundary or the
// target CTE is missing, which is how a reshaped reopen makes the differential
// fail loudly instead of comparing against a stale copy.
func deriveReopenClassification(reopen string) (string, bool) {
	cut := strings.Index(reopen, reopenTargetBoundary)
	if cut < 0 || !strings.Contains(reopen[:cut], "WITH target AS MATERIALIZED (") {
		return "", false
	}
	return reopen[:cut] + ")\nSELECT target.prior_status FROM target ORDER BY target.work_item_id COLLATE \"C\"\n", true
}

// TestDerivedReopenClassificationIsAPrefixOfTheShippedStatement is the hermetic
// prefix guard for the derived differential statement: the derived text is a
// byte prefix of production up to the boundary, and the shipped statement
// matches the prefilter's status set. The seeded violation proves the guard
// fails when the reopen is reshaped.
func TestDerivedReopenClassificationIsAPrefixOfTheShippedStatement(t *testing.T) {
	t.Parallel()

	reopen := postgres.ReopenSupplyChainImpactHolderQueryForTest
	derived, ok := deriveReopenClassification(reopen)
	if !ok {
		t.Fatal("the shipped reopen statement no longer has the target CTE boundary the differential derives from")
	}
	cut := strings.Index(reopen, reopenTargetBoundary)
	if !strings.HasPrefix(derived, reopen[:cut]) {
		t.Fatal("derived classification is not a byte prefix of the shipped reopen statement")
	}
	if !strings.Contains(derived, postgres.ReopenSupplyChainImpactMatchedStatusesForTest) {
		t.Fatal("derived classification does not read the shared matched status set")
	}
	if !strings.Contains(postgres.FindQueuedSupplyChainImpactHoldersQueryForTest, postgres.ReopenSupplyChainImpactMatchedStatusesForTest) {
		t.Fatal("the prefilter does not read the reopen's matched status set")
	}

	// Seeded violation: a reopen without the boundary must not derive.
	reshaped := strings.Replace(reopen, reopenTargetBoundary, "), writes AS (", 1)
	if _, ok := deriveReopenClassification(reshaped); ok {
		t.Fatal("a reshaped reopen statement still derived a classification: the guard is inert")
	}
}

// TestQueuedHolderPlanViolationSeededRedGreen pins the plan checker itself and
// needs no database: a plan that sequentially scans the queue table is a
// violation, an unnamed index is refused, and both accepted shapes pass (the
// per-scope probe and the stage, domain, status range read).
func TestQueuedHolderPlanViolationSeededRedGreen(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		plan string
		bad  bool
	}{
		{"seq scan", "Seq Scan on fact_work_items work\n  Filter: (scope_id = ANY ($1))", true},
		{"seq scan beside an accepted index", "Seq Scan on fact_work_items work\nIndex Scan using fact_work_items_scope_generation_idx on fact_work_items work", true},
		{"unnamed index", "Index Scan using fact_work_items_pkey on fact_work_items work", true},
		{"scope probe", "Index Scan using fact_work_items_scope_generation_idx on fact_work_items work", false},
		{"stage domain status range", "Index Scan using fact_work_items_stage_domain_status_idx on fact_work_items work\n  Filter: (scope_id = ANY ($1))", false},
	}
	for _, tc := range cases {
		why := queuedPlanViolation(tc.plan)
		if tc.bad && why == "" {
			t.Errorf("%s: plan was accepted, want a violation: the checker is inert", tc.name)
		}
		if !tc.bad && why != "" {
			t.Errorf("%s: plan was refused (%s), want accepted", tc.name, why)
		}
	}
}

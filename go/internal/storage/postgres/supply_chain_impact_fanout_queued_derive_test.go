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

// TestQueuedHolderPlanViolationSeededRedGreen pins the plan checker itself: a
// plan that scans the queue table is a violation, and the probe shape is not.
func TestQueuedHolderPlanViolationSeededRedGreen(t *testing.T) {
	t.Parallel()

	good := "Index Scan using fact_work_items_scope_generation_idx on fact_work_items work"
	bad := "Seq Scan on fact_work_items work\n  Filter: (scope_id = ANY ($1))"
	if got := queuedPlanViolation(good); got != "" {
		t.Fatalf("probe plan flagged: %s", got)
	}
	if queuedPlanViolation(bad) == "" {
		t.Fatal("a sequential scan of fact_work_items was not flagged: the checker is inert")
	}
}

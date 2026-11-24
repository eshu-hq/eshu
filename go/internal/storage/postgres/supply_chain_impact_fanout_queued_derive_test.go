// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestQueuedHolderPrefilterReadsTheReopenStatusSet is the hermetic guard that
// the prefilter and the reopen read one matched-status set.
func TestQueuedHolderPrefilterReadsTheReopenStatusSet(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"prefilter": postgres.FindQueuedSupplyChainImpactHoldersQueryForTest,
		"reopen":    postgres.ReopenSupplyChainImpactHolderQueryForTest,
	} {
		if !strings.Contains(query, postgres.ReopenSupplyChainImpactMatchedStatusesForTest) {
			t.Fatalf("the %s statement does not read the shared matched status set", name)
		}
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

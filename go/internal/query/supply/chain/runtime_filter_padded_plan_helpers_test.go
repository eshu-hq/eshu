// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// assertSupplyChainPaddedRuntimePlans exercises the noncanonical entity path
// after the original high-cardinality plan cases have used their canonical seed.
func assertSupplyChainPaddedRuntimePlans(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	baseFilter impact.FindingFilter,
) {
	t.Helper()
	result, err := tx.ExecContext(ctx, `
UPDATE fact_records
SET payload = jsonb_set(
  payload,
  '{entity_keys}',
  jsonb_build_array('  ' || (payload->'entity_keys'->>0) || '  ')
)
WHERE fact_kind = 'reducer_workload_identity'
  AND stable_fact_key LIKE 'stable:5747:perf:workload:%'`)
	if err != nil {
		t.Fatalf("pad 100k entity keys: %v", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("count padded entity keys: %v", err)
	}
	if rows != 100000 {
		t.Fatalf("padded identity facts = %d, want 100000", rows)
	}
	if _, err := tx.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze padded entity keys: %v", err)
	}

	for _, tc := range []struct {
		name       string
		workloadID string
		wantRows   float64
	}{
		{"padded_entity_hit", runtimeNormalizedArrayWorkload, 1},
		{"padded_entity_miss", "workload:5747:absent-padded", 0},
	} {
		filter := withSupplyChainRuntimeFilterDimensions(baseFilter, tc.workloadID, "", "")
		plan := explainSupplyChainRuntimeFilterPlan(
			t, ctx, tx, tc.name, impact.ListFindingsQuery,
			supplyChainRuntimeFilterListArgs(filter)...,
		)
		if !strings.Contains(plan, "fact_records_workload_identity_entity_trim_idx") {
			t.Fatalf("%s plan missed padded entity index:\n%s", tc.name, plan)
		}
		assertSupplyChainRuntimePlanOutputRows(t, plan, tc.wantRows)
	}
}

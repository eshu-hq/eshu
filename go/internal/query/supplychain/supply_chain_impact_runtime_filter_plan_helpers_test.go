// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

type supplyChainRuntimePlan struct {
	Plan          supplyChainRuntimePlanNode `json:"Plan"`
	PlanningTime  float64                    `json:"Planning Time"`
	ExecutionTime float64                    `json:"Execution Time"`
}

type supplyChainRuntimePlanNode struct {
	IndexName        string                       `json:"Index Name"`
	ActualRows       float64                      `json:"Actual Rows"`
	ActualLoops      float64                      `json:"Actual Loops"`
	SharedHitBlocks  int64                        `json:"Shared Hit Blocks"`
	SharedReadBlocks int64                        `json:"Shared Read Blocks"`
	Plans            []supplyChainRuntimePlanNode `json:"Plans"`
}

func explainSupplyChainRuntimeFilterPlan(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	name string,
	query string,
	args ...any,
) string {
	t.Helper()
	var rawJSON []byte
	if err := tx.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		args...,
	).Scan(&rawJSON); err != nil {
		t.Fatalf("explain %s: %v", name, err)
	}
	plan := decodeSupplyChainRuntimePlan(t, name, rawJSON)
	t.Logf(
		"%s: planning=%.3fms execution=%.3fms rows=%.0f shared_hit=%d shared_read=%d",
		name,
		plan.PlanningTime,
		plan.ExecutionTime,
		plan.Plan.ActualRows*plan.Plan.ActualLoops,
		plan.Plan.SharedHitBlocks,
		plan.Plan.SharedReadBlocks,
	)
	if plan.ExecutionTime > 500 {
		t.Fatalf("%s execution = %.3fms, want <= 500ms", name, plan.ExecutionTime)
	}
	return string(rawJSON)
}

func assertSupplyChainRuntimePlanIndexRows(
	t *testing.T,
	rawJSON string,
	indexName string,
	wantRows float64,
) {
	t.Helper()
	plan := decodeSupplyChainRuntimePlan(t, indexName, []byte(rawJSON))
	gotRows, found := supplyChainRuntimePlanIndexRows(plan.Plan, indexName)
	if !found {
		t.Fatalf("plan missing index %q", indexName)
	}
	if gotRows != wantRows {
		t.Fatalf("%s matched rows = %.0f, want %.0f", indexName, gotRows, wantRows)
	}
}

func assertSupplyChainRuntimePlanOutputRows(t *testing.T, rawJSON string, wantRows float64) {
	t.Helper()
	plan := decodeSupplyChainRuntimePlan(t, "output rows", []byte(rawJSON))
	gotRows := plan.Plan.ActualRows * plan.Plan.ActualLoops
	if gotRows != wantRows {
		t.Fatalf("plan output rows = %.0f, want %.0f", gotRows, wantRows)
	}
}

func decodeSupplyChainRuntimePlan(t *testing.T, name string, rawJSON []byte) supplyChainRuntimePlan {
	t.Helper()
	var plans []supplyChainRuntimePlan
	if err := json.Unmarshal(rawJSON, &plans); err != nil {
		t.Fatalf("decode %s plan: %v", name, err)
	}
	if len(plans) != 1 {
		t.Fatalf("%s plan count = %d, want 1", name, len(plans))
	}
	return plans[0]
}

func supplyChainRuntimePlanIndexRows(
	node supplyChainRuntimePlanNode,
	indexName string,
) (float64, bool) {
	if node.IndexName == indexName {
		return node.ActualRows * node.ActualLoops, true
	}
	for _, child := range node.Plans {
		if rows, found := supplyChainRuntimePlanIndexRows(child, indexName); found {
			return rows, true
		}
	}
	return 0, false
}

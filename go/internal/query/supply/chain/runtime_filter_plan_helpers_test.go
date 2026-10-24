// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

type supplyChainRuntimePlan struct {
	Plan          supplyChainRuntimePlanNode `json:"Plan"`
	PlanningTime  float64                    `json:"Planning Time"`
	ExecutionTime float64                    `json:"Execution Time"`
}

type supplyChainRuntimePlanNode struct {
	NodeType          string                       `json:"Node Type"`
	IndexName         string                       `json:"Index Name"`
	ActualRows        float64                      `json:"Actual Rows"`
	ActualLoops       float64                      `json:"Actual Loops"`
	SharedHitBlocks   int64                        `json:"Shared Hit Blocks"`
	SharedReadBlocks  int64                        `json:"Shared Read Blocks"`
	TempReadBlocks    int64                        `json:"Temp Read Blocks"`
	TempWrittenBlocks int64                        `json:"Temp Written Blocks"`
	Plans             []supplyChainRuntimePlanNode `json:"Plans"`
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
	// Three runner-local EXPLAIN runs over the same tx snapshot, gated on
	// the stability of the last two (#8835; arbiter verdict on the ~10%
	// shard-1/4 flake). The first run warms shared_buffers: a cold-cache
	// fill reads pages the warm rerun hits, so run 1 vs run 2 can differ
	// by more than the gate band on small cases (observed 174 vs 157
	// shared blocks on legacy_workload_scalar). Wall time is reported,
	// never gated: the 400-624ms distribution of the high-cardinality
	// cases on shared CI runners made the old 500ms ceiling fail green
	// trees. Index-presence and row-count assertions at the call sites
	// stay hard; they pin the plan shape.
	runSupplyChainRuntimeFilterExplain(t, ctx, tx, name+" (warmup)", query, args...)
	first := runSupplyChainRuntimeFilterExplain(t, ctx, tx, name+" (run 2)", query, args...)
	second := runSupplyChainRuntimeFilterExplain(t, ctx, tx, name+" (run 3)", query, args...)
	assertSupplyChainRuntimePlanStability(t, name, first, second)
	return string(first.rawJSON)
}

type supplyChainRuntimeExplainProof struct {
	plan    supplyChainRuntimePlan
	rawJSON []byte
}

func runSupplyChainRuntimeFilterExplain(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	name string,
	query string,
	args ...any,
) supplyChainRuntimeExplainProof {
	t.Helper()
	var rawJSON []byte
	if err := tx.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		args...,
	).Scan(&rawJSON); err != nil {
		t.Fatalf("explain %s: %v", name, err)
	}
	proof := supplyChainRuntimeExplainProof{
		plan:    decodeSupplyChainRuntimePlan(t, name, rawJSON),
		rawJSON: rawJSON,
	}
	t.Logf(
		"%s: planning=%.3fms execution=%.3fms rows=%.0f shared_hit=%d shared_read=%d",
		name,
		proof.plan.PlanningTime,
		proof.plan.ExecutionTime,
		proof.plan.Plan.ActualRows*proof.plan.Plan.ActualLoops,
		proof.plan.Plan.SharedHitBlocks,
		proof.plan.Plan.SharedReadBlocks,
	)
	return proof
}

func assertSupplyChainRuntimePlanStability(
	t *testing.T,
	name string,
	first, second supplyChainRuntimeExplainProof,
) {
	t.Helper()
	if breach := supplyChainRuntimePlanStabilityBreach(t, name, first.plan, second.plan); breach != "" {
		t.Fatalf("%s plan unstable between runs: %s", name, breach)
	}
}

// supplyChainRuntimeBlockNoiseFloor is the block count below which the
// 1-percent gate does not fire: a path that touches a handful of blocks on
// one run and none on the other has not drifted. Seeded regressions move
// thousands of blocks, so the floor costs no signal (#7553).
const supplyChainRuntimeBlockNoiseFloor = 16

// supplyChainRuntimeBlocksDrifted is the pure predicate behind the 1-percent
// gate: drift beyond 1 percent at or above the noise floor.
func supplyChainRuntimeBlocksDrifted(first, second int64) bool {
	diff := first - second
	if diff < 0 {
		diff = -diff
	}
	ceiling := max(first, second)
	return ceiling >= supplyChainRuntimeBlockNoiseFloor && diff*100 > ceiling
}

func supplyChainRuntimePlanNodeTypes(node supplyChainRuntimePlanNode, acc []string) []string {
	acc = append(acc, node.NodeType)
	for _, child := range node.Plans {
		acc = supplyChainRuntimePlanNodeTypes(child, acc)
	}
	return acc
}

// supplyChainRuntimePlanStabilityBreach checks two consecutive EXPLAIN runs
// of one case and returns "" on pass: identical whole-tree node-type sets,
// shared touches (hit+read; the split shifts with cache warmth) within
// 1 percent, and temp read/written within 1 percent. Temp gates on
// stability rather than zero because the high-cardinality cases spill at
// stock 4MB work_mem on both runs (#7553 verdict C).
func supplyChainRuntimePlanStabilityBreach(
	t *testing.T,
	name string,
	first, second supplyChainRuntimePlan,
) string {
	t.Helper()
	firstTypes := supplyChainRuntimePlanNodeTypes(first.Plan, nil)
	secondTypes := supplyChainRuntimePlanNodeTypes(second.Plan, nil)
	if !slices.Equal(firstTypes, secondTypes) {
		return fmt.Sprintf("node set drifted between runs:\nfirst=%v\nsecond=%v", firstTypes, secondTypes)
	}
	firstShared := first.Plan.SharedHitBlocks + first.Plan.SharedReadBlocks
	secondShared := second.Plan.SharedHitBlocks + second.Plan.SharedReadBlocks
	if supplyChainRuntimeBlocksDrifted(firstShared, secondShared) {
		return fmt.Sprintf("shared blocks drifted: %d vs %d, want within 1 percent", firstShared, secondShared)
	}
	if supplyChainRuntimeBlocksDrifted(first.Plan.TempReadBlocks, second.Plan.TempReadBlocks) {
		return fmt.Sprintf("temp_read drifted: %d vs %d, want within 1 percent",
			first.Plan.TempReadBlocks, second.Plan.TempReadBlocks)
	}
	if supplyChainRuntimeBlocksDrifted(first.Plan.TempWrittenBlocks, second.Plan.TempWrittenBlocks) {
		return fmt.Sprintf("temp_written drifted: %d vs %d, want within 1 percent",
			first.Plan.TempWrittenBlocks, second.Plan.TempWrittenBlocks)
	}
	return ""
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

func supplyChainRuntimeFilterExplainArgs(filter impact.ExplanationFilter) []any {
	return []any{
		impact.FindingFactKind,
		filter.FindingID,
		filter.AdvisoryID,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.WorkloadID,
		filter.ServiceID,
		filter.ImageRef,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		impact.SuppressionReadAt(nil),
	}
}

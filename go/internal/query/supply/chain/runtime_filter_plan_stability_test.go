// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import "testing"

// Seeded-violation proof for the runner-local plan/buffer gate that replaced
// the host-bound 500ms EXPLAIN ceiling (#8835; arbiter verdict on the
// ~10% shard-1/4 flake: 400-624ms distribution split by a 500ms line on
// shared CI runners). Stable plan pairs pass; a planted node-set, shared,
// or temp drift fails. Hermetic: canned plans only, no Postgres.
func TestSupplyChainRuntimePlanStabilityGate(t *testing.T) {
	stable := supplyChainRuntimePlan{
		PlanningTime:  13.892,
		ExecutionTime: 584.278,
		Plan: supplyChainRuntimePlanNode{
			NodeType:          "Aggregate",
			ActualRows:        1,
			ActualLoops:       1,
			SharedHitBlocks:   225830,
			SharedReadBlocks:  0,
			TempReadBlocks:    2258,
			TempWrittenBlocks: 4392,
			Plans: []supplyChainRuntimePlanNode{
				{NodeType: "Index Scan", IndexName: "fact_records_ci_cd_run_correlations_environment_lookup_idx", ActualRows: 100000, ActualLoops: 1},
			},
		},
	}
	if breach := supplyChainRuntimePlanStabilityBreach(t, "stable", stable, stable); breach != "" {
		t.Fatalf("stable pair breached: %s", breach)
	}

	nodeDrifted := stable
	// Struct copy shares the Plans backing array, so copy it before
	// mutating or the planted drift leaks into the stable baseline.
	nodeDrifted.Plan.Plans = append([]supplyChainRuntimePlanNode(nil), stable.Plan.Plans...)
	nodeDrifted.Plan.Plans[0].NodeType = "Seq Scan"
	if breach := supplyChainRuntimePlanStabilityBreach(t, "node-drift", stable, nodeDrifted); breach == "" {
		t.Fatal("node-set drift passed, want breach")
	}

	sharedDrifted := stable
	sharedDrifted.Plan.SharedHitBlocks = 100000
	if breach := supplyChainRuntimePlanStabilityBreach(t, "shared-drift", stable, sharedDrifted); breach == "" {
		t.Fatal("shared-buffer drift passed, want breach")
	}

	tempDrifted := stable
	tempDrifted.Plan.TempWrittenBlocks = 8000
	if breach := supplyChainRuntimePlanStabilityBreach(t, "temp-drift", stable, tempDrifted); breach == "" {
		t.Fatal("temp-block drift passed, want breach")
	}

	// The observed cold-cache fill (174 shared blocks on the cold run,
	// 157 warm on legacy_workload_scalar) breaches: the gate compares two
	// warm runs, so a fill-sized gap is always drift, never warmth.
	cold := supplyChainRuntimePlan{Plan: supplyChainRuntimePlanNode{NodeType: "Index Scan", SharedHitBlocks: 40, SharedReadBlocks: 134}}
	warm := supplyChainRuntimePlan{Plan: supplyChainRuntimePlanNode{NodeType: "Index Scan", SharedHitBlocks: 157}}
	if breach := supplyChainRuntimePlanStabilityBreach(t, "cold-vs-warm", cold, warm); breach == "" {
		t.Fatal("cold-vs-warm pair passed, want breach")
	}

	// Below the noise floor a handful of blocks is warmth, not drift.
	quiet := supplyChainRuntimePlan{Plan: supplyChainRuntimePlanNode{NodeType: "Index Scan", SharedHitBlocks: 5}}
	quieter := supplyChainRuntimePlan{Plan: supplyChainRuntimePlanNode{NodeType: "Index Scan", SharedHitBlocks: 10}}
	if breach := supplyChainRuntimePlanStabilityBreach(t, "noise-floor", quiet, quieter); breach != "" {
		t.Fatalf("noise-floor pair breached: %s", breach)
	}

	// Zero-temp plans stay zero-temp on both runs.
	clean := supplyChainRuntimePlan{Plan: supplyChainRuntimePlanNode{NodeType: "Index Scan", SharedHitBlocks: 41, SharedReadBlocks: 133}}
	if breach := supplyChainRuntimePlanStabilityBreach(t, "clean", clean, clean); breach != "" {
		t.Fatalf("clean pair breached: %s", breach)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestAWSCloudRuntimeDriftStateARNJoinProbesARNIndexLive is the #7666
// regression. The production statement looped the active-state join once
// per AWS ARN: the planner estimated the ARN set at 1 row, put it on the
// outer side of a nested loop, and walked every active state scope's
// fact_records_scope_generation_idx per ARN (129,618 index scans and 11.16M
// buffers for 379 ARNs x 342 scopes on the shim; 14-20 minutes per run in
// production). With stale statistics the planner instead walked the whole
// ARN index once and probed ingestion_scopes per entry.
//
// The fixed statement must reach Terraform state by probing the ARN index
// with each requested ARN as the key (never walking it whole), touch
// fact_records at most once per requested ARN, and stay inside a buffer
// budget linear in the ARN count. That must hold for fresh and stale
// planner statistics, and for both the custom plan and the generic plan a
// cached prepared statement can switch to. No expression statistics object
// exists, so the plan cannot lean on one.
func TestAWSCloudRuntimeDriftStateARNJoinProbesARNIndexLive(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "fresh_statistics"
		if stale {
			name = "stale_statistics"
		}
		t.Run(name, func(t *testing.T) {
			database, ctx := openIsolatedLiveDB(t, "aws_drift_arn_plan",
				"set ESHU_POSTGRES_DSN to run the #7666 aws_cloud_runtime_drift ARN-join plan proof")
			fixture := seedAWSDriftStateARNFixture(t, ctx, database, 320, stale)

			// Generous against the fixed plan (a few buffers per ARN probe plus
			// one heap page per matched state row), and far below the per-scope
			// loop, which pays an index descent per ARN per active scope.
			maxBuffers := int64(40*len(fixture.arns) + 2000)
			// A keyed probe returns at most one row per retained generation of
			// the ARN; a walk of the whole index returns every ARN-bearing
			// state row in the table.
			maxIndexRows := float64(awsDriftARNFixtureGenerations * len(fixture.arns))

			for _, generic := range []bool{false, true} {
				plan := explainAWSDriftStateARNJoin(t, ctx, database, fixture, generic)
				assertAWSDriftStateARNPlan(t, plan, fixture, generic, maxBuffers, maxIndexRows)
			}
		})
	}
}

// assertAWSDriftStateARNPlan checks one EXPLAIN ANALYZE plan of the state
// join against the #7666 contract.
func assertAWSDriftStateARNPlan(
	t *testing.T,
	plan awsDriftARNPlanNode,
	fixture awsDriftARNFixture,
	generic bool,
	maxBuffers int64,
	maxIndexRows float64,
) {
	t.Helper()
	if got := int(plan.ActualRows); got != fixture.wantRows {
		t.Errorf("generic=%t: state join returned %d rows, want %d", generic, got, fixture.wantRows)
	}
	usesARNIndex := false
	indexRows := 0.0
	plan.walk(func(node awsDriftARNPlanNode) {
		if node.IndexName == awsDriftStateARNIndexName {
			usesARNIndex = true
			indexRows += node.ActualRows * node.ActualLoops
		}
		touchesFacts := node.RelationName == "fact_records" || strings.HasPrefix(node.IndexName, "fact_records_")
		if touchesFacts && node.ActualLoops > float64(len(fixture.arns)) {
			t.Errorf("generic=%t: %s (relation %q, index %q) looped %.0f times for %d requested ARNs; the state join must not re-run per ARN per scope",
				generic, node.NodeType, node.RelationName, node.IndexName, node.ActualLoops, len(fixture.arns))
		}
	})
	if !usesARNIndex {
		t.Errorf("generic=%t: plan does not probe %s for the state side", generic, awsDriftStateARNIndexName)
	}
	if indexRows > maxIndexRows {
		t.Errorf("generic=%t: %s produced %.0f entries, want <= %.0f; the plan walked the index instead of probing it per ARN",
			generic, awsDriftStateARNIndexName, indexRows, maxIndexRows)
	}
	buffers := plan.SharedHitBlocks + plan.SharedReadBlocks
	if buffers > maxBuffers {
		t.Errorf("generic=%t: state join touched %d shared buffers, want <= %d", generic, buffers, maxBuffers)
	}
	t.Logf("generic=%t: rows=%.0f shared_buffers=%d (budget %d) arn_index_entries=%.0f (budget %.0f) arns=%d",
		generic, plan.ActualRows, buffers, maxBuffers, indexRows, maxIndexRows, len(fixture.arns))
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// supplyChainImpactBudgetRelPath is the committed cost budget for the
// supply_chain_impact scenario (C-14 issue #4367, Tier-2 Postgres cost
// slice). PostgresSupplyChainImpactWriter operates over
// []SupplyChainImpactFinding Go values, not a CanonicalMaterialization, so
// the fixture findings live inline in this file, matching the
// container_image_identity_cost_test.go convention.
var supplyChainImpactBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "supply-chain-impact.cost-budget.json",
)

const supplyChainImpactCostIntentID = "intent-supply-chain-impact-cost"

// supplyChainImpactFixtureFindings is the deterministic input for this
// scenario: two affected_exact findings for distinct CVEs in one scope.
// WriteSupplyChainImpactFindings (go/internal/reducer/supply_chain_impact_
// writer.go) issues one ExecContext PER finding in a loop — there is no
// reducerBatchInsertFacts call here — so this writer's Postgres write cost is
// O(N) round-trips, not O(N/batchSize).
func supplyChainImpactFixtureFindings() []reducer.SupplyChainImpactFinding {
	row := func(id string) reducer.SupplyChainImpactFinding {
		return reducer.SupplyChainImpactFinding{
			CVEID:           "CVE-2026-" + id,
			AdvisoryID:      "GHSA-" + id,
			PackageID:       "npm:left-pad@" + id,
			Ecosystem:       "npm",
			PackageName:     "left-pad",
			PURL:            "pkg:npm/left-pad@" + id,
			ObservedVersion: "1.0." + id,
			Status:          reducer.SupplyChainImpactAffectedExact,
			RepositoryID:    "repo:team-api",
			CanonicalWrites: 1,
		}
	}
	return []reducer.SupplyChainImpactFinding{row("1"), row("2")}
}

// TestCostBudget_SupplyChainImpact is the exact-equality cost-counting gate
// for the supply_chain_impact reducer projection. It drives the production
// PostgresSupplyChainImpactWriter.WriteSupplyChainImpactFindings over two
// findings in one scope, through a real InstrumentedDB-backed
// sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count EQUALS the committed budget exactly.
//
// This writer has no batched insert path: WriteSupplyChainImpactFindings
// issues one ExecContext per finding in a plain loop, so its Postgres write
// cost is inherently O(N) round-trips — there is no batching boundary for a
// within-writer N+1 control to break (splitting N findings across N separate
// Write calls costs the identical N round-trips as one call carrying all N;
// confirmed empirically for the structurally identical aws_cloud_runtime_
// drift writer, see aws_cloud_runtime_drift_cost_test.go's "N+1 control
// shape" doc comment). The exact-equality assertion (== budget) is the
// regression gate for this domain. Migrating this writer onto the shared
// reducerBatchInsertFacts bounded bulk-insert path is a follow-on tracked
// separately (C-14 issue #4367 orchestration); this budget intentionally
// encodes the CURRENT known per-row write amplification rather than
// absorbing it silently.
func TestCostBudget_SupplyChainImpact(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, supplyChainImpactBudgetRelPath)
	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresSupplyChainImpactWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), reducer.SupplyChainImpactWrite{
		IntentID:     supplyChainImpactCostIntentID,
		ScopeID:      "repo:team-api",
		GenerationID: "generation-supply-chain-impact-cost",
		SourceSystem: "github_dependabot",
		Cause:        "reducer/supply_chain_impact",
		Findings:     supplyChainImpactFixtureFindings(),
	})
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if result.FactsWritten != 2 {
		t.Fatalf("FactsWritten = %d, want 2", result.FactsWritten)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_postgres_query_duration_seconds's
	// write-attributed observation count off the real otel reader, asserted
	// EXACT (this per-row writer's cost is O(N)).
	writes := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
	wantWrites, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_postgres_query_duration_seconds")
	}
	if writes != uint64(wantWrites) {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds write observations = %d, want exactly %d "+
				"(scenario=%s): this per-row writer's cost is O(N) round-trips — any deviation, "+
				"up or down, is a regression against the committed known-amplification budget",
			writes, wantWrites, budget.Scenario,
		)
	}
	if writes == 0 {
		t.Fatal("eshu_dp_postgres_query_duration_seconds write observations = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw ExecContext call count from the counting fake.
	execs := fake.totalExecs()
	if wantExecs, ok := budget.Budgets["statements_executed"]; ok {
		if execs != wantExecs {
			t.Fatalf(
				"statements_executed = %d, want exactly %d (scenario=%s)",
				execs, wantExecs, budget.Scenario,
			)
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_postgres_query_duration_seconds_writes=%d (budget=%d, exact) statements_executed=%d (budget=%d, exact)",
		budget.Scenario, writes, wantWrites, execs, budget.Budgets["statements_executed"],
	)
}

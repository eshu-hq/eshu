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
// writer.go) now calls the shared reducerBatchInsertVersionedFacts bounded
// chunked bulk insert (issue #5317), so two findings fit in one 1000-row
// chunk and cost exactly one ExecContext round-trip.
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

// TestCostBudget_SupplyChainImpact is the positive cost-counting gate for the
// supply_chain_impact reducer projection. It drives the production
// PostgresSupplyChainImpactWriter.WriteSupplyChainImpactFindings over two
// findings in one scope, through a real InstrumentedDB-backed
// sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count is within the committed budget.
//
// WriteSupplyChainImpactFindings now calls the shared
// reducerBatchInsertVersionedFacts bounded chunked bulk insert (issue #5317)
// instead of one ExecContext per finding, so two findings fit one chunk and
// this scenario asserts exactly one write observation. The companion N+1
// negative control below (TestCostBudget_SupplyChainImpact_N1_ExceedsBudget)
// proves the budget still catches a per-finding regression.
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
	// write-attributed observation count off the real otel reader.
	writes := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
	maxWrites, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_postgres_query_duration_seconds")
	}
	if writes > uint64(maxWrites) {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds write observations = %d exceeds budget %d "+
				"(scenario=%s): algorithmic regression detected",
			writes, maxWrites, budget.Scenario,
		)
	}
	if writes == 0 {
		t.Fatal("eshu_dp_postgres_query_duration_seconds write observations = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw ExecContext call count from the counting fake.
	execs := fake.totalExecs()
	if maxExecs, ok := budget.Budgets["statements_executed"]; ok {
		if execs > maxExecs {
			t.Fatalf(
				"statements_executed = %d exceeds budget %d (scenario=%s): too many Postgres write operations",
				execs, maxExecs, budget.Scenario,
			)
		}
		if execs == 0 {
			t.Fatal("statements_executed = 0: fake not recording (false green guard)")
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_postgres_query_duration_seconds_writes=%d (budget=%d) statements_executed=%d (budget=%d)",
		budget.Scenario, writes, maxWrites, execs, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_SupplyChainImpact_N1_ExceedsBudget is the mandatory negative
// control, run through the SAME production batched dispatch as the positive
// test. It calls WriteSupplyChainImpactFindings once per fixture finding
// instead of once for the whole batch — the classic N+1 anti-pattern for a
// batched writer — and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget.
func TestCostBudget_SupplyChainImpact_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, supplyChainImpactBudgetRelPath)
	findings := supplyChainImpactFixtureFindings()
	if len(findings) < 2 {
		t.Fatalf("N+1 control needs >=2 findings to exceed the budget; fixture has %d", len(findings))
	}

	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresSupplyChainImpactWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	for _, finding := range findings {
		if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), reducer.SupplyChainImpactWrite{
			IntentID:     supplyChainImpactCostIntentID,
			ScopeID:      "repo:team-api",
			GenerationID: "generation-supply-chain-impact-cost",
			SourceSystem: "github_dependabot",
			Cause:        "reducer/supply_chain_impact",
			Findings:     []reducer.SupplyChainImpactFinding{finding},
		}); err != nil {
			t.Fatalf("N+1 WriteSupplyChainImpactFindings() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	writes := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
	maxWrites, ok := budget.Budgets["eshu_dp_postgres_query_duration_seconds"]
	if !ok {
		t.Fatal("budget has no eshu_dp_postgres_query_duration_seconds entry")
	}

	if writes <= uint64(maxWrites) {
		t.Fatalf(
			"N+1 negative control: eshu_dp_postgres_query_duration_seconds write observations = %d did NOT "+
				"exceed budget %d — budget is too loose to catch N+1 regressions or the negative control is "+
				"generating too few writes; tighten the budget or increase the N+1 fanout",
			writes, maxWrites,
		)
	}

	t.Logf(
		"N+1 negative control passed: eshu_dp_postgres_query_duration_seconds write observations = %d > budget %d "+
			"(N=%d findings, scenario=%s)",
		writes, maxWrites, len(findings), budget.Scenario,
	)
}

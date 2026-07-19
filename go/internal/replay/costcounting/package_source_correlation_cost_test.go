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

// packageSourceCorrelationBudgetRelPath is the committed cost budget for the
// package_source_correlation scenario (C-14 issue #4367, Tier-2 Postgres
// cost slice). PostgresPackageCorrelationWriter operates over
// []PackageSourceCorrelationDecision Go values (the ownership decisions this
// domain projects), not a CanonicalMaterialization, so the fixture decisions
// live inline in this file, matching the container_image_identity_cost_
// test.go convention.
var packageSourceCorrelationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "package-source-correlation.cost-budget.json",
)

const packageSourceCorrelationCostIntentID = "intent-package-source-correlation-cost"

// packageSourceCorrelationFixtureDecisions is the deterministic input for
// this scenario: two exact-outcome package OWNERSHIP decisions for distinct
// packages in one scope. WritePackageCorrelations
// (go/internal/reducer/package_correlation_writer.go) now combines the
// ownership, consumption, and publication decision lists into ONE
// reducerBatchInsertVersionedFacts bulk-insert call (issue #5317) instead of
// one ExecContext per decision spread across three separate loops; this
// scenario exercises ONLY OwnershipDecisions (leaving Consumption/
// PublicationDecisions empty) since package_source_correlation is the
// ownership-candidate projection — consumption and publication are covered
// by the domain's own writer share, not this manifest surface.
func packageSourceCorrelationFixtureDecisions() []reducer.PackageSourceCorrelationDecision {
	row := func(id string) reducer.PackageSourceCorrelationDecision {
		return reducer.PackageSourceCorrelationDecision{
			PackageID:    "npm:left-pad-" + id,
			VersionID:    "1.0." + id,
			HintKind:     "repository_url",
			SourceURL:    "https://github.com/team/left-pad-" + id,
			RepositoryID: "repo:team-left-pad-" + id,
			Outcome:      reducer.PackageSourceCorrelationExact,
		}
	}
	return []reducer.PackageSourceCorrelationDecision{row("a"), row("b")}
}

// TestCostBudget_PackageSourceCorrelation is the positive cost-counting gate
// for the package_source_correlation reducer projection. It drives the
// production PostgresPackageCorrelationWriter.WritePackageCorrelations over
// two ownership decisions in one scope (no consumption or publication
// decisions), through a real InstrumentedDB-backed sdkmetric.ManualReader,
// then asserts eshu_dp_postgres_query_duration_seconds's write-attributed
// observation count is within the committed budget.
//
// WritePackageCorrelations now combines its three decision lists (ownership,
// consumption, publication) into ONE reducerBatchInsertVersionedFacts
// bulk-insert call (issue #5317) instead of one ExecContext per decision, so
// two ownership decisions fit one chunk and this scenario asserts exactly one
// write observation. The companion N+1 negative control below
// (TestCostBudget_PackageSourceCorrelation_N1_ExceedsBudget) proves the
// budget still catches a per-decision regression.
func TestCostBudget_PackageSourceCorrelation(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, packageSourceCorrelationBudgetRelPath)
	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresPackageCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	result, err := writer.WritePackageCorrelations(context.Background(), reducer.PackageCorrelationWrite{
		IntentID:           packageSourceCorrelationCostIntentID,
		ScopeID:            "repo:team-left-pad",
		GenerationID:       "generation-package-source-correlation-cost",
		SourceSystem:       "npm",
		Cause:              "reducer/package_source_correlation",
		OwnershipDecisions: packageSourceCorrelationFixtureDecisions(),
	})
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v", err)
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

// TestCostBudget_PackageSourceCorrelation_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production batched dispatch as the
// positive test. It calls WritePackageCorrelations once per fixture ownership
// decision instead of once for the whole batch — the classic N+1
// anti-pattern for a batched writer — and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget.
func TestCostBudget_PackageSourceCorrelation_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, packageSourceCorrelationBudgetRelPath)
	decisions := packageSourceCorrelationFixtureDecisions()
	if len(decisions) < 2 {
		t.Fatalf("N+1 control needs >=2 decisions to exceed the budget; fixture has %d", len(decisions))
	}

	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresPackageCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	for _, decision := range decisions {
		if _, err := writer.WritePackageCorrelations(context.Background(), reducer.PackageCorrelationWrite{
			IntentID:           packageSourceCorrelationCostIntentID,
			ScopeID:            "repo:team-left-pad",
			GenerationID:       "generation-package-source-correlation-cost",
			SourceSystem:       "npm",
			Cause:              "reducer/package_source_correlation",
			OwnershipDecisions: []reducer.PackageSourceCorrelationDecision{decision},
		}); err != nil {
			t.Fatalf("N+1 WritePackageCorrelations() error = %v", err)
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
			"(N=%d decisions, scenario=%s)",
		writes, maxWrites, len(decisions), budget.Scenario,
	)
}

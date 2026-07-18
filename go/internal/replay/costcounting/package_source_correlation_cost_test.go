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
// (go/internal/reducer/package_correlation_writer.go) fans out over three
// separate decision loops (ownership, consumption, publication), each calling
// w.writePayload -> one ExecContext per decision; this scenario exercises
// ONLY OwnershipDecisions (leaving Consumption/PublicationDecisions empty)
// since package_source_correlation is the ownership-candidate projection —
// consumption and publication are covered by the domain's own writer share,
// not this manifest surface. There is no reducerBatchInsertFacts call
// anywhere in this writer, so its Postgres write cost is O(N) round-trips per
// decision list, not O(N/batchSize).
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

// TestCostBudget_PackageSourceCorrelation is the exact-equality cost-counting
// gate for the package_source_correlation reducer projection. It drives the
// production PostgresPackageCorrelationWriter.WritePackageCorrelations over
// two ownership decisions in one scope (no consumption or publication
// decisions), through a real InstrumentedDB-backed sdkmetric.ManualReader,
// then asserts eshu_dp_postgres_query_duration_seconds's write-attributed
// observation count EQUALS the committed budget exactly.
//
// This writer has no batched insert path: each of its three decision loops
// (ownership, consumption, publication) issues one ExecContext per decision
// via w.writePayload, so its Postgres write cost is inherently O(N)
// round-trips per list — there is no batching boundary for a within-writer
// N+1 control to break (splitting N decisions across N separate Write calls
// costs the identical N round-trips as one call carrying all N; confirmed
// empirically for the structurally identical aws_cloud_runtime_drift writer,
// see aws_cloud_runtime_drift_cost_test.go's "N+1 control shape" doc
// comment). The exact-equality assertion (== budget) is the regression gate
// for this domain. Migrating this writer onto the shared
// reducerBatchInsertFacts bounded bulk-insert path is a follow-on tracked
// separately (C-14 issue #4367 orchestration); this budget intentionally
// encodes the CURRENT known per-row write amplification rather than
// absorbing it silently.
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

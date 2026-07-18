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

// securityAlertReconciliationBudgetRelPath is the committed cost budget for
// the security_alert_reconciliation scenario (C-14 issue #4367, Tier-2
// Postgres cost slice). PostgresSecurityAlertReconciliationWriter operates
// over []SecurityAlertReconciliationDecision Go values, not a
// CanonicalMaterialization, so the fixture decisions live inline in this
// file, matching the container_image_identity_cost_test.go convention.
var securityAlertReconciliationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "security-alert-reconciliation.cost-budget.json",
)

const securityAlertReconciliationCostIntentID = "intent-security-alert-reconciliation-cost"

// securityAlertReconciliationFixtureDecisions is the deterministic input for
// this scenario: two matched-status decisions for distinct provider alerts in
// one scope. WriteSecurityAlertReconciliations (go/internal/reducer/
// security_alert_reconciliation_writer.go) issues one ExecContext PER
// decision in a loop — there is no reducerBatchInsertFacts call here — so
// this writer's Postgres write cost is O(N) round-trips, not O(N/batchSize).
func securityAlertReconciliationFixtureDecisions() []reducer.SecurityAlertReconciliationDecision {
	row := func(id string) reducer.SecurityAlertReconciliationDecision {
		return reducer.SecurityAlertReconciliationDecision{
			Provider:             "github_dependabot",
			ProviderAlertID:      "alert-" + id,
			ProviderAlertNumber:  1,
			ProviderRepositoryID: "repo:team-api",
			RepositoryID:         "repo:team-api",
			PackageID:            "npm:left-pad@" + id,
			Ecosystem:            "npm",
			PackageName:          "left-pad",
			CVEIDs:               []string{"CVE-2026-" + id},
			Status:               reducer.SecurityAlertReconciliationMatched,
			ObservedVersion:      "1.0." + id,
		}
	}
	return []reducer.SecurityAlertReconciliationDecision{row("1"), row("2")}
}

// TestCostBudget_SecurityAlertReconciliation is the exact-equality
// cost-counting gate for the security_alert_reconciliation reducer
// projection. It drives the production
// PostgresSecurityAlertReconciliationWriter.WriteSecurityAlertReconciliations
// over two decisions in one scope, through a real InstrumentedDB-backed
// sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count EQUALS the committed budget exactly.
//
// This writer has no batched insert path: WriteSecurityAlertReconciliations
// issues one ExecContext per decision in a plain loop, so its Postgres write
// cost is inherently O(N) round-trips — there is no batching boundary for a
// within-writer N+1 control to break (splitting N decisions across N separate
// Write calls costs the identical N round-trips as one call carrying all N;
// confirmed empirically for the structurally identical aws_cloud_runtime_
// drift writer, see aws_cloud_runtime_drift_cost_test.go's "N+1 control
// shape" doc comment). The exact-equality assertion (== budget) is the
// regression gate for this domain. Migrating this writer onto the shared
// reducerBatchInsertFacts bounded bulk-insert path is a follow-on tracked
// separately (C-14 issue #4367 orchestration); this budget intentionally
// encodes the CURRENT known per-row write amplification rather than
// absorbing it silently.
func TestCostBudget_SecurityAlertReconciliation(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, securityAlertReconciliationBudgetRelPath)
	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresSecurityAlertReconciliationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	result, err := writer.WriteSecurityAlertReconciliations(context.Background(), reducer.SecurityAlertReconciliationWrite{
		IntentID:     securityAlertReconciliationCostIntentID,
		ScopeID:      "repo:team-api",
		GenerationID: "generation-security-alert-reconciliation-cost",
		SourceSystem: "github_dependabot",
		Cause:        "reducer/security_alert_reconciliation",
		Decisions:    securityAlertReconciliationFixtureDecisions(),
	})
	if err != nil {
		t.Fatalf("WriteSecurityAlertReconciliations() error = %v", err)
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

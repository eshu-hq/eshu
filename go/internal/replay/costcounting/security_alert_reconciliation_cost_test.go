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
// security_alert_reconciliation_writer.go) now calls the shared
// reducerBatchInsertFacts bounded chunked bulk insert (issue #5317), so two
// decisions fit in one 1000-row chunk and cost exactly one ExecContext
// round-trip.
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

// TestCostBudget_SecurityAlertReconciliation is the positive cost-counting
// gate for the security_alert_reconciliation reducer projection. It drives
// the production
// PostgresSecurityAlertReconciliationWriter.WriteSecurityAlertReconciliations
// over two decisions in one scope, through a real InstrumentedDB-backed
// sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count is within the committed budget.
//
// WriteSecurityAlertReconciliations now calls the shared
// reducerBatchInsertFacts bounded chunked bulk insert (issue #5317) instead of
// one ExecContext per decision, so two decisions fit one chunk and this
// scenario asserts exactly one write observation. The companion N+1 negative
// control below (TestCostBudget_SecurityAlertReconciliation_N1_ExceedsBudget)
// proves the budget still catches a per-decision regression.
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

// TestCostBudget_SecurityAlertReconciliation_N1_ExceedsBudget is the
// mandatory negative control, run through the SAME production batched
// dispatch as the positive test. It calls
// WriteSecurityAlertReconciliations once per fixture decision instead of once
// for the whole batch — the classic N+1 anti-pattern for a batched writer —
// and asserts the accumulated eshu_dp_postgres_query_duration_seconds write
// observation count EXCEEDS the committed budget.
func TestCostBudget_SecurityAlertReconciliation_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, securityAlertReconciliationBudgetRelPath)
	decisions := securityAlertReconciliationFixtureDecisions()
	if len(decisions) < 2 {
		t.Fatalf("N+1 control needs >=2 decisions to exceed the budget; fixture has %d", len(decisions))
	}

	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresSecurityAlertReconciliationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	for _, decision := range decisions {
		if _, err := writer.WriteSecurityAlertReconciliations(context.Background(), reducer.SecurityAlertReconciliationWrite{
			IntentID:     securityAlertReconciliationCostIntentID,
			ScopeID:      "repo:team-api",
			GenerationID: "generation-security-alert-reconciliation-cost",
			SourceSystem: "github_dependabot",
			Cause:        "reducer/security_alert_reconciliation",
			Decisions:    []reducer.SecurityAlertReconciliationDecision{decision},
		}); err != nil {
			t.Fatalf("N+1 WriteSecurityAlertReconciliations() error = %v", err)
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

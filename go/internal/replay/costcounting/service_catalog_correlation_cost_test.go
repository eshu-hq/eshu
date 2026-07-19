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

// serviceCatalogCorrelationBudgetRelPath is the committed cost budget for the
// service_catalog_correlation scenario (C-14 issue #4367, Tier-2 Postgres
// cost slice). PostgresServiceCatalogCorrelationWriter operates over
// []ServiceCatalogCorrelationDecision Go values, not a
// CanonicalMaterialization, so the fixture decisions live inline in this
// file, matching the container_image_identity_cost_test.go convention.
var serviceCatalogCorrelationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "service-catalog-correlation.cost-budget.json",
)

const serviceCatalogCorrelationCostIntentID = "intent-service-catalog-correlation-cost"

// serviceCatalogCorrelationFixtureDecisions is the deterministic input for
// this scenario: two correlated decisions for distinct provider entity
// references in one scope. WriteServiceCatalogCorrelations
// (go/internal/reducer/service_catalog_correlation_writer.go) now calls the
// shared reducerBatchInsertFacts bounded chunked bulk insert (issue #5317),
// the same batching container_image_identity/ci_cd_run_correlation/
// sbom_attestation_attachment already use, so two decisions fit in one 1000-row
// chunk and cost exactly one ExecContext round-trip.
func serviceCatalogCorrelationFixtureDecisions() []reducer.ServiceCatalogCorrelationDecision {
	row := func(id string) reducer.ServiceCatalogCorrelationDecision {
		return reducer.ServiceCatalogCorrelationDecision{
			Provider:     "backstage",
			EntityRef:    "component:default/api-" + id,
			EntityType:   "component",
			DisplayName:  "api-" + id,
			RepositoryID: "repo:team-api-" + id,
			ServiceID:    "service:api-" + id,
			Outcome:      reducer.ServiceCatalogCorrelationExact,
			Lifecycle:    "production",
			Tier:         "1",
		}
	}
	return []reducer.ServiceCatalogCorrelationDecision{row("a"), row("b")}
}

// TestCostBudget_ServiceCatalogCorrelation is the positive cost-counting gate
// for the service_catalog_correlation reducer projection. It drives the
// production PostgresServiceCatalogCorrelationWriter.WriteServiceCatalog
// Correlations over two decisions in one scope, through a real
// InstrumentedDB-backed sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count is within the committed budget.
//
// WriteServiceCatalogCorrelations now calls the shared reducerBatchInsertFacts
// bounded chunked bulk insert (go/internal/reducer/reducer_fact_batch_insert.go,
// batch size 1000, issue #5317) instead of one ExecContext per decision, so two
// decisions fit one chunk and this scenario asserts exactly one write
// observation. The companion N+1 negative control below
// (TestCostBudget_ServiceCatalogCorrelation_N1_ExceedsBudget) proves the
// budget still catches a per-decision regression.
func TestCostBudget_ServiceCatalogCorrelation(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, serviceCatalogCorrelationBudgetRelPath)
	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresServiceCatalogCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	result, err := writer.WriteServiceCatalogCorrelations(context.Background(), reducer.ServiceCatalogCorrelationWrite{
		IntentID:     serviceCatalogCorrelationCostIntentID,
		ScopeID:      "repo:team-api",
		GenerationID: "generation-service-catalog-correlation-cost",
		SourceSystem: "backstage",
		Cause:        "reducer/service_catalog_correlation",
		Decisions:    serviceCatalogCorrelationFixtureDecisions(),
	})
	if err != nil {
		t.Fatalf("WriteServiceCatalogCorrelations() error = %v", err)
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

// TestCostBudget_ServiceCatalogCorrelation_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production batched dispatch as the
// positive test. It calls WriteServiceCatalogCorrelations once per fixture
// decision instead of once for the whole batch — the classic N+1 anti-pattern
// for a batched writer — and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget.
func TestCostBudget_ServiceCatalogCorrelation_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, serviceCatalogCorrelationBudgetRelPath)
	decisions := serviceCatalogCorrelationFixtureDecisions()
	if len(decisions) < 2 {
		t.Fatalf("N+1 control needs >=2 decisions to exceed the budget; fixture has %d", len(decisions))
	}

	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresServiceCatalogCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	for _, decision := range decisions {
		if _, err := writer.WriteServiceCatalogCorrelations(context.Background(), reducer.ServiceCatalogCorrelationWrite{
			IntentID:     serviceCatalogCorrelationCostIntentID,
			ScopeID:      "repo:team-api",
			GenerationID: "generation-service-catalog-correlation-cost",
			SourceSystem: "backstage",
			Cause:        "reducer/service_catalog_correlation",
			Decisions:    []reducer.ServiceCatalogCorrelationDecision{decision},
		}); err != nil {
			t.Fatalf("N+1 WriteServiceCatalogCorrelations() error = %v", err)
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

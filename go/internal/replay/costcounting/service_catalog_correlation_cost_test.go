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
// (go/internal/reducer/service_catalog_correlation_writer.go) issues one
// ExecContext PER decision in a loop — unlike container_image_identity/
// ci_cd_run_correlation/sbom_attestation_attachment, there is no
// reducerBatchInsertFacts call here — so this writer's Postgres write cost is
// O(N) round-trips, not O(N/batchSize).
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

// TestCostBudget_ServiceCatalogCorrelation is the exact-equality cost-counting
// gate for the service_catalog_correlation reducer projection. It drives the
// production PostgresServiceCatalogCorrelationWriter.WriteServiceCatalog
// Correlations over two decisions in one scope, through a real
// InstrumentedDB-backed sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count EQUALS the committed budget exactly.
//
// This writer has no batched insert path (unlike container_image_identity/
// ci_cd_run_correlation/sbom_attestation_attachment): WriteServiceCatalog
// Correlations issues one ExecContext per decision in a plain loop
// (go/internal/reducer/service_catalog_correlation_writer.go), so its
// Postgres write cost is inherently O(N) round-trips — there is no batching
// boundary for a within-writer N+1 control to break, since splitting the SAME
// N decisions across N separate Write calls costs the identical N
// ExecContext round-trips as one Write call carrying all N (this was
// confirmed empirically for the structurally identical aws_cloud_runtime_
// drift writer — see aws_cloud_runtime_drift_cost_test.go's "N+1 control
// shape" doc comment). The exact-equality assertion here (== budget, not <=
// budget) is therefore the regression gate for this domain: any change that
// adds or removes a round-trip per decision — an extra read-back, a retry
// without idempotency, or a batching migration that changes the count — trips
// it either direction. Migrating this writer to the shared
// reducerBatchInsertFacts bounded bulk-insert path (as container_image_
// identity/ci_cd_run_correlation/sbom_attestation_attachment already use) is
// a follow-on tracked separately (C-14 issue #4367 orchestration); this
// budget intentionally encodes the CURRENT known per-row write amplification
// rather than silently absorbing it.
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
	// write-attributed observation count off the real otel reader, asserted
	// EXACT (this per-row writer's cost is O(N); == pins the current known
	// amplification rather than only capping a ceiling).
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

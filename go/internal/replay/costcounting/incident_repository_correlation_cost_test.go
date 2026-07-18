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

// incidentRepositoryCorrelationBudgetRelPath is the committed cost budget for
// the incident_repository_correlation scenario (C-14 issue #4367, Tier-2
// Postgres cost slice). PostgresIncidentRepositoryCorrelationWriter operates
// over []IncidentRepositoryCorrelationDecision Go values, not a
// CanonicalMaterialization, so the fixture decisions live inline in this
// file, matching the container_image_identity_cost_test.go convention.
var incidentRepositoryCorrelationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "incident-repository-correlation.cost-budget.json",
)

const incidentRepositoryCorrelationCostIntentID = "intent-incident-repository-correlation-cost"

// incidentRepositoryCorrelationFixtureDecisions is the deterministic input
// for this scenario: two exact-outcome decisions for distinct provider
// services in one scope. WriteIncidentRepositoryCorrelations
// (go/internal/reducer/incident_repository_correlation_writer.go) issues one
// ExecContext PER decision in a loop — there is no reducerBatchInsertFacts
// call here — so this writer's Postgres write cost is O(N) round-trips, not
// O(N/batchSize).
func incidentRepositoryCorrelationFixtureDecisions() []reducer.IncidentRepositoryCorrelationDecision {
	row := func(id string) reducer.IncidentRepositoryCorrelationDecision {
		return reducer.IncidentRepositoryCorrelationDecision{
			Provider:          "pagerduty",
			ProviderServiceID: "service-" + id,
			BackendKind:       "s3",
			LocatorHash:       "locator-" + id,
			RepositoryID:      "repo:team-api-" + id,
			Outcome:           reducer.IncidentRepositoryCorrelationExact,
		}
	}
	return []reducer.IncidentRepositoryCorrelationDecision{row("a"), row("b")}
}

// TestCostBudget_IncidentRepositoryCorrelation is the exact-equality
// cost-counting gate for the incident_repository_correlation reducer
// projection. It drives the production
// PostgresIncidentRepositoryCorrelationWriter.WriteIncidentRepositoryCorrelations
// over two decisions in one scope, through a real InstrumentedDB-backed
// sdkmetric.ManualReader, then asserts
// eshu_dp_postgres_query_duration_seconds's write-attributed observation
// count EQUALS the committed budget exactly.
//
// This writer has no batched insert path: WriteIncidentRepositoryCorrelations
// issues one ExecContext per decision in a plain loop, so its Postgres write
// cost is inherently O(N) round-trips — there is no batching boundary for a
// within-writer N+1 control to break (splitting N decisions across N
// separate Write calls costs the identical N round-trips as one call
// carrying all N; confirmed empirically for the structurally identical
// aws_cloud_runtime_drift writer, see aws_cloud_runtime_drift_cost_test.go's
// "N+1 control shape" doc comment). The exact-equality assertion (== budget)
// is the regression gate for this domain. Migrating this writer onto the
// shared reducerBatchInsertFacts bounded bulk-insert path is a follow-on
// tracked separately (C-14 issue #4367 orchestration); this budget
// intentionally encodes the CURRENT known per-row write amplification rather
// than absorbing it silently.
func TestCostBudget_IncidentRepositoryCorrelation(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, incidentRepositoryCorrelationBudgetRelPath)
	fake := &countingExecQueryer{}
	db, reader := newInstrumentedReducerDB(t, fake)
	writer := reducer.PostgresIncidentRepositoryCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}

	result, err := writer.WriteIncidentRepositoryCorrelations(context.Background(), reducer.IncidentRepositoryCorrelationWrite{
		IntentID:     incidentRepositoryCorrelationCostIntentID,
		ScopeID:      "state_snapshot:s3:team-api",
		GenerationID: "generation-incident-repository-correlation-cost",
		SourceSystem: "pagerduty",
		Cause:        "reducer/incident_repository_correlation",
		Decisions:    incidentRepositoryCorrelationFixtureDecisions(),
	})
	if err != nil {
		t.Fatalf("WriteIncidentRepositoryCorrelations() error = %v", err)
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

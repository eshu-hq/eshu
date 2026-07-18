// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// cloudAssetResolutionBudgetRelPath is the committed cost budget for the
// cloud_asset_resolution scenario (C-14 issue #4367, Tier-2 Postgres cost
// slice). PostgresCloudAssetResolutionWriter operates over one
// CloudAssetResolutionWrite Go value per call, not a CanonicalMaterialization,
// so the fixture writes live inline in this file, matching the
// container_image_identity_cost_test.go convention.
var cloudAssetResolutionBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "cloud-asset-resolution.cost-budget.json",
)

const cloudAssetResolutionCostIntentID = "intent-cloud-asset-resolution-cost"

// newInstrumentedCloudAssetResolutionWriter builds the PRODUCTION Postgres
// write dispatch for this domain: reducer.PostgresCloudAssetResolutionWriter
// over a postgres.InstrumentedDB (StoreName "reducer") wrapping a
// countingExecQueryer. Unlike container_image_identity/ci_cd_run_correlation/
// sbom_attestation_attachment (all batched via reducerBatchInsertFacts),
// WriteCloudAssetResolution (go/internal/reducer/cloud_asset_resolution_
// writer.go) persists exactly ONE canonical fact record per call via
// canonicalReducerFactInsertQuery — there is no []Decision slice to batch,
// the identity is the EntityKeys/RelatedScopeIDs the ONE write already
// carries, so one production call for a cross-scope resolution costs exactly
// one ExecContext round-trip regardless of how many entity keys that single
// resolution correlates.
func newInstrumentedCloudAssetResolutionWriter(t *testing.T) (
	writer reducer.PostgresCloudAssetResolutionWriter,
	fake *countingExecQueryer,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	fake = &countingExecQueryer{}
	db, manualReader := newInstrumentedReducerDB(t, fake)
	writer = reducer.PostgresCloudAssetResolutionWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}
	return writer, fake, manualReader
}

// TestCostBudget_CloudAssetResolution is the positive cost-counting gate for
// the cloud_asset_resolution reducer projection. It drives the production
// PostgresCloudAssetResolutionWriter.WriteCloudAssetResolution ONCE with a
// resolution that correlates two distinct entity keys across two related
// scopes — the real production shape: one reducer intent resolves one
// cross-scope cloud asset identity, however many entity keys that identity
// spans — through a real InstrumentedDB-backed sdkmetric.ManualReader, then
// asserts eshu_dp_postgres_query_duration_seconds's write-attributed
// observation count is within the committed budget.
func TestCostBudget_CloudAssetResolution(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, cloudAssetResolutionBudgetRelPath)
	writer, fake, reader := newInstrumentedCloudAssetResolutionWriter(t)

	result, err := writer.WriteCloudAssetResolution(context.Background(), reducer.CloudAssetResolutionWrite{
		IntentID:        cloudAssetResolutionCostIntentID,
		ScopeID:         "aws:123456789012:us-east-1",
		GenerationID:    "generation-cloud-asset-resolution-cost",
		SourceSystem:    "aws",
		Cause:           "reducer/cloud_asset_resolution",
		EntityKeys:      []string{"entity-a", "entity-b"},
		RelatedScopeIDs: []string{"aws:123456789012:us-west-2"},
	})
	if err != nil {
		t.Fatalf("WriteCloudAssetResolution() error = %v", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
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

// TestCostBudget_CloudAssetResolution_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production single-fact-per-call
// dispatch as the positive test. PostgresCloudAssetResolutionWriter has no
// batching to break (it never receives a []Decision slice), so this domain's
// N+1 anti-pattern is splitting ONE cross-scope resolution into ONE
// WriteCloudAssetResolution call PER entity key instead of a single call
// carrying both — the same regression class ci_cd_run_correlation/
// container_image_identity/sbom_attestation_attachment expose via
// per-decision splitting, just one layer up (per-entity-key instead of
// per-decision). This asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget.
func TestCostBudget_CloudAssetResolution_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, cloudAssetResolutionBudgetRelPath)
	entityKeys := []string{"entity-a", "entity-b"}

	writer, _, reader := newInstrumentedCloudAssetResolutionWriter(t)

	for _, key := range entityKeys {
		if _, err := writer.WriteCloudAssetResolution(context.Background(), reducer.CloudAssetResolutionWrite{
			IntentID:        cloudAssetResolutionCostIntentID,
			ScopeID:         "aws:123456789012:us-east-1",
			GenerationID:    "generation-cloud-asset-resolution-cost",
			SourceSystem:    "aws",
			Cause:           "reducer/cloud_asset_resolution",
			EntityKeys:      []string{key},
			RelatedScopeIDs: []string{"aws:123456789012:us-west-2"},
		}); err != nil {
			t.Fatalf("N+1 WriteCloudAssetResolution() error = %v", err)
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
			"(N=%d per-entity-key calls, scenario=%s)",
		writes, maxWrites, len(entityKeys), budget.Scenario,
	)
}

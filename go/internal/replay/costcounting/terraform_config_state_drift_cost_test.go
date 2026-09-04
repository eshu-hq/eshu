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

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/reducer/tfconfigstate"
)

// terraformConfigStateDriftBudgetRelPath is the committed cost budget for the
// config_state_drift scenario (fact-kind-registry family terraform_state,
// reducer_domain config_state_drift, kind
// reducer_terraform_config_state_drift_finding, issue #6416). The production
// writer,
// tfconfigstate.PostgresTerraformConfigStateDriftWriter.WriteTerraformConfigStateDriftFindings
// (go/internal/reducer/tfconfigstate/terraform_config_state_drift_writer.go),
// operates over []model.Candidate Go values, not a CanonicalMaterialization,
// so the fixture candidates live inline in this file, matching the
// multi_cloud_runtime_drift_cost_test.go convention for writers with no
// committed cassette.
var terraformConfigStateDriftBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "terraform-config-state-drift.cost-budget.json",
)

const terraformConfigStateDriftCostIntentID = "intent-terraform-config-state-drift-cost"

// terraformConfigStateDriftFixtureCandidates is the deterministic input for
// this scenario: two admitted per-address candidates with distinct canonical
// address identity (one added_in_state, one added_in_config), shaped like the
// exactDriftCandidate fixture
// TestPostgresTerraformConfigStateDriftWriterPersistsOneFactPerFinding drives
// (go/internal/reducer/tfconfigstate/terraform_config_state_drift_writer_test.go).
// Built directly at the Candidate level so this scenario stays independent of
// the BuildCandidates classification path, mirroring
// aws_cloud_runtime_drift_cost_test.go's manual-Evidence-atom convention.
func terraformConfigStateDriftFixtureCandidates() []model.Candidate {
	candidate := func(address, driftKind string) model.Candidate {
		candidateID := "drift:hash-1:" + address + ":" + driftKind
		return model.Candidate{
			ID:             candidateID,
			Kind:           rules.TerraformConfigStateDriftPackName,
			CorrelationKey: address,
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{
					ID:           candidateID + "/drift_kind",
					SourceSystem: "reducer/terraform_config_state_drift",
					EvidenceType: "terraform_drift_kind",
					ScopeID:      "state_snapshot:s3:hash-1",
					Key:          "drift_kind",
					Value:        driftKind,
					Confidence:   1,
				},
			},
		}
	}
	return []model.Candidate{
		candidate("aws_s3_bucket.drift_cost_added_state", "added_in_state"),
		candidate("aws_iam_role.drift_cost_added_config", "added_in_config"),
	}
}

// newInstrumentedTerraformConfigStateDriftWriter builds the PRODUCTION
// Postgres write dispatch for this domain:
// tfconfigstate.PostgresTerraformConfigStateDriftWriter over the shared
// newInstrumentedReducerDB seam (postgres_cost_helpers_test.go), the same
// postgres.InstrumentedDB shape go/cmd/reducer/observed_service_wiring.go
// wires for every reducer Postgres writer (StoreName "reducer").
// WriteTerraformConfigStateDriftFindings calls the shared
// factwrite.BatchInsertVersionedFacts bounded chunked bulk insert (batch size
// 1000) instead of one ExecContext per candidate, then runs the
// generation-authoritative retire, so two candidates cost exactly 2
// statements: one insert chunk plus one retire.
func newInstrumentedTerraformConfigStateDriftWriter(t *testing.T) (
	writer tfconfigstate.PostgresTerraformConfigStateDriftWriter,
	fake *countingExecQueryer,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	fake = &countingExecQueryer{}
	db, manualReader := newInstrumentedReducerDB(t, fake)
	writer = tfconfigstate.PostgresTerraformConfigStateDriftWriter{
		DB:  db,
		Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
	}
	return writer, fake, manualReader
}

// terraformConfigStateDriftCostWrite builds the single-batch write this
// scenario drives: both fixture candidates in one scope and generation.
func terraformConfigStateDriftCostWrite(candidates []model.Candidate) tfconfigstate.TerraformConfigStateDriftWrite {
	return tfconfigstate.TerraformConfigStateDriftWrite{
		IntentID:     terraformConfigStateDriftCostIntentID,
		ScopeID:      "state_snapshot:s3:hash-1",
		GenerationID: "generation-terraform-config-state-drift-cost",
		SourceSystem: "collector/terraform-state",
		Cause:        "drift intent",
		BackendKind:  "s3",
		LocatorHash:  "hash-1",
		Candidates:   candidates,
	}
}

// TestCostBudget_TerraformConfigStateDrift is the positive cost-counting gate
// for the config_state_drift reducer projection (issue #6416: the domain's
// Postgres write path is implemented and wired, so the cost axis must be
// bounded by a scenario, not an exemption). It drives the production
// PostgresTerraformConfigStateDriftWriter.WriteTerraformConfigStateDriftFindings
// over two admitted candidates with distinct canonical address identity in
// one scope, through a real InstrumentedDB-backed sdkmetric.ManualReader,
// then asserts eshu_dp_postgres_query_duration_seconds's write-attributed
// observation count is within the committed budget.
//
// Instrument read: eshu_dp_postgres_query_duration_seconds{operation="write"}.
// postgres.InstrumentedDB.ExecContext (go/internal/storage/postgres/
// instrumented.go) records this once per ExecContext round-trip.
// WriteTerraformConfigStateDriftFindings calls the shared
// factwrite.BatchInsertVersionedFacts bounded chunked bulk insert instead of
// one ExecContext per candidate, so two candidates fit one chunk, and the
// generation-authoritative retire adds exactly one more statement: this
// scenario asserts exactly 2 write observations. The companion N+1 negative
// control below
// (TestCostBudget_TerraformConfigStateDrift_N1_ExceedsBudget) proves the
// budget still catches a per-candidate regression.
func TestCostBudget_TerraformConfigStateDrift(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, terraformConfigStateDriftBudgetRelPath)
	writer, fake, reader := newInstrumentedTerraformConfigStateDriftWriter(t)

	result, err := writer.WriteTerraformConfigStateDriftFindings(
		context.Background(),
		terraformConfigStateDriftCostWrite(terraformConfigStateDriftFixtureCandidates()),
	)
	if err != nil {
		t.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v", err)
	}
	if result.CanonicalWrites != 2 {
		t.Fatalf("CanonicalWrites = %d, want 2 (both fixture candidates are distinct admitted findings)", result.CanonicalWrites)
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
	if writes == 0 {
		t.Fatal("eshu_dp_postgres_query_duration_seconds write observations = 0: instrument not recording (false green guard)")
	}
	if writes != uint64(maxWrites) {
		t.Fatalf(
			"eshu_dp_postgres_query_duration_seconds write observations = %d, want exactly budget %d "+
				"(scenario=%s): every count change requires a budget refresh",
			writes, maxWrites, budget.Scenario,
		)
	}

	// SECONDARY assertion: raw ExecContext call count from the counting fake.
	execs := fake.totalExecs()
	if maxExecs, ok := budget.Budgets["statements_executed"]; ok {
		if execs == 0 {
			t.Fatal("statements_executed = 0: fake not recording (false green guard)")
		}
		if execs != maxExecs {
			t.Fatalf(
				"statements_executed = %d, want exactly budget %d (scenario=%s): every count change requires a budget refresh",
				execs, maxExecs, budget.Scenario,
			)
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_postgres_query_duration_seconds_writes=%d (budget=%d) statements_executed=%d (budget=%d)",
		budget.Scenario, writes, maxWrites, execs, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_TerraformConfigStateDrift_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production batched dispatch as the
// positive test. It calls WriteTerraformConfigStateDriftFindings once per
// fixture candidate instead of once for the whole batch — the classic N+1
// anti-pattern for a batched writer — and asserts the accumulated
// eshu_dp_postgres_query_duration_seconds write observation count EXCEEDS the
// committed budget. Each per-candidate Write costs one insert plus one
// retire, so N=2 candidates cost 4 observations against a budget of 2.
func TestCostBudget_TerraformConfigStateDrift_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, terraformConfigStateDriftBudgetRelPath)
	candidates := terraformConfigStateDriftFixtureCandidates()
	if len(candidates) < 2 {
		t.Fatalf("N+1 control needs >=2 candidates to exceed the budget; fixture has %d", len(candidates))
	}

	writer, _, reader := newInstrumentedTerraformConfigStateDriftWriter(t)

	for _, candidate := range candidates {
		if _, err := writer.WriteTerraformConfigStateDriftFindings(
			context.Background(),
			terraformConfigStateDriftCostWrite([]model.Candidate{candidate}),
		); err != nil {
			t.Fatalf("N+1 WriteTerraformConfigStateDriftFindings() error = %v", err)
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
			"(N=%d candidates, scenario=%s)",
		writes, maxWrites, len(candidates), budget.Scenario,
	)
}

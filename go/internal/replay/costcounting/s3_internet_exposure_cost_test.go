// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// s3InternetExposureBudgetRelPath is the committed cost budget for the
// s3_internet_exposure_materialization scenario (fact-kind-registry family
// s3_bucket_posture, specs/fact-kind-registry.v1.yaml:284-294). Like the
// semantic-entity and documentation-edges scenarios, this projection has no
// committed cassette: cypher.S3InternetExposureNodeWriter operates over flat
// map[string]any rows, not a CanonicalMaterialization, so the fixture rows
// live inline in this file and the budget records that explicitly instead of
// pointing at a cassette path.
var s3InternetExposureBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "s3-internet-exposure-materialization.cost-budget.json",
)

const s3InternetExposureCostEvidenceSource = "reducer/s3-internet-exposure"

// s3InternetExposureFixtureRows is the deterministic input for both the
// positive and N+1 scenarios: two S3 internet-exposure rows in one scope,
// shaped like s3_internet_exposure_node_writer_test.go's
// s3InternetExposureRows fixture. S3InternetExposureNodeWriter has exactly one
// Cypher template with no per-row vocabulary split, so any two distinct rows
// are sufficient to prove the N+1 control.
func s3InternetExposureFixtureRows() []map[string]any {
	row := func(id string) map[string]any {
		return map[string]any{
			"uid":              "cloud-resource-" + id,
			"state":            "public",
			"internet_exposed": true,
			"reason":           "policy_public_grant",
			"source_fact_id":   "fact-posture-" + id,
		}
	}
	return []map[string]any{row("a"), row("b")}
}

// newInstrumentedS3InternetExposureNodeWriter builds the production
// cypher.S3InternetExposureNodeWriter (the writer
// graphowner.NewS3InternetExposureLockedWriter wraps at
// go/cmd/reducer/canonical_graph_writers.go:56/88-90 — the #5062 lock-only
// gate serializes the SAME inner WriteS3InternetExposureNodes call under a
// Postgres advisory lock and does not change the Cypher statement shape, so
// driving the raw writer directly reproduces the identical statement/
// instrument shape production emits), wired over a groupCountingExecutor
// wrapped by the production cypher.InstrumentedExecutor — the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec, which canonical_graph_writers.go then uses to construct
// S3InternetExposureNodeWriter. InstrumentedExecutor records
// eshu_dp_neo4j_batches_executed_total on every UNWIND-shaped statement (one
// per statement carrying a "rows" parameter) — the PRIMARY instrument this
// scenario asserts, not a hand-counted statement slice.
func newInstrumentedS3InternetExposureNodeWriter(t *testing.T) (
	writer *cypher.S3InternetExposureNodeWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewS3InternetExposureNodeWriter(instrumented, echoAllExistenceReader{}, 500)
	return writer, exec, manualReader
}

// TestCostBudget_S3InternetExposureMaterialization is the positive
// cost-counting gate for the s3_internet_exposure_materialization reducer
// projection (the s3_bucket_posture family in
// specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production cypher.S3InternetExposureNodeWriter.WriteS3InternetExposureNodes
// over two deterministic rows through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total, incremented once per
// UNWIND-shaped statement passed through Execute or ExecuteGroup
// (recordStatementBatchMetrics). The writer's single Cypher template and the
// default batch size (500) comfortably covering two rows means one
// WriteS3InternetExposureNodes call emits exactly one UNWIND batch. Any
// increase — an N+1 write cycle or an extra batch split — trips the gate.
func TestCostBudget_S3InternetExposureMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, s3InternetExposureBudgetRelPath)
	writer, exec, reader := newInstrumentedS3InternetExposureNodeWriter(t)

	if err := writer.WriteS3InternetExposureNodes(
		context.Background(), s3InternetExposureFixtureRows(), "scope-1", "gen-1", s3InternetExposureCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteS3InternetExposureNodes() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_neo4j_batches_executed_total off the
	// real otel reader. This counter is incremented by the production
	// InstrumentedExecutor, NOT by a hand-counted statement slice.
	batches := collectCounter(rm, "eshu_dp_neo4j_batches_executed_total")
	maxBatches, ok := budget.Budgets["eshu_dp_neo4j_batches_executed_total"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_neo4j_batches_executed_total")
	}
	if batches > maxBatches {
		t.Fatalf(
			"eshu_dp_neo4j_batches_executed_total = %d exceeds budget %d "+
				"(scenario=%s): algorithmic regression detected",
			batches, maxBatches, budget.Scenario,
		)
	}
	if batches == 0 {
		t.Fatal("eshu_dp_neo4j_batches_executed_total = 0: instrument not recording (false green guard)")
	}

	// SECONDARY assertion: raw statement count from the counting executor.
	stmts := exec.totalStatements()
	if maxStmts, ok := budget.Budgets["statements_executed"]; ok {
		if stmts > maxStmts {
			t.Fatalf(
				"statements_executed = %d exceeds budget %d (scenario=%s): too many Cypher write operations",
				stmts, maxStmts, budget.Scenario,
			)
		}
		if stmts == 0 {
			t.Fatal("statements_executed = 0: executor not recording (false green guard)")
		}
	}

	t.Logf(
		"scenario=%s eshu_dp_neo4j_batches_executed_total=%d (budget=%d) statements_executed=%d (budget=%d)",
		budget.Scenario, batches, budget.Budgets["eshu_dp_neo4j_batches_executed_total"],
		stmts, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_S3InternetExposureMaterialization_N1_ExceedsBudget is the
// mandatory negative control. It calls WriteS3InternetExposureNodes once per
// fixture row instead of once for the whole batch — the classic N+1
// anti-pattern — and asserts the accumulated
// eshu_dp_neo4j_batches_executed_total EXCEEDS the committed budget. This is a
// REAL negative control: it drives the SAME production writer, through the
// SAME InstrumentedExecutor wrapper, reading the SAME instrument off the SAME
// real otel reader as the positive test.
func TestCostBudget_S3InternetExposureMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, s3InternetExposureBudgetRelPath)
	rows := s3InternetExposureFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedS3InternetExposureNodeWriter(t)

	for _, row := range rows {
		if err := writer.WriteS3InternetExposureNodes(
			context.Background(), []map[string]any{row}, "scope-1", "gen-1", s3InternetExposureCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteS3InternetExposureNodes() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	batches := collectCounter(rm, "eshu_dp_neo4j_batches_executed_total")
	maxBatches, ok := budget.Budgets["eshu_dp_neo4j_batches_executed_total"]
	if !ok {
		t.Fatal("budget has no eshu_dp_neo4j_batches_executed_total entry")
	}

	if batches <= maxBatches {
		t.Fatalf(
			"N+1 negative control: eshu_dp_neo4j_batches_executed_total = %d did NOT exceed budget %d — "+
				"budget is too loose to catch N+1 regressions or the negative control is generating too "+
				"few writes; tighten the budget or increase the N+1 fanout",
			batches, maxBatches,
		)
	}

	t.Logf(
		"N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = %d > budget %d (N=%d rows, scenario=%s)",
		batches, maxBatches, len(rows), budget.Scenario,
	)
}

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

// s3ExternalPrincipalGrantBudgetRelPath is the committed cost budget for the
// s3_external_principal_grant_materialization scenario (fact-kind-registry
// family s3_external_principal_grant,
// specs/fact-kind-registry.v1.yaml:295-305). Like the semantic-entity and
// documentation-edges scenarios, this projection has no committed cassette:
// cypher.S3ExternalPrincipalGrantWriter operates over flat map[string]any
// rows, not a CanonicalMaterialization, so the fixture rows live inline in
// this file and the budget records that explicitly instead of pointing at a
// cassette path.
var s3ExternalPrincipalGrantBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "s3-external-principal-grant-materialization.cost-budget.json",
)

const s3ExternalPrincipalGrantCostEvidenceSource = "reducer/s3-external-principal-grant"

// s3ExternalPrincipalGrantFixtureRows is the deterministic input for both the
// positive and N+1 scenarios: two GRANTS_ACCESS_TO rows in one scope, shaped
// like s3_external_principal_grant_writer_test.go's
// s3ExternalPrincipalGrantRows fixture. S3ExternalPrincipalGrantWriter has
// exactly one Cypher template (relationship_type is validated against a
// single-entry vocabulary, "GRANTS_ACCESS_TO"), so any two distinct rows are
// sufficient to prove the N+1 control.
func s3ExternalPrincipalGrantFixtureRows() []map[string]any {
	row := func(id string) map[string]any {
		return map[string]any{
			"source_uid":           "bucket-" + id,
			"principal_uid":        "principal-" + id,
			"principal_kind":       "aws_account",
			"principal_value":      "999988887770",
			"principal_account_id": "999988887770",
			"principal_partition":  "aws",
			"principal_service":    "",
			"relationship_type":    "GRANTS_ACCESS_TO",
			"grant_outcome":        "cross_account",
			"is_public":            false,
			"is_cross_account":     true,
			"is_service_principal": false,
			"resolution_mode":      "bucket_name",
		}
	}
	return []map[string]any{row("a"), row("b")}
}

// newInstrumentedS3ExternalPrincipalGrantWriter builds the production
// cypher.S3ExternalPrincipalGrantWriter (constructed unwrapped at
// go/cmd/reducer/canonical_graph_writers.go:76 — no owner-ledger/lock-only
// gate wraps this writer, unlike EC2/RDS/S3-internet-exposure), wired over a
// groupCountingExecutor wrapped by the production
// cypher.InstrumentedExecutor — the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec, which canonical_graph_writers.go then uses to construct
// S3ExternalPrincipalGrantWriter. InstrumentedExecutor records
// eshu_dp_neo4j_batches_executed_total on every UNWIND-shaped statement (one
// per statement carrying a "rows" parameter) — the PRIMARY instrument this
// scenario asserts, not a hand-counted statement slice.
func newInstrumentedS3ExternalPrincipalGrantWriter(t *testing.T) (
	writer *cypher.S3ExternalPrincipalGrantWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewS3ExternalPrincipalGrantWriter(instrumented, 500)
	return writer, exec, manualReader
}

// TestCostBudget_S3ExternalPrincipalGrantMaterialization is the positive
// cost-counting gate for the s3_external_principal_grant_materialization
// reducer projection (the s3_external_principal_grant family in
// specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production
// cypher.S3ExternalPrincipalGrantWriter.WriteS3ExternalPrincipalGrants over
// two deterministic rows through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total, incremented once per
// UNWIND-shaped statement passed through Execute or ExecuteGroup
// (recordStatementBatchMetrics). The writer's single Cypher template and the
// default batch size (500) comfortably covering two rows means one
// WriteS3ExternalPrincipalGrants call emits exactly one UNWIND batch. Any
// increase — an N+1 write cycle or an extra batch split — trips the gate.
func TestCostBudget_S3ExternalPrincipalGrantMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, s3ExternalPrincipalGrantBudgetRelPath)
	writer, exec, reader := newInstrumentedS3ExternalPrincipalGrantWriter(t)

	if err := writer.WriteS3ExternalPrincipalGrants(
		context.Background(), s3ExternalPrincipalGrantFixtureRows(), "scope-1", "gen-1", s3ExternalPrincipalGrantCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteS3ExternalPrincipalGrants() error = %v", err)
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

// TestCostBudget_S3ExternalPrincipalGrantMaterialization_N1_ExceedsBudget is
// the mandatory negative control. It calls
// WriteS3ExternalPrincipalGrants once per fixture row instead of once for the
// whole batch — the classic N+1 anti-pattern — and asserts the accumulated
// eshu_dp_neo4j_batches_executed_total EXCEEDS the committed budget. This is a
// REAL negative control: it drives the SAME production writer, through the
// SAME InstrumentedExecutor wrapper, reading the SAME instrument off the SAME
// real otel reader as the positive test.
func TestCostBudget_S3ExternalPrincipalGrantMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, s3ExternalPrincipalGrantBudgetRelPath)
	rows := s3ExternalPrincipalGrantFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedS3ExternalPrincipalGrantWriter(t)

	for _, row := range rows {
		if err := writer.WriteS3ExternalPrincipalGrants(
			context.Background(), []map[string]any{row}, "scope-1", "gen-1", s3ExternalPrincipalGrantCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteS3ExternalPrincipalGrants() error = %v", err)
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

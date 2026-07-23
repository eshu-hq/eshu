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

// rdsPostureBudgetRelPath is the committed cost budget for the
// rds_posture_materialization scenario (fact-kind-registry family
// rds_posture, specs/fact-kind-registry.v1.yaml:273-283). Like the
// semantic-entity and documentation-edges scenarios, this projection has no
// committed cassette: cypher.RDSPostureNodeWriter operates over flat
// map[string]any rows, not a CanonicalMaterialization, so the fixture rows
// live inline in this file and the budget records that explicitly instead of
// pointing at a cassette path.
var rdsPostureBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "rds-posture-materialization.cost-budget.json",
)

const rdsPostureCostEvidenceSource = "reducer/rds-posture"

// rdsPostureFixtureRows is the deterministic input for both the positive and
// N+1 scenarios: two RDS posture rows in one scope, shaped like the
// production WriteRDSPostureNodes row contract
// (go/internal/storage/cypher/rds_posture_node_writer.go
// canonicalRDSPostureUpdateCypher's SET clause, same fields as
// rds_posture_node_writer_test.go's rdsPostureNodeRows). RDSPostureNodeWriter
// has exactly one Cypher template with no per-row vocabulary split, so any two
// distinct rows are sufficient to prove the N+1 control.
func rdsPostureFixtureRows() []map[string]any {
	row := func(id string) map[string]any {
		return map[string]any{
			"uid":                       "rds-" + id,
			"rds_identifier":            "orders-" + id,
			"rds_resource_type":         "aws_rds_db_instance",
			"rds_engine":                "postgres",
			"rds_publicly_accessible":   false,
			"rds_public_exposure_state": "not_public_endpoint",
			"rds_storage_encrypted":     true,
			"rds_kms_key_id":            "arn:aws:kms:us-east-1:111111111111:key/db",
			"rds_iam_database_authentication_enabled": true,
			"rds_multi_az":                            true,
			"rds_deletion_protection":                 true,
			"rds_backup_retention_period":             int64(7),
			"rds_performance_insights_enabled":        true,
			"rds_performance_insights_retention_days": int64(31),
			"rds_performance_insights_kms_key_id":     "arn:aws:kms:us-east-1:111111111111:key/pi",
			"rds_ca_certificate_identifier":           "rds-ca-rsa2048-g1",
			"rds_parameter_groups":                    []string{"orders-params-" + id},
			"rds_option_groups":                       []string{"orders-options-" + id},
			"rds_security_parameters":                 []string{"rds.force_ssl=1"},
			"source_fact_id":                          "fact-posture-" + id,
		}
	}
	return []map[string]any{row("a"), row("b")}
}

// newInstrumentedRDSPostureNodeWriter builds the production
// cypher.RDSPostureNodeWriter (the writer graphowner.NewRDSPostureLockedWriter
// wraps at go/cmd/reducer/canonical_graph_writers.go:53/77-79 — the #5062
// lock-only gate serializes the SAME inner WriteRDSPostureNodes call under a
// Postgres advisory lock and does not change the Cypher statement shape, so
// driving the raw writer directly reproduces the identical statement/
// instrument shape production emits), wired over a groupCountingExecutor
// wrapped by the production cypher.InstrumentedExecutor — the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec, which canonical_graph_writers.go then uses to construct
// RDSPostureNodeWriter. InstrumentedExecutor records
// eshu_dp_neo4j_batches_executed_total on every UNWIND-shaped statement (one
// per statement carrying a "rows" parameter) — the PRIMARY instrument this
// scenario asserts, not a hand-counted statement slice.
func newInstrumentedRDSPostureNodeWriter(t *testing.T) (
	writer *cypher.RDSPostureNodeWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewRDSPostureNodeWriter(instrumented, echoAllExistenceReader{}, 500)
	return writer, exec, manualReader
}

// TestCostBudget_RDSPostureMaterialization is the positive cost-counting gate
// for the rds_posture_materialization reducer projection (the rds_posture
// family in specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production cypher.RDSPostureNodeWriter.WriteRDSPostureNodes over two
// deterministic rows through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total, incremented once per
// UNWIND-shaped statement passed through Execute or ExecuteGroup
// (recordStatementBatchMetrics). RDSPostureNodeWriter has exactly one Cypher
// template with no per-row vocabulary split, and the default batch size (500)
// comfortably covers two rows, so one WriteRDSPostureNodes call emits exactly
// one UNWIND batch. Any increase — an N+1 write cycle or an extra batch split
// — trips the gate.
func TestCostBudget_RDSPostureMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, rdsPostureBudgetRelPath)
	writer, exec, reader := newInstrumentedRDSPostureNodeWriter(t)

	if err := writer.WriteRDSPostureNodes(
		context.Background(), rdsPostureFixtureRows(), "scope-1", "gen-1", rdsPostureCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteRDSPostureNodes() error = %v", err)
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

// TestCostBudget_RDSPostureMaterialization_N1_ExceedsBudget is the mandatory
// negative control. It calls WriteRDSPostureNodes once per fixture row
// instead of once for the whole batch — the classic N+1 anti-pattern — and
// asserts the accumulated eshu_dp_neo4j_batches_executed_total EXCEEDS the
// committed budget. This is a REAL negative control: it drives the SAME
// production writer, through the SAME InstrumentedExecutor wrapper, reading
// the SAME instrument off the SAME real otel reader as the positive test.
func TestCostBudget_RDSPostureMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, rdsPostureBudgetRelPath)
	rows := rdsPostureFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedRDSPostureNodeWriter(t)

	for _, row := range rows {
		if err := writer.WriteRDSPostureNodes(
			context.Background(), []map[string]any{row}, "scope-1", "gen-1", rdsPostureCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteRDSPostureNodes() error = %v", err)
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

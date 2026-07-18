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

// observabilityCoverageEdgeBudgetRelPath is the committed cost budget for the
// observability_coverage_correlation scenario (fact-kind-registry family
// observability, specs/fact-kind-registry.v1.yaml:216-234, reducer_domain
// observability_coverage_correlation). Like ec2_instance_node_materialization,
// this projection writes through cypher.ObservabilityCoverageEdgeWriter over
// flat map[string]any rows, not a CanonicalMaterialization, so the fixture
// rows live inline in this file and the budget records that explicitly
// instead of pointing at a cassette path.
var observabilityCoverageEdgeBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "observability-coverage-edge-materialization.cost-budget.json",
)

const (
	observabilityCoverageEdgeCostEvidenceSource = "reducer/observability-coverage"
	observabilityCoverageEdgeCostScopeID        = "scope-observability-a"
	observabilityCoverageEdgeCostGenerationID   = "generation-1"
)

// observabilityCoverageEdgeFixtureRows is the deterministic input for the
// positive and N+1 scenarios: two COVERS edge rows sharing the SAME
// coverage_signal ("alarm") but distinct (observability_uid, target_uid)
// pairs, shaped EXACTLY like the current production row contract
// (go/internal/reducer/observability_coverage_edge_rows.go
// ExtractObservabilityCoverageEdgeRows, lines 92-97: observability_uid,
// target_uid, coverage_signal, resolution_mode). scope_id, generation_id, and
// evidence_source are NOT fixture fields — WriteObservabilityCoverageEdges
// stamps those itself (cloud clones the row and injects them), mirroring the
// writer's real call signature.
//
// ObservabilityCoverageEdgeWriter is NOT the gated CloudResource/EC2/
// KubernetesWorkload node family (no #5007 owner-ledger gate wraps it — see
// canonical_graph_writers.go:70, constructed unwrapped), but it DOES group
// rows by coverage_signal into a separate Cypher statement per signal
// (WriteObservabilityCoverageEdges, cypher relationship type is the
// signal-derived AWS_COVERS_<signal> token). Sharing coverage_signal across
// both fixture rows is therefore required for the N+1 control to be
// meaningful: two rows with DIFFERENT signals would already emit two
// statements regardless of call count, making the negative control a no-op.
func observabilityCoverageEdgeFixtureRows() []map[string]any {
	row := func(id string) map[string]any {
		return map[string]any{
			"observability_uid": "obs-alarm-uid-" + id,
			"target_uid":        "target-cloud-resource-uid-" + id,
			"coverage_signal":   "alarm",
			"resolution_mode":   "exact",
		}
	}
	return []map[string]any{row("a"), row("b")}
}

// newInstrumentedObservabilityCoverageEdgeWriter builds the PRODUCTION
// observability-coverage write dispatch: the raw
// cypher.ObservabilityCoverageEdgeWriter (constructed unwrapped at
// go/cmd/reducer/canonical_graph_writers.go:70 — no graphowner gate wraps
// this edge writer) over a groupCountingExecutor wrapped by the production
// cypher.InstrumentedExecutor, the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec. InstrumentedExecutor records eshu_dp_neo4j_batches_executed_total
// on every UNWIND-shaped statement — the PRIMARY instrument this scenario
// asserts, not a hand-counted statement slice.
func newInstrumentedObservabilityCoverageEdgeWriter(t *testing.T) (
	writer *cypher.ObservabilityCoverageEdgeWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewObservabilityCoverageEdgeWriter(instrumented, 500)
	return writer, exec, manualReader
}

// TestCostBudget_ObservabilityCoverageEdgeMaterialization is the positive
// cost-counting gate for the observability_coverage_correlation reducer
// projection (the observability family in
// specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production DIRECT dispatch —
// cypher.ObservabilityCoverageEdgeWriter.WriteObservabilityCoverageEdges,
// unwrapped by any owner-ledger gate — over two deterministic same-signal
// rows in one scope, through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total. InstrumentedExecutor
// records this once per UNWIND-shaped statement (a statement whose Parameters
// carry a "rows" key) passed through Execute or ExecuteGroup. Both fixture
// rows share coverage_signal "alarm", so they group into ONE Cypher statement
// (one relationship-type-specific UNWIND), which fits in one raw-writer batch
// (default 500) — the writer emits exactly one batch. Any increase — an N+1
// write cycle, an extra signal-group split, or an extra batch split — trips
// the gate.
func TestCostBudget_ObservabilityCoverageEdgeMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, observabilityCoverageEdgeBudgetRelPath)
	writer, exec, reader := newInstrumentedObservabilityCoverageEdgeWriter(t)

	if err := writer.WriteObservabilityCoverageEdges(
		context.Background(),
		observabilityCoverageEdgeFixtureRows(),
		observabilityCoverageEdgeCostScopeID,
		observabilityCoverageEdgeCostGenerationID,
		observabilityCoverageEdgeCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteObservabilityCoverageEdges() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

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

// TestCostBudget_ObservabilityCoverageEdgeMaterialization_N1_ExceedsBudget is
// the mandatory negative control, run through the SAME production writer as
// the positive test. It calls WriteObservabilityCoverageEdges once per
// fixture row (both still sharing coverage_signal "alarm", so the batching
// key is unchanged) instead of once for the whole batch — the classic N+1
// anti-pattern — and asserts the accumulated
// eshu_dp_neo4j_batches_executed_total EXCEEDS the committed budget.
func TestCostBudget_ObservabilityCoverageEdgeMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, observabilityCoverageEdgeBudgetRelPath)
	rows := observabilityCoverageEdgeFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedObservabilityCoverageEdgeWriter(t)

	for _, row := range rows {
		if err := writer.WriteObservabilityCoverageEdges(
			context.Background(),
			[]map[string]any{row},
			observabilityCoverageEdgeCostScopeID,
			observabilityCoverageEdgeCostGenerationID,
			observabilityCoverageEdgeCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteObservabilityCoverageEdges() error = %v", err)
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

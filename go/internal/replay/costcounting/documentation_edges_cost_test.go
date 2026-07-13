// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// documentationMaterializationBudgetRelPath is the committed cost budget for
// the documentation_materialization scenario. Like semantic-entity, this
// projection has no committed cassette: cypher.EdgeWriter.WriteEdges operates
// over flat reducer.SharedProjectionIntentRow values, not a
// CanonicalMaterialization, so the fixture rows live inline in this file and
// the budget records that explicitly instead of pointing at a cassette path.
var documentationMaterializationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "documentation-materialization.cost-budget.json",
)

const documentationCostEvidenceSource = "reducer/documentation"

// documentationEdgeFixtureRows is the deterministic input for both the
// positive and N+1 scenarios: two DOCUMENTS edge rows targeting the two
// resolvable kinds documentation_edge_materialization.go projects — an exact
// code-entity mention and a workload mention — for the reducer_domain
// documentation_materialization (specs/fact-kind-registry.v1.yaml
// "documentation" family, DomainDocumentationMaterialization intent /
// DomainDocumentationEdges write route). Shaped exactly like
// buildDocumentationIntentRows (go/internal/reducer/documentation_edge_materialization.go)
// builds them from extracted mention rows.
func documentationEdgeFixtureRows() []reducer.SharedProjectionIntentRow {
	entityPayload := map[string]any{
		"section_uid":      "docsection:doc-1|sec-1",
		"scope_id":         "scope-1",
		"document_id":      "doc-1",
		"section_id":       "sec-1",
		"target_entity_id": "uid:func:handler",
		"target_kind":      "entity",
		"mention_kind":     "code_symbol",
	}
	workloadPayload := map[string]any{
		"section_uid":      "docsection:doc-1|sec-2",
		"scope_id":         "scope-1",
		"document_id":      "doc-1",
		"section_id":       "sec-2",
		"target_entity_id": "workload-1",
		"target_kind":      "workload",
		"mention_kind":     "workload_name",
	}
	return []reducer.SharedProjectionIntentRow{
		{
			ProjectionDomain: reducer.DomainDocumentationEdges,
			PartitionKey:     "docsection:doc-1|sec-1->uid:func:handler",
			ScopeID:          "scope-1",
			Payload:          entityPayload,
		},
		{
			ProjectionDomain: reducer.DomainDocumentationEdges,
			PartitionKey:     "docsection:doc-1|sec-2->workload-1",
			ScopeID:          "scope-1",
			Payload:          workloadPayload,
		},
	}
}

// newInstrumentedDocumentationEdgeWriter builds the production cypher.EdgeWriter
// used by DocumentationEdgeMaterializationHandler
// (go/internal/reducer/documentation_edge_materialization.go), wired over a
// groupCountingExecutor that implements GroupExecutor so WriteEdges takes its
// atomic-transaction path. EdgeWriter.Instruments is the same field
// go/cmd/reducer/endpoint_presence_wiring.go newHandlerEdgeWriter sets from the
// real telemetry.Instruments registry, and EdgeWriter.recordGroupedWrite
// increments eshu_dp_shared_edge_write_groups_total on every grouped WriteEdges
// call — the PRIMARY instrument this scenario asserts, not a hand-counted
// statement slice.
func newInstrumentedDocumentationEdgeWriter(t *testing.T) (
	writer *cypher.EdgeWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	writer = cypher.NewEdgeWriter(exec, 500)
	writer.Instruments = inst
	return writer, exec, manualReader
}

// TestCostBudget_DocumentationMaterialization is the positive cost-counting
// gate for the documentation_materialization reducer projection (the
// "documentation" family in specs/fact-kind-registry.v1.yaml, C-14 issue
// #4367). It drives the production cypher.EdgeWriter.WriteEdges over two
// deterministic DOCUMENTS edge rows through a real telemetry.Instruments
// registry backed by an sdkmetric.ManualReader, then asserts
// eshu_dp_shared_edge_write_groups_total is within the committed budget.
//
// This scenario calls WriteEdges only, not RetractEdges: the whole-scope
// RetractEdges path for documentation issues a single non-grouped
// Executor.Execute call (edge_writer_retract.go), which never reaches
// EdgeWriter.recordGroupedWrite, so including it would not move the asserted
// instrument while adding an unreviewable extra statement to the fixture —
// the same simplification the nested-directory-tree scenario makes by relying
// on FirstGeneration=true to keep its retract phases empty.
//
// Instrument read: eshu_dp_shared_edge_write_groups_total. EdgeWriter.WriteEdges
// batches every row for a domain into one atomic transaction and calls
// ge.ExecuteGroup(ctx, stmts) exactly once per WriteEdges call when the
// executor implements GroupExecutor and no per-domain group-batch override
// applies (recordGroupedWrite in edge_writer.go); documentation has no
// override (groupBatchSizeForDomain only special-cases CodeCalls,
// InheritanceEdges, SQLRelationships, ShellExec). So the counter is exactly 1
// per WriteEdges call regardless of row count — any increase means the
// reducer handler is issuing more than one write transaction per intent, the
// N+1 pattern this gate exists to catch.
func TestCostBudget_DocumentationMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, documentationMaterializationBudgetRelPath)
	writer, exec, reader := newInstrumentedDocumentationEdgeWriter(t)

	if err := writer.WriteEdges(
		context.Background(),
		reducer.DomainDocumentationEdges,
		documentationEdgeFixtureRows(),
		documentationCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// PRIMARY assertion: read eshu_dp_shared_edge_write_groups_total off the
	// real otel reader. This counter is incremented by the production
	// EdgeWriter.recordGroupedWrite, NOT by a hand-counted statement slice.
	groups := collectCounter(rm, "eshu_dp_shared_edge_write_groups_total")
	maxGroups, ok := budget.Budgets["eshu_dp_shared_edge_write_groups_total"]
	if !ok {
		t.Fatal("budget missing required key eshu_dp_shared_edge_write_groups_total")
	}
	if groups > maxGroups {
		t.Fatalf(
			"eshu_dp_shared_edge_write_groups_total = %d exceeds budget %d "+
				"(scenario=%s): algorithmic regression detected",
			groups, maxGroups, budget.Scenario,
		)
	}
	if groups == 0 {
		t.Fatal("eshu_dp_shared_edge_write_groups_total = 0: instrument not recording (false green guard)")
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
		"scenario=%s eshu_dp_shared_edge_write_groups_total=%d (budget=%d) statements_executed=%d (budget=%d)",
		budget.Scenario, groups, budget.Budgets["eshu_dp_shared_edge_write_groups_total"],
		stmts, budget.Budgets["statements_executed"],
	)
}

// TestCostBudget_DocumentationMaterialization_N1_ExceedsBudget is the
// mandatory negative control. It calls WriteEdges once per fixture row instead
// of once for the whole intent — the classic N+1 anti-pattern — and asserts
// the accumulated eshu_dp_shared_edge_write_groups_total EXCEEDS the committed
// budget. This is a REAL negative control: it drives the SAME production
// EdgeWriter, reads the SAME instrument off the SAME real otel reader as the
// positive test.
func TestCostBudget_DocumentationMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, documentationMaterializationBudgetRelPath)
	rows := documentationEdgeFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedDocumentationEdgeWriter(t)

	for _, row := range rows {
		if err := writer.WriteEdges(
			context.Background(),
			reducer.DomainDocumentationEdges,
			[]reducer.SharedProjectionIntentRow{row},
			documentationCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteEdges() error = %v", err)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	groups := collectCounter(rm, "eshu_dp_shared_edge_write_groups_total")
	maxGroups, ok := budget.Budgets["eshu_dp_shared_edge_write_groups_total"]
	if !ok {
		t.Fatal("budget has no eshu_dp_shared_edge_write_groups_total entry")
	}

	if groups <= maxGroups {
		t.Fatalf(
			"N+1 negative control: eshu_dp_shared_edge_write_groups_total = %d did NOT exceed budget %d — "+
				"budget is too loose to catch N+1 regressions or the negative control is generating too "+
				"few writes; tighten the budget or increase the N+1 fanout",
			groups, maxGroups,
		)
	}

	t.Logf(
		"N+1 negative control passed: eshu_dp_shared_edge_write_groups_total = %d > budget %d (N=%d rows, scenario=%s)",
		groups, maxGroups, len(rows), budget.Scenario,
	)
}

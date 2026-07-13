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

// semanticEntityBudgetRelPath is the committed cost budget for the
// semantic-entity-materialization scenario. Unlike the nested-directory-tree
// scenario, this projection has no committed cassette: cypher.SemanticEntityWriter
// operates over flat reducer.SemanticEntityRow values, not a
// CanonicalMaterialization, so the fixture rows live inline in this file (the
// same convention semantic_entity_test.go already uses) and the budget records
// that explicitly instead of pointing at a cassette path.
var semanticEntityBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "semantic-entity-materialization.cost-budget.json",
)

// semanticEntityFixtureRows is the deterministic input for both the positive
// and N+1 scenarios: two rows of the SAME semantic entity label (Annotation)
// in one repository, matching the reducer_domain semantic_entity_materialization
// (specs/fact-kind-registry.v1.yaml "semantic" family). Same-label rows are
// required, not incidental: cypher.SemanticEntityWriter batches same-label rows
// from one WriteSemanticEntities call into a single UNWIND statement, so the
// positive (one call, two rows) and N+1 (two calls, one row each) scenarios
// only diverge on eshu_dp_neo4j_batches_executed_total when both fixture rows
// share a label — two distinct labels would batch to one statement each either
// way and the N+1 control would not prove anything.
func semanticEntityFixtureRows() []reducer.SemanticEntityRow {
	return []reducer.SemanticEntityRow{
		{
			RepoID:       "repo-1",
			EntityID:     "annotation-1",
			EntityType:   "Annotation",
			EntityName:   "Logged",
			FilePath:     "/repo/src/Logged.java",
			RelativePath: "src/Logged.java",
			Language:     "java",
			StartLine:    12,
			EndLine:      12,
			Metadata: map[string]any{
				"kind":        "applied",
				"target_kind": "method_declaration",
			},
		},
		{
			RepoID:       "repo-1",
			EntityID:     "annotation-2",
			EntityType:   "Annotation",
			EntityName:   "Deprecated",
			FilePath:     "/repo/src/Deprecated.java",
			RelativePath: "src/Deprecated.java",
			Language:     "java",
			StartLine:    4,
			EndLine:      4,
			Metadata: map[string]any{
				"kind":        "applied",
				"target_kind": "class_declaration",
			},
		},
	}
}

// newInstrumentedSemanticEntityWriter builds the production
// cypher.SemanticEntityWriter using the same constructor
// (NewSemanticEntityWriterWithCanonicalNodeRows + WithLabelScopedRetract) the
// NornicDB deployment path selects (go/cmd/reducer/neo4j_wiring.go
// semanticEntityWriterForGraphBackend), wired over a groupCountingExecutor
// wrapped by the production cypher.InstrumentedExecutor. InstrumentedExecutor
// is the same wrapper go/cmd/reducer/observed_service_wiring.go applies to the
// real Neo4j/NornicDB executor, and it records
// eshu_dp_neo4j_batches_executed_total on every UNWIND-shaped statement (one
// per statement carrying a "rows" parameter) via its production
// recordStatementBatchMetrics helper — the PRIMARY instrument this scenario
// asserts, not a hand-counted statement slice.
func newInstrumentedSemanticEntityWriter(t *testing.T) (
	writer *cypher.SemanticEntityWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewSemanticEntityWriterWithCanonicalNodeRows(instrumented, 500).WithLabelScopedRetract()
	return writer, exec, manualReader
}

// TestCostBudget_SemanticEntityMaterialization is the positive cost-counting
// gate for the semantic_entity_materialization reducer projection (the
// "semantic" family in specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It
// drives the production cypher.SemanticEntityWriter.WriteSemanticEntities over
// two deterministic rows through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total. InstrumentedExecutor
// records this once per UNWIND-shaped statement (a statement whose Parameters
// carry a "rows" key) passed through Execute or ExecuteGroup
// (recordStatementBatchMetrics). The semantic writer's retract statements key
// on "repo_ids"/label-scoped MATCH, not "rows", so only write batches increment
// the counter. Both fixture rows share the Annotation label and the default
// batch size (500) comfortably covers two rows, so the writer emits exactly
// one Annotation UNWIND batch for the whole call, giving a deterministic count
// of 1. Any increase — an N+1 write cycle or an extra batch split — trips the
// gate.
func TestCostBudget_SemanticEntityMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, semanticEntityBudgetRelPath)
	writer, exec, reader := newInstrumentedSemanticEntityWriter(t)

	_, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    semanticEntityFixtureRows(),
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
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

// TestCostBudget_SemanticEntityMaterialization_N1_ExceedsBudget is the
// mandatory negative control. It calls WriteSemanticEntities once per fixture
// row instead of once for the whole batch — the classic N+1 anti-pattern — and
// asserts the accumulated eshu_dp_neo4j_batches_executed_total EXCEEDS the
// committed budget. This is a REAL negative control: it drives the SAME
// production writer, through the SAME InstrumentedExecutor wrapper, reading
// the SAME instrument off the SAME real otel reader as the positive test.
func TestCostBudget_SemanticEntityMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, semanticEntityBudgetRelPath)
	rows := semanticEntityFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedSemanticEntityWriter(t)

	for _, row := range rows {
		if _, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
			RepoIDs: []string{row.RepoID},
			Rows:    []reducer.SemanticEntityRow{row},
		}); err != nil {
			t.Fatalf("N+1 WriteSemanticEntities() error = %v", err)
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

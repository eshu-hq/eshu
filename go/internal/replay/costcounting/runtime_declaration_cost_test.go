// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"path/filepath"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

var runtimeDeclarationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "runtime-declaration-projection.cost-budget.json",
)

type runtimeDeclarationFactLoader struct{ envelopes []facts.Envelope }

func (l runtimeDeclarationFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// runtimeDeclarationCostFacts is one full generation of one repository: a
// .tool-versions line pinning two node versions and a .python-version line.
func runtimeDeclarationCostFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	out := []facts.Envelope{{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-1"}}}
	for _, v := range []runtimedeclarationv1.Version{
		{RelativePath: ".tool-versions", OrderIndex: 0, Runtime: "node", RawSpec: "20.11.1", SourceKind: "tool_versions"},
		{RelativePath: ".tool-versions", OrderIndex: 1, Runtime: "node", RawSpec: "18.19.0", SourceKind: "tool_versions"},
		{RelativePath: ".python-version", OrderIndex: 0, Runtime: "python", RawSpec: "3.12.1", SourceKind: "python_version"},
	} {
		v.RepoID, v.ScopeDir, v.LineNumber, v.SpecKind = "repo-1", ".", 1, runtimedeclarationv1.SpecKindExact
		payload, err := factschema.EncodeRuntimeDeclarationVersion(v)
		if err != nil {
			t.Fatalf("encode runtime declaration: %v", err)
		}
		out = append(out, facts.Envelope{
			FactKind:      facts.RuntimeDeclarationVersionFactKind,
			SchemaVersion: facts.RuntimeDeclarationSchemaVersionV1,
			Payload:       payload,
		})
	}
	return out
}

func newInstrumentedRuntimeDeclarationWriter(t *testing.T) (*edgewriter.RuntimeDeclarationWriter, *groupCountingExecutor, *sdkmetric.ManualReader) {
	t.Helper()
	inst, reader := newManualReaderInstruments(t)
	exec := &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	return edgewriter.NewRuntimeDeclarationWriter(instrumented, 500), exec, reader
}

// TestCostBudget_RuntimeDeclarationProjection is the positive cost gate for
// the runtime_declaration_projection reducer domain (#8816). It drives the
// production declaration.Handler over the production RuntimeDeclarationWriter
// through cypher.InstrumentedExecutor and asserts
// eshu_dp_neo4j_batches_executed_total (one per UNWIND statement carrying a
// "rows" parameter) stays within the committed budget: one write batch for
// the whole intent. The whole-repository retract carries no "rows" parameter,
// so it shows only in statements_executed.
func TestCostBudget_RuntimeDeclarationProjection(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, runtimeDeclarationBudgetRelPath)
	writer, exec, reader := newInstrumentedRuntimeDeclarationWriter(t)
	handler := declaration.Handler{
		FactLoader:           runtimeDeclarationFactLoader{envelopes: runtimeDeclarationCostFacts(t)},
		Writer:               writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID: "intent-1", ScopeID: "scope-1", GenerationID: "gen-1",
		Domain: reducercontract.DomainRuntimeDeclarationProjection, AttemptCount: 1,
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	batches := collectCounter(rm, "eshu_dp_neo4j_batches_executed_total")
	maxBatches := budget.Budgets["eshu_dp_neo4j_batches_executed_total"]
	if batches == 0 {
		t.Fatal("eshu_dp_neo4j_batches_executed_total = 0: instrument not recording (false green guard)")
	}
	if batches > maxBatches {
		t.Fatalf("eshu_dp_neo4j_batches_executed_total = %d exceeds budget %d (scenario=%s)", batches, maxBatches, budget.Scenario)
	}
	if stmts, maxStmts := exec.totalStatements(), budget.Budgets["statements_executed"]; stmts == 0 || stmts > maxStmts {
		t.Fatalf("statements_executed = %d, want 1..%d (scenario=%s)", stmts, maxStmts, budget.Scenario)
	}
}

// TestCostBudget_RuntimeDeclarationProjection_N1_ExceedsBudget is the
// negative control: writing each declaration in its own call (the N+1 shape)
// through the same production writer and instrument must exceed the budget.
func TestCostBudget_RuntimeDeclarationProjection_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, runtimeDeclarationBudgetRelPath)
	rows, _, err := declaration.ExtractRows(runtimeDeclarationCostFacts(t))
	if err != nil {
		t.Fatalf("ExtractRows() error = %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows; fixture has %d", len(rows))
	}
	writer, _, reader := newInstrumentedRuntimeDeclarationWriter(t)
	for _, row := range rows {
		if err := writer.WriteRuntimeDeclarations(context.Background(), []map[string]any{row}, "gen-1", declaration.EvidenceSource); err != nil {
			t.Fatalf("N+1 WriteRuntimeDeclarations() error = %v", err)
		}
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if batches, maxBatches := collectCounter(rm, "eshu_dp_neo4j_batches_executed_total"), budget.Budgets["eshu_dp_neo4j_batches_executed_total"]; batches <= maxBatches {
		t.Fatalf("N+1 negative control: batches = %d did NOT exceed budget %d", batches, maxBatches)
	}
}

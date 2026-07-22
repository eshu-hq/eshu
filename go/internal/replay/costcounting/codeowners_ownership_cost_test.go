// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// codeownersOwnershipBudgetRelPath is the committed cost budget for the
// codeowners_ownership scenario. Like documentation_materialization, this
// projection has no committed cassette: cypher.EdgeWriter.WriteEdges operates
// over flat reducer.SharedProjectionIntentRow values, not a
// CanonicalMaterialization, so the fixture rows live inline in this file and
// the budget records that explicitly instead of pointing at a cassette path.
var codeownersOwnershipBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "codeowners-ownership.cost-budget.json",
)

const codeownersOwnershipCostEvidenceSource = "reducer/codeowners"

// codeownersOwnershipEdgeFixtureRows is the deterministic input for both the
// positive and N+1 scenarios: two DECLARES_CODEOWNER edge rows for one
// repository, shaped exactly like buildCodeownersOwnershipIntentRows
// (go/internal/reducer/codeowners_ownership_materialization.go) builds them
// from extracted (pattern, owner) rows, for the reducer_domain
// codeowners_ownership (specs/fact-kind-registry.v1.yaml "codeowners" family,
// DomainCodeownersOwnership intent / DomainCodeownersOwnershipEdges write
// route). The pattern/owner values reuse the real golden-corpus fixture at
// tests/fixtures/ecosystems/go_comprehensive/.github/CODEOWNERS (issue #5419
// Phase 5) instead of inventing unrelated ones.
func codeownersOwnershipEdgeFixtureRows() []reducer.SharedProjectionIntentRow {
	rows := []map[string]any{
		{
			"repo_id":       "repo-1",
			"owner_ref":     "@eshu-hq/platform",
			"pattern":       "*.go",
			"source_path":   ".github/CODEOWNERS",
			"order_index":   0,
			"generation_id": "gen-1",
		},
		{
			"repo_id":       "repo-1",
			"owner_ref":     "@eshu-hq/docs",
			"pattern":       "/docs/",
			"source_path":   ".github/CODEOWNERS",
			"order_index":   1,
			"generation_id": "gen-1",
		},
	}
	intents := make([]reducer.SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, reducer.SharedProjectionIntentRow{
			ProjectionDomain: reducer.DomainCodeownersOwnershipEdges,
			PartitionKey: fmt.Sprintf(
				"%s->%s->%s->%s", row["repo_id"], row["source_path"], row["pattern"], row["owner_ref"],
			),
			ScopeID:      "scope-1",
			RepositoryID: row["repo_id"].(string),
			Payload:      row,
		})
	}
	return intents
}

// newInstrumentedCodeownersOwnershipEdgeWriter builds the production
// cypher.EdgeWriter used by CodeownersOwnershipEdgeMaterializationHandler
// (go/internal/reducer/codeowners_ownership_materialization.go), wired over a
// groupCountingExecutor that implements GroupExecutor so WriteEdges takes its
// atomic-transaction path. EdgeWriter.Instruments is the same field
// go/cmd/reducer/endpoint_presence_wiring.go newHandlerEdgeWriter sets from
// the real telemetry.Instruments registry, and EdgeWriter.recordGroupedWrite
// increments eshu_dp_shared_edge_write_groups_total on every grouped
// WriteEdges call -- the PRIMARY instrument this scenario asserts, not a
// hand-counted statement slice.
func newInstrumentedCodeownersOwnershipEdgeWriter(t *testing.T) (
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

// TestCostBudget_CodeownersOwnership is the positive cost-counting gate for
// the codeowners_ownership reducer projection (the "codeowners" family in
// specs/fact-kind-registry.v1.yaml, issue #5419 Phase 6 replay-coverage
// gap-close). It drives the production cypher.EdgeWriter.WriteEdges over two
// deterministic DECLARES_CODEOWNER edge rows through a real
// telemetry.Instruments registry backed by an sdkmetric.ManualReader, then
// asserts eshu_dp_shared_edge_write_groups_total is within the committed
// budget.
//
// This scenario calls WriteEdges only, not RetractEdges: the whole-repository
// RetractEdges path for codeowners issues a single non-grouped
// Executor.Execute call (canonical_codeowners_edges.go's
// retractCodeownersOwnershipEdgesCypher, dispatched via edge_writer_retract.go),
// which never reaches EdgeWriter.recordGroupedWrite, so including it would not
// move the asserted instrument while adding an unreviewable extra statement to
// the fixture -- the same simplification documentation_edges_cost_test.go
// makes.
//
// Instrument read: eshu_dp_shared_edge_write_groups_total.
// EdgeWriter.WriteEdges routes both fixture rows to the SAME single Cypher
// template (batchCanonicalCodeownersOwnershipEdgeCypher -- codeowners has no
// per-kind template branch, unlike documentation's entity/workload split),
// batches them into one UNWIND statement, and calls ge.ExecuteGroup(ctx,
// stmts) exactly once per WriteEdges call when the executor implements
// GroupExecutor and no per-domain group-batch override applies
// (recordGroupedWrite in edge_writer.go); codeowners has no override
// (groupBatchSizeForDomain only special-cases CodeCalls, InheritanceEdges,
// SQLRelationships, ShellExec). So the counter is exactly 1 per WriteEdges
// call and the statement count is exactly 1 (one batched UNWIND covering both
// rows) regardless of row count -- any increase means the reducer handler is
// issuing more than one write transaction (or more than one statement) per
// intent, the N+1 pattern this gate exists to catch.
func TestCostBudget_CodeownersOwnership(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, codeownersOwnershipBudgetRelPath)
	writer, exec, reader := newInstrumentedCodeownersOwnershipEdgeWriter(t)

	if err := writer.WriteEdges(
		context.Background(),
		reducer.DomainCodeownersOwnershipEdges,
		codeownersOwnershipEdgeFixtureRows(),
		codeownersOwnershipCostEvidenceSource,
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

// TestCostBudget_CodeownersOwnership_N1_ExceedsBudget is the mandatory
// negative control. It calls WriteEdges once per fixture row instead of once
// for the whole intent -- the classic N+1 anti-pattern -- and asserts the
// accumulated eshu_dp_shared_edge_write_groups_total EXCEEDS the committed
// budget. This is a REAL negative control: it drives the SAME production
// EdgeWriter, reads the SAME instrument off the SAME real otel reader as the
// positive test.
func TestCostBudget_CodeownersOwnership_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, codeownersOwnershipBudgetRelPath)
	rows := codeownersOwnershipEdgeFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedCodeownersOwnershipEdgeWriter(t)

	for _, row := range rows {
		if err := writer.WriteEdges(
			context.Background(),
			reducer.DomainCodeownersOwnershipEdges,
			[]reducer.SharedProjectionIntentRow{row},
			codeownersOwnershipCostEvidenceSource,
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
			"N+1 negative control: eshu_dp_shared_edge_write_groups_total = %d did NOT exceed budget %d -- "+
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

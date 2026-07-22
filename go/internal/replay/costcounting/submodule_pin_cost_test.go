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

// submodulePinBudgetRelPath is the committed cost budget for the
// submodule_pin scenario. Like codeowners_ownership, this projection has no
// committed cassette: cypher.EdgeWriter.WriteEdges operates over flat
// reducer.SharedProjectionIntentRow values, not a CanonicalMaterialization,
// so the fixture rows live inline in this file and the budget records that
// explicitly instead of pointing at a cassette path.
var submodulePinBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "submodule-pin.cost-budget.json",
)

const submodulePinCostEvidenceSource = "reducer/submodule"

// submodulePinEdgeFixtureRows is the deterministic input for both the
// positive and N+1 scenarios: two PINS_SUBMODULE edge rows for one parent
// repository, shaped exactly like buildSubmodulePinIntentRows
// (go/internal/reducer/submodule_pin_materialization.go) builds them from
// extracted (parent_repo_id, resolved_repo_id, submodule_path, pinned_sha)
// rows, for the reducer_domain submodule_pin (specs/fact-kind-registry.v1.yaml
// "submodule" family, DomainSubmodulePin intent / DomainSubmodulePinEdges
// write route). The submodule_path value reuses the real golden-corpus
// fixture's declared path at tests/fixtures/ecosystems/deployable-config/.gitmodules
// (issue #5420 Phase 5, registered as a git gitlink by
// scripts/verify-golden-corpus-gate.sh) instead of inventing an unrelated one.
func submodulePinEdgeFixtureRows() []reducer.SharedProjectionIntentRow {
	rows := []map[string]any{
		{
			"parent_repo_id":   "repo-1",
			"resolved_repo_id": "repo-2",
			"submodule_path":   "vendor/deployable-source",
			"pinned_sha":       "5420542054205420542054205420542054205420",
			"generation_id":    "gen-1",
		},
		{
			"parent_repo_id":   "repo-1",
			"resolved_repo_id": "repo-3",
			"submodule_path":   "vendor/second-submodule",
			"pinned_sha":       "",
			"generation_id":    "gen-1",
		},
	}
	intents := make([]reducer.SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, reducer.SharedProjectionIntentRow{
			ProjectionDomain: reducer.DomainSubmodulePinEdges,
			PartitionKey: fmt.Sprintf(
				"%s->%s", row["parent_repo_id"], row["submodule_path"],
			),
			ScopeID:      "scope-1",
			RepositoryID: row["parent_repo_id"].(string),
			Payload:      row,
		})
	}
	return intents
}

// newInstrumentedSubmodulePinEdgeWriter builds the production
// cypher.EdgeWriter used by SubmodulePinEdgeMaterializationHandler
// (go/internal/reducer/submodule_pin_materialization.go), wired over a
// groupCountingExecutor that implements GroupExecutor so WriteEdges takes its
// atomic-transaction path. EdgeWriter.Instruments is the same field
// go/cmd/reducer/endpoint_presence_wiring.go newHandlerEdgeWriter sets from
// the real telemetry.Instruments registry, and EdgeWriter.recordGroupedWrite
// increments eshu_dp_shared_edge_write_groups_total on every grouped
// WriteEdges call -- the PRIMARY instrument this scenario asserts, not a
// hand-counted statement slice.
func newInstrumentedSubmodulePinEdgeWriter(t *testing.T) (
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

// TestCostBudget_SubmodulePin is the positive cost-counting gate for the
// submodule_pin reducer projection (the "submodule" family in
// specs/fact-kind-registry.v1.yaml, issue #5420 Phase 5 replay-coverage
// gap-close). It drives the production cypher.EdgeWriter.WriteEdges over two
// deterministic PINS_SUBMODULE edge rows through a real telemetry.Instruments
// registry backed by an sdkmetric.ManualReader, then asserts
// eshu_dp_shared_edge_write_groups_total is within the committed budget.
//
// This scenario calls WriteEdges only, not RetractEdges: the whole-repository
// RetractEdges path for submodule pins issues a single non-grouped
// Executor.Execute call (canonical_submodule_edges.go's
// retractSubmodulePinEdgesCypher, dispatched via edge_writer_retract.go),
// which never reaches EdgeWriter.recordGroupedWrite, so including it would
// not move the asserted instrument while adding an unreviewable extra
// statement to the fixture -- the same simplification
// codeowners_ownership_cost_test.go makes.
//
// Instrument read: eshu_dp_shared_edge_write_groups_total. EdgeWriter.WriteEdges
// routes both fixture rows to the SAME single Cypher template
// (batchCanonicalSubmodulePinEdgeCypher -- submodule_pin has no per-kind
// template branch), batches them into one UNWIND statement, and calls
// ge.ExecuteGroup(ctx, stmts) exactly once per WriteEdges call when the
// executor implements GroupExecutor and no per-domain group-batch override
// applies (recordGroupedWrite in edge_writer.go); submodule_pin has no
// override (groupBatchSizeForDomain only special-cases CodeCalls,
// InheritanceEdges, SQLRelationships, ShellExec). So the counter is exactly 1
// per WriteEdges call and the statement count is exactly 1 (one batched
// UNWIND covering both rows) regardless of row count -- any increase means
// the reducer handler is issuing more than one write transaction (or more
// than one statement) per intent, the N+1 pattern this gate exists to catch.
func TestCostBudget_SubmodulePin(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, submodulePinBudgetRelPath)
	writer, exec, reader := newInstrumentedSubmodulePinEdgeWriter(t)

	if err := writer.WriteEdges(
		context.Background(),
		reducer.DomainSubmodulePinEdges,
		submodulePinEdgeFixtureRows(),
		submodulePinCostEvidenceSource,
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

// TestCostBudget_SubmodulePin_N1_ExceedsBudget is the mandatory negative
// control. It calls WriteEdges once per fixture row instead of once for the
// whole intent -- the classic N+1 anti-pattern -- and asserts the
// accumulated eshu_dp_shared_edge_write_groups_total EXCEEDS the committed
// budget. This is a REAL negative control: it drives the SAME production
// EdgeWriter, reads the SAME instrument off the SAME real otel reader as the
// positive test.
func TestCostBudget_SubmodulePin_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, submodulePinBudgetRelPath)
	rows := submodulePinEdgeFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedSubmodulePinEdgeWriter(t)

	for _, row := range rows {
		if err := writer.WriteEdges(
			context.Background(),
			reducer.DomainSubmodulePinEdges,
			[]reducer.SharedProjectionIntentRow{row},
			submodulePinCostEvidenceSource,
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

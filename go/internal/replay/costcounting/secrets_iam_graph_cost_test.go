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

// secretsIAMGraphBudgetRelPath is the committed cost budget for the
// secrets_iam_trust_chain scenario (fact-kind-registry family secrets_iam,
// specs/fact-kind-registry.v1.yaml:336-339, reducer_domain
// secrets_iam_trust_chain). Like the semantic-entity and documentation-edges
// scenarios, this projection has no committed cassette:
// cypher.SecretsIAMGraphWriter operates over flat map[string]any rows, not a
// CanonicalMaterialization, so the fixture rows live inline in this file and
// the budget records that explicitly instead of pointing at a cassette path.
var secretsIAMGraphBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "secrets-iam-trust-chain.cost-budget.json",
)

// secretsIAMGraphCostEvidence mirrors the evidence-source shape the
// production secrets/IAM graph projection handler stamps
// (go/internal/reducer/secrets_iam_graph_projection.go).
const secretsIAMGraphCostEvidence = "reducer/secrets-iam-graph"

// secretsIAMServiceAccountFixtureRows is the deterministic input for both the
// positive and N+1 scenarios: two SecretsIAMServiceAccount node rows in one
// scope, shaped like secrets_iam_graph_writer_test.go's saNodeRows fixture.
// WriteServiceAccountNodes has exactly one Cypher template, so any two
// distinct rows are sufficient to prove the N+1 control.
func secretsIAMServiceAccountFixtureRows() []map[string]any {
	row := func(id string) map[string]any {
		return map[string]any{
			"uid":             "sha256:sa-" + id,
			"scope_id":        "scope-1",
			"generation_id":   "gen-1",
			"evidence_source": secretsIAMGraphCostEvidence,
			"confidence":      "exact",
		}
	}
	return []map[string]any{row("a"), row("b")}
}

// newInstrumentedSecretsIAMGraphWriter builds the production
// cypher.SecretsIAMGraphWriter using the SAME unwrapped constructor
// production wiring calls
// (go/cmd/reducer/secrets_iam_graph_wiring.go:63
// secretsIAMGraphProjectionWriter -> sourcecypher.NewSecretsIAMGraphWriter(executor,
// batchSize)), wired over a groupCountingExecutor wrapped by the production
// cypher.InstrumentedExecutor — the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec, which secrets_iam_graph_wiring.go then receives as its executor
// parameter. Driving cypher.SecretsIAMGraphWriter directly at the writer
// level, never enabling ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED or
// touching cmd/reducer's domain registry, is the established precedent for
// this ADR #1314-governed writer (see
// go/internal/storage/cypher/evidence-4367-iam-variable-retract.md
// "Governance decision: secrets/IAM writer-level exercise (ADR #1314)" —
// TestReducerSecretsIAMEdgeRetractGraphTruth uses the identical
// writer-level-only pattern). InstrumentedExecutor records
// eshu_dp_neo4j_batches_executed_total on every UNWIND-shaped statement (one
// per statement carrying a "rows" parameter) — the PRIMARY instrument this
// scenario asserts, not a hand-counted statement slice.
func newInstrumentedSecretsIAMGraphWriter(t *testing.T) (
	writer *cypher.SecretsIAMGraphWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewSecretsIAMGraphWriter(instrumented, 500)
	return writer, exec, manualReader
}

// TestCostBudget_SecretsIAMTrustChain is the positive cost-counting gate for
// the secrets_iam_trust_chain reducer projection (the secrets_iam family in
// specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production cypher.SecretsIAMGraphWriter.WriteServiceAccountNodes over two
// deterministic rows through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Scoping rationale (why one of nine Write* families is representative): the
// live projection handler (reducer.SecretsIAMGraphProjectionHandler,
// go/internal/reducer/secrets_iam_graph_projection.go:28-36) can call nine
// Write* families per intent — four node families and five edge families.
// Every one of the nine has exactly one fixed-const UNWIND Cypher template
// (secrets_iam_graph_writer.go, ADR #1314 §5/§6: no data-driven token is ever
// interpolated), and all nine dispatch through the SAME
// SecretsIAMGraphWriter.writeBatched helper — buildBatchedStatements over the
// same batch size, the same GroupExecutor/sequential dispatch, the same
// instrumented executor. The per-family-call cost shape this scenario pins
// (one WriteX call with same-batch rows == one UNWIND batch; N calls == N
// batches) is therefore structurally identical across all nine families;
// asserting WriteServiceAccountNodes pins the shared writeBatched shape they
// all inherit. A full per-intent multi-family budget would require driving
// the governance-gated projection handler itself (ADR #1314,
// ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED), which writer-level
// scenarios deliberately never do.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total, incremented once per
// UNWIND-shaped statement passed through Execute or ExecuteGroup
// (recordStatementBatchMetrics). WriteServiceAccountNodes uses exactly one
// Cypher template, and the default batch size (500) comfortably covers two
// rows, so one call emits exactly one UNWIND batch. Any increase — an N+1
// write cycle or an extra batch split — trips the gate.
func TestCostBudget_SecretsIAMTrustChain(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, secretsIAMGraphBudgetRelPath)
	writer, exec, reader := newInstrumentedSecretsIAMGraphWriter(t)

	if err := writer.WriteServiceAccountNodes(
		context.Background(), secretsIAMServiceAccountFixtureRows(),
	); err != nil {
		t.Fatalf("WriteServiceAccountNodes() error = %v", err)
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

// TestCostBudget_SecretsIAMTrustChain_N1_ExceedsBudget is the mandatory
// negative control. It calls WriteServiceAccountNodes once per fixture row
// instead of once for the whole batch — the classic N+1 anti-pattern — and
// asserts the accumulated eshu_dp_neo4j_batches_executed_total EXCEEDS the
// committed budget. This is a REAL negative control: it drives the SAME
// production writer, through the SAME InstrumentedExecutor wrapper, reading
// the SAME instrument off the SAME real otel reader as the positive test.
func TestCostBudget_SecretsIAMTrustChain_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, secretsIAMGraphBudgetRelPath)
	rows := secretsIAMServiceAccountFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedSecretsIAMGraphWriter(t)

	for _, row := range rows {
		if err := writer.WriteServiceAccountNodes(
			context.Background(), []map[string]any{row},
		); err != nil {
			t.Fatalf("N+1 WriteServiceAccountNodes() error = %v", err)
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

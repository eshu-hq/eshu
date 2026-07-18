// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/graphowner"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// kubernetesWorkloadNodeBudgetRelPath is the committed cost budget for the
// kubernetes_correlation scenario (fact-kind-registry family kubernetes_live,
// specs/fact-kind-registry.v1.yaml:202-213, reducer_domain
// kubernetes_correlation). Like ec2_instance_node_materialization, this
// projection writes through cypher.KubernetesWorkloadNodeWriter over flat
// map[string]any rows, not a CanonicalMaterialization, so the fixture rows
// live inline in this file and the budget records that explicitly instead of
// pointing at a cassette path.
var kubernetesWorkloadNodeBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "kubernetes-workload-node-materialization.cost-budget.json",
)

const kubernetesWorkloadNodeCostEvidenceSource = "reducer/kubernetes-workloads"

// Kubernetes fixture source-order keys, mirroring the EC2/Azure/GCP
// scenarios: each row's source_order_key is what the production row builder
// stamps (go/internal/reducer/kubernetes_workload_materialization.go
// kubernetesWorkloadNodeRow, sourceOrderKeyField /
// go/internal/reducer/source_order_key.go).
const (
	kubernetesWorkloadOrderKeyRowA          = "2026-07-01T00:00:00.000000000Z|k8s-fact-a"
	kubernetesWorkloadOrderKeyRowB          = "2026-07-01T00:00:00.000000000Z|k8s-fact-b"
	kubernetesWorkloadOrderKeyForeignWinner = "2026-07-02T00:00:00.000000000Z|k8s-fact-foreign"
)

// kubernetesWorkloadNodeFixtureRows is the deterministic input for the
// positive, contended-loss, and N+1 scenarios: two KubernetesWorkload rows in
// one cluster/namespace, shaped EXACTLY like the current production
// kubernetesWorkloadNodeRow row contract
// (go/internal/reducer/kubernetes_workload_materialization.go:295-325). The
// node uid is the live object_id (never the raw metadata.uid, which is
// carried as workload_uid only — see that function's doc comment).
// cypher.KubernetesWorkloadNodeWriter has one Cypher template and no per-row
// batching split, so any two distinct rows are sufficient to prove the N+1
// control.
func kubernetesWorkloadNodeFixtureRows() []map[string]any {
	row := func(id, orderKey string) map[string]any {
		return map[string]any{
			"uid":                    "k8s-object-" + id,
			"cluster_id":             "cluster-demo",
			"namespace":              "default",
			"name":                   "web-" + id,
			"workload_uid":           "k8s-metadata-uid-" + id,
			"group_version_resource": "apps/v1/deployments",
			"service_account":        "default",
			"image_refs":             []string{"ghcr.io/eshu-hq/web:" + id},
			"selector":               []string{"app=web-" + id},
			"correlation_anchors":    []string{"web-" + id},
			"source_fact_id":         "k8s-fact-" + id,
			"stable_fact_key":        "k8s-key-" + id,
			"source_system":          "kubernetes",
			"source_record_id":       "k8s-rec-" + id,
			"source_confidence":      "reported",
			"collector_kind":         "kubernetes",
			"source_order_key":       orderKey,
		}
	}
	return []map[string]any{row("a", kubernetesWorkloadOrderKeyRowA), row("b", kubernetesWorkloadOrderKeyRowB)}
}

// kubernetesFakeOwnerRows is a postgres.Rows yielding (uid, source_order_key)
// winner pairs for the owner ledger's winners read-back query. Local to this
// file per the C-14 executor split (cost_scenario_helpers_test.go is
// orchestrator-owned); it mirrors ec2FakeOwnerRows exactly.
type kubernetesFakeOwnerRows struct {
	pairs [][2]string
	idx   int
}

func (r *kubernetesFakeOwnerRows) Next() bool { r.idx++; return r.idx <= len(r.pairs) }

func (r *kubernetesFakeOwnerRows) Scan(dest ...any) error {
	pair := r.pairs[r.idx-1]
	*(dest[0].(*string)) = pair[0]
	*(dest[1].(*string)) = pair[1]
	return nil
}

func (r *kubernetesFakeOwnerRows) Err() error   { return nil }
func (r *kubernetesFakeOwnerRows) Close() error { return nil }

// kubernetesFakeOwnerTx is a fake postgres.Transaction the REAL
// postgres.GraphNodeOwnerStore runs its REAL SQL against, mirroring
// ec2FakeOwnerTx: the advisory-lock acquisition and max-order-key ledger
// upsert land on ExecContext (both succeed; results discarded), and the
// winners read-back lands on QueryContext, answered from the configured
// winners map.
type kubernetesFakeOwnerTx struct {
	winners    map[string]string
	committed  bool
	rolledBack bool
}

func (t *kubernetesFakeOwnerTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (t *kubernetesFakeOwnerTx) QueryContext(_ context.Context, _ string, args ...any) (postgres.Rows, error) {
	uids, _ := args[0].([]string)
	pairs := make([][2]string, 0, len(uids))
	for _, uid := range uids {
		if win, ok := t.winners[uid]; ok {
			pairs = append(pairs, [2]string{uid, win})
		}
	}
	return &kubernetesFakeOwnerRows{pairs: pairs}, nil
}

func (t *kubernetesFakeOwnerTx) Commit() error   { t.committed = true; return nil }
func (t *kubernetesFakeOwnerTx) Rollback() error { t.rolledBack = true; return nil }

// kubernetesFakeOwnerBeginner is a fake postgres.Beginner handing out
// kubernetesFakeOwnerTx transactions configured with the same winners map.
type kubernetesFakeOwnerBeginner struct {
	winners map[string]string
}

func (b *kubernetesFakeOwnerBeginner) Begin(context.Context) (postgres.Transaction, error) {
	return &kubernetesFakeOwnerTx{winners: b.winners}, nil
}

// newGatedInstrumentedKubernetesWorkloadWriter builds the PRODUCTION
// Kubernetes write dispatch: graphowner.NewKubernetesWorkloadGatedWriter over
// the raw cypher.KubernetesWorkloadNodeWriter's WriteKubernetesWorkloadNodes
// method, mirroring go/cmd/reducer/canonical_graph_writers.go:52/64 exactly.
// As with EC2/Azure/GCP, the #5007 owner-ledger Gate chunks rows, opens one
// owner-ledger Postgres transaction per chunk against the REAL
// postgres.GraphNodeOwnerStore.ResolveOwnedUIDs (run against the fake
// transaction above), and FILTERS contended-lost rows before delegating to
// the raw writer. The raw writer sits over a groupCountingExecutor wrapped by
// the production cypher.InstrumentedExecutor, the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor. winners configures the fake owner ledger's
// read-back (uid -> winning order key).
func newGatedInstrumentedKubernetesWorkloadWriter(t *testing.T, winners map[string]string) (
	gated *graphowner.KubernetesWorkloadGatedWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	raw := cypher.NewKubernetesWorkloadNodeWriter(instrumented, 500)
	gate := graphowner.NewGate(&kubernetesFakeOwnerBeginner{winners: winners})
	gate.Instruments = inst
	gated = graphowner.NewKubernetesWorkloadGatedWriter(gate, raw.WriteKubernetesWorkloadNodes)
	return gated, exec, manualReader
}

// kubernetesWorkloadOwnedAllWinners is the non-contended winners map: every
// fixture uid's winning order key equals the fixture row's own key, so this
// batch owns both uids and the gate delegates both rows to the raw writer
// unfiltered.
func kubernetesWorkloadOwnedAllWinners() map[string]string {
	return map[string]string{
		"k8s-object-a": kubernetesWorkloadOrderKeyRowA,
		"k8s-object-b": kubernetesWorkloadOrderKeyRowB,
	}
}

// TestCostBudget_KubernetesWorkloadNodeMaterialization is the positive
// cost-counting gate for the kubernetes_correlation reducer projection (the
// kubernetes_live family in specs/fact-kind-registry.v1.yaml, C-14 issue
// #4367). It drives the production GATED dispatch —
// graphowner.KubernetesWorkloadGatedWriter.WriteKubernetesWorkloadNodes over
// the raw cypher.KubernetesWorkloadNodeWriter, per
// canonical_graph_writers.go:52/64 — over two deterministic rows in the
// owned-all (non-contended) case, through a real InstrumentedExecutor-backed
// sdkmetric.ManualReader, then asserts eshu_dp_neo4j_batches_executed_total is
// within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total, recorded once per
// UNWIND-shaped statement passed through Execute or ExecuteGroup. Both
// fixture rows fit in one gate chunk (lockChunkSize = 500) and one raw-writer
// batch (default 500), so the FULL gated dispatch emits exactly one batch. The
// owned-all case also pins eshu_dp_cross_scope_ownership_contended_rows_total
// at 0.
func TestCostBudget_KubernetesWorkloadNodeMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, kubernetesWorkloadNodeBudgetRelPath)
	gated, exec, reader := newGatedInstrumentedKubernetesWorkloadWriter(t, kubernetesWorkloadOwnedAllWinners())

	if err := gated.WriteKubernetesWorkloadNodes(
		context.Background(), kubernetesWorkloadNodeFixtureRows(), kubernetesWorkloadNodeCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteKubernetesWorkloadNodes() error = %v", err)
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

	if contended := collectCounter(rm, "eshu_dp_cross_scope_ownership_contended_rows_total"); contended != 0 {
		t.Fatalf("eshu_dp_cross_scope_ownership_contended_rows_total = %d, want 0 in the owned-all case", contended)
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

// TestCostBudget_KubernetesWorkloadNodeMaterialization_ContendedLossFiltersRow
// proves the gated dispatch's contended path keeps the same batch cost while
// filtering the lost row: the fake owner ledger reports object-b already
// owned by a strictly-higher-order-key contributor from another scope, so
// Gate.write's filterOwnedRows drops that row and the raw writer receives ONE
// row.
func TestCostBudget_KubernetesWorkloadNodeMaterialization_ContendedLossFiltersRow(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, kubernetesWorkloadNodeBudgetRelPath)
	winners := map[string]string{
		"k8s-object-a": kubernetesWorkloadOrderKeyRowA,          // owned: winner is this batch's own key
		"k8s-object-b": kubernetesWorkloadOrderKeyForeignWinner, // lost: a higher-order-key contributor won
	}
	gated, exec, reader := newGatedInstrumentedKubernetesWorkloadWriter(t, winners)

	if err := gated.WriteKubernetesWorkloadNodes(
		context.Background(), kubernetesWorkloadNodeFixtureRows(), kubernetesWorkloadNodeCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteKubernetesWorkloadNodes() error = %v", err)
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
	if batches != maxBatches {
		t.Fatalf(
			"contended case: eshu_dp_neo4j_batches_executed_total = %d, want exactly %d — "+
				"losing one row must not change the UNWIND batch count",
			batches, maxBatches,
		)
	}

	if payload := collectFloat64HistogramSum(rm, "eshu_dp_neo4j_batch_size"); payload != 1 {
		t.Fatalf(
			"contended case: eshu_dp_neo4j_batch_size Sum = %v, want 1 — the surviving batch "+
				"must carry ONLY the owned row (filterOwnedRows dropped the contended-lost row)",
			payload,
		)
	}

	if contended := collectCounter(rm, "eshu_dp_cross_scope_ownership_contended_rows_total"); contended != 1 {
		t.Fatalf(
			"contended case: eshu_dp_cross_scope_ownership_contended_rows_total = %d, want 1 — "+
				"the gate must record the lost row on the production contention counter",
			contended,
		)
	}

	t.Logf(
		"contended-loss case: batches=%d batch_size_sum=1 contended_rows=1 statements=%d (scenario=%s)",
		batches, exec.totalStatements(), budget.Scenario,
	)
}

// TestCostBudget_KubernetesWorkloadNodeMaterialization_N1_ExceedsBudget is the
// mandatory negative control, run through the SAME production gated dispatch
// as the positive test. It calls the gated WriteKubernetesWorkloadNodes once
// per fixture row instead of once for the whole batch and asserts the
// accumulated eshu_dp_neo4j_batches_executed_total EXCEEDS the committed
// budget.
func TestCostBudget_KubernetesWorkloadNodeMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, kubernetesWorkloadNodeBudgetRelPath)
	rows := kubernetesWorkloadNodeFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	gated, _, reader := newGatedInstrumentedKubernetesWorkloadWriter(t, kubernetesWorkloadOwnedAllWinners())

	for _, row := range rows {
		if err := gated.WriteKubernetesWorkloadNodes(
			context.Background(), []map[string]any{row}, kubernetesWorkloadNodeCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteKubernetesWorkloadNodes() error = %v", err)
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

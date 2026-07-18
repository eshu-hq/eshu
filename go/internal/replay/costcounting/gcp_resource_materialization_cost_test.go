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

// gcpResourceMaterializationBudgetRelPath is the committed cost budget for the
// gcp_resource_materialization scenario (fact-kind-registry family gcp,
// specs/fact-kind-registry.v1.yaml:154-172, reducer_domain
// gcp_resource_materialization). Like ec2_instance_node_materialization, this
// projection writes through the SHARED cypher.CloudResourceNodeWriter over
// flat map[string]any rows, not a CanonicalMaterialization, so the fixture
// rows live inline in this file and the budget records that explicitly
// instead of pointing at a cassette path.
var gcpResourceMaterializationBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "gcp-resource-materialization.cost-budget.json",
)

const gcpResourceCostEvidenceSource = "reducer/gcp-resources"

// GCP fixture source-order keys, mirroring the EC2/Azure scenarios: each row's
// source_order_key is what the production row builder stamps
// (go/internal/reducer/gcp_resource_materialization.go gcpCloudResourceNodeRow,
// sourceOrderKeyField / go/internal/reducer/source_order_key.go).
const (
	gcpOrderKeyRowA          = "2026-07-01T00:00:00.000000000Z|gcp-fact-a"
	gcpOrderKeyRowB          = "2026-07-01T00:00:00.000000000Z|gcp-fact-b"
	gcpOrderKeyForeignWinner = "2026-07-02T00:00:00.000000000Z|gcp-fact-foreign"
)

// gcpResourceFixtureRows is the deterministic input for the positive,
// contended-loss, and N+1 scenarios: two GCP CloudResource rows in one
// project/location, shaped EXACTLY like the current production
// gcpCloudResourceNodeRow row contract
// (go/internal/reducer/gcp_resource_materialization.go:292-359), INCLUDING the
// 7 explicit empty-value parity keys (workload_id, service_name,
// service_anchor_status, service_anchor_source, service_anchor_reason,
// service_anchor_names, service_anchor_name_tokens) issue #4995 requires as
// PRESENT (not omitted) keys because the pinned NornicDB backend does not
// evaluate a missing UNWIND row map key as null in an unconditional SET
// clause. cypher.CloudResourceNodeWriter has one Cypher template and no
// per-row batching split, so any two distinct rows are sufficient to prove
// the N+1 control.
func gcpResourceFixtureRows() []map[string]any {
	row := func(id, orderKey string) map[string]any {
		return map[string]any{
			"uid":                 "gcp-uid-" + id,
			"arn":                 "",
			"resource_id":         "//compute.googleapis.com/projects/eshu-demo/zones/us-central1-a/instances/vm-" + id,
			"resource_type":       "compute.googleapis.com/Instance",
			"name":                "vm-" + id,
			"state":               "RUNNING",
			"account_id":          "eshu-demo",
			"region":              "us-central1-a",
			"service_kind":        "compute",
			"correlation_anchors": []string{"vm-" + id},
			"source_fact_id":      "gcp-fact-" + id,
			"stable_fact_key":     "gcp-key-" + id,
			"source_system":       "gcp",
			"source_record_id":    "gcp-rec-" + id,
			"source_confidence":   "reported",
			"collector_kind":      "gcpcloud",
			"source_order_key":    orderKey,
			// Explicit empty-value parity fields for
			// canonicalCloudResourceUpsertCypher's unconditional SET clause
			// (issue #4995) — GCP has no service-anchor decision source
			// today, mirroring gcpCloudResourceNodeRow's no-anchor values.
			"workload_id":                "",
			"service_name":               "",
			"service_anchor_status":      "",
			"service_anchor_source":      "",
			"service_anchor_reason":      "",
			"service_anchor_names":       []string{},
			"service_anchor_name_tokens": "",
		}
	}
	return []map[string]any{row("a", gcpOrderKeyRowA), row("b", gcpOrderKeyRowB)}
}

// gcpFakeOwnerRows is a postgres.Rows yielding (uid, source_order_key) winner
// pairs for the owner ledger's winners read-back query. Local to this file
// per the C-14 executor split (cost_scenario_helpers_test.go is
// orchestrator-owned); it mirrors ec2FakeOwnerRows exactly.
type gcpFakeOwnerRows struct {
	pairs [][2]string
	idx   int
}

func (r *gcpFakeOwnerRows) Next() bool { r.idx++; return r.idx <= len(r.pairs) }

func (r *gcpFakeOwnerRows) Scan(dest ...any) error {
	pair := r.pairs[r.idx-1]
	*(dest[0].(*string)) = pair[0]
	*(dest[1].(*string)) = pair[1]
	return nil
}

func (r *gcpFakeOwnerRows) Err() error   { return nil }
func (r *gcpFakeOwnerRows) Close() error { return nil }

// gcpFakeOwnerTx is a fake postgres.Transaction the REAL
// postgres.GraphNodeOwnerStore runs its REAL SQL against, mirroring
// ec2FakeOwnerTx: the advisory-lock acquisition and max-order-key ledger
// upsert land on ExecContext (both succeed; results discarded), and the
// winners read-back lands on QueryContext, answered from the configured
// winners map.
type gcpFakeOwnerTx struct {
	winners    map[string]string
	committed  bool
	rolledBack bool
}

func (t *gcpFakeOwnerTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (t *gcpFakeOwnerTx) QueryContext(_ context.Context, _ string, args ...any) (postgres.Rows, error) {
	uids, _ := args[0].([]string)
	pairs := make([][2]string, 0, len(uids))
	for _, uid := range uids {
		if win, ok := t.winners[uid]; ok {
			pairs = append(pairs, [2]string{uid, win})
		}
	}
	return &gcpFakeOwnerRows{pairs: pairs}, nil
}

func (t *gcpFakeOwnerTx) Commit() error   { t.committed = true; return nil }
func (t *gcpFakeOwnerTx) Rollback() error { t.rolledBack = true; return nil }

// gcpFakeOwnerBeginner is a fake postgres.Beginner handing out gcpFakeOwnerTx
// transactions configured with the same winners map.
type gcpFakeOwnerBeginner struct {
	winners map[string]string
}

func (b *gcpFakeOwnerBeginner) Begin(context.Context) (postgres.Transaction, error) {
	return &gcpFakeOwnerTx{winners: b.winners}, nil
}

// newGatedInstrumentedGCPResourceWriter builds the PRODUCTION GCP write
// dispatch: graphowner.NewCloudResourceGatedWriter (the SAME gated writer
// type AWS/Azure/GCP CloudResource nodes share, family_writers.go
// familyCloudResource) over the raw cypher.CloudResourceNodeWriter's
// WriteCloudResourceNodes method, mirroring go/cmd/reducer/canonical_graph_writers.go:50/58
// exactly. As with EC2/Azure, the #5007 owner-ledger Gate chunks rows, opens
// one owner-ledger Postgres transaction per chunk against the REAL
// postgres.GraphNodeOwnerStore.ResolveOwnedUIDs (run against the fake
// transaction above), and FILTERS contended-lost rows before delegating to
// the raw writer. The raw writer sits over a groupCountingExecutor wrapped by
// the production cypher.InstrumentedExecutor, the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor. winners configures the fake owner ledger's
// read-back (uid -> winning order key).
func newGatedInstrumentedGCPResourceWriter(t *testing.T, winners map[string]string) (
	gated *graphowner.CloudResourceGatedWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	raw := cypher.NewCloudResourceNodeWriter(instrumented, 500)
	gate := graphowner.NewGate(&gcpFakeOwnerBeginner{winners: winners})
	gate.Instruments = inst
	gated = graphowner.NewCloudResourceGatedWriter(gate, raw.WriteCloudResourceNodes)
	return gated, exec, manualReader
}

// gcpOwnedAllWinners is the non-contended winners map: every fixture uid's
// winning order key equals the fixture row's own key, so this batch owns both
// uids and the gate delegates both rows to the raw writer unfiltered.
func gcpOwnedAllWinners() map[string]string {
	return map[string]string{
		"gcp-uid-a": gcpOrderKeyRowA,
		"gcp-uid-b": gcpOrderKeyRowB,
	}
}

// TestCostBudget_GCPResourceMaterialization is the positive cost-counting
// gate for the gcp_resource_materialization reducer projection (the gcp
// family in specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production GATED dispatch — graphowner.CloudResourceGatedWriter.WriteCloudResourceNodes
// over the raw cypher.CloudResourceNodeWriter, per canonical_graph_writers.go:50/58 —
// over two deterministic rows in the owned-all (non-contended) case, through a
// real InstrumentedExecutor-backed sdkmetric.ManualReader, then asserts
// eshu_dp_neo4j_batches_executed_total is within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total, recorded once per
// UNWIND-shaped statement passed through Execute or ExecuteGroup. Both
// fixture rows fit in one gate chunk (lockChunkSize = 500) and one raw-writer
// batch (default 500), so the FULL gated dispatch emits exactly one batch. The
// owned-all case also pins eshu_dp_cross_scope_ownership_contended_rows_total
// at 0.
func TestCostBudget_GCPResourceMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, gcpResourceMaterializationBudgetRelPath)
	gated, exec, reader := newGatedInstrumentedGCPResourceWriter(t, gcpOwnedAllWinners())

	if err := gated.WriteCloudResourceNodes(
		context.Background(), gcpResourceFixtureRows(), gcpResourceCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteCloudResourceNodes() error = %v", err)
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

// TestCostBudget_GCPResourceMaterialization_ContendedLossFiltersRow proves the
// gated dispatch's contended path keeps the same batch cost while filtering
// the lost row: the fake owner ledger reports uid-b already owned by a
// strictly-higher-order-key contributor from another scope, so Gate.write's
// filterOwnedRows drops that row and the raw writer receives ONE row.
func TestCostBudget_GCPResourceMaterialization_ContendedLossFiltersRow(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, gcpResourceMaterializationBudgetRelPath)
	winners := map[string]string{
		"gcp-uid-a": gcpOrderKeyRowA,          // owned: winner is this batch's own key
		"gcp-uid-b": gcpOrderKeyForeignWinner, // lost: a higher-order-key contributor won
	}
	gated, exec, reader := newGatedInstrumentedGCPResourceWriter(t, winners)

	if err := gated.WriteCloudResourceNodes(
		context.Background(), gcpResourceFixtureRows(), gcpResourceCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteCloudResourceNodes() error = %v", err)
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

// TestCostBudget_GCPResourceMaterialization_N1_ExceedsBudget is the mandatory
// negative control, run through the SAME production gated dispatch as the
// positive test. It calls the gated WriteCloudResourceNodes once per fixture
// row instead of once for the whole batch and asserts the accumulated
// eshu_dp_neo4j_batches_executed_total EXCEEDS the committed budget.
func TestCostBudget_GCPResourceMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, gcpResourceMaterializationBudgetRelPath)
	rows := gcpResourceFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	gated, _, reader := newGatedInstrumentedGCPResourceWriter(t, gcpOwnedAllWinners())

	for _, row := range rows {
		if err := gated.WriteCloudResourceNodes(
			context.Background(), []map[string]any{row}, gcpResourceCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteCloudResourceNodes() error = %v", err)
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

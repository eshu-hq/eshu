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

// ec2InstanceNodeBudgetRelPath is the committed cost budget for the
// ec2_instance_node_materialization scenario (fact-kind-registry family
// ec2_instance_posture, specs/fact-kind-registry.v1.yaml:140-149). Like the
// semantic-entity and documentation-edges scenarios, this projection has no
// committed cassette: the gated writer operates over flat map[string]any
// rows, not a CanonicalMaterialization, so the fixture rows live inline in
// this file and the budget records that explicitly instead of pointing at a
// cassette path.
var ec2InstanceNodeBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "ec2-instance-node-materialization.cost-budget.json",
)

const ec2InstanceNodeCostEvidenceSource = "reducer/ec2-instances"

// EC2 fixture source-order keys. The #5007 owner-ledger gate reads each row's
// source_order_key to resolve ownership, exactly as the production reducer
// row builder stamps it (go/internal/reducer/ec2_instance_node_rows.go,
// sourceOrderKeyField / go/internal/reducer/source_order_key.go).
const (
	ec2OrderKeyRowA = "2026-07-01T00:00:00.000000000Z|fact-a"
	ec2OrderKeyRowB = "2026-07-01T00:00:00.000000000Z|fact-b"
	// ec2OrderKeyForeignWinner is strictly greater than ec2OrderKeyRowB, so a
	// winners response carrying it for uid-b means a higher-order-key
	// contributor from another scope already owns that node — this batch LOSES
	// uid-b and the gate must filter that row out before the graph write.
	ec2OrderKeyForeignWinner = "2026-07-02T00:00:00.000000000Z|fact-foreign"
)

// ec2InstanceNodeFixtureRows is the deterministic input for the positive,
// contended-loss, and N+1 scenarios: two EC2 instance rows in one
// account/region, shaped like the production WriteEC2InstanceNodes row
// contract (go/internal/storage/cypher/ec2_instance_node_writer.go
// canonicalEC2InstanceUpsertCypher's SET clause, plus the #5007
// source_order_key the reducer row builder stamps). EC2InstanceNodeWriter has
// no per-label batching split — every row shares the same single UNWIND
// template regardless of content — so any two distinct rows are sufficient to
// prove the N+1 control.
func ec2InstanceNodeFixtureRows() []map[string]any {
	row := func(id, orderKey string) map[string]any {
		return map[string]any{
			"uid":                         "ec2-uid-" + id,
			"arn":                         "arn:aws:ec2:us-east-1:111122223333:instance/i-" + id,
			"resource_id":                 "i-" + id,
			"resource_type":               "aws_ec2_instance",
			"name":                        "i-" + id,
			"state":                       "running",
			"account_id":                  "111122223333",
			"region":                      "us-east-1",
			"service_kind":                "ec2",
			"correlation_anchors":         []string{"i-" + id},
			"imds_v2_required":            true,
			"imds_http_endpoint":          "enabled",
			"imds_http_put_hop_limit":     int32(1),
			"user_data_present":           false,
			"detailed_monitoring_enabled": false,
			"ebs_optimized":               true,
			"public_ip_associated":        true,
			"instance_profile_arn":        "arn:aws:iam::111122223333:instance-profile/app",
			"tenancy":                     "default",
			"nitro_enclave_enabled":       false,
			"source_fact_id":              "fact-" + id,
			"stable_fact_key":             "key-" + id,
			"source_system":               "aws",
			"source_record_id":            "rec-" + id,
			"source_confidence":           "reported",
			"collector_kind":              "awscloud",
			"source_order_key":            orderKey,
		}
	}
	return []map[string]any{row("a", ec2OrderKeyRowA), row("b", ec2OrderKeyRowB)}
}

// ec2FakeOwnerRows is a postgres.Rows yielding (uid, source_order_key) winner
// pairs for the owner ledger's winners read-back query.
type ec2FakeOwnerRows struct {
	pairs [][2]string
	idx   int
}

func (r *ec2FakeOwnerRows) Next() bool { r.idx++; return r.idx <= len(r.pairs) }

func (r *ec2FakeOwnerRows) Scan(dest ...any) error {
	pair := r.pairs[r.idx-1]
	*(dest[0].(*string)) = pair[0]
	*(dest[1].(*string)) = pair[1]
	return nil
}

func (r *ec2FakeOwnerRows) Err() error   { return nil }
func (r *ec2FakeOwnerRows) Close() error { return nil }

// ec2FakeOwnerTx is a fake postgres.Transaction the REAL
// postgres.GraphNodeOwnerStore runs its REAL SQL against: the advisory-lock
// acquisition and the max-order-key ledger upsert land on ExecContext (both
// succeed; results are discarded by the store), and the winners read-back
// lands on QueryContext, answered from the configured winners map (uid ->
// winning source_order_key). This is the credential-free stand-in for the
// ledger's Postgres ops; the ledger op count per chunk is deterministic (one
// lock exec + one upsert exec + one winners query), so faking the transaction
// changes no counted graph cost, only removes the Postgres dependency.
type ec2FakeOwnerTx struct {
	winners    map[string]string
	committed  bool
	rolledBack bool
}

func (t *ec2FakeOwnerTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (t *ec2FakeOwnerTx) QueryContext(_ context.Context, _ string, args ...any) (postgres.Rows, error) {
	uids, _ := args[0].([]string)
	pairs := make([][2]string, 0, len(uids))
	for _, uid := range uids {
		if win, ok := t.winners[uid]; ok {
			pairs = append(pairs, [2]string{uid, win})
		}
	}
	return &ec2FakeOwnerRows{pairs: pairs}, nil
}

func (t *ec2FakeOwnerTx) Commit() error   { t.committed = true; return nil }
func (t *ec2FakeOwnerTx) Rollback() error { t.rolledBack = true; return nil }

// ec2FakeOwnerBeginner is a fake postgres.Beginner handing out ec2FakeOwnerTx
// transactions configured with the same winners map. It satisfies the Gate's
// db seam (graphowner.NewGate takes a postgres.Beginner), mirroring the
// fakeChunkBeginner seam go/internal/graphowner/gated_writer_chunk_test.go
// established.
type ec2FakeOwnerBeginner struct {
	winners map[string]string
}

func (b *ec2FakeOwnerBeginner) Begin(context.Context) (postgres.Transaction, error) {
	return &ec2FakeOwnerTx{winners: b.winners}, nil
}

// newGatedInstrumentedEC2Writer builds the PRODUCTION EC2 write dispatch:
// graphowner.NewEC2InstanceGatedWriter over the raw
// cypher.EC2InstanceNodeWriter's WriteEC2InstanceNodes method, mirroring
// go/cmd/reducer/canonical_graph_writers.go:51/59 exactly — including
// gate.Instruments (set by go/cmd/reducer/main.go:102 in production). The
// #5007 owner-ledger Gate is NOT a pass-through wrapper: Gate.write
// (go/internal/graphowner/gated_writer.go:110-152) chunks rows by
// lockChunkSize, opens one owner-ledger Postgres transaction per chunk, calls
// the REAL postgres.GraphNodeOwnerStore.ResolveOwnedUIDs (real
// lock/upsert/winners SQL, run against the fake transaction above), and
// FILTERS contended-lost rows before delegating to the raw writer — so this
// scenario drives that full gated dispatch rather than assuming it away. The
// raw writer sits over a groupCountingExecutor wrapped by the production
// cypher.InstrumentedExecutor — the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec. InstrumentedExecutor records
// eshu_dp_neo4j_batches_executed_total on every UNWIND-shaped statement — the
// PRIMARY instrument this scenario asserts, not a hand-counted statement
// slice. winners configures the fake owner ledger's read-back (uid -> winning
// order key).
func newGatedInstrumentedEC2Writer(t *testing.T, winners map[string]string) (
	gated *graphowner.EC2InstanceGatedWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	raw := cypher.NewEC2InstanceNodeWriter(instrumented, 500)
	gate := graphowner.NewGate(&ec2FakeOwnerBeginner{winners: winners})
	gate.Instruments = inst
	gated = graphowner.NewEC2InstanceGatedWriter(gate, raw.WriteEC2InstanceNodes)
	return gated, exec, manualReader
}

// ec2OwnedAllWinners is the non-contended winners map: every fixture uid's
// winning order key equals the fixture row's own key, so this batch owns both
// uids and the gate delegates both rows to the raw writer unfiltered.
func ec2OwnedAllWinners() map[string]string {
	return map[string]string{
		"ec2-uid-a": ec2OrderKeyRowA,
		"ec2-uid-b": ec2OrderKeyRowB,
	}
}

// collectFloat64HistogramSum reads one named Float64 histogram's total Sum off
// a Collect snapshot. Used to assert the row-payload size the raw writer's
// UNWIND batch carried (eshu_dp_neo4j_batch_size records float64(rowCount)
// once per UNWIND statement in
// InstrumentedExecutor.recordStatementBatchMetrics).
func collectFloat64HistogramSum(rm metricdata.ResourceMetrics, name string) float64 {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				return 0
			}
			var total float64
			for _, dp := range hist.DataPoints {
				total += dp.Sum
			}
			return total
		}
	}
	return 0
}

// TestCostBudget_EC2InstanceNodeMaterialization is the positive cost-counting
// gate for the ec2_instance_node_materialization reducer projection (the
// ec2_instance_posture family in specs/fact-kind-registry.v1.yaml, C-14 issue
// #4367). It drives the production GATED dispatch —
// graphowner.EC2InstanceGatedWriter.WriteEC2InstanceNodes over the raw
// cypher.EC2InstanceNodeWriter, per canonical_graph_writers.go:51/59 — over
// two deterministic rows in the owned-all (non-contended) case, through a
// real InstrumentedExecutor-backed sdkmetric.ManualReader, then asserts
// eshu_dp_neo4j_batches_executed_total is within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total. InstrumentedExecutor
// records this once per UNWIND-shaped statement (a statement whose Parameters
// carry a "rows" key) passed through Execute or ExecuteGroup
// (recordStatementBatchMetrics). Both fixture rows fit in one gate chunk
// (lockChunkSize = 500) and one raw-writer batch (default 500), so the FULL
// gated dispatch — chunking, one ledger transaction, ownership resolution,
// owned-row filtering, then the raw writer's single UNWIND — emits exactly
// one batch, proven through production dispatch rather than assumed. Any
// increase — an N+1 write cycle, an extra chunk split, or an extra batch
// split — trips the gate. The owned-all case also pins
// eshu_dp_cross_scope_ownership_contended_rows_total at 0 (the gate's #5007
// contention counter records nothing when the batch owns everything).
func TestCostBudget_EC2InstanceNodeMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, ec2InstanceNodeBudgetRelPath)
	gated, exec, reader := newGatedInstrumentedEC2Writer(t, ec2OwnedAllWinners())

	if err := gated.WriteEC2InstanceNodes(
		context.Background(), ec2InstanceNodeFixtureRows(), ec2InstanceNodeCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteEC2InstanceNodes() error = %v", err)
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

	// Owned-all means zero cross-scope contention: the gate's #5007 counter
	// (recorded via gate.Instruments, the same field main.go:102 wires) must
	// stay silent.
	if contended := collectCounter(rm, "eshu_dp_cross_scope_ownership_contended_rows_total"); contended != 0 {
		t.Fatalf("eshu_dp_cross_scope_ownership_contended_rows_total = %d, want 0 in the owned-all case", contended)
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

// TestCostBudget_EC2InstanceNodeMaterialization_ContendedLossFiltersRow
// proves the gated dispatch's contended path keeps the same batch cost while
// filtering the lost row: the fake owner ledger reports uid-b already owned
// by a strictly-higher-order-key contributor from another scope, so
// Gate.write's filterOwnedRows drops that row and the raw writer receives ONE
// row. Assertions, all off real production instruments on the real otel
// reader:
//
//   - eshu_dp_neo4j_batches_executed_total stays 1 (one UNWIND batch — losing
//     a row must not change the batch count);
//   - eshu_dp_neo4j_batch_size Sum is 1 (the surviving batch carried exactly
//     the ONE owned row, not both fixture rows);
//   - eshu_dp_cross_scope_ownership_contended_rows_total is 1 (the gate's
//     recordCrossScopeContention recorded the lost row on the SAME
//     instruments registry production wires via main.go:102).
func TestCostBudget_EC2InstanceNodeMaterialization_ContendedLossFiltersRow(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, ec2InstanceNodeBudgetRelPath)
	winners := map[string]string{
		"ec2-uid-a": ec2OrderKeyRowA,          // owned: winner is this batch's own key
		"ec2-uid-b": ec2OrderKeyForeignWinner, // lost: a higher-order-key contributor won
	}
	gated, exec, reader := newGatedInstrumentedEC2Writer(t, winners)

	if err := gated.WriteEC2InstanceNodes(
		context.Background(), ec2InstanceNodeFixtureRows(), ec2InstanceNodeCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteEC2InstanceNodes() error = %v", err)
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

// TestCostBudget_EC2InstanceNodeMaterialization_N1_ExceedsBudget is the
// mandatory negative control, run through the SAME production gated dispatch
// as the positive test. It calls the gated WriteEC2InstanceNodes once per
// fixture row instead of once for the whole batch — the classic N+1
// anti-pattern — and asserts the accumulated
// eshu_dp_neo4j_batches_executed_total EXCEEDS the committed budget. This is
// a REAL negative control: it drives the SAME gated production writer,
// through the SAME InstrumentedExecutor wrapper, reading the SAME instrument
// off the SAME real otel reader as the positive test.
func TestCostBudget_EC2InstanceNodeMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, ec2InstanceNodeBudgetRelPath)
	rows := ec2InstanceNodeFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	gated, _, reader := newGatedInstrumentedEC2Writer(t, ec2OwnedAllWinners())

	for _, row := range rows {
		if err := gated.WriteEC2InstanceNodes(
			context.Background(), []map[string]any{row}, ec2InstanceNodeCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteEC2InstanceNodes() error = %v", err)
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

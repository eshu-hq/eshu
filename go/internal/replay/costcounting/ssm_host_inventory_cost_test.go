// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	ssm "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/ssm"
)

// ssmHostInventoryBudgetRelPath is the committed cost budget for the
// ssm_host_inventory_materialization scenario (aws_ssm_host family,
// specs/fact-kind-registry.v1.yaml). Like the EC2 scenario, this projection
// has no committed cassette: HostNodeWriter operates over flat
// map[string]any rows, not a CanonicalMaterialization, so the fixture rows
// live inline in this file and the budget records that explicitly instead of
// pointing at a cassette path.
const ssmHostInventoryBudgetRelPath = "../../../../testdata/cassettes/replayoffline/ssm-host-inventory-materialization.cost-budget.json"

const ssmHostInventoryCostEvidenceSource = "reducer/ssm-host-inventory"

// ssmCostHost is one deterministic host's observation triple: one
// application row, one platform row, and the REPORTED_WITH pairing edge
// between them, shaped like the production HostNodeWriter row contract
// (go/internal/storage/cypher/cloud/aws/ssm/host_writer.go:
// capture_time/observation_time RFC3339 inputs, from_uid/to_uid edge
// endpoints, closed-vocabulary relationship).
type ssmCostHost struct {
	apps      []map[string]any
	platforms []map[string]any
	edges     []map[string]any
}

func ssmCostHostFixture(instance, appUID, platformUID string) ssmCostHost {
	return ssmCostHost{
		apps: []map[string]any{{
			"uid": appUID, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
			"instance_id": instance, "resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application",
			"schema_version": "1.0.0", "capture_time": "2026-10-29T22:31:19Z", "name": "alpha",
			"source_fact_id": "a-" + instance, "source_order_key": "k-" + instance,
		}},
		platforms: []map[string]any{{
			"uid": platformUID, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
			"instance_id": instance, "resource_type": "AWS::EC2::Instance", "source_type": "ssm_inventory",
			"platform_type": "Linux", "observation_time": "2026-10-29T22:33:02Z",
			"source_fact_id": "p-" + instance, "source_order_key": "k-" + instance,
		}},
		edges: []map[string]any{{
			"from_uid": appUID, "to_uid": platformUID, "relationship": "REPORTED_WITH",
		}},
	}
}

// ssmCostHosts is the deterministic two-host fixture for the positive and
// N+1 scenarios. Two hosts prove the N+1 control: one batched call emits one
// UNWIND per node/edge kind, while per-host calls multiply that by the host
// count.
func ssmCostHosts() []ssmCostHost {
	return []ssmCostHost{
		ssmCostHostFixture("i-1", "ssm-cost-app-i-1", "ssm-cost-platform-i-1"),
		ssmCostHostFixture("i-2", "ssm-cost-app-i-2", "ssm-cost-platform-i-2"),
	}
}

// newInstrumentedSSMHostWriter builds the PRODUCTION SSM host write dispatch:
// ssm.HostNodeWriter.WriteHostObservationBatch over a groupCountingExecutor
// wrapped by the production cypher.InstrumentedExecutor — the same wrapper
// go/cmd/reducer applies to the graph executor — mirroring
// go/cmd/reducer/canonical_graph_writers.go (ssm.NewHostNodeWriter) as driven
// through the ssmHostGraphWriterAdapter.WriteHostNodes adapter in
// go/cmd/reducer/main.go. A nil posture reader matches the write path: the
// reader backs ReadCommittedHosts/retract planning, never the batch write.
// InstrumentedExecutor records eshu_dp_neo4j_batches_executed_total on every
// UNWIND-shaped statement — the PRIMARY instrument this scenario asserts,
// not a hand-counted statement slice.
func newInstrumentedSSMHostWriter(t *testing.T) (*ssm.HostNodeWriter, *groupCountingExecutor, *sdkmetric.ManualReader) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec := &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	return ssm.NewHostNodeWriter(instrumented, nil, 500), exec, manualReader
}

// ssmCostBatch merges every fixture host into one adapter-shaped batch call,
// the production fan-in: the reducer materialization emits one HostGraphWrite
// per host and the adapter forwards each host's triple in a single
// WriteHostObservationBatch. Batching all hosts here is the cost-optimal
// pole: each node/edge kind dispatches exactly one UNWIND.
func ssmCostBatch(hosts []ssmCostHost) (apps, platforms, edges []map[string]any) {
	for _, h := range hosts {
		apps = append(apps, h.apps...)
		platforms = append(platforms, h.platforms...)
		edges = append(edges, h.edges...)
	}
	return apps, platforms, edges
}

// TestCostBudget_SSMHostInventoryMaterialization is the positive cost-counting
// gate for the ssm_host_inventory_materialization reducer projection (the
// aws_ssm_host family in specs/fact-kind-registry.v1.yaml, C-14 follow-up to
// #4367 for eshu-hq/eshu#9558). It drives the production batch dispatch —
// HostNodeWriter.WriteHostObservationBatch over two deterministic hosts —
// through a real InstrumentedExecutor-backed sdkmetric.ManualReader, then
// asserts eshu_dp_neo4j_batches_executed_total is within the committed
// budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total. Both hosts' rows fit
// in one writer batch (batch size 500), and WriteHostObservationBatch fans
// out to exactly three kind dispatches — application nodes, platform nodes,
// REPORTED_WITH edges, node-then-edge — so the FULL production dispatch
// emits exactly three batches, proven through production dispatch rather than
// assumed. Any increase — an N+1 write cycle, an extra batch split, or a new
// kind dispatch — trips the gate.
func TestCostBudget_SSMHostInventoryMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, ssmHostInventoryBudgetRelPath)
	writer, exec, reader := newInstrumentedSSMHostWriter(t)

	apps, platforms, edges := ssmCostBatch(ssmCostHosts())
	if err := writer.WriteHostObservationBatch(
		context.Background(), apps, platforms, edges,
		"scope-cost", "gen-cost", ssmHostInventoryCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteHostObservationBatch() error = %v", err)
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

// TestCostBudget_SSMHostInventoryMaterialization_N1_ExceedsBudget is the
// mandatory negative control, run through the SAME production batch dispatch
// as the positive test. It calls WriteHostObservationBatch once per fixture
// host instead of once for the whole batch — the classic N+1 anti-pattern —
// and asserts the accumulated eshu_dp_neo4j_batches_executed_total EXCEEDS
// the committed budget. This is a REAL negative control: it drives the SAME
// production writer, through the SAME InstrumentedExecutor wrapper, reading
// the SAME instrument off the SAME real otel reader as the positive test.
func TestCostBudget_SSMHostInventoryMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, ssmHostInventoryBudgetRelPath)
	hosts := ssmCostHosts()
	if len(hosts) < 2 {
		t.Fatalf("N+1 control needs >=2 hosts to exceed the budget; fixture has %d", len(hosts))
	}

	writer, _, reader := newInstrumentedSSMHostWriter(t)

	for _, h := range hosts {
		if err := writer.WriteHostObservationBatch(
			context.Background(), h.apps, h.platforms, h.edges,
			"scope-cost", "gen-cost", ssmHostInventoryCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteHostObservationBatch() error = %v", err)
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
		"N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = %d > budget %d (N=%d hosts, scenario=%s)",
		batches, maxBatches, len(hosts), budget.Scenario,
	)
}

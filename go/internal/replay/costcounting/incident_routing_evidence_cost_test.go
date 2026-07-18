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

// incidentRoutingEvidenceBudgetRelPath is the committed cost budget for the
// incident_routing_materialization scenario (fact-kind-registry family
// incident_routing, specs/fact-kind-registry.v1.yaml:186-200, reducer_domain
// incident_routing_materialization). Like ec2_instance_node_materialization,
// this projection writes through cypher.IncidentRoutingEvidenceWriter over
// flat map[string]any rows, not a CanonicalMaterialization, so the fixture
// rows live inline in this file and the budget records that explicitly
// instead of pointing at a cassette path.
var incidentRoutingEvidenceBudgetRelPath = filepath.Join(
	"..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "incident-routing-evidence-materialization.cost-budget.json",
)

const (
	incidentRoutingEvidenceCostEvidenceSource = "reducer/incident-routing"
	incidentRoutingEvidenceCostScopeID        = "scope-incident-routing-a"
	incidentRoutingEvidenceCostGenerationID   = "generation-1"
)

// incidentRoutingEvidenceFixtureRows is the deterministic input for the
// positive and N+1 scenarios: two "applied_routing" evidence rows from two
// DISTINCT incidents in one scope, shaped like the current production row
// contract for the applied slot
// (go/internal/reducer/incident_routing_evidence_rows.go
// incidentRoutingBaseRow lines 213-241 plus incidentRoutingAppliedDecision's
// extra map, lines 161-173): incident_uid, uid (the routing-evidence node
// identity), slot, source_class, truth_label, provider, provider_incident_id,
// service_id, service_url, service_name_hash, evidence_kind, evidence_id,
// incident_fact_id, plus the applied-slot extras (source_kind, resource_class,
// provider_object_id, terraform_state_address, provider_address,
// module_address, state_generation_id, declared_match_state,
// redaction_state). repo_id/relative_path (intended-slot-only extras) are
// included as explicit empty-string parity fields since
// canonicalIncidentRoutingEvidenceUpsertCypherFormat's routing-node SET
// clause reads row.repo_id/row.relative_path unconditionally for every slot.
//
// IncidentRoutingEvidenceWriter is a DIRECT writer (canonical_graph_writers.go:71,
// constructed unwrapped — no #5007 owner-ledger gate applies to evidence
// nodes) that groups rows by routing slot into a separate Cypher statement
// per slot (the relationship type is the slot-derived static token). Sharing
// slot "applied_routing" across both fixture rows is required for the N+1
// control to be meaningful: two rows with DIFFERENT slots would already emit
// two statements regardless of call count.
func incidentRoutingEvidenceFixtureRows() []map[string]any {
	row := func(id string) map[string]any {
		return map[string]any{
			"uid":                     "incident-routing-uid-" + id,
			"incident_uid":            "incident-uid-" + id,
			"slot":                    "applied_routing",
			"source_class":            "applied",
			"truth_label":             "exact",
			"provider":                "pagerduty",
			"provider_incident_id":    "PD-" + id,
			"service_id":              "PDSERVICE" + id,
			"service_url":             "https://eshu-demo.pagerduty.com/services/PDSERVICE" + id,
			"service_name_hash":       "hash-" + id,
			"evidence_kind":           "incident_routing.applied_pagerduty_resource",
			"evidence_id":             "applied-fact-" + id,
			"incident_fact_id":        "incident-fact-" + id,
			"source_kind":             "terraform_state",
			"resource_class":          "pagerduty_service",
			"provider_object_id":      "PDSERVICE" + id,
			"terraform_state_address": "pagerduty_service.web_" + id,
			"provider_address":        "pagerduty_service.web_" + id,
			"module_address":          "",
			"state_generation_id":     "state-gen-" + id,
			"declared_match_state":    "matched",
			"redaction_state":         "none",
			// intended-slot-only extras, present as empty-string parity
			// fields since the routing-node SET clause reads them for every
			// row regardless of slot.
			"repo_id":       "",
			"relative_path": "",
		}
	}
	return []map[string]any{row("a"), row("b")}
}

// newInstrumentedIncidentRoutingEvidenceWriter builds the PRODUCTION
// incident-routing write dispatch: the raw
// cypher.IncidentRoutingEvidenceWriter (constructed unwrapped at
// go/cmd/reducer/canonical_graph_writers.go:71 — no graphowner gate wraps
// this evidence writer) over a groupCountingExecutor wrapped by the
// production cypher.InstrumentedExecutor, the same wrapper
// go/cmd/reducer/observed_service_wiring.go buildObservedReducerService
// applies to neo4jExecutor before threading it into buildReducerService as
// neo4jExec. InstrumentedExecutor records eshu_dp_neo4j_batches_executed_total
// on every UNWIND-shaped statement — the PRIMARY instrument this scenario
// asserts, not a hand-counted statement slice.
func newInstrumentedIncidentRoutingEvidenceWriter(t *testing.T) (
	writer *cypher.IncidentRoutingEvidenceWriter,
	exec *groupCountingExecutor,
	reader *sdkmetric.ManualReader,
) {
	t.Helper()

	inst, manualReader := newManualReaderInstruments(t)
	exec = &groupCountingExecutor{}
	instrumented := &cypher.InstrumentedExecutor{Inner: exec, Instruments: inst}
	writer = cypher.NewIncidentRoutingEvidenceWriter(instrumented, 500)
	return writer, exec, manualReader
}

// TestCostBudget_IncidentRoutingEvidenceMaterialization is the positive
// cost-counting gate for the incident_routing_materialization reducer
// projection (the incident_routing family in
// specs/fact-kind-registry.v1.yaml, C-14 issue #4367). It drives the
// production DIRECT dispatch —
// cypher.IncidentRoutingEvidenceWriter.WriteIncidentRoutingEvidence,
// unwrapped by any owner-ledger gate — over two deterministic same-slot rows
// from distinct incidents in one scope, through a real
// InstrumentedExecutor-backed sdkmetric.ManualReader, then asserts
// eshu_dp_neo4j_batches_executed_total is within the committed budget.
//
// Instrument read: eshu_dp_neo4j_batches_executed_total. InstrumentedExecutor
// records this once per UNWIND-shaped statement (a statement whose Parameters
// carry a "rows" key) passed through Execute or ExecuteGroup. Both fixture
// rows share slot "applied_routing", so they group into ONE Cypher statement
// (the shared MERGE incident + MERGE routing + MERGE edge template with one
// slot-derived relationship type), which fits in one raw-writer batch
// (default 500) — the writer emits exactly one batch despite writing TWO node
// pairs and TWO edges (mixed node+edge MERGE per UNWIND row, still one
// statement). Any increase — an N+1 write cycle, an extra slot-group split, or
// an extra batch split — trips the gate.
func TestCostBudget_IncidentRoutingEvidenceMaterialization(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, incidentRoutingEvidenceBudgetRelPath)
	writer, exec, reader := newInstrumentedIncidentRoutingEvidenceWriter(t)

	if err := writer.WriteIncidentRoutingEvidence(
		context.Background(),
		incidentRoutingEvidenceFixtureRows(),
		incidentRoutingEvidenceCostScopeID,
		incidentRoutingEvidenceCostGenerationID,
		incidentRoutingEvidenceCostEvidenceSource,
	); err != nil {
		t.Fatalf("WriteIncidentRoutingEvidence() error = %v", err)
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

// TestCostBudget_IncidentRoutingEvidenceMaterialization_N1_ExceedsBudget is
// the mandatory negative control, run through the SAME production writer as
// the positive test. It calls WriteIncidentRoutingEvidence once per fixture
// row (both still sharing slot "applied_routing", so the batching key is
// unchanged) instead of once for the whole batch — the classic N+1
// anti-pattern — and asserts the accumulated
// eshu_dp_neo4j_batches_executed_total EXCEEDS the committed budget.
func TestCostBudget_IncidentRoutingEvidenceMaterialization_N1_ExceedsBudget(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, incidentRoutingEvidenceBudgetRelPath)
	rows := incidentRoutingEvidenceFixtureRows()
	if len(rows) < 2 {
		t.Fatalf("N+1 control needs >=2 rows to exceed the budget; fixture has %d", len(rows))
	}

	writer, _, reader := newInstrumentedIncidentRoutingEvidenceWriter(t)

	for _, row := range rows {
		if err := writer.WriteIncidentRoutingEvidence(
			context.Background(),
			[]map[string]any{row},
			incidentRoutingEvidenceCostScopeID,
			incidentRoutingEvidenceCostGenerationID,
			incidentRoutingEvidenceCostEvidenceSource,
		); err != nil {
			t.Fatalf("N+1 WriteIncidentRoutingEvidence() error = %v", err)
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

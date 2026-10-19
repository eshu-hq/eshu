// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8169 disposable PostgreSQL 18 proof that the batch workload_materialization ACK re-triggers the linked service catalog row once and records eshu_dp_service_catalog_runtime_retrigger_total and the re-trigger log line; a later ACK with nothing to re-trigger records outcome none. Local run about 2s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReducerContentionGateServiceCatalogRetriggerBatchAckAndTelemetry drives
// the batch ACK: two workload rows of the repository ack in one AckBatch, the
// linked catalog row is reopened once, and the counter and log line say so. A
// later single ACK finds the catalog row already pending and records none.
func TestReducerContentionGateServiceCatalogRetriggerBatchAckAndTelemetry(t *testing.T) {
	ctx := context.Background()
	p := newRetriggerProof(t, ctx)
	p.runCatalog(t, ctx, "first catalog run (no producer)")

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("retrigger-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	q := p.queue(p.db, "workload-worker", reducer.DomainWorkloadMaterialization)
	q.Instruments = instruments
	q.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	var intents []reducercontract.Intent
	for _, id := range []string{"wm-checkout-8169-a", "wm-checkout-8169-b", "wm-checkout-8169-c"} {
		p.addProducer(t, ctx, id)
		intent, claimed, err := q.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("Claim(%s) claimed=%v err=%v", id, claimed, err)
		}
		intents = append(intents, intent)
	}
	p.graph.committed.Store(true)
	if err := q.AckBatch(ctx, intents[:2], []reducercontract.Result{{}, {}}); err != nil {
		t.Fatalf("AckBatch() error = %v", err)
	}
	if status, _ := readRetriggerItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "pending" {
		t.Fatalf("catalog row after the batch ack = %s, want pending", status)
	}
	if !strings.Contains(logs.String(), `"reopened_count":1`) || !strings.Contains(logs.String(), `"producer_domain":"workload_materialization"`) {
		t.Fatalf("re-trigger log = %s, want reopened_count 1 for workload_materialization", logs.String())
	}
	if err := q.Ack(ctx, intents[2], reducercontract.Result{}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}

	got := retriggerCounter(t, reader)
	want := map[string]int64{ServiceCatalogRetriggerReopened: 1, ServiceCatalogRetriggerNone: 1}
	if len(got) != len(want) || got[ServiceCatalogRetriggerReopened] != 1 || got[ServiceCatalogRetriggerNone] != 1 {
		t.Fatalf("eshu_dp_service_catalog_runtime_retrigger_total by outcome = %v, want %v", got, want)
	}
}

// retriggerCounter collects the re-trigger counter by outcome and fails on a
// domain label other than workload_materialization.
func retriggerCounter(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_service_catalog_runtime_retrigger_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("retrigger counter data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				domain, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain))
				if domain.AsString() != string(reducer.DomainWorkloadMaterialization) {
					t.Fatalf("retrigger counter domain = %q, want workload_materialization", domain.AsString())
				}
				outcome, _ := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				out[outcome.AsString()] += dp.Value
			}
		}
	}
	return out
}

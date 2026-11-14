// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fakeBatchWorkerOccupancyObserver struct {
	snapshot []BatchWorkerOccupancy
	err      error
}

func (f *fakeBatchWorkerOccupancyObserver) BatchWorkerOccupancy(context.Context) ([]BatchWorkerOccupancy, error) {
	return f.snapshot, f.err
}

// TestRegisterReducerBatchWorkerOccupancyGauge_NilInputs rejects nil
// instruments and nil meters, and registers nothing for a nil observer, so
// lanes without a batch pool keep today's instruments byte-identical.
func TestRegisterReducerBatchWorkerOccupancyGauge_NilInputs(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	if err := RegisterReducerBatchWorkerOccupancyGauge(nil, meter, &fakeBatchWorkerOccupancyObserver{}); err == nil {
		t.Fatal("expected error for nil instruments")
	}
	if err := RegisterReducerBatchWorkerOccupancyGauge(inst, nil, &fakeBatchWorkerOccupancyObserver{}); err == nil {
		t.Fatal("expected error for nil meter")
	}
	if err := RegisterReducerBatchWorkerOccupancyGauge(inst, meter, nil); err != nil {
		t.Fatalf("nil observer error = %v, want nil", err)
	}
	if inst.ReducerBatchWorkerExecutionAge != nil {
		t.Fatal("nil observer registered a gauge, want no registration")
	}
}

// TestRegisterReducerBatchWorkerOccupancyGauge_ObservesBusyWorkers is the
// #10480 metric gate: a busy worker observes its execution age with the
// closed queue, worker, and domain labels, and an idle worker observes
// nothing (absent series means idle), so no unbounded intent id ever
// becomes a label.
func TestRegisterReducerBatchWorkerOccupancyGauge_ObservesBusyWorkers(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst := &Instruments{}

	observer := &fakeBatchWorkerOccupancyObserver{snapshot: []BatchWorkerOccupancy{
		{Worker: 0, Busy: false},
		{Worker: 3, Busy: true, Domain: "code_call_materialization", IntentID: "intent-stuck", ExecutionAgeSeconds: 3723.5},
	}}
	if err := RegisterReducerBatchWorkerOccupancyGauge(inst, provider.Meter("test"), observer); err != nil {
		t.Fatalf("RegisterReducerBatchWorkerOccupancyGauge() error = %v", err)
	}
	if inst.ReducerBatchWorkerExecutionAge == nil {
		t.Fatal("ReducerBatchWorkerExecutionAge gauge was not registered")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var points []metricdata.DataPoint[float64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_batch_worker_execution_age_seconds" {
				continue
			}
			data, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("occupancy data = %T, want Gauge[float64]", m.Data)
			}
			points = append(points, data.DataPoints...)
		}
	}
	if len(points) != 1 {
		t.Fatalf("occupancy points = %d, want 1 (busy worker only; idle is absent)", len(points))
	}
	point := points[0]
	if point.Value != 3723.5 {
		t.Fatalf("occupancy age = %v, want 3723.5", point.Value)
	}
	wantAttrs := map[string]string{"queue": "reducer", "worker": "3", "domain": "code_call_materialization"}
	for key, want := range wantAttrs {
		value, ok := point.Attributes.Value(attribute.Key(key))
		if !ok || value.AsString() != want {
			t.Fatalf("occupancy attrs missing %s=%s: %+v", key, want, point.Attributes)
		}
	}
	if len(point.Attributes.ToSlice()) != len(wantAttrs) {
		t.Fatalf("occupancy attrs = %+v, want exactly %v (closed label set)", point.Attributes, wantAttrs)
	}
}

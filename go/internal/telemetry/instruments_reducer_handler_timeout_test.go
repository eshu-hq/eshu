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

type fakeHandlerTimeoutBounds struct {
	bounds map[string]float64
	err    error
}

func (f *fakeHandlerTimeoutBounds) HandlerTimeoutBounds(context.Context) (map[string]float64, error) {
	return f.bounds, f.err
}

// TestRegisterReducerHandlerTimeoutGauge_NilInputs rejects nil instruments
// and nil meters, and registers nothing for a nil bounds source, so lanes
// without a configured reducer service keep today's instruments
// byte-identical.
func TestRegisterReducerHandlerTimeoutGauge_NilInputs(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	if err := RegisterReducerHandlerTimeoutGauge(nil, meter, &fakeHandlerTimeoutBounds{}); err == nil {
		t.Fatal("expected error for nil instruments")
	}
	if err := RegisterReducerHandlerTimeoutGauge(inst, nil, &fakeHandlerTimeoutBounds{}); err == nil {
		t.Fatal("expected error for nil meter")
	}
	if err := RegisterReducerHandlerTimeoutGauge(inst, meter, nil); err != nil {
		t.Fatalf("nil bounds error = %v, want nil", err)
	}
	if inst.ReducerHandlerTimeoutSeconds != nil {
		t.Fatal("nil bounds registered a gauge, want no registration")
	}
}

// TestRegisterReducerHandlerTimeoutGauge_ExportsBoundsPerDomain is the #10866
// metric gate: the gauge observes one point per known domain with the bound
// in seconds (0 = unbounded) and the closed domain label only, so the hosted
// alert can compare live execution age against the bound that applies to it.
func TestRegisterReducerHandlerTimeoutGauge_ExportsBoundsPerDomain(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst := &Instruments{}

	bounds := &fakeHandlerTimeoutBounds{bounds: map[string]float64{
		"security_alert_reconciliation": 1800,
		"supply_chain_impact":           0,
	}}
	if err := RegisterReducerHandlerTimeoutGauge(inst, provider.Meter("test"), bounds); err != nil {
		t.Fatalf("RegisterReducerHandlerTimeoutGauge() error = %v, want nil", err)
	}
	if inst.ReducerHandlerTimeoutSeconds == nil {
		t.Fatal("ReducerHandlerTimeoutSeconds gauge was not registered")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	points := map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_handler_timeout_seconds" {
				continue
			}
			data, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("timeout bounds data = %T, want Gauge[float64]", m.Data)
			}
			for _, point := range data.DataPoints {
				domain, ok := point.Attributes.Value(attribute.Key("domain"))
				if !ok {
					t.Fatalf("timeout bound point missing domain label: %+v", point.Attributes)
				}
				if len(point.Attributes.ToSlice()) != 1 {
					t.Fatalf("timeout bound attrs = %+v, want exactly {domain} (closed label set)", point.Attributes)
				}
				points[domain.AsString()] = point.Value
			}
		}
	}
	if len(points) != 2 {
		t.Fatalf("timeout bound points = %d, want 2 (one per configured domain)", len(points))
	}
	if got := points["security_alert_reconciliation"]; got != 1800 {
		t.Fatalf("security_alert_reconciliation bound = %v, want 1800", got)
	}
	if got := points["supply_chain_impact"]; got != 0 {
		t.Fatalf("supply_chain_impact bound = %v, want 0 (unbounded)", got)
	}
}

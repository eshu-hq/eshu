// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeMarkedWriteQueueObserver is a queue observer that also reports the
// projector marked-write age.
type fakeMarkedWriteQueueObserver struct {
	fakeQueueObserver
	age float64
}

func (f *fakeMarkedWriteQueueObserver) ProjectorMarkedWriteOldestAge(context.Context) (float64, error) {
	return f.age, nil
}

// TestRegisterObservableGaugesReportsProjectorMarkedWriteAge proves the #7471
// age gauge is registered on the queue-status cadence when the queue observer
// can report it, and that it carries its observed age with no labels.
func TestRegisterObservableGaugesReportsProjectorMarkedWriteAge(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	observer := &fakeMarkedWriteQueueObserver{age: 90.5}

	if err := RegisterObservableGauges(inst, meter, observer, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ProjectorMarkedWriteOldestAgeSeconds == nil {
		t.Fatal("expected the projector marked-write age gauge to be set")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	observed := false
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_projector_marked_write_oldest_age_seconds" {
				continue
			}
			gauge, isGauge := m.Data.(metricdata.Gauge[float64])
			if !isGauge || len(gauge.DataPoints) != 1 {
				t.Fatalf("%s data = %#v, want one float64 point", m.Name, m.Data)
			}
			if got := gauge.DataPoints[0].Value; got != 90.5 {
				t.Fatalf("%s value = %v, want 90.5", m.Name, got)
			}
			if gauge.DataPoints[0].Attributes.Len() != 0 {
				t.Fatalf("%s attributes = %v, want none", m.Name, gauge.DataPoints[0].Attributes)
			}
			observed = true
		}
	}
	if !observed {
		t.Fatal("marked-write age gauge not collected")
	}
}

// TestRegisterObservableGaugesSkipsProjectorMarkedWriteAgeWithoutObserver
// keeps queue observers that cannot report the age working unchanged.
func TestRegisterObservableGaugesSkipsProjectorMarkedWriteAgeWithoutObserver(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	if err := RegisterObservableGauges(inst, meter, &fakeQueueObserver{}, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ProjectorMarkedWriteOldestAgeSeconds != nil {
		t.Fatal("projector marked-write age gauge registered without a marked-write observer")
	}
}

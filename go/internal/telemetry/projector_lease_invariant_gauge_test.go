// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeProjectorLeaseQueueObserver is a queue observer that also reports the
// projector lease invariant.
type fakeProjectorLeaseQueueObserver struct {
	fakeQueueObserver
	overlapping int64
}

func (f *fakeProjectorLeaseQueueObserver) ProjectorScopesWithMultipleLiveLeases(context.Context) (int64, error) {
	return f.overlapping, nil
}

// TestRegisterObservableGaugesReportsProjectorLeaseInvariant proves the #7115
// invariant gauge is registered on the queue-status cadence when the queue
// observer can report it, and that it carries the observed scope count with
// no labels.
func TestRegisterObservableGaugesReportsProjectorLeaseInvariant(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	observer := &fakeProjectorLeaseQueueObserver{overlapping: 2}

	if err := RegisterObservableGauges(inst, meter, observer, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ProjectorScopesMultipleLiveLeases == nil {
		t.Fatal("expected ProjectorScopesMultipleLiveLeases gauge to be set")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_projector_scopes_multiple_live_leases" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok || len(gauge.DataPoints) != 1 {
				t.Fatalf("gauge data = %#v, want one int64 point", m.Data)
			}
			if got := gauge.DataPoints[0].Value; got != 2 {
				t.Fatalf("gauge value = %d, want 2", got)
			}
			if gauge.DataPoints[0].Attributes.Len() != 0 {
				t.Fatalf("gauge attributes = %v, want none", gauge.DataPoints[0].Attributes)
			}
			return
		}
	}
	t.Fatal("eshu_dp_projector_scopes_multiple_live_leases was not collected")
}

// TestRegisterObservableGaugesSkipsProjectorLeaseInvariantWithoutObserver
// keeps queue observers that cannot report the invariant working unchanged.
func TestRegisterObservableGaugesSkipsProjectorLeaseInvariantWithoutObserver(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	if err := RegisterObservableGauges(inst, meter, &fakeQueueObserver{}, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ProjectorScopesMultipleLiveLeases != nil {
		t.Fatal("ProjectorScopesMultipleLiveLeases registered without a projector lease observer")
	}
}

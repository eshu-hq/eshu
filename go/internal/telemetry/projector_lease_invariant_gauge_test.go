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
	overlapping  int64
	missingFence int64
}

func (f *fakeProjectorLeaseQueueObserver) ProjectorScopesWithMultipleLiveLeases(context.Context) (int64, error) {
	return f.overlapping, nil
}

func (f *fakeProjectorLeaseQueueObserver) ProjectorScopesMissingClaimFence(context.Context) (int64, error) {
	return f.missingFence, nil
}

// TestRegisterObservableGaugesReportsProjectorLeaseInvariant proves the #7115
// invariant gauges are registered on the queue-status cadence when the queue
// observer can report them, and that each carries its observed scope count
// with no labels.
func TestRegisterObservableGaugesReportsProjectorLeaseInvariant(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	observer := &fakeProjectorLeaseQueueObserver{overlapping: 2, missingFence: 3}

	if err := RegisterObservableGauges(inst, meter, observer, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ProjectorScopesMultipleLiveLeases == nil || inst.ProjectorScopesMissingClaimFence == nil {
		t.Fatal("expected both projector claim invariant gauges to be set")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	want := map[string]int64{
		"eshu_dp_projector_scopes_multiple_live_leases": 2,
		"eshu_dp_projector_scopes_missing_claim_fence":  3,
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			value, ok := want[m.Name]
			if !ok {
				continue
			}
			gauge, isGauge := m.Data.(metricdata.Gauge[int64])
			if !isGauge || len(gauge.DataPoints) != 1 {
				t.Fatalf("%s data = %#v, want one int64 point", m.Name, m.Data)
			}
			if got := gauge.DataPoints[0].Value; got != value {
				t.Fatalf("%s value = %d, want %d", m.Name, got, value)
			}
			if gauge.DataPoints[0].Attributes.Len() != 0 {
				t.Fatalf("%s attributes = %v, want none", m.Name, gauge.DataPoints[0].Attributes)
			}
			delete(want, m.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("gauges not collected: %v", want)
	}
}

// TestRegisterObservableGaugesSkipsProjectorLeaseInvariantWithoutObserver
// keeps queue observers that cannot report the invariant working unchanged.
func TestRegisterObservableGaugesSkipsProjectorLeaseInvariantWithoutObserver(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	if err := RegisterObservableGauges(inst, meter, &fakeQueueObserver{}, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ProjectorScopesMultipleLiveLeases != nil || inst.ProjectorScopesMissingClaimFence != nil {
		t.Fatal("projector claim invariant gauges registered without a projector claim observer")
	}
}

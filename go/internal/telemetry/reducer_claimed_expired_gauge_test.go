// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeClaimedExpiredQueueObserver is a queue observer that also reports the
// reducer reclaim backlog.
type fakeClaimedExpiredQueueObserver struct {
	fakeQueueObserver
	count int64
	age   float64
}

func (f *fakeClaimedExpiredQueueObserver) ReducerClaimedExpiredCounts(context.Context) (int64, float64, error) {
	return f.count, f.age, nil
}

// TestRegisterObservableGaugesReportsReducerClaimedExpired proves the #10865
// count and oldest-age gauges are registered on the queue-status cadence
// when the queue observer can report them, and that they carry their
// observed values with no labels.
func TestRegisterObservableGaugesReportsReducerClaimedExpired(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	observer := &fakeClaimedExpiredQueueObserver{count: 6, age: 18240}

	if err := RegisterObservableGauges(inst, meter, observer, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ReducerClaimedExpired == nil {
		t.Fatal("expected the reducer claimed-expired count gauge to be set")
	}
	if inst.ReducerClaimedExpiredOldestAgeSeconds == nil {
		t.Fatal("expected the reducer claimed-expired oldest-age gauge to be set")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var sawCount, sawAge bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_reducer_claimed_expired":
				gauge, isGauge := m.Data.(metricdata.Gauge[int64])
				if !isGauge || len(gauge.DataPoints) != 1 {
					t.Fatalf("%s data = %#v, want one int64 point", m.Name, m.Data)
				}
				if got := gauge.DataPoints[0].Value; got != 6 {
					t.Fatalf("%s value = %v, want 6", m.Name, got)
				}
				if gauge.DataPoints[0].Attributes.Len() != 0 {
					t.Fatalf("%s attributes = %v, want none", m.Name, gauge.DataPoints[0].Attributes)
				}
				sawCount = true
			case "eshu_dp_reducer_claimed_expired_oldest_age_seconds":
				gauge, isGauge := m.Data.(metricdata.Gauge[float64])
				if !isGauge || len(gauge.DataPoints) != 1 {
					t.Fatalf("%s data = %#v, want one float64 point", m.Name, m.Data)
				}
				if got := gauge.DataPoints[0].Value; got != 18240 {
					t.Fatalf("%s value = %v, want 18240", m.Name, got)
				}
				sawAge = true
			}
		}
	}
	if !sawCount {
		t.Fatal("claimed-expired count gauge not collected")
	}
	if !sawAge {
		t.Fatal("claimed-expired oldest-age gauge not collected")
	}
}

// TestRegisterObservableGaugesSkipsReducerClaimedExpiredWithoutObserver keeps
// queue observers that cannot report the reclaim backlog working unchanged.
func TestRegisterObservableGaugesSkipsReducerClaimedExpiredWithoutObserver(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	if err := RegisterObservableGauges(inst, meter, &fakeQueueObserver{}, nil); err != nil {
		t.Fatalf("RegisterObservableGauges() error = %v", err)
	}
	if inst.ReducerClaimedExpired != nil {
		t.Fatal("reducer claimed-expired count gauge registered without a claimed-expired observer")
	}
	if inst.ReducerClaimedExpiredOldestAgeSeconds != nil {
		t.Fatal("reducer claimed-expired oldest-age gauge registered without a claimed-expired observer")
	}
}

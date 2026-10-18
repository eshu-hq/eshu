// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeCorpusFenceObserver reports the #8247 completeness-fence gauges.
type fakeCorpusFenceObserver struct {
	holders  int64
	deferred map[string]int64
	oldest   map[string]float64
}

func (f fakeCorpusFenceObserver) CorpusFenceCounts(context.Context) (int64, map[string]int64, map[string]float64, error) {
	return f.holders, f.deferred, f.oldest, nil
}

// TestRegisterCorpusFenceObservableGaugesReportsFenceState proves the #8247
// gauges report the holder count and the per-domain deferred count and oldest
// deferred age under a held fence.
func TestRegisterCorpusFenceObservableGaugesReportsFenceState(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	observer := fakeCorpusFenceObserver{
		holders:  3,
		deferred: map[string]int64{"deployable_unit_correlation": 782, "workload_materialization": 67},
		oldest:   map[string]float64{"deployable_unit_correlation": 1866240, "workload_materialization": 417},
	}
	if err := RegisterCorpusFenceObservableGauges(inst, meter, observer); err != nil {
		t.Fatalf("RegisterCorpusFenceObservableGauges() error = %v", err)
	}
	if inst.CorpusFenceHolders == nil || inst.CorpusFenceDeferredConsumers == nil || inst.CorpusFenceOldestDeferredAgeSeconds == nil {
		t.Fatalf("corpus fence gauges not all registered: %+v", inst)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	gotInt := map[string]map[string]int64{}
	gotFloat := map[string]map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Gauge[int64]:
				gotInt[m.Name] = gaugeIntByDomain(data)
			case metricdata.Gauge[float64]:
				gotFloat[m.Name] = gaugeFloatByDomain(data)
			}
		}
	}
	if got := gotInt["eshu_dp_corpus_fence_holders"][""]; got != 3 {
		t.Fatalf("eshu_dp_corpus_fence_holders = %d, want 3 (all: %+v)", got, gotInt)
	}
	if got := gotInt["eshu_dp_corpus_fence_deferred_consumers"]["deployable_unit_correlation"]; got != 782 {
		t.Fatalf("deferred deployable_unit_correlation = %d, want 782 (all: %+v)", got, gotInt)
	}
	if got := gotFloat["eshu_dp_corpus_fence_oldest_deferred_age_seconds"]["deployable_unit_correlation"]; got != 1866240 {
		t.Fatalf("oldest deferred deployable_unit_correlation = %v, want 1866240 (all: %+v)", got, gotFloat)
	}
}

func gaugeIntByDomain(gauge metricdata.Gauge[int64]) map[string]int64 {
	out := map[string]int64{}
	for _, dp := range gauge.DataPoints {
		domain, _ := dp.Attributes.Value("domain")
		out[domain.AsString()] = dp.Value
	}
	return out
}

func gaugeFloatByDomain(gauge metricdata.Gauge[float64]) map[string]float64 {
	out := map[string]float64{}
	for _, dp := range gauge.DataPoints {
		domain, _ := dp.Attributes.Value("domain")
		out[domain.AsString()] = dp.Value
	}
	return out
}

// TestRegisterCorpusFenceObservableGaugesNilInputs keeps the nil contracts.
func TestRegisterCorpusFenceObservableGaugesNilInputs(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	if err := RegisterCorpusFenceObservableGauges(nil, meter, fakeCorpusFenceObserver{}); err == nil {
		t.Fatal("expected error for nil instruments")
	}
	if err := RegisterCorpusFenceObservableGauges(inst, nil, fakeCorpusFenceObserver{}); err == nil {
		t.Fatal("expected error for nil meter")
	}
	if err := RegisterCorpusFenceObservableGauges(inst, meter, nil); err != nil {
		t.Fatalf("nil observer error = %v, want nil", err)
	}
	if inst.CorpusFenceHolders != nil {
		t.Fatal("holders gauge registered without an observer")
	}
}

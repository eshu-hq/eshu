// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// stubGenerationAgeObserver reports canned active-generation age buckets.
type stubGenerationAgeObserver struct {
	counts map[string]int64
	err    error
}

func (s stubGenerationAgeObserver) ActiveGenerationsByAge(context.Context) (map[string]int64, error) {
	return s.counts, s.err
}

// stubPoisonObserver reports canned dead-letter class sizes.
type stubPoisonObserver struct {
	scopes int64
	items  int64
	oldest float64
	err    error
}

func (s stubPoisonObserver) PoisonDeadLetterCounts(context.Context) (int64, int64, float64, error) {
	return s.scopes, s.items, s.oldest, s.err
}

// stubFairnessDepthObserver reports canned fairness-key depths.
type stubFairnessDepthObserver struct {
	depths map[string]map[string]WorkflowFairnessFamilyDepth
	err    error
}

func (s stubFairnessDepthObserver) WorkflowFairnessKeyDepths(context.Context) (map[string]map[string]WorkflowFairnessFamilyDepth, error) {
	return s.depths, s.err
}

// gaugeTestMeter returns a meter backed by a manual reader for assertions.
func gaugeTestMeter() (metric.Meter, *sdkmetric.ManualReader) {
	reader := sdkmetric.NewManualReader()
	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), reader
}

// errGaugeBoom is the sentinel registration failure the fake meters report.
var errGaugeBoom = errors.New("gauge boom")

// flakyGaugeMeter delegates to a real meter until its failOn-th gauge
// registration, which fails, so each registration error branch is reachable.
type flakyGaugeMeter struct {
	metric.Meter
	calls  int
	failOn int
}

func (m *flakyGaugeMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	m.calls++
	if m.calls == m.failOn {
		var gauge metric.Int64ObservableGauge
		return gauge, errGaugeBoom
	}
	return m.Meter.Int64ObservableGauge(name, opts...)
}

func (m *flakyGaugeMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	m.calls++
	if m.calls == m.failOn {
		var gauge metric.Float64ObservableGauge
		return gauge, errGaugeBoom
	}
	return m.Meter.Float64ObservableGauge(name, opts...)
}

func (m *flakyGaugeMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Int64Counter
		return inst, errGaugeBoom
	}
	return m.Meter.Int64Counter(name, opts...)
}

func (m *flakyGaugeMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Int64UpDownCounter
		return inst, errGaugeBoom
	}
	return m.Meter.Int64UpDownCounter(name, opts...)
}

func (m *flakyGaugeMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Int64Histogram
		return inst, errGaugeBoom
	}
	return m.Meter.Int64Histogram(name, opts...)
}

func (m *flakyGaugeMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Int64Gauge
		return inst, errGaugeBoom
	}
	return m.Meter.Int64Gauge(name, opts...)
}

func (m *flakyGaugeMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Int64ObservableCounter
		return inst, errGaugeBoom
	}
	return m.Meter.Int64ObservableCounter(name, opts...)
}

func (m *flakyGaugeMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Int64ObservableUpDownCounter
		return inst, errGaugeBoom
	}
	return m.Meter.Int64ObservableUpDownCounter(name, opts...)
}

func (m *flakyGaugeMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Float64Counter
		return inst, errGaugeBoom
	}
	return m.Meter.Float64Counter(name, opts...)
}

func (m *flakyGaugeMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Float64UpDownCounter
		return inst, errGaugeBoom
	}
	return m.Meter.Float64UpDownCounter(name, opts...)
}

func (m *flakyGaugeMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Float64Histogram
		return inst, errGaugeBoom
	}
	return m.Meter.Float64Histogram(name, opts...)
}

func (m *flakyGaugeMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Float64Gauge
		return inst, errGaugeBoom
	}
	return m.Meter.Float64Gauge(name, opts...)
}

func (m *flakyGaugeMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Float64ObservableCounter
		return inst, errGaugeBoom
	}
	return m.Meter.Float64ObservableCounter(name, opts...)
}

func (m *flakyGaugeMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	m.calls++
	if m.calls == m.failOn {
		var inst metric.Float64ObservableUpDownCounter
		return inst, errGaugeBoom
	}
	return m.Meter.Float64ObservableUpDownCounter(name, opts...)
}

// callbackFailMeter delegates to a real meter except RegisterCallback,
// which always fails, reaching the callback-registration error branches.
type callbackFailMeter struct {
	metric.Meter
}

func (m callbackFailMeter) RegisterCallback(_ metric.Callback, _ ...metric.Observable) (metric.Registration, error) {
	return nil, errGaugeBoom
}

// flakyTestMeter wraps gaugeTestMeter with failOn-th gauge failure.
func flakyTestMeter(failOn int) metric.Meter {
	meter, _ := gaugeTestMeter()
	return &flakyGaugeMeter{Meter: meter, failOn: failOn}
}

// collectIntGauges gathers int64 gauge values by metric name.
func collectIntGauges(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok {
				for _, dp := range gauge.DataPoints {
					got[m.Name] += dp.Value
				}
			}
		}
	}
	return got
}

// collectFloatGauges gathers float64 gauge values by metric name.
func collectFloatGauges(t *testing.T, reader *sdkmetric.ManualReader) map[string]float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if gauge, ok := m.Data.(metricdata.Gauge[float64]); ok {
				for _, dp := range gauge.DataPoints {
					got[m.Name] += dp.Value
				}
			}
		}
	}
	return got
}

// TestRegisterActiveGenerationAgeGauge proves guards, reporting, and
// observer-error propagation.
func TestRegisterActiveGenerationAgeGauge(t *testing.T) {
	t.Parallel()

	meter, _ := gaugeTestMeter()
	if err := RegisterActiveGenerationAgeObservableGauge(nil, meter, stubGenerationAgeObserver{}); err == nil {
		t.Fatal("nil instruments registered without error, want a failure")
	}
	if err := RegisterActiveGenerationAgeObservableGauge(&Instruments{}, nil, stubGenerationAgeObserver{}); err == nil {
		t.Fatal("nil meter registered without error, want a failure")
	}
	if err := RegisterActiveGenerationAgeObservableGauge(&Instruments{}, meter, nil); err != nil {
		t.Fatalf("nil observer err = %v, want nil skip", err)
	}
	meter, reader := gaugeTestMeter()
	inst := &Instruments{}
	observer := stubGenerationAgeObserver{counts: map[string]int64{"fresh": 2, "stuck": 1}}
	if err := RegisterActiveGenerationAgeObservableGauge(inst, meter, observer); err != nil {
		t.Fatalf("register err = %v, want nil", err)
	}
	if inst.ActiveGenerationsByAge == nil {
		t.Fatal("ActiveGenerationsByAge gauge is nil after registration")
	}
	if got := collectIntGauges(t, reader)["eshu_dp_active_generations"]; got != 3 {
		t.Fatalf("active generations total = %d, want 3", got)
	}
	failingMeter, failingReader := gaugeTestMeter()
	if err := RegisterActiveGenerationAgeObservableGauge(&Instruments{}, failingMeter,
		stubGenerationAgeObserver{err: errors.New("store down")}); err != nil {
		t.Fatalf("register err = %v, want nil (errors surface at collect)", err)
	}
	var rm metricdata.ResourceMetrics
	if err := failingReader.Collect(context.Background(), &rm); err == nil {
		t.Fatal("Collect with a failing observer succeeded, want the observer error")
	}
	if err := RegisterActiveGenerationAgeObservableGauge(&Instruments{}, flakyTestMeter(1),
		stubGenerationAgeObserver{}); err == nil {
		t.Fatal("failing gauge registration succeeded, want the wrapped error")
	}
}

// TestRegisterPoisonLivenessGauges proves the one-query snapshot reports
// scopes, items, and oldest age together.
func TestRegisterPoisonLivenessGauges(t *testing.T) {
	t.Parallel()

	meter, _ := gaugeTestMeter()
	if err := RegisterPoisonLivenessObservableGauges(nil, meter, stubPoisonObserver{}); err == nil {
		t.Fatal("nil instruments registered without error, want a failure")
	}
	if err := RegisterPoisonLivenessObservableGauges(&Instruments{}, nil, stubPoisonObserver{}); err == nil {
		t.Fatal("nil meter registered without error, want a failure")
	}
	if err := RegisterPoisonLivenessObservableGauges(&Instruments{}, meter, nil); err != nil {
		t.Fatalf("nil observer err = %v, want nil skip", err)
	}
	meter, reader := gaugeTestMeter()
	inst := &Instruments{}
	observer := stubPoisonObserver{scopes: 2, items: 5, oldest: 3600}
	if err := RegisterPoisonLivenessObservableGauges(inst, meter, observer); err != nil {
		t.Fatalf("register err = %v, want nil", err)
	}
	if inst.PoisonDeadLetterScopes == nil || inst.PoisonDeadLetterItems == nil || inst.PoisonDeadLetterOldestAgeSeconds == nil {
		t.Fatalf("poison gauges not all registered: %+v", inst)
	}
	ints := collectIntGauges(t, reader)
	if ints["eshu_dp_poison_dead_letter_scopes"] != 2 || ints["eshu_dp_poison_dead_letter_items"] != 5 {
		t.Fatalf("poison counts = %#v, want scopes 2 and items 5", ints)
	}
	floats := collectFloatGauges(t, reader)
	if floats["eshu_dp_poison_dead_letter_oldest_age_seconds"] != 3600 {
		t.Fatalf("poison oldest age = %#v, want 3600", floats)
	}
	for failOn := 1; failOn <= 3; failOn++ {
		if err := RegisterPoisonLivenessObservableGauges(&Instruments{}, flakyTestMeter(failOn),
			stubPoisonObserver{}); err == nil {
			t.Fatalf("poison gauge %d failing registration succeeded, want the wrapped error", failOn)
		}
	}
	realMeter, _ := gaugeTestMeter()
	if err := RegisterPoisonLivenessObservableGauges(&Instruments{},
		callbackFailMeter{Meter: realMeter}, stubPoisonObserver{}); err == nil {
		t.Fatal("poison callback failing registration succeeded, want the wrapped error")
	}
}

// TestRegisterFairnessKeyDepthGauges proves depth and count series report
// with the top-keys bound enforced.
func TestRegisterFairnessKeyDepthGauges(t *testing.T) {
	t.Parallel()

	meter, _ := gaugeTestMeter()
	if err := RegisterWorkflowFairnessKeyDepthObservableGauges(nil, meter, stubFairnessDepthObserver{}); err == nil {
		t.Fatal("nil instruments registered without error, want a failure")
	}
	if err := RegisterWorkflowFairnessKeyDepthObservableGauges(&Instruments{}, nil, stubFairnessDepthObserver{}); err == nil {
		t.Fatal("nil meter registered without error, want a failure")
	}
	if err := RegisterWorkflowFairnessKeyDepthObservableGauges(&Instruments{}, meter, nil); err != nil {
		t.Fatalf("nil observer err = %v, want nil skip", err)
	}
	top := make([]WorkflowFairnessKeyPending, 0, WorkflowFairnessTopKeys+2)
	for i := 0; i < WorkflowFairnessTopKeys+2; i++ {
		top = append(top, WorkflowFairnessKeyPending{Key: "k-" + string(rune(int('a')+i)), Pending: 1})
	}
	meter, reader := gaugeTestMeter()
	inst := &Instruments{}
	observer := stubFairnessDepthObserver{depths: map[string]map[string]WorkflowFairnessFamilyDepth{
		"git": {"i-1": {DistinctKeys: 4, Top: top}},
	}}
	if err := RegisterWorkflowFairnessKeyDepthObservableGauges(inst, meter, observer); err != nil {
		t.Fatalf("register err = %v, want nil", err)
	}
	ints := collectIntGauges(t, reader)
	if ints["eshu_dp_workflow_fairness_key_depth"] != WorkflowFairnessTopKeys {
		t.Fatalf("fairness depth total = %d, want the top-keys bound %d", ints["eshu_dp_workflow_fairness_key_depth"], WorkflowFairnessTopKeys)
	}
	if ints["eshu_dp_workflow_fairness_key_count"] != 4 {
		t.Fatalf("fairness key count = %d, want 4", ints["eshu_dp_workflow_fairness_key_count"])
	}
	for failOn := 1; failOn <= 2; failOn++ {
		if err := RegisterWorkflowFairnessKeyDepthObservableGauges(&Instruments{}, flakyTestMeter(failOn),
			stubFairnessDepthObserver{}); err == nil {
			t.Fatalf("fairness gauge %d failing registration succeeded, want the wrapped error", failOn)
		}
	}
}

// TestRegisterTfstateSchemaResolverEntries proves nil skipping, value
// reporting, and duplicate-registration failure.
func TestRegisterTfstateSchemaResolverEntries(t *testing.T) {
	t.Parallel()

	meter, _ := gaugeTestMeter()
	if err := RegisterTfstateSchemaResolverEntries(nil, func() int { return 1 }); err != nil {
		t.Fatalf("nil meter err = %v, want nil skip", err)
	}
	if err := RegisterTfstateSchemaResolverEntries(meter, nil); err != nil {
		t.Fatalf("nil counter err = %v, want nil skip", err)
	}
	meter, reader := gaugeTestMeter()
	if err := RegisterTfstateSchemaResolverEntries(meter, func() int { return 7 }); err != nil {
		t.Fatalf("register err = %v, want nil", err)
	}
	if got := collectIntGauges(t, reader)["eshu_dp_tfstate_schema_resolver_entries"]; got != 7 {
		t.Fatalf("resolver entries = %d, want 7", got)
	}
	if err := RegisterTfstateSchemaResolverEntries(flakyTestMeter(1), func() int { return 7 }); err == nil {
		t.Fatal("failing registration succeeded, want the SDK error")
	}
}

// TestRecordGOMEMLIMIT proves nil skipping, value reporting, and
// duplicate-registration failure.
func TestRecordGOMEMLIMIT(t *testing.T) {
	t.Parallel()

	if err := RecordGOMEMLIMIT(nil, 1024); err != nil {
		t.Fatalf("nil meter err = %v, want nil skip", err)
	}
	meter, reader := gaugeTestMeter()
	if err := RecordGOMEMLIMIT(meter, 2048); err != nil {
		t.Fatalf("record err = %v, want nil", err)
	}
	if got := collectIntGauges(t, reader)["eshu_dp_gomemlimit_bytes"]; got != 2048 {
		t.Fatalf("gomemlimit = %d, want 2048", got)
	}
	if err := RecordGOMEMLIMIT(flakyTestMeter(1), 2048); err == nil {
		t.Fatal("failing registration succeeded, want the SDK error")
	}
}

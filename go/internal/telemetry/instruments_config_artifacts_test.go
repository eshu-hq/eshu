// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

const (
	configArtifactDerivesName        = "eshu_dp_config_artifact_derives_total"
	configArtifactDeriveDurationName = "eshu_dp_config_artifact_derive_duration_seconds"
)

// TestConfigArtifactDeriveLabelValuesAreClosed pins the closed trigger and
// outcome sets of the config-artifact derive instruments (#8155). The
// triggers match artifacts.Trigger in
// go/internal/storage/postgres/config/artifacts; the outcomes are the eight
// the arbiter ruling of PR3b-2c names, canceled included.
func TestConfigArtifactDeriveLabelValuesAreClosed(t *testing.T) {
	t.Parallel()

	triggers := ConfigArtifactDeriveTriggers()
	if want := []string{"write", "backfill", "repair"}; !slices.Equal(triggers, want) {
		t.Fatalf("ConfigArtifactDeriveTriggers() = %v, want %v", triggers, want)
	}
	outcomes := ConfigArtifactDeriveOutcomes()
	want := []string{
		"ok", "superseded", "error", "panic", "timeout", "canceled",
		"encoding_unsupported", "fence_missing",
	}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("ConfigArtifactDeriveOutcomes() = %v, want %v", outcomes, want)
	}

	// The accessors return copies: a caller cannot widen the closed set.
	triggers[0] = "mutated"
	outcomes[0] = "mutated"
	if ConfigArtifactDeriveTriggers()[0] != "write" || ConfigArtifactDeriveOutcomes()[0] != "ok" {
		t.Fatal("label value accessors returned shared slices")
	}
}

// TestConfigArtifactDeriveLabelsMatchStorageConstants pins the telemetry
// trigger and outcome sets to the artifacts package's Trigger and Outcome
// constants (#8155 PR3b-2c-2): a renamed, added or dropped outcome must change
// both sides, or the wiring counts a label the storage package never returns
// (or drops one it does).
func TestConfigArtifactDeriveLabelsMatchStorageConstants(t *testing.T) {
	t.Parallel()
	storageTriggers := []string{
		string(artifacts.TriggerWrite),
		string(artifacts.TriggerBackfill),
		string(artifacts.TriggerRepair),
	}
	if got := ConfigArtifactDeriveTriggers(); !slices.Equal(got, storageTriggers) {
		t.Fatalf("telemetry triggers = %v, storage triggers = %v", got, storageTriggers)
	}
	storageOutcomes := []string{
		string(artifacts.OutcomeOK),
		string(artifacts.OutcomeSuperseded),
		string(artifacts.OutcomeError),
		string(artifacts.OutcomePanic),
		string(artifacts.OutcomeTimeout),
		string(artifacts.OutcomeCanceled),
		string(artifacts.OutcomeEncodingUnsupported),
		string(artifacts.OutcomeFenceMissing),
	}
	if got := ConfigArtifactDeriveOutcomes(); !slices.Equal(got, storageOutcomes) {
		t.Fatalf("telemetry outcomes = %v, storage outcomes = %v", got, storageOutcomes)
	}
}

// TestConfigArtifactDeriveDimensionAndSpanRegistered pins the trigger
// dimension key and the derive span name in the frozen contract lists.
func TestConfigArtifactDeriveDimensionAndSpanRegistered(t *testing.T) {
	t.Parallel()

	if MetricDimensionTrigger != "trigger" {
		t.Fatalf("MetricDimensionTrigger = %q, want %q", MetricDimensionTrigger, "trigger")
	}
	if !slices.Contains(MetricDimensionKeys(), MetricDimensionTrigger) {
		t.Fatalf("MetricDimensionKeys() missing %q", MetricDimensionTrigger)
	}
	if got := AttrTrigger(ConfigArtifactDeriveTriggerWrite); string(got.Key) != "trigger" || got.Value.AsString() != "write" {
		t.Fatalf("AttrTrigger(write) = %v, want trigger=write", got)
	}
	if SpanConfigArtifactsDerive != "config_artifacts.derive" {
		t.Fatalf("SpanConfigArtifactsDerive = %q, want %q", SpanConfigArtifactsDerive, "config_artifacts.derive")
	}
	if !slices.Contains(SpanNames(), SpanConfigArtifactsDerive) {
		t.Fatalf("SpanNames() missing %q", SpanConfigArtifactsDerive)
	}
}

// TestConfigArtifactDeriveInstrumentsExportBoundedLabels records one point
// per closed label value through the registered instruments and reads them
// back from an SDK reader: the counter carries exactly {trigger, outcome},
// the histogram exactly {trigger}, and neither carries a repository id
// (ADR 7126 section 3.5 forbids ids on metrics).
func TestConfigArtifactDeriveInstrumentsExportBoundedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("config-artifact-derive-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx := context.Background()
	for _, trigger := range ConfigArtifactDeriveTriggers() {
		for _, outcome := range ConfigArtifactDeriveOutcomes() {
			inst.ConfigArtifactDerives.Add(ctx, 1, metric.WithAttributes(AttrTrigger(trigger), AttrOutcome(outcome)))
		}
		inst.ConfigArtifactDeriveDuration.Record(ctx, 0.92, metric.WithAttributes(AttrTrigger(trigger)))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counter := findConfigArtifactDeriveMetric(t, rm, configArtifactDerivesName)
	sum, ok := counter.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s data = %T, want metricdata.Sum[int64]", configArtifactDerivesName, counter.Data)
	}
	if !sum.IsMonotonic {
		t.Fatalf("%s is not monotonic", configArtifactDerivesName)
	}
	if got, want := len(sum.DataPoints), len(ConfigArtifactDeriveTriggers())*len(ConfigArtifactDeriveOutcomes()); got != want {
		t.Fatalf("%s series = %d, want %d", configArtifactDerivesName, got, want)
	}
	for _, point := range sum.DataPoints {
		assertConfigArtifactDeriveKeys(t, configArtifactDerivesName, point.Attributes, []string{"outcome", "trigger"})
	}

	hist := findConfigArtifactDeriveMetric(t, rm, configArtifactDeriveDurationName)
	if hist.Unit != "s" {
		t.Fatalf("%s unit = %q, want s", configArtifactDeriveDurationName, hist.Unit)
	}
	data, ok := hist.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("%s data = %T, want metricdata.Histogram[float64]", configArtifactDeriveDurationName, hist.Data)
	}
	if got := len(data.DataPoints); got != len(ConfigArtifactDeriveTriggers()) {
		t.Fatalf("%s series = %d, want %d", configArtifactDeriveDurationName, got, len(ConfigArtifactDeriveTriggers()))
	}
	for _, point := range data.DataPoints {
		assertConfigArtifactDeriveKeys(t, configArtifactDeriveDurationName, point.Attributes, []string{"trigger"})
		if !slices.Equal(point.Bounds, ConfigArtifactDeriveDurationBuckets()) {
			t.Fatalf("%s bounds = %v, want %v", configArtifactDeriveDurationName, point.Bounds, ConfigArtifactDeriveDurationBuckets())
		}
	}
}

// TestConfigArtifactDeriveDurationBucketsCoverTheContract pins the bucket
// list and the boundaries the derive contract reads directly: the 1 ms
// round-trip floor of a tiny repository, the 2 s S1 per-repository cap, the
// 10 s default deadline, and the 15 s caller-facing worst case (deadline plus
// the 5 s failure-record bound).
func TestConfigArtifactDeriveDurationBucketsCoverTheContract(t *testing.T) {
	t.Parallel()

	want := []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30}
	got := ConfigArtifactDeriveDurationBuckets()
	if !slices.Equal(got, want) {
		t.Fatalf("ConfigArtifactDeriveDurationBuckets() = %v, want %v", got, want)
	}
	if !slices.IsSorted(got) {
		t.Fatalf("buckets not ascending: %v", got)
	}
	for _, boundary := range []float64{0.001, 2, 10, 15} {
		if !slices.Contains(got, boundary) {
			t.Fatalf("buckets %v miss contract boundary %v", got, boundary)
		}
	}
	got[0] = 99
	if ConfigArtifactDeriveDurationBuckets()[0] != 0.001 {
		t.Fatal("ConfigArtifactDeriveDurationBuckets returned a shared slice")
	}
}

func findConfigArtifactDeriveMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %s not collected", name)
	return metricdata.Metrics{}
}

func assertConfigArtifactDeriveKeys(t *testing.T, name string, set attribute.Set, want []string) {
	t.Helper()
	keys := make([]string, 0, set.Len())
	for _, kv := range set.ToSlice() {
		keys = append(keys, string(kv.Key))
	}
	slices.Sort(keys)
	if !slices.Equal(keys, want) {
		t.Fatalf("%s label keys = %v, want %v", name, keys, want)
	}
}

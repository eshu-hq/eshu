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

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
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

// TestConfigArtifactReconcileOutcomesAreClosed pins the closed outcome set
// of the config-artifact walker counter (#8155 PR3b-2): the five outcomes of
// the decision record section 3.5, in declaration order.
func TestConfigArtifactReconcileOutcomesAreClosed(t *testing.T) {
	t.Parallel()

	outcomes := ConfigArtifactReconcileOutcomes()
	want := []string{"match", "repaired", "superseded", "error", "skipped"}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("ConfigArtifactReconcileOutcomes() = %v, want %v", outcomes, want)
	}

	// The accessor returns a copy: a caller cannot widen the closed set.
	outcomes[0] = "mutated"
	if ConfigArtifactReconcileOutcomes()[0] != "match" {
		t.Fatal("reconcile outcome accessor returned a shared slice")
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

// TestConfigArtifactShadowCompareLabelValuesAreClosed pins the closed outcome
// set of the config-artifact shadow compare counter (#8155 PR3c).
func TestConfigArtifactShadowCompareLabelValuesAreClosed(t *testing.T) {
	t.Parallel()

	if want := []string{"match", "mismatch", "stored_missing"}; !slices.Equal(ConfigArtifactShadowCompareOutcomes(), want) {
		t.Fatalf("ConfigArtifactShadowCompareOutcomes() = %v, want %v", ConfigArtifactShadowCompareOutcomes(), want)
	}
	outcomes := ConfigArtifactShadowCompareOutcomes()
	outcomes[0] = "mutated"
	if ConfigArtifactShadowCompareOutcomes()[0] != "match" {
		t.Fatal("label value accessor returned a shared slice")
	}
}

// TestConfigArtifactShadowCompareLabelsMatchRowConstants pins the telemetry
// outcome set to the neutral package's shadow compare alphabet (#8155 PR3c):
// a renamed, added or dropped outcome must change both sides, or the wiring
// counts a label the compare never returns (or drops one it does).
func TestConfigArtifactShadowCompareLabelsMatchRowConstants(t *testing.T) {
	t.Parallel()

	rows := []string{
		contentartifacts.ShadowOutcomeMatch,
		contentartifacts.ShadowOutcomeMismatch,
		contentartifacts.ShadowOutcomeStoredMissing,
	}
	if got := ConfigArtifactShadowCompareOutcomes(); !slices.Equal(got, rows) {
		t.Fatalf("telemetry outcomes = %v, row outcomes = %v", got, rows)
	}
}

// TestConfigArtifactReadLabelValuesAreClosed pins the closed source and
// reason sets of the config-artifact reads counter (#8155 PR3c stored
// cutover). stored/trusted is a repository served from trusted stored rows;
// live carries the fallback reason, mode_live when the mode serves everything
// live, stored_error when the whole stored read failed.
func TestConfigArtifactReadLabelValuesAreClosed(t *testing.T) {
	t.Parallel()

	if want := []string{"stored", "live"}; !slices.Equal(ConfigArtifactReadSources(), want) {
		t.Fatalf("ConfigArtifactReadSources() = %v, want %v", ConfigArtifactReadSources(), want)
	}
	want := []string{"trusted", "missing", "stale_version", "stale_extractor", "failed", "mode_live", "stored_error"}
	if got := ConfigArtifactReadReasons(); !slices.Equal(got, want) {
		t.Fatalf("ConfigArtifactReadReasons() = %v, want %v", got, want)
	}

	// The accessors return copies: a caller cannot widen the closed set.
	sources := ConfigArtifactReadSources()
	reasons := ConfigArtifactReadReasons()
	sources[0] = "mutated"
	reasons[0] = "mutated"
	if ConfigArtifactReadSources()[0] != "stored" || ConfigArtifactReadReasons()[0] != "trusted" {
		t.Fatal("label value accessors returned shared slices")
	}
}

// TestConfigArtifactReadReasonsMatchStorageConstants pins the telemetry
// fallback reasons to the storage read-model reasons (#8155 PR3c): a renamed,
// added or dropped reason must change both sides, or the wiring counts a
// label the stored reader never returns (or drops one it does). Every
// declared storage Reason has a pinned disposition below, so a new constant
// has nowhere to hide: list it here with its expected label or absence.
func TestConfigArtifactReadReasonsMatchStorageConstants(t *testing.T) {
	t.Parallel()

	// The full declared storage set, with the intended telemetry
	// disposition of each: fallback reasons map 1:1 to the label,
	// ReasonTrusted maps to the "trusted" label, and ReasonRetry must stay
	// absent because Classify never returns it (the rows are still exact).
	storage := map[artifacts.Reason]string{
		artifacts.ReasonMissing:        "missing",
		artifacts.ReasonStaleVersion:   "stale_version",
		artifacts.ReasonStaleExtractor: "stale_extractor",
		artifacts.ReasonFailed:         "failed",
		artifacts.ReasonTrusted:        "trusted",
	}
	reasons := map[string]bool{}
	for _, reason := range ConfigArtifactReadReasons() {
		reasons[reason] = true
	}
	for reason, label := range storage {
		if string(reason) != "" && string(reason) != label && reason != artifacts.ReasonTrusted {
			t.Fatalf("storage reason has value %q: the adapter maps reasons 1:1, so the label must be identical", string(reason))
		}
		if !reasons[label] {
			t.Fatalf("telemetry reasons lack the label %q for storage reason %q", label, string(reason))
		}
	}
	if reasons[string(artifacts.ReasonRetry)] {
		t.Fatalf("telemetry reasons carry %q, which Classify never returns", string(artifacts.ReasonRetry))
	}
	if reasons[string(artifacts.ReasonTrusted)] {
		t.Fatalf("telemetry reasons carry the raw trusted value %q; it must map to %q", string(artifacts.ReasonTrusted), "trusted")
	}
	// Exact set equality in the other direction: every telemetry reason is
	// either a pinned storage label above or a declared wiring-only reason.
	// A fifth storage reason flowing 1:1 into a label fails here.
	wiring := map[string]bool{"mode_live": true, "stored_error": true}
	for _, reason := range ConfigArtifactReadReasons() {
		pinned := wiring[reason]
		for _, label := range storage {
			if reason == label {
				pinned = true
			}
		}
		if !pinned {
			t.Fatalf("telemetry reason %q is neither a pinned storage label nor a wiring reason", reason)
		}
	}
}

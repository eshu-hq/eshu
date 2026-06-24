package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDrainCollectorEmitsContentEntityCounterByFileKind drives the real
// drainCollector loop with a live SDK meter and asserts that the
// eshu_dp_content_entity_emitted_total counter is incremented per
// source_file_kind from the collected DiscoveryAdvisory. This is the
// end-to-end wiring (advisory -> metric) that would have surfaced the #3676
// lockfile explosion from the metrics port without manual SQL.
func TestDrainCollectorEmitsContentEntityCounterByFileKind(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("bootstrap-index-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	source := &fakeSource{generations: []collector.CollectedGeneration{
		{
			Scope:      scope.IngestionScope{ScopeID: "scope-noisy"},
			Generation: scope.ScopeGeneration{GenerationID: "gen-1"},
			FactCount:  0,
			DiscoveryAdvisory: &collector.DiscoveryAdvisoryReport{
				SchemaVersion: "discovery_advisory.v1",
				Run:           collector.DiscoveryAdvisoryRun{RepoPath: "/repo"},
				EntityCounts: collector.DiscoveryAdvisoryEntityCount{
					BySourceFileKind: map[string]int{
						telemetry.SourceFileKindCode:            40,
						telemetry.SourceFileKindPackageManifest: 900, // lockfile explosion
						telemetry.SourceFileKindConfig:          12,
					},
				},
			},
		},
	}}

	if err := drainCollector(
		context.Background(),
		source,
		&fakeCommitter{},
		nil,
		instruments,
		nil,
	); err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	wantByKind := map[string]int64{
		telemetry.SourceFileKindCode:            40,
		telemetry.SourceFileKindPackageManifest: 900,
		telemetry.SourceFileKindConfig:          12,
	}
	for kind, want := range wantByKind {
		got := contentEntityEmittedValue(t, rm, kind)
		if got != want {
			t.Errorf("content_entity_emitted_total[%s] = %d, want %d", kind, got, want)
		}
	}
}

// TestRunPipelinedEmitsBootstrapPhaseTimings drives runPipelined with a live
// SDK meter and asserts that every non-collection bootstrap phase records a
// data point on eshu_dp_bootstrap_pipeline_phase_seconds. Collection timing is
// recorded inside drainCollector and verified separately; here we prove the
// post-collection phases (backfill, projection, iac_reachability,
// config_state_drift) each emit their histogram point.
func TestRunPipelinedEmitsBootstrapPhaseTimings(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("bootstrap-index-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	source := &fakeSource{generations: []collector.CollectedGeneration{
		{Scope: scope.IngestionScope{ScopeID: "s1"}, FactCount: 0},
	}}
	ws := &concurrentWorkSource{
		items: []projector.ScopeGenerationWork{
			{Scope: scope.IngestionScope{ScopeID: "s1"}},
		},
	}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	if err := runPipelined(context.Background(), cd, pd, 2, nil, instruments, nil); err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	wantPhases := []string{
		telemetry.BootstrapPhaseCollection,
		telemetry.BootstrapPhaseRelationshipBackfill,
		telemetry.BootstrapPhaseProjection,
		telemetry.BootstrapPhaseIaCReachability,
		telemetry.BootstrapPhaseConfigStateDrift,
	}
	for _, phase := range wantPhases {
		if !bootstrapPhaseRecorded(t, rm, phase) {
			t.Errorf("bootstrap_pipeline_phase_seconds missing data point for phase %q", phase)
		}
	}
}

// contentEntityEmittedValue returns the counter value for
// eshu_dp_content_entity_emitted_total at the given source_file_kind under the
// bootstrap-index collector_kind.
func contentEntityEmittedValue(t *testing.T, rm metricdata.ResourceMetrics, kind string) int64 {
	t.Helper()
	const name = "eshu_dp_content_entity_emitted_total"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Sum[int64]", name, m.Data)
			}
			for _, p := range sum.DataPoints {
				if metricPointHasAttrs(p.Attributes, map[string]string{
					telemetry.MetricDimensionSourceFileKind: kind,
					telemetry.MetricDimensionCollectorKind:  "bootstrap-index",
				}) {
					return p.Value
				}
			}
		}
	}
	t.Fatalf("metric %s for source_file_kind %q not found", name, kind)
	return 0
}

// bootstrapPhaseRecorded reports whether a histogram data point exists for the
// named phase on eshu_dp_bootstrap_pipeline_phase_seconds.
func bootstrapPhaseRecorded(t *testing.T, rm metricdata.ResourceMetrics, phase string) bool {
	t.Helper()
	const name = "eshu_dp_bootstrap_pipeline_phase_seconds"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", name, m.Data)
			}
			for _, p := range hist.DataPoints {
				if metricPointHasAttrs(p.Attributes, map[string]string{
					telemetry.MetricDimensionBootstrapPhase: phase,
					telemetry.MetricDimensionCollectorKind:  "bootstrap-index",
				}) {
					return true
				}
			}
		}
	}
	return false
}

// metricPointHasAttrs reports whether every want key/value pair is present in
// the data point attribute set.
func metricPointHasAttrs(set attribute.Set, want map[string]string) bool {
	for k, v := range want {
		val, ok := set.Value(attribute.Key(k))
		if !ok || val.AsString() != v {
			return false
		}
	}
	return true
}

package main

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDrainCollectorEmitsContentEntityCounterByFileKind covers ONLY the
// advisory -> metric wiring layer: given a collected DiscoveryAdvisory whose
// EntityCounts.BySourceFileKind is already populated (the collector builds it
// upstream), drainCollector must increment eshu_dp_content_entity_emitted_total
// once per bounded source_file_kind with the right value and labels.
//
// The parser -> classifier -> BySourceFileKind path (the part that actually
// decides a go.mod dependency is package_manifest) is proven separately by
// TestDiscoveryAdvisoryClassifiesRealManifestAndConfigFixtures in the collector
// package against REAL fixtures. This test deliberately does not re-derive the
// classification; it asserts the emission is faithful to the advisory it is
// handed.
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
		telemetry.BootstrapPhaseDeploymentReopen,
		telemetry.BootstrapPhaseConfigStateDrift,
	}
	for _, phase := range wantPhases {
		if !bootstrapPhaseRecorded(t, rm, phase) {
			t.Errorf("bootstrap_pipeline_phase_seconds missing data point for phase %q", phase)
		}
	}
}

// TestRunPipelinedRecordsPhaseDurationOnError proves the #3678 P2(a) fix: a
// post-collection phase that FAILS still records its duration, so an operator
// can see which phase was the long pole even when it errors out. Here the IaC
// reachability phase returns an error; the iac_reachability histogram point must
// still be present.
func TestRunPipelinedRecordsPhaseDurationOnError(t *testing.T) {
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

	committer := &fakeCommitter{iacErr: errInjectedIaCFailure}
	cd := collectorDeps{source: source, committer: committer}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	err = runPipelined(context.Background(), cd, pd, 2, nil, instruments, nil)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want injected IaC failure")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// The failing phase must still have recorded its duration.
	if !bootstrapPhaseRecorded(t, rm, telemetry.BootstrapPhaseIaCReachability) {
		t.Error("iac_reachability phase duration not recorded on error path (P2(a) regression)")
	}
	// Phases that completed before the failure must also be present.
	for _, phase := range []string{
		telemetry.BootstrapPhaseCollection,
		telemetry.BootstrapPhaseRelationshipBackfill,
		telemetry.BootstrapPhaseProjection,
	} {
		if !bootstrapPhaseRecorded(t, rm, phase) {
			t.Errorf("phase %q not recorded before the failing phase", phase)
		}
	}
}

// TestRunPipelinedProjectionPhaseExcludesBackfillWait proves the #3678 P2#1
// fix: the projection phase duration is measured from projector start to
// projector completion and must NOT fold in the relationship-backfill wait that
// runs after the projector finishes. A slow backfill is injected; the recorded
// projection duration must stay well below the backfill duration.
func TestRunPipelinedProjectionPhaseExcludesBackfillWait(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("bootstrap-index-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	// Empty queue: the projector enters drain mode and completes after the fixed
	// drain wait (maxEmptyPolls * pollInterval ~= 2.5s in drainProjectorPipelined).
	// The injected backfill delay is chosen LARGER than that drain so the bug is
	// observable: under the old time.Since-after-backfill code, the projection
	// phase would be recorded at the post-backfill receive point and thus include
	// the (longer) backfill wait; under the fix it equals the projector's own
	// completion time.
	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	// Backfill runs concurrently with the projector drain and is made the long
	// pole so old vs new projection timing diverge measurably.
	const backfillDelay = 4 * time.Second
	const projectionUpperBound = 3.5 // seconds; > ~2.5s drain, < backfillDelay
	committer := &fakeCommitter{backfillDelay: backfillDelay}
	cd := collectorDeps{source: source, committer: committer}
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

	projectionSeconds := bootstrapPhaseDurationSeconds(t, rm, telemetry.BootstrapPhaseProjection)
	backfillSeconds := bootstrapPhaseDurationSeconds(t, rm, telemetry.BootstrapPhaseRelationshipBackfill)

	// The injected backfill delay must show up in the backfill phase.
	if backfillSeconds < backfillDelay.Seconds()*0.8 {
		t.Errorf("relationship_backfill = %.3fs, want >= ~%.3fs (delay not attributed to backfill)",
			backfillSeconds, backfillDelay.Seconds())
	}
	// The projection phase must reflect ONLY the projector's own wall time, not
	// the longer backfill wait. Old bug: projection >= backfillDelay (~4s).
	// Fixed: projection ~= drain time (~2.5s) < projectionUpperBound.
	if projectionSeconds >= projectionUpperBound {
		t.Errorf("projection = %.3fs, want < %.1fs: backfill wait leaked into projection (P2#1 regression)",
			projectionSeconds, projectionUpperBound)
	}
}

// TestRunPipelinedRecordsDeploymentReopenPhaseOnError proves the #3678 P2#2
// fix: ReopenDeploymentMappingWorkItems now runs inside its own bounded
// deployment_reopen phase, recorded even when it errors, so the reopen step is
// independently identifiable as a long pole instead of an unaccounted gap.
func TestRunPipelinedRecordsDeploymentReopenPhaseOnError(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("bootstrap-index-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	committer := &fakeCommitter{reopenErr: errInjected("injected reopen failure")}
	cd := collectorDeps{source: source, committer: committer}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	if err := runPipelined(context.Background(), cd, pd, 2, nil, instruments, nil); err == nil {
		t.Fatal("runPipelined() error = nil, want injected reopen failure")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if !bootstrapPhaseRecorded(t, rm, telemetry.BootstrapPhaseDeploymentReopen) {
		t.Error("deployment_reopen phase duration not recorded on error path (P2#2 regression)")
	}
	// config_state_drift runs after reopen, so a reopen failure must NOT have
	// recorded it (the failing reopen short-circuits the pipeline).
	if bootstrapPhaseRecorded(t, rm, telemetry.BootstrapPhaseConfigStateDrift) {
		t.Error("config_state_drift recorded despite reopen failure; ordering is wrong")
	}
}

var errInjectedIaCFailure = errInjected("injected iac reachability failure")

type errInjected string

func (e errInjected) Error() string { return string(e) }

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

// bootstrapPhaseDurationSeconds returns the recorded histogram Sum (total
// seconds) for the named phase. A single Record per run means Sum equals that
// run's measured duration, which lets a test assert relative phase magnitudes.
func bootstrapPhaseDurationSeconds(t *testing.T, rm metricdata.ResourceMetrics, phase string) float64 {
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
					return p.Sum
				}
			}
		}
	}
	t.Fatalf("metric %s for phase %q not found", name, phase)
	return 0
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

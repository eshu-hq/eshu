// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRecordContentSearchIndexFinalizationDurationPublishesBootstrapPhase(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("content-index-finalization-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	recordContentSearchIndexFinalizationDuration(
		context.Background(),
		instruments,
		time.Now().Add(-time.Second),
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !bootstrapPhaseRecorded(t, rm, telemetry.BootstrapPhaseContentIndexFinalization) {
		t.Fatal("bootstrap phase histogram missing content index finalization")
	}
}

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
			Scope:              scope.IngestionScope{ScopeID: "scope-noisy"},
			Generation:         scope.ScopeGeneration{GenerationID: "gen-1"},
			EstimatedFactCount: 0,
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
		1,
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
		{Scope: scope.IngestionScope{ScopeID: "s1"}, EstimatedFactCount: 0},
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
		{Scope: scope.IngestionScope{ScopeID: "s1"}, EstimatedFactCount: 0},
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

// TestRunPipelinedLogsRelationshipBackfillPhaseStartBeforeCompletion proves the
// #4271 fix: runPipelined must emit an explicit "bootstrap phase start" log
// signal for relationship_backfill BEFORE BackfillAllRelationshipEvidence
// RETURNS — the exact silent gap #4271 describes, where operators watching
// logs see "bootstrap projection complete" and then nothing until the
// (possibly very long) backfill call returns.
//
// An earlier version of this test only asserted that the "bootstrap phase
// start" log line's byte offset preceded the "bootstrap phase complete" log
// line's offset. That is necessary but not sufficient: "bootstrap phase
// complete" is logged AFTER BackfillAllRelationshipEvidence returns, so a
// regression that moved the start-log call to right after the (still
// blocking) call returns but before the completion log would still satisfy
// startIdx < completeIdx and pass — without ever proving the signal fires
// while the call is in flight (review finding on PR #4521; codex + Copilot,
// same gap, 3 comments).
//
// This version closes that gap directly: the fake committer's
// BackfillAllRelationshipEvidence blocks on a channel the instant it is
// entered. runPipelined runs in a goroutine; the test waits for that
// channel, then asserts the "bootstrap phase start" line is ALREADY present
// in the captured logs while the call is still blocked (release has not
// been signaled), before letting the call return. This proves the start
// signal is emitted before/during the blocked call, not merely before a
// later log line.
func TestRunPipelinedLogsRelationshipBackfillPhaseStartBeforeCompletion(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(lockedWriter{mu: &mu, w: &logs}, nil))

	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	started := make(chan struct{})
	release := make(chan struct{})
	committer := &fakeCommitter{backfillStarted: started, backfillRelease: release}
	cd := collectorDeps{source: source, committer: committer}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	done := make(chan error, 1)
	go func() {
		done <- runPipelined(context.Background(), cd, pd, 2, nil, nil, logger)
	}()

	select {
	case <-started:
		// BackfillAllRelationshipEvidence has been entered and is now
		// blocked on release. Fall through to assert on the logs while it
		// is still blocked.
	case err := <-done:
		t.Fatalf("runPipelined() returned (err=%v) before BackfillAllRelationshipEvidence was ever entered", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for BackfillAllRelationshipEvidence to start")
	}

	backfillPhaseAttr := `"bootstrap_phase":"` + telemetry.BootstrapPhaseRelationshipBackfill + `"`
	mu.Lock()
	snapshot := logs.String()
	mu.Unlock()
	found := false
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.Contains(line, backfillPhaseAttr) && strings.Contains(line, `"msg":"bootstrap phase start"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("logs while backfill still blocked = %q, want a %q line with %s BEFORE the call returns",
			snapshot, "bootstrap phase start", backfillPhaseAttr)
	}
	// The completion log for this phase must NOT exist yet: the call has not
	// returned, so recordPhase(relationship_backfill, ...) cannot have run.
	// This is the assertion the byte-offset-only version of this test could
	// never make, because it only inspected logs after runPipelined had
	// already returned.
	for _, line := range strings.Split(snapshot, "\n") {
		if strings.Contains(line, backfillPhaseAttr) && strings.Contains(line, `"msg":"bootstrap phase complete"`) {
			t.Fatalf("logs while backfill still blocked = %q, unexpectedly already contain the phase complete line for %s; the call has not returned yet", snapshot, backfillPhaseAttr)
		}
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runPipelined() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runPipelined to finish after releasing backfill")
	}
}

// lockedWriter serializes writes with an external mutex so a test can safely
// read the underlying buffer's contents from the test goroutine while
// runPipelined's goroutine may still be writing log lines concurrently.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// --- pipelined bootstrap tests ---

func TestPipelinedBootstrapProjectsDuringCollection(t *testing.T) {
	t.Parallel()

	// Simulate a slow collector that produces 5 scopes with a delay between each.
	// The projector should start processing items while the collector is still running.
	source := &slowSource{
		generations: []collector.CollectedGeneration{
			{Scope: scope.IngestionScope{ScopeID: "s1"}, EstimatedFactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s2"}, EstimatedFactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s3"}, EstimatedFactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s4"}, EstimatedFactCount: 0},
			{Scope: scope.IngestionScope{ScopeID: "s5"}, EstimatedFactCount: 0},
		},
		delay: 100 * time.Millisecond,
	}

	// Track when projections happen relative to collection.
	tracker := &projectionTracker{}

	ws := &concurrentWorkSource{
		items: []projector.ScopeGenerationWork{
			{Scope: scope.IngestionScope{ScopeID: "s1"}},
			{Scope: scope.IngestionScope{ScopeID: "s2"}},
			{Scope: scope.IngestionScope{ScopeID: "s3"}},
			{Scope: scope.IngestionScope{ScopeID: "s4"}},
			{Scope: scope.IngestionScope{ScopeID: "s5"}},
		},
	}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     tracker,
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	if got := sink.acked.Load(); got != 5 {
		t.Fatalf("runPipelined() acked = %d, want 5", got)
	}

	// Verify projections started before all collections finished.
	// Collection takes ~500ms (5 × 100ms). If projections started during collection,
	// the first projection timestamp should be before collection ends.
	firstProjection := tracker.firstProjectionTime()
	if firstProjection.IsZero() {
		t.Fatal("no projections were recorded")
	}
	collectionDone := source.collectionFinishedTime()
	if collectionDone.IsZero() {
		t.Fatal("collection completion was not recorded")
	}
	if !firstProjection.Before(collectionDone) {
		t.Fatalf("first projection at %s, want before collection completed at %s", firstProjection, collectionDone)
	}
}

func TestPipelinedBootstrapDrainsQueueAfterCollectorExits(t *testing.T) {
	t.Parallel()

	// Collector finishes immediately with 0 items.
	// Queue has items that were pre-populated (simulating items from a previous
	// collector run or items that appeared just before collector exited).
	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{
		items: []projector.ScopeGenerationWork{
			{Scope: scope.IngestionScope{ScopeID: "s1"}},
			{Scope: scope.IngestionScope{ScopeID: "s2"}},
			{Scope: scope.IngestionScope{ScopeID: "s3"}},
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

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	if got := sink.acked.Load(); got != 3 {
		t.Fatalf("runPipelined() acked = %d, want 3 (should drain remaining queue)", got)
	}
}

func TestPipelinedBootstrapExitsCleanlyWhenQueueEmpty(t *testing.T) {
	t.Parallel()

	// Both collector and queue are empty — should exit cleanly and quickly.
	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	start := time.Now()
	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 0 {
		t.Fatalf("runPipelined() acked = %d, want 0", got)
	}
	// Should exit within a few seconds (maxEmptyPolls × pollInterval).
	if elapsed > 10*time.Second {
		t.Fatalf("runPipelined() took %v, want < 10s (drain should exit quickly)", elapsed)
	}
}

func TestPipelinedBootstrapCollectorErrorCancelsProjector(t *testing.T) {
	t.Parallel()

	collectorErr := errors.New("collector exploded")
	source := &failingSource{err: collectorErr}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}

	cd := collectorDeps{source: source, committer: &fakeCommitter{}}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err == nil {
		t.Fatal("runPipelined() expected error, got nil")
	}
	if !errors.Is(err, collectorErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, collectorErr)
	}
}

func TestPipelinedBootstrapRunsDeferredBackfillWorkflow(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: nil}
	ws := &concurrentWorkSource{items: nil}
	sink := &concurrentWorkSink{}
	committer := &fakeCommitter{}

	cd := collectorDeps{source: source, committer: committer}
	pd := projectorDeps{
		workSource: ws,
		factStore:  &fakeFactStore{},
		runner:     &fakeProjectionRunner{},
		workSink:   sink,
	}

	err := runPipelined(context.Background(), cd, pd, 2, nil, nil, nil)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}

	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen", "reopen_code_import", "reopen_correlation", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
	if got := sink.acked.Load(); got != 0 {
		t.Fatalf("runPipelined() acked = %d, want 0", got)
	}
}

func TestPipelinedBootstrapBackfillFailureIsFatal(t *testing.T) {
	t.Parallel()

	backfillErr := errors.New("backfill failed")
	committer := &fakeCommitter{backfillErr: backfillErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, backfillErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, backfillErr)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapIaCReachabilityFailureIsFatal(t *testing.T) {
	t.Parallel()

	iacErr := errors.New("iac reachability failed")
	committer := &fakeCommitter{iacErr: iacErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, iacErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, iacErr)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapReopenFailureIsFatal(t *testing.T) {
	t.Parallel()

	reopenErr := errors.New("reopen failed")
	committer := &fakeCommitter{reopenErr: reopenErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, reopenErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, reopenErr)
	}
	// enqueue_drift must NOT be called when reopen fails — the pipeline
	// returns before Phase 3.5 runs.
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapDriftEnqueueFailureIsFatal(t *testing.T) {
	t.Parallel()

	driftErr := errors.New("drift enqueue failed")
	committer := &fakeCommitter{driftEnqueueErr: driftErr}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, driftErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, driftErr)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen", "reopen_code_import", "reopen_correlation", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapWaitsForProjectorDrainBeforeReopen(t *testing.T) {
	t.Parallel()

	sink := &concurrentWorkSink{}
	committer := &fakeCommitter{}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: committer},
		projectorDeps{
			workSource: &concurrentWorkSource{
				items: []projector.ScopeGenerationWork{
					{Scope: scope.IngestionScope{ScopeID: "s1"}},
				},
			},
			factStore: &fakeFactStore{},
			runner:    &delayedProjectionRunner{delay: 50 * time.Millisecond},
			workSink:  sink,
		},
		2,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}
	// Projector must have drained (acked the work item) before reopen ran.
	if got := sink.acked.Load(); got != 1 {
		t.Fatalf("projector not drained before reopen: acked=%d, want 1", got)
	}
	if got, want := committer.snapshotCalls(), []string{"backfill", "iac_reachability", "reopen", "reopen_code_import", "reopen_correlation", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapHeartbeatsLongProjectorWork(t *testing.T) {
	t.Parallel()

	heartbeater := &recordingProjectorHeartbeater{}
	started := make(chan struct{})
	release := make(chan struct{})

	errc := make(chan error, 1)
	go func() {
		errc <- runPipelined(
			context.Background(),
			collectorDeps{source: &fakeSource{generations: nil}, committer: &fakeCommitter{}},
			projectorDeps{
				workSource: &concurrentWorkSource{
					items: []projector.ScopeGenerationWork{{
						Scope:      scope.IngestionScope{ScopeID: "scope-long"},
						Generation: scope.ScopeGeneration{GenerationID: "generation-long"},
					}},
				},
				factStore:         &fakeFactStore{},
				runner:            &blockingProjectionRunner{started: started, release: release},
				workSink:          &concurrentWorkSink{},
				heartbeater:       heartbeater,
				heartbeatInterval: time.Millisecond,
			},
			1,
			nil,
			nil,
			nil,
		)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("projection did not start")
	}
	if !heartbeater.waitForHeartbeats(1, time.Second) {
		t.Fatalf("heartbeat count = %d, want at least 1 before projection completes", heartbeater.count())
	}
	close(release)

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("runPipelined() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPipelined() did not return")
	}
}

func TestBootstrapProjectorHeartbeatStopIgnoresStopContextCancellation(t *testing.T) {
	t.Parallel()

	heartbeater := &contextCanceledProjectorHeartbeater{
		entered: make(chan struct{}),
	}
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-heartbeat"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-heartbeat"},
	}

	_, stopHeartbeat := startBootstrapProjectorHeartbeat(
		context.Background(),
		work,
		heartbeater,
		time.Millisecond,
		0,
		nil,
	)

	select {
	case <-heartbeater.entered:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not start")
	}

	if err := stopHeartbeat(); err != nil {
		t.Fatalf("stopHeartbeat() error = %v, want nil", err)
	}
}

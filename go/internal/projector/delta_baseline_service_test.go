// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func deltaBaselineServiceWork() ScopeGenerationWork {
	return ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID: "scope-7319", SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: "repo-7319",
		},
		Generation: scope.ScopeGeneration{
			ScopeID: "scope-7319", GenerationID: "gen-d",
			ObservedAt: time.Date(2026, time.September, 28, 1, 0, 0, 0, time.UTC),
			IngestedAt: time.Date(2026, time.September, 28, 1, 0, 0, 0, time.UTC),
			Status:     scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		},
		AttemptCount: 1,
	}
}

type deltaBaselineServiceProbe struct {
	factStore   *stubFactStore
	runner      *stubProjectionRunner
	sink        *stubProjectorWorkSink
	heartbeater *stubProjectorWorkHeartbeater
	counter     *countingFactCounter
}

// countingFactCounter records whether the large-generation semaphore ran.
type countingFactCounter struct{ calls int }

func (c *countingFactCounter) CountFacts(context.Context, string, string) (int, error) {
	c.calls++
	return 1, nil
}

func runDeltaBaselineService(t *testing.T, fence DeltaBaselineFence) (deltaBaselineServiceProbe, error) {
	t.Helper()
	probe := deltaBaselineServiceProbe{
		factStore:   &stubFactStore{},
		runner:      &stubProjectionRunner{result: runtime.Result{ScopeID: "scope-7319", GenerationID: "gen-d"}},
		sink:        &stubProjectorWorkSink{},
		heartbeater: &stubProjectorWorkHeartbeater{},
		counter:     &countingFactCounter{},
	}
	service := Service{
		PollInterval:       time.Millisecond,
		WorkSource:         &stubProjectorWorkSource{workItems: []ScopeGenerationWork{deltaBaselineServiceWork()}},
		FactStore:          probe.factStore,
		Runner:             probe.runner,
		WorkSink:           probe.sink,
		Heartbeater:        probe.heartbeater,
		HeartbeatInterval:  time.Millisecond,
		DeltaBaselineFence: fence,
		FactCounter:        probe.counter,
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}
	service.InitLargeGenSemaphore()
	return probe, service.Run(context.Background())
}

// TestServicePreflightRefusalProjectsNothing is the pre-Project proof for the
// projector and ingester loop: a refused delta loads no facts, runs no
// projection (so no graph, content, or intent write), starts no heartbeat or
// semaphore, and is neither acked nor failed.
func TestServicePreflightRefusalProjectsNothing(t *testing.T) {
	t.Parallel()
	fence := refusingFence()
	probe, err := runDeltaBaselineService(t, fence)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if len(fence.refusals) != 1 || fence.refusals[0].Phase != DeltaBaselinePhasePreflight {
		t.Fatalf("refusals = %+v, want one preflight refusal", fence.refusals)
	}
	if probe.factStore.loadCalls != 0 || probe.runner.runCalls != 0 || probe.counter.calls != 0 {
		t.Fatalf("load=%d project=%d count=%d, want 0 each", probe.factStore.loadCalls,
			probe.runner.runCalls, probe.counter.calls)
	}
	if probe.sink.ackCalls != 0 || probe.sink.failCalls != 0 || probe.heartbeater.calls != 0 {
		t.Fatalf("ack=%d fail=%d heartbeat=%d, want 0 each", probe.sink.ackCalls,
			probe.sink.failCalls, probe.heartbeater.calls)
	}
}

// TestServicePreflightReadErrorFailsClosed proves a fence that cannot read does
// not project: the work goes to Fail with a retryable cause.
func TestServicePreflightReadErrorFailsClosed(t *testing.T) {
	t.Parallel()
	probe, err := runDeltaBaselineService(t, &fakeDeltaBaselineFence{readErr: errors.New("connection reset")})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if probe.factStore.loadCalls != 0 || probe.runner.runCalls != 0 || probe.sink.ackCalls != 0 {
		t.Fatalf("load=%d project=%d ack=%d, want 0 each", probe.factStore.loadCalls,
			probe.runner.runCalls, probe.sink.ackCalls)
	}
	if probe.sink.failCalls != 1 || !failure.IsRetryable(probe.sink.failedWith) {
		t.Fatalf("fail calls=%d cause=%v, want one retryable failure", probe.sink.failCalls, probe.sink.failedWith)
	}
}

// TestServicePreflightClaimLostDropsWork proves a claim lost before the mark
// is dropped without Fail, like any other lost claim.
func TestServicePreflightClaimLostDropsWork(t *testing.T) {
	t.Parallel()
	fence := refusingFence()
	fence.refuseErr = fmt.Errorf("mark: %w", failure.ErrWorkClaimLost)
	probe, err := runDeltaBaselineService(t, fence)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if probe.runner.runCalls != 0 || probe.sink.failCalls != 0 || probe.sink.ackCalls != 0 {
		t.Fatalf("project=%d fail=%d ack=%d, want 0 each", probe.runner.runCalls,
			probe.sink.failCalls, probe.sink.ackCalls)
	}
}

// TestServicePreflightPassProjects proves a passing fence leaves the loop as it
// was: load, project, and ack once.
func TestServicePreflightPassProjects(t *testing.T) {
	t.Parallel()
	fence := &fakeDeltaBaselineFence{}
	probe, err := runDeltaBaselineService(t, fence)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if fence.reads != 1 || probe.runner.runCalls != 1 || probe.sink.ackCalls != 1 {
		t.Fatalf("reads=%d project=%d ack=%d, want 1 each", fence.reads, probe.runner.runCalls, probe.sink.ackCalls)
	}
}

// TestServiceRequiresDeltaBaselineFence proves the fence is an explicit,
// required dependency: a Service without one refuses to run.
func TestServiceRequiresDeltaBaselineFence(t *testing.T) {
	t.Parallel()
	service := Service{
		WorkSource: &stubProjectorWorkSource{}, FactStore: &stubFactStore{},
		Runner: &stubProjectionRunner{}, WorkSink: &stubProjectorWorkSink{},
	}
	if err := service.Run(context.Background()); err == nil {
		t.Fatal("Run() without DeltaBaselineFence = nil, want error")
	}
}

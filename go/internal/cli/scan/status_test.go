// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scan

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvaluateReadinessTreatsActiveGenerationAsCurrentWhenDrained(t *testing.T) {
	status := PipelineStatus{
		Health:            Health{State: "healthy"},
		Queue:             Queue{Succeeded: 9},
		GenerationHistory: GenerationHistory{Active: 1},
	}

	verdict := EvaluateReadiness(status)

	if !verdict.Ready {
		t.Fatalf("verdict.Ready = false, want true; reason=%q", verdict.Reason)
	}
}

func TestEvaluateReadinessWaitsForPendingGeneration(t *testing.T) {
	status := PipelineStatus{
		Health:            Health{State: "healthy"},
		Queue:             Queue{Succeeded: 9},
		GenerationHistory: GenerationHistory{Pending: 1},
	}

	verdict := EvaluateReadiness(status)

	if verdict.Ready {
		t.Fatal("verdict.Ready = true, want false while a generation is still pending")
	}
	if verdict.Terminal {
		t.Fatal("verdict.Terminal = true, want a retryable pending-generation verdict")
	}
}

func TestEvaluateReadinessTerminalCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status PipelineStatus
		reason string
	}{
		{
			name:   "queue dead letter",
			status: PipelineStatus{Queue: Queue{DeadLetter: 1}},
			reason: "queue has dead-letter work",
		},
		{
			name:   "queue failed",
			status: PipelineStatus{Queue: Queue{Failed: 1}},
			reason: "queue has failed work",
		},
		{
			name:   "stage failed",
			status: PipelineStatus{StageSummaries: []StageSummary{{Stage: "parse", Failed: 1}}},
			reason: "stage parse has failed or dead-letter work",
		},
		{
			name:   "domain dead letter",
			status: PipelineStatus{DomainBacklogs: []DomainBacklog{{Domain: "iac", DeadLetter: 1}}},
			reason: "domain iac has failed or dead-letter work",
		},
		{
			name:   "generation failed",
			status: PipelineStatus{GenerationHistory: GenerationHistory{Failed: 1}},
			reason: "generation history has failed generations",
		},
		{
			name:   "health degraded",
			status: PipelineStatus{Health: Health{State: "Degraded", Reasons: []string{"backend slow"}}},
			reason: "backend slow",
		},
		{
			name:   "health stalled",
			status: PipelineStatus{Health: Health{State: "stalled", Reasons: []string{"no progress"}}},
			reason: "no progress",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict := EvaluateReadiness(tc.status)
			if !verdict.Terminal {
				t.Fatalf("verdict.Terminal = false, want true; reason=%q", verdict.Reason)
			}
			if verdict.Reason != tc.reason {
				t.Fatalf("verdict.Reason = %q, want %q", verdict.Reason, tc.reason)
			}
		})
	}
}

func TestEvaluateReadinessWaitsForOutstandingQueueWork(t *testing.T) {
	verdict := EvaluateReadiness(PipelineStatus{
		Health: Health{State: "healthy"},
		Queue:  Queue{Outstanding: 3, Pending: 3},
	})

	if verdict.Ready || verdict.Terminal {
		t.Fatalf("verdict = %#v, want a retryable not-ready verdict", verdict)
	}
	if verdict.Reason != "queue still has outstanding work" {
		t.Fatalf("verdict.Reason = %q, want the outstanding-work reason", verdict.Reason)
	}
}

func TestEvaluateReadinessRefusesAnEmptyGenerationHistory(t *testing.T) {
	verdict := EvaluateReadiness(PipelineStatus{Health: Health{State: "healthy"}})

	if verdict.Ready {
		t.Fatal("verdict.Ready = true, want false with no completed or active generation")
	}
	if verdict.Reason != "no completed or active generation observed" {
		t.Fatalf("verdict.Reason = %q, want the empty-history reason", verdict.Reason)
	}
}

func TestWaitForReadinessStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var fetches atomic.Int64
	rt := stubRuntime()
	rt.FetchStatus = func(Client) (PipelineStatus, error) {
		fetches.Add(1)
		return PipelineStatus{Health: Health{State: "progressing"}}, nil
	}

	_, err := waitForReadiness(
		ctx,
		*rt,
		Options{Timeout: time.Minute, PollInterval: time.Second},
		Result{StatusReport: PipelineStatus{Health: Health{State: "progressing"}}},
		time.Now(),
		time.Now(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForReadiness() error = %v, want context canceled", err)
	}
	if got := fetches.Load(); got != 0 {
		t.Fatalf("status fetches after a canceled context = %d, want 0", got)
	}
}

func TestWaitForReadinessTimesOutWithTheBlockingReason(t *testing.T) {
	rt := stubRuntime()
	rt.FetchStatus = func(Client) (PipelineStatus, error) {
		return PipelineStatus{Health: Health{State: "healthy"}, Queue: Queue{Outstanding: 1}}, nil
	}
	started := time.Now()
	rt.Now = func() time.Time { return started.Add(time.Hour) }

	_, err := waitForReadiness(
		context.Background(),
		*rt,
		Options{Timeout: time.Minute, PollInterval: time.Second},
		Result{},
		started,
		started,
	)
	if err == nil {
		t.Fatal("waitForReadiness() error = nil, want a timeout")
	}
	if !strings.Contains(err.Error(), "scan readiness timed out: queue still has outstanding work") {
		t.Fatalf("waitForReadiness() error = %q, want the timeout to carry the blocking reason", err.Error())
	}
}

func TestWaitForReadinessRecordsTimingsOnceReady(t *testing.T) {
	rt := stubRuntime()
	started := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	bootstrapDone := started.Add(30 * time.Second)
	rt.Now = func() time.Time { return started.Add(90 * time.Second) }

	result, err := waitForReadiness(
		context.Background(),
		*rt,
		Options{Timeout: time.Hour, PollInterval: time.Second},
		Result{},
		started,
		bootstrapDone,
	)
	if err != nil {
		t.Fatalf("waitForReadiness() error = %v, want nil", err)
	}
	if result.Timings.QueueZeroMS == nil || *result.Timings.QueueZeroMS != 90_000 {
		t.Fatalf("Timings.QueueZeroMS = %v, want 90000", result.Timings.QueueZeroMS)
	}
	if result.Timings.ReadinessWaitMS == nil || *result.Timings.ReadinessWaitMS != 60_000 {
		t.Fatalf("Timings.ReadinessWaitMS = %v, want 60000", result.Timings.ReadinessWaitMS)
	}
}

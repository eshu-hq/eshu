// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeCrossScopeCompletionQueue struct {
	lease          CrossScopeCompletionLease
	claimed        bool
	claimErr       error
	fanoutResult   CrossScopeCompletionResult
	fanoutErr      error
	heartbeatCalls int
	retryCalls     int
	retryAt        time.Time
	claimCalls     int
}

type drainingCrossScopeCompletionQueue struct {
	claimCalls int
}

func (q *drainingCrossScopeCompletionQueue) Claim(
	context.Context, string, time.Duration,
) (CrossScopeCompletionLease, bool, error) {
	q.claimCalls++
	if q.claimCalls > 1 {
		return CrossScopeCompletionLease{}, false, nil
	}
	return CrossScopeCompletionLease{
		EventID:        1,
		ProducerDomain: DomainContainerImageIdentity,
		LeaseOwner:     "drain-test",
		ClaimEpoch:     1,
		AttemptCount:   1,
	}, true, nil
}

func (*drainingCrossScopeCompletionQueue) Heartbeat(
	context.Context, CrossScopeCompletionLease, time.Duration,
) error {
	return nil
}

func (*drainingCrossScopeCompletionQueue) Fanout(
	context.Context, CrossScopeCompletionLease, int,
) (CrossScopeCompletionResult, error) {
	return CrossScopeCompletionResult{EventsProcessed: 1}, nil
}

func (*drainingCrossScopeCompletionQueue) Retry(
	context.Context, CrossScopeCompletionLease, error, time.Time,
) error {
	return nil
}

func (f *fakeCrossScopeCompletionQueue) Claim(
	context.Context, string, time.Duration,
) (CrossScopeCompletionLease, bool, error) {
	f.claimCalls++
	return f.lease, f.claimed, f.claimErr
}

func (f *fakeCrossScopeCompletionQueue) Heartbeat(
	context.Context, CrossScopeCompletionLease, time.Duration,
) error {
	f.heartbeatCalls++
	return nil
}

func (f *fakeCrossScopeCompletionQueue) Fanout(
	context.Context, CrossScopeCompletionLease, int,
) (CrossScopeCompletionResult, error) {
	return f.fanoutResult, f.fanoutErr
}

func (f *fakeCrossScopeCompletionQueue) Retry(
	_ context.Context,
	_ CrossScopeCompletionLease,
	_ error,
	visibleAt time.Time,
) error {
	f.retryCalls++
	f.retryAt = visibleAt
	return nil
}

func TestCrossScopeCompletionRunnerRunOnceFanout(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 20, 0, 0, 0, time.UTC)
	queue := &fakeCrossScopeCompletionQueue{
		claimed: true,
		lease: CrossScopeCompletionLease{
			EventID:        7,
			ProducerDomain: DomainContainerImageIdentity,
			ClaimEpoch:     2,
			AttemptCount:   1,
		},
		fanoutResult: CrossScopeCompletionResult{EventsProcessed: 3, IntentsEnqueued: 18},
	}
	runner := CrossScopeCompletionRunner{
		Queue:      queue,
		LeaseOwner: "runner-test",
		Now:        func() time.Time { return now },
	}

	processed, result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed || result.EventsProcessed != 3 || result.IntentsEnqueued != 18 {
		t.Fatalf("RunOnce() = %v, %+v, want processed 3/18", processed, result)
	}
	if queue.retryCalls != 0 {
		t.Fatalf("Retry() calls = %d, want 0", queue.retryCalls)
	}
}

func TestCrossScopeCompletionRunnerRunOnceRetriesIndefinitely(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 20, 0, 0, 0, time.UTC)
	cause := errors.New("synthetic fanout failure")
	queue := &fakeCrossScopeCompletionQueue{
		claimed: true,
		lease: CrossScopeCompletionLease{
			EventID:        9,
			ProducerDomain: DomainCICDRunCorrelation,
			ClaimEpoch:     4,
			AttemptCount:   7,
		},
		fanoutErr: cause,
	}
	runner := CrossScopeCompletionRunner{
		Queue:      queue,
		LeaseOwner: "runner-test",
		Now:        func() time.Time { return now },
		RetryDelay: time.Second,
	}

	processed, _, err := runner.RunOnce(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("RunOnce() error = %v, want %v", err, cause)
	}
	if processed {
		t.Fatal("RunOnce() processed = true after failed fanout")
	}
	if queue.retryCalls != 1 {
		t.Fatalf("Retry() calls = %d, want 1", queue.retryCalls)
	}
	if got, want := queue.retryAt, now.Add(64*time.Second); !got.Equal(want) {
		t.Fatalf("Retry() visibleAt = %s, want %s", got, want)
	}
}

func TestCrossScopeCompletionRunnerDrainsBacklogBeforePolling(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	queue := &drainingCrossScopeCompletionQueue{}
	waitCalls := 0
	runner := CrossScopeCompletionRunner{
		Queue:      queue,
		LeaseOwner: "drain-test",
		Wait: func(context.Context, time.Duration) error {
			waitCalls++
			cancel()
			return context.Canceled
		},
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if queue.claimCalls != 2 {
		t.Fatalf("Claim() calls = %d, want 2 (one work item, then empty)", queue.claimCalls)
	}
	if waitCalls != 1 {
		t.Fatalf("Wait() calls = %d, want 1 after queue drained", waitCalls)
	}
}

func TestCrossScopeCompletionRunnerBacksOffAndJittersIdlePolling(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	queue := &fakeCrossScopeCompletionQueue{}
	delays := make([]time.Duration, 0, 4)
	runner := CrossScopeCompletionRunner{
		Queue:        queue,
		LeaseOwner:   "idle-owner-a",
		PollInterval: 250 * time.Millisecond,
		Wait: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			if len(delays) == 4 {
				cancel()
				return context.Canceled
			}
			return nil
		},
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() idle backoff error = %v", err)
	}
	if len(delays) != 4 || queue.claimCalls != 4 {
		t.Fatalf("idle polling = delays:%v claims:%d, want four of each", delays, queue.claimCalls)
	}
	for index := 1; index < len(delays); index++ {
		if delays[index] <= delays[index-1] {
			t.Fatalf("idle delays = %v, want increasing backoff", delays)
		}
	}
	other := runner
	other.LeaseOwner = "idle-owner-b"
	if runner.jitteredIdleDelay(time.Second) == other.jitteredIdleDelay(time.Second) {
		t.Fatal("distinct lease owners received identical stable idle jitter")
	}
}

func TestServiceStartsCrossScopeCompletionRunner(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	claimed := make(chan struct{}, 1)
	queue := &fakeCrossScopeCompletionQueue{}
	runner := &CrossScopeCompletionRunner{
		Queue:      queue,
		LeaseOwner: "side-runner-test",
		Wait: func(context.Context, time.Duration) error {
			select {
			case claimed <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{CrossScopeCompletionRunner: runner}
	var wg sync.WaitGroup
	service.startSideRunners(ctx, &wg, func(err error) {
		t.Errorf("side runner error = %v", err)
	})
	select {
	case <-claimed:
	case <-time.After(time.Second):
		t.Fatal("cross-scope completion side runner did not start")
	}
	cancel()
	wg.Wait()
}

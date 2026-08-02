// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultCrossScopeCompletionPollInterval = 250 * time.Millisecond
	defaultCrossScopeCompletionLeaseTTL     = 30 * time.Second
	defaultCrossScopeCompletionBatchSize    = 500
	defaultCrossScopeCompletionRetryDelay   = time.Second
	maxCrossScopeCompletionRetryDelay       = 5 * time.Minute
	maxCrossScopeCompletionIdlePollInterval = 2 * time.Second
)

// CrossScopeCompletionLease is the durable ownership token for one producer
// domain's completion-event backlog.
type CrossScopeCompletionLease struct {
	EventID        int64
	ProducerDomain Domain
	LeaseOwner     string
	ClaimEpoch     int64
	AttemptCount   int
}

// CrossScopeCompletionResult reports one atomic producer-to-consumer fanout.
type CrossScopeCompletionResult struct {
	ProducerDomain         Domain
	EventsProcessed        int
	ProducerItemsProcessed int64
	IntentsEnqueued        int
	FanoutDuration         time.Duration
}

// CrossScopeCompletionQueue persists, leases, and fans out reducer producer
// completion events. Implementations must fence every mutation by owner and
// claim epoch.
type CrossScopeCompletionQueue interface {
	Claim(context.Context, string, time.Duration) (CrossScopeCompletionLease, bool, error)
	Heartbeat(context.Context, CrossScopeCompletionLease, time.Duration) error
	Fanout(context.Context, CrossScopeCompletionLease, int) (CrossScopeCompletionResult, error)
	Retry(context.Context, CrossScopeCompletionLease, error, time.Time) error
}

// CrossScopeCompletionRunner drains the durable producer-completion queue. A
// transient failure is persisted as a bounded-backoff retry; events never age
// into a terminal state that could silently strand convergence.
type CrossScopeCompletionRunner struct {
	Queue        CrossScopeCompletionQueue
	LeaseOwner   string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	BatchSize    int
	RetryDelay   time.Duration
	Now          func() time.Time
	Wait         func(context.Context, time.Duration) error
	Logger       *slog.Logger
}

func (r CrossScopeCompletionRunner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r CrossScopeCompletionRunner) pollInterval() time.Duration {
	if r.PollInterval > 0 {
		return r.PollInterval
	}
	return defaultCrossScopeCompletionPollInterval
}

func (r CrossScopeCompletionRunner) leaseTTL() time.Duration {
	if r.LeaseTTL > 0 {
		return r.LeaseTTL
	}
	return defaultCrossScopeCompletionLeaseTTL
}

func (r CrossScopeCompletionRunner) batchSize() int {
	if r.BatchSize > 0 {
		return r.BatchSize
	}
	return defaultCrossScopeCompletionBatchSize
}

func (r CrossScopeCompletionRunner) retryDelay(attempt int) time.Duration {
	base := r.RetryDelay
	if base <= 0 {
		base = defaultCrossScopeCompletionRetryDelay
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for range attempt - 1 {
		if delay >= maxCrossScopeCompletionRetryDelay/2 {
			return maxCrossScopeCompletionRetryDelay
		}
		delay *= 2
	}
	if delay > maxCrossScopeCompletionRetryDelay {
		return maxCrossScopeCompletionRetryDelay
	}
	return delay
}

func (r CrossScopeCompletionRunner) validate() error {
	if r.Queue == nil {
		return errors.New("cross-scope completion runner requires a queue")
	}
	if r.LeaseOwner == "" {
		return errors.New("cross-scope completion runner requires a lease owner")
	}
	return nil
}

// RunOnce claims and fans out one producer domain's pending event set.
func (r CrossScopeCompletionRunner) RunOnce(
	ctx context.Context,
) (bool, CrossScopeCompletionResult, error) {
	if err := r.validate(); err != nil {
		return false, CrossScopeCompletionResult{}, err
	}
	lease, ok, err := r.Queue.Claim(ctx, r.LeaseOwner, r.leaseTTL())
	if err != nil {
		return false, CrossScopeCompletionResult{}, fmt.Errorf("claim cross-scope completion: %w", err)
	}
	if !ok {
		return false, CrossScopeCompletionResult{}, nil
	}

	fanoutCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go r.heartbeat(fanoutCtx, lease, heartbeatDone)
	fanoutStarted := time.Now()
	result, fanoutErr := r.Queue.Fanout(fanoutCtx, lease, r.batchSize())
	result.ProducerDomain = lease.ProducerDomain
	result.FanoutDuration = time.Since(fanoutStarted)
	cancel()
	heartbeatErr := <-heartbeatDone
	if fanoutErr == nil {
		return true, result, nil
	}
	fanoutErr = fmt.Errorf(
		"fanout producer_domain=%s event_id=%d claim_epoch=%d attempt=%d: %w",
		lease.ProducerDomain,
		lease.EventID,
		lease.ClaimEpoch,
		lease.AttemptCount,
		fanoutErr,
	)
	visibleAt := r.now().Add(r.retryDelay(lease.AttemptCount))
	retryErr := r.Queue.Retry(ctx, lease, fanoutErr, visibleAt)
	if retryErr != nil {
		fanoutErr = errors.Join(fanoutErr, fmt.Errorf("retry cross-scope completion: %w", retryErr))
	}
	if heartbeatErr != nil && !errors.Is(heartbeatErr, context.Canceled) {
		fanoutErr = errors.Join(fanoutErr, fmt.Errorf("heartbeat cross-scope completion: %w", heartbeatErr))
	}
	return false, CrossScopeCompletionResult{}, fanoutErr
}

func (r CrossScopeCompletionRunner) heartbeat(
	ctx context.Context,
	lease CrossScopeCompletionLease,
	done chan<- error,
) {
	ticker := time.NewTicker(r.leaseTTL() / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := r.Queue.Heartbeat(ctx, lease, r.leaseTTL()); err != nil {
				done <- err
				return
			}
		}
	}
}

// Run polls until cancellation and keeps retrying transient queue failures.
func (r CrossScopeCompletionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	idleDelay := r.pollInterval()
	for {
		if ctx.Err() != nil {
			return nil
		}
		processed, result, err := r.RunOnce(ctx)
		if err != nil && r.Logger != nil {
			r.Logger.Error("cross-scope completion fanout failed", "error", err)
		}
		if processed && r.Logger != nil {
			r.Logger.Debug(
				"cross-scope completion fanout committed",
				"producer_domain", result.ProducerDomain,
				"events_processed", result.EventsProcessed,
				"producer_items_processed", result.ProducerItemsProcessed,
				"intents_enqueued", result.IntentsEnqueued,
				"fanout_duration_ms", result.FanoutDuration.Milliseconds(),
			)
		}
		if processed {
			idleDelay = r.pollInterval()
			continue
		}
		if err := r.wait(ctx, r.jitteredIdleDelay(idleDelay)); err != nil {
			return nil
		}
		idleDelay *= 2
		if idleDelay > maxCrossScopeCompletionIdlePollInterval {
			idleDelay = maxCrossScopeCompletionIdlePollInterval
		}
	}
}

func (r CrossScopeCompletionRunner) jitteredIdleDelay(delay time.Duration) time.Duration {
	var hash uint32 = 2166136261
	for _, value := range []byte(r.LeaseOwner) {
		hash ^= uint32(value)
		hash *= 16777619
	}
	// Stable per-owner jitter in [80%, 120%] prevents synchronized reducer
	// replicas from polling an empty queue in lockstep.
	percent := 80 + int(hash%41)
	return time.Duration(int64(delay) * int64(percent) / 100)
}

func (r CrossScopeCompletionRunner) wait(ctx context.Context, delay time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

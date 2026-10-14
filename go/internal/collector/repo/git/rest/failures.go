// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// collectorKind is the collector_kind label of the git collector's discovery
// signals.
const collectorKind = "git"

// RetryableError reports that a repository discovery cycle failed for a reason
// the provider is expected to clear on its own: a GitHub 5xx, a transient
// transport fault, a rate limit, or the same faults on the GitHub App token
// mint (#8084). It implements collector.RetryableError so the collector loop
// skips the cycle and waits RetryAfter instead of exiting the whole ingester,
// which also hosts the projector.
//
// The fault classes that never clear on retry (401, other 403, 404, 422,
// invalid JSON on a fully read body) are not reported through this type; they
// stay plain errors and remain fatal.
//
// Do fills Operation, Class, Attempts and, from a provider hint, RetryAfter.
// Failures fills Consecutive and raises RetryAfter to its own backoff.
type RetryableError struct {
	// Operation names the GitHub call that failed: OperationList or
	// OperationMint.
	Operation string
	// Class is the bounded failure class of the last attempt: server_error,
	// rate_limited, or a transport class from TransportClass.
	Class string
	// Attempts is how many requests the in-call retry made before giving up.
	Attempts int
	// Consecutive is how many discovery cycles in a row have ended in a
	// retryable failure, counting this one. Failures sets it; Do leaves it
	// zero.
	Consecutive int
	// RetryAfter is how long the collector loop should wait before the next
	// cycle. Do sets it only from a provider rate-limit hint (capped at
	// RetryAfterCap); Failures raises it to its own backoff when the hint is
	// smaller.
	RetryAfter time.Duration

	cause error
}

// NewRetryableError builds a RetryableError for a retryable fault that
// outlasted the in-call budget. Do builds its own; the constructor exists for
// callers that classify a fault themselves, and for tests.
func NewRetryableError(operation, class string, attempts int, retryAfter time.Duration, cause error) *RetryableError {
	return &RetryableError{Operation: operation, Class: class, Attempts: attempts, RetryAfter: retryAfter, cause: cause}
}

// Error describes the failure with its class and attempt count.
func (e *RetryableError) Error() string {
	return fmt.Sprintf(
		"GitHub %s retryable failure (class=%s attempts=%d): %v",
		e.Operation, e.Class, e.Attempts, e.cause,
	)
}

// Unwrap returns the underlying cause so errors.Is and errors.As reach the
// transport error or status error behind the class.
func (e *RetryableError) Unwrap() error { return e.cause }

// Retryable reports true: the provider is expected to clear this fault.
func (e *RetryableError) Retryable() bool { return true }

// CycleSkip makes the error a collector.CycleSkipper: it tells the collector
// loop to skip the cycle and wait RetryAfter instead of exiting.
func (e *RetryableError) CycleSkip() collector.CycleSkip {
	return collector.CycleSkip{
		CollectorKind: collectorKind,
		FailureClass:  e.Class,
		Operation:     e.Operation,
		Attempts:      e.Attempts,
		Consecutive:   e.Consecutive,
		RetryAfter:    e.RetryAfter,
	}
}

const (
	// retryBackoffBase is the between-cycle wait after the first retryable
	// discovery failure; each further consecutive failure doubles it.
	retryBackoffBase = 5 * time.Second
	// retryBackoffMax caps the doubled wait before jitter.
	retryBackoffMax = 60 * time.Second
)

// Failures is the consecutive-failure policy of one discovery process (#8084).
// It counts per GitHub operation (OperationList, OperationMint): a good mint
// proves nothing about the listing, so each operation has its own count, cap
// and backoff. One value is shared by every selector of the process; they run
// one cycle at a time inside a single collector loop, so it takes no lock.
type Failures struct {
	// Jitter spreads the between-cycle wait. Nil means the production 75% to
	// 125% Jitter; a test injects the identity function.
	Jitter func(time.Duration) time.Duration

	counts map[string]int
}

// Reset clears the counter of each named operation, or of every operation
// when none is named.
func (f *Failures) Reset(operations ...string) {
	if len(operations) == 0 {
		clear(f.counts)
		return
	}
	for _, operation := range operations {
		delete(f.counts, operation)
	}
}

// Count reports the consecutive retryable failures of one operation so far.
func (f *Failures) Count(operation string) int { return f.counts[operation] }

// Account applies the policy to the error one cycle returned.
//
// A *RetryableError is counted once for the cycle against its operation.
// Below sdk.MaxConsecutiveTransportFailures it comes back with Consecutive set
// and RetryAfter raised to a jittered 5 s doubling backoff (base capped at
// 60 s, so 45 to 75 s after jitter at the ceiling) unless the provider hint is
// larger, itself capped at RetryAfterCap. At the cap it comes back as a plain,
// non-retryable error that names the operation, class and count and wraps
// only the cause, so the collector loop exits and a permanent fault is a crash
// loop an operator can alert on.
//
// Any other error, including nil, passes through untouched and leaves every
// counter alone.
func (f *Failures) Account(err error) error {
	var typed *RetryableError
	if !errors.As(err, &typed) {
		return err
	}
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[typed.Operation]++
	count := f.counts[typed.Operation]
	if count >= sdk.MaxConsecutiveTransportFailures {
		return fmt.Errorf(
			"GitHub %s failed %d consecutive discovery cycles (class=%s): %w",
			typed.Operation, count, typed.Class, typed.cause,
		)
	}
	backoff := retryBackoffBase
	for step := 1; step < count && backoff < retryBackoffMax; step++ {
		backoff *= 2
	}
	backoff = min(backoff, retryBackoffMax)
	jitter := f.Jitter
	if jitter == nil {
		jitter = Jitter
	}
	accounted := *typed
	accounted.Consecutive = count
	accounted.RetryAfter = min(max(jitter(backoff), typed.RetryAfter), RetryAfterCap)
	return &accounted
}

// Observe is Account plus the consecutive-failure gauge of the failing
// operation. A selector calls it with the error one selection cycle returned.
func (f *Failures) Observe(ctx context.Context, instruments *telemetry.Instruments, err error) error {
	accounted := f.Account(err)
	var typed *RetryableError
	if errors.As(err, &typed) {
		RecordConsecutiveFailures(ctx, instruments, typed.Operation, f.Count(typed.Operation))
	}
	return accounted
}

// Clear resets the named operations (all when none is named) and records 0 on
// their gauges. A selector calls it when a cycle proved the operation healthy.
func (f *Failures) Clear(ctx context.Context, instruments *telemetry.Instruments, operations ...string) {
	if len(operations) == 0 {
		operations = []string{OperationList, OperationMint}
	}
	f.Reset(operations...)
	for _, operation := range operations {
		RecordConsecutiveFailures(ctx, instruments, operation, 0)
	}
}

// Strip returns err unchanged unless it carries a *RetryableError, in which
// case it returns a plain error that keeps the message and wraps only the
// cause, so the result is never a collector.CycleSkipper. An owner without a
// failure counter uses it to keep a retryable failure fatal.
func Strip(err error) error {
	var typed *RetryableError
	if !errors.As(err, &typed) {
		return err
	}
	return fmt.Errorf("%s: %w", err.Error(), typed.cause)
}

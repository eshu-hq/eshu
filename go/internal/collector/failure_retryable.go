// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"
	"time"
)

// RetryableError marks collector failures that should re-enter the durable
// dead-letter/replay path instead of tearing down the collector Run loop on the
// first failure. It mirrors the projector and reducer RetryableError
// conventions so retry classification stays uniform across ingester services.
type RetryableError interface {
	error
	Retryable() bool
}

// IsRetryable reports whether err explicitly opts into bounded retry. It checks
// the wrapped error chain for an error implementing RetryableError, and also
// treats RegistryFailure values whose class is the retryable transport class as
// retryable so transient registry transport faults do not become terminal.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var retryable RetryableError
	if errors.As(err, &retryable) && retryable.Retryable() {
		return true
	}

	var registryFailure RegistryFailure
	if errors.As(err, &registryFailure) && registryFailure.FailureClass() == RegistryFailureRetryable {
		return true
	}

	return false
}

// CycleSkip describes a retryable source failure that ended one discovery
// cycle before any generation was collected (#8084). The source's owner has
// already classified and counted it; Service.Run only waits and continues.
type CycleSkip struct {
	// CollectorKind is the bounded collector kind label, for example "git".
	CollectorKind string
	// FailureClass is the bounded failure class label, for example
	// "server_error" or "rate_limited".
	FailureClass string
	// Operation names the provider call that failed, for example "list" or
	// "mint". It is a bounded log and gauge label; empty when unknown.
	Operation string
	// Attempts is how many requests the in-call retry made before giving up.
	Attempts int
	// Consecutive is how many discovery cycles in a row have ended this way,
	// counting this one. The owner of the counter sets it.
	Consecutive int
	// RetryAfter is how long to wait before the next discovery cycle. Zero
	// means the Service poll interval.
	RetryAfter time.Duration
}

// CycleSkipper is implemented by a RetryableError that a source returns from
// Next when its discovery cycle failed in a way the provider is expected to
// clear. Service.Run skips the cycle and waits RetryAfter instead of exiting
// the process, which also hosts the projector. A retryable error that does
// not implement it keeps the fatal path, so a source opts in by owning its
// own consecutive-failure cap (see Failures in
// repo/git/rest) rather than inheriting an unbounded loop. The error a source
// returns at its cap MUST NOT implement CycleSkipper and SHOULD wrap only the
// cause, never the skippable error itself, so errors.As cannot find the skip.
type CycleSkipper interface {
	RetryableError
	// CycleSkip returns the skip description.
	CycleSkip() CycleSkip
}

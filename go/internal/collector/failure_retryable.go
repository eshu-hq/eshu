// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "errors"

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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "errors"

// ErrGenerationFetchTimeout fails a projector generation-fact load
// when a bounded fact page dies to a statement timeout (SQLSTATE 57014) or a
// context deadline (#11432). A page that dies partway is a failed fetch,
// never a short success with the rows read so far: the next attempt re-reads
// the generation from its first page, so the failure stays retryable through
// the normal Fail path instead of projecting a partial generation.
var ErrGenerationFetchTimeout = errors.New("projector generation fact fetch timed out")

// generationFetchTimeoutError marks a timed-out generation-fact page load as
// retryable and self-describing. The queue dead-letters any error that does
// not report Retryable() == true, so without this a transient statement
// timeout would dead-letter the pass as a projection bug instead of retrying
// it. Like the reducer fan-out error, the timeout cause stays detectable
// downstream; unlike it, this type also reports Retryable.
type generationFetchTimeoutError struct{ cause error }

// NewGenerationFetchTimeoutError wraps a statement-timeout or deadline cause
// so the fetch failure stays retryable through the normal Fail path while
// keeping the cause detectable (errors.Is/As) downstream.
func NewGenerationFetchTimeoutError(cause error) error {
	return generationFetchTimeoutError{cause: cause}
}

func (e generationFetchTimeoutError) Error() string {
	if e.cause == nil {
		return ErrGenerationFetchTimeout.Error()
	}
	return ErrGenerationFetchTimeout.Error() + ": " + e.cause.Error()
}

// Unwrap exposes both the sentinel (so errors.Is still matches
// ErrGenerationFetchTimeout) and the cause (so DeadlineExceeded and the
// 57014 PgError stay detectable downstream).
func (e generationFetchTimeoutError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrGenerationFetchTimeout}
	}
	return []error{ErrGenerationFetchTimeout, e.cause}
}

// Retryable reports that the queue should retry the whole fetch.
func (generationFetchTimeoutError) Retryable() bool { return true }

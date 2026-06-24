// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticqueue

import (
	"errors"
	"fmt"
	"time"
)

// Retry returns a semantic queue record to a bounded retry state.
func Retry(record Record, now time.Time, nextAttempt time.Time, failure Failure) (Record, error) {
	if record.WorkItemID == "" || record.Fingerprint == "" {
		return Record{}, errors.New("semantic record identity is required")
	}
	if nextAttempt.Before(now) {
		return Record{}, errors.New("next attempt cannot be before now")
	}
	switch record.Status {
	case StatusPending, StatusRetrying, StatusProviderUnavailable:
	default:
		return Record{}, fmt.Errorf("cannot retry semantic record in status %q", record.Status)
	}
	out := record
	out.Status = StatusRetrying
	out.ProviderJob = true
	out.Retryable = true
	out.AttemptCount++
	out.Failure = failure
	lastAttemptAt := now.UTC()
	nextAttemptAt := nextAttempt.UTC()
	out.LastAttemptAt = &lastAttemptAt
	out.NextAttemptAt = &nextAttemptAt
	out.UpdatedAt = now.UTC()
	return out, nil
}

// DeadLetter returns a semantic queue record to a terminal dead-letter state.
func DeadLetter(record Record, now time.Time, failure Failure) (Record, error) {
	if record.WorkItemID == "" || record.Fingerprint == "" {
		return Record{}, errors.New("semantic record identity is required")
	}
	switch record.Status {
	case StatusPending, StatusRetrying, StatusProviderUnavailable:
	default:
		return Record{}, fmt.Errorf("cannot dead-letter semantic record in status %q", record.Status)
	}
	out := record
	out.Status = StatusDeadLetter
	out.ProviderJob = false
	out.Retryable = false
	out.Failure = failure
	lastAttemptAt := now.UTC()
	out.LastAttemptAt = &lastAttemptAt
	out.NextAttemptAt = nil
	out.UpdatedAt = now.UTC()
	return out, nil
}

// Succeed returns a semantic queue record to a terminal success state.
func Succeed(record Record, now time.Time, responseHash string) (Record, error) {
	if record.WorkItemID == "" || record.Fingerprint == "" {
		return Record{}, errors.New("semantic record identity is required")
	}
	switch record.Status {
	case StatusPending, StatusRetrying:
	default:
		return Record{}, fmt.Errorf("cannot succeed semantic record in status %q", record.Status)
	}
	out := record
	out.Status = StatusSucceeded
	out.ProviderJob = false
	out.Retryable = false
	out.Failure = Failure{}
	out.ResponseHash = responseHash
	out.NextAttemptAt = nil
	out.UpdatedAt = now.UTC()
	return out, nil
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"errors"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

// defaultRetryAfterSeconds paces retries after a retryable analyzer
// failure. The SDK contract rejects a retryable failure status with a zero
// delay, so the runner must request one; the host applies
// max(poll-interval, this) per attempt while the claim's attempt budget
// lasts, and clamps the request to one hour, so a minute paces transient
// source outages without retry-storming (#9591).
const defaultRetryAfterSeconds = 60

// failureClass maps an analysis error to a bounded failure class for the
// status record. Analyzer failures carry the vocabulary class; anything else
// is an undifferentiated component failure. Values are never interpolated:
// raw causes can carry repository paths, image names, or scope IDs.
func failureClass(err error) string {
	var failure scannerworker.AnalyzerFailure
	if errors.As(err, &failure) {
		return string(failure.FailureClass())
	}
	return "component_failed"
}

// failureState maps an analysis error to the claimed result state:
// retryable analyzer failures release the claim for a bounded retry,
// everything else fails terminal.
func failureState(err error) sdkcollector.ResultState {
	var failure scannerworker.AnalyzerFailure
	if errors.As(err, &failure) && failure.Disposition() == scannerworker.FailureRetryable {
		return sdkcollector.ResultRetryable
	}
	return sdkcollector.ResultTerminal
}

// emitFailure emits a single-shot failure result for an analysis error.
// No page envelope; see emitTerminal for why resumed calls omit it.
// A retryable failure carries the default retry delay: the SDK rejects a
// retryable failure status with a zero delay, so omitting it turns every
// retryable failure into an invalid_result terminal (#9591). Terminal
// failures carry no delay.
func (r Runner) emitFailure(claim sdkcollector.Claim, observedAt time.Time, err error) error {
	state := failureState(err)
	retryAfter := 0
	if state == sdkcollector.ResultRetryable {
		retryAfter = defaultRetryAfterSeconds
	}
	return r.emit(claim, observedAt, state, nil, []sdkcollector.Status{{
		Class:             sdkcollector.StatusFailure,
		FailureClass:      failureClass(err),
		RetryAfterSeconds: retryAfter,
	}}, nil)
}

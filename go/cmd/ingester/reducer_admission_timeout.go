// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	reducerAdmissionMaxWaitEnv = "ESHU_REDUCER_ADMISSION_MAX_WAIT"
	// defaultReducerAdmissionMaxWait bounds one Enqueue admission wait to a
	// single projector claim lease (wiring.go claims projector work with a
	// 5-minute lease). A worker never parks longer than this in admission;
	// on expiry Enqueue fails so the claim is released (attempt++) and another
	// worker can pick the item up (#10485).
	defaultReducerAdmissionMaxWait = 5 * time.Minute
)

// ErrReducerAdmissionTimeout reports that a reducer admission Enqueue waited
// for queue depth to fall below the admission marks longer than the
// claim-scoped MaxWait. Callers treat it as a retryable work failure: the
// projector's failWork -> Fail path releases the claim (attempt++) so another
// worker can pick the item up. It never fires after admission is granted, so
// an in-flight inner Enqueue (graph writes) is never orphaned by it.
var ErrReducerAdmissionTimeout = errors.New("reducer admission wait timed out")

// reducerAdmissionTimeoutFailureClass is the durable failure_class a
// dead-lettered admission wait timeout carries (#10502). The dead-letter
// path honors an error's own class, so the row names backpressure instead of
// the projection_bug default. The retry path is unchanged:
// ProjectorQueue.Fail hardcodes projection_retryable for every retried item,
// so no SQL status or class semantics change before the budget is spent.
const reducerAdmissionTimeoutFailureClass = "admission_timeout"

// reducerAdmissionTimeoutError is the concrete Enqueue timeout failure. It
// unwraps to ErrReducerAdmissionTimeout for errors.Is matching and reports
// Retryable so ProjectorQueue.Fail releases the claim for another attempt
// instead of marking the generation terminal (failure.IsRetryable is the
// Fail retry gate).
type reducerAdmissionTimeoutError struct {
	waited  time.Duration
	maxWait time.Duration
	reason  string
	depth   int64
}

func (e reducerAdmissionTimeoutError) Error() string {
	return fmt.Sprintf("%s: waited %s exceeding %s (reason %s, queue_depth %d)",
		ErrReducerAdmissionTimeout, e.waited.Truncate(time.Millisecond), e.maxWait, e.reason, e.depth)
}

func (e reducerAdmissionTimeoutError) Unwrap() error {
	return ErrReducerAdmissionTimeout
}

func (reducerAdmissionTimeoutError) Retryable() bool {
	return true
}

// FailureClass reports the dead-letter class for this backpressure timeout.
// The queue keeps a self-classifying error's own class on the dead-letter
// row, so an admission timeout that exhausts the attempt budget lands as
// admission_timeout instead of retry_exhausted-with-projection_bug-detail.
func (reducerAdmissionTimeoutError) FailureClass() string {
	return reducerAdmissionTimeoutFailureClass
}

// IsAdmissionTimeout opts this error into the failure.AdmissionTimeoutError
// contract so ClassifyFailure maps it to the admission_timeout class instead
// of the projection_bug default (#10502).
func (reducerAdmissionTimeoutError) IsAdmissionTimeout() bool {
	return true
}

// admissionTimeoutExceeded reports whether the admission wait that started at
// waitStart has exceeded the claim-scoped MaxWait. A non-positive MaxWait
// disables the timeout. Timeout ownership: the waiting worker goroutine calls
// this inline — no new goroutine, no shared state, no lock-order change.
func (w reducerAdmissionWriter) admissionTimeoutExceeded(ctx context.Context, waitStart time.Time, reason string, depth int64, intentCount int) error {
	maxWait := w.config.MaxWait
	if maxWait <= 0 {
		return nil
	}
	waited := time.Since(waitStart)
	if waited < maxWait {
		return nil
	}
	if w.logger != nil {
		w.logger.WarnContext(ctx,
			"reducer admission wait timed out, failing work item for retry",
			slog.String("reason", reason),
			log.FailureClass(""),
			slog.Int64("queue_depth", depth),
			slog.Duration("waited", waited.Truncate(time.Millisecond)),
			slog.Duration("max_wait", maxWait),
			slog.Int("intent_count", intentCount),
		)
	}
	return reducerAdmissionTimeoutError{waited: waited, maxWait: maxWait, reason: reason, depth: depth}
}

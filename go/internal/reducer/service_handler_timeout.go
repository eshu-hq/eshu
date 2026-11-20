// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// HandlerTimeoutFailureClass is the failure class of an intent whose handler
// never returned before its configured per-domain bound expired, so the
// worker stopped the lease heartbeat and failed the intent for retry
// (#10866). It is a counting retryable class: the item re-enters the queue
// under the normal attempt budget (ESHU_REDUCER_HANDLER_TIMEOUT bounds one
// execution, the budget bounds the retries) instead of dead-lettering on the
// spot or retrying forever. It deliberately reuses no existing class:
// reusing graph_write_timeout would falsely trigger the #3560 producer
// backpressure signal, which scopes to graph-write timeouts only.
const HandlerTimeoutFailureClass = "reducer_handler_timeout"

// handlerTimeoutError reports a handler that never returned before its
// per-domain bound expired. It self-classifies (so failIntent preserves the
// class on the retrying row and counts it on eshu_dp_reducer_retry_surge_total)
// and opts into bounded retry (so the item is retried, never dead-lettered).
// It wraps the bound expiry (context.DeadlineExceeded) so errors.Is still
// matches the deadline.
type handlerTimeoutError struct {
	intentID string
	domain   reducercontract.Domain
	elapsed  time.Duration
	bound    time.Duration
	attempt  int
	cause    error
}

func (e *handlerTimeoutError) Error() string {
	return fmt.Sprintf(
		"reducer handler for intent %s (domain %s, attempt %d) exceeded the %s bound after %s: %v",
		e.intentID, e.domain, e.attempt, e.bound, e.elapsed, e.cause,
	)
}

func (e *handlerTimeoutError) Unwrap() error { return e.cause }

func (e *handlerTimeoutError) FailureClass() string {
	return HandlerTimeoutFailureClass
}

func (e *handlerTimeoutError) Retryable() bool { return true }

// newHandlerTimeoutError builds the release cause for an intent whose bound expired.
// attempt is read off the intent so the operator log line carries the retry
// position without a second lookup.
func newHandlerTimeoutError(intent reducercontract.Intent, bound, elapsed time.Duration, cause error) *handlerTimeoutError {
	return &handlerTimeoutError{
		intentID: intent.IntentID,
		domain:   intent.Domain,
		elapsed:  elapsed,
		bound:    bound,
		attempt:  intent.AttemptCount,
		cause:    cause,
	}
}

// ownDeadlineExpired reports whether a returned handler outcome carries a
// DeadlineExceeded caused by our own bound: the outcome's error wraps the
// deadline AND our own timer already fired (Stop reports false). A
// downstream store or query timeout that beats our bound leaves the timer
// running, so it keeps its own failure class and never triggers timeout
// backpressure.
func ownDeadlineExpired(out handlerOutcome, timer *time.Timer) bool {
	return errors.Is(out.err, context.DeadlineExceeded) && !timer.Stop()
}

// recordReducerHandlerTimedOut counts one handler released because its
// per-domain bound expired. The structured log line for the release is the
// recordReducerResult "failed" line the caller emits with the same error,
// which carries the domain, the intent id, and (in the error text) the
// elapsed time, the bound, and the attempt.
func (s Service) recordReducerHandlerTimedOut(ctx context.Context, intent reducercontract.Intent) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.ReducerHandlerTimedOut.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(intent.Domain)),
	))
}

// recordStuckHandlerRelease counts one stuck-handler release on the cause
// signal: the timeout counter for an expired bound, the abandonment counter
// for a shutdown release. The two signals stay separate so an operator can
// tell bound pressure (tune the bound) from shutdown wedging (fix handler
// cancellation).
func (s Service) recordStuckHandlerRelease(ctx context.Context, intent reducercontract.Intent, timedOut bool) {
	if timedOut {
		s.recordReducerHandlerTimedOut(ctx, intent)
		return
	}
	s.recordReducerHandlerAbandoned(ctx, intent)
}

// releaseStuckHandler is the shared release tail for handlers that never
// returned: expired-bound timeouts (cause is *handlerTimeoutError) and
// shutdown abandonments (cause is the shutdown error, wrapped by the caller
// in *handlerAbandonedError). It stops the lease heartbeat BEFORE failing
// the intent, then triages through the existing WorkSink.Fail path (retry vs
// dead_letter via failIntent) and records the standard failed outcome plus
// the cause counter. Fail and the records run on a detached context: the
// trigger (bound expiry or shutdown) already ended the caller's context, and
// the release UPDATE must still reach the queue. A Fail rejected by the
// lease fence is a lost claim, recorded once like a lost heartbeat (#7142).
// Any other Fail error is returned so the run stops instead of silently
// dropping the item.
func (s Service) releaseStuckHandler(
	ctx context.Context,
	intent reducercontract.Intent,
	releaseErr error,
	timedOut bool,
	start time.Time,
	execStart time.Time,
	queueWait float64,
	workerID int,
	stopHeartbeat func() error,
) error {
	duration := time.Since(start).Seconds()
	err := releaseErr
	if stopErr := stopHeartbeat(); stopErr != nil {
		err = errors.Join(err, stopErr)
	}
	failCtx := context.WithoutCancel(ctx)
	failErr := s.WorkSink.Fail(failCtx, intent, err)
	if failErr != nil && errors.Is(failErr, ErrExecutionClaimRejected) {
		s.recordReducerResult(failCtx, intent, reducercontract.Result{}, duration, queueWait, "lease_lost_during_execution", workerID, failErr)
		s.recordStuckHandlerRelease(failCtx, intent, timedOut)
		return nil
	}
	s.recordReducerResult(failCtx, intent, reducercontract.Result{}, duration, queueWait, "failed", workerID, err)
	s.recordStuckHandlerRelease(failCtx, intent, timedOut)
	if failErr != nil {
		return errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
	}
	return nil
}

// failTimedOutHandler is the per-item release tail for an expired bound. It
// shares the stop-heartbeat-before-Fail ordering and Fail triage with the
// shutdown path; only the counter differs.
func (s Service) failTimedOutHandler(
	ctx context.Context,
	intent reducercontract.Intent,
	timeoutErr *handlerTimeoutError,
	start time.Time,
	execStart time.Time,
	queueWait float64,
	workerID int,
	stopHeartbeat func() error,
) error {
	return s.releaseStuckHandler(ctx, intent, timeoutErr, true, start, execStart, queueWait, workerID, stopHeartbeat)
}

// failTimedOutBatchHandler is the batch release tail for an expired bound.
// Same stop-heartbeat-before-Fail ordering and Fail triage as
// failTimedOutHandler; the second return is false because Fail already
// terminalized the row (or the claim was lost), so the caller owes no
// acknowledgment. A non-nil error carries a don't-care false value, matching
// the executeAndReport contract.
func (s Service) failTimedOutBatchHandler(
	ctx context.Context,
	intent reducercontract.Intent,
	timeoutErr *handlerTimeoutError,
	start time.Time,
	execStart time.Time,
	queueWait float64,
	workerID int,
	stopHeartbeat func() error,
) (batchAckItem, bool, error) {
	if err := s.releaseStuckHandler(ctx, intent, timeoutErr, true, start, execStart, queueWait, workerID, stopHeartbeat); err != nil {
		return batchAckItem{}, false, err
	}
	return batchAckItem{}, false, nil
}

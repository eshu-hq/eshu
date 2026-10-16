// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// reducerQueueWaitSeconds returns how long reducer work was visible before a
// worker started executing it. Negative or missing timestamps are clamped so
// clock skew and legacy rows do not pollute latency histograms.
func reducerQueueWaitSeconds(start time.Time, availableAt time.Time) float64 {
	if availableAt.IsZero() {
		return 0
	}
	wait := start.Sub(availableAt)
	if wait < 0 {
		return 0
	}
	return wait.Seconds()
}

func (s Service) recordReducerResult(ctx context.Context, intent Intent, result Result, duration float64, queueWait float64, status string, workerID int, execErr error) {
	if status == "failed" && s.isDeferredFailure(execErr) {
		status = executionStatusDeferred
	}
	if s.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
			attribute.String("queue", "reducer"),
			attribute.String("status", status),
		)
		s.Instruments.ReducerRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
		s.Instruments.ReducerQueueWaitDuration.Record(ctx, queueWait, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
		s.Instruments.ReducerExecutions.Add(ctx, 1, attrs)
	}

	if s.Logger != nil {
		partitionKey := ""
		if len(intent.EntityKeys) > 0 {
			partitionKey = intent.EntityKeys[0]
		}
		domainAttrs := telemetry.DomainAttrs(string(intent.Domain), partitionKey)
		logAttrs := make([]any, 0, len(domainAttrs)+4)
		for _, a := range domainAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, log.Queue("reducer"))
		logAttrs = append(logAttrs, log.IntentID(intent.IntentID))
		logAttrs = append(logAttrs, log.Status(status))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("handler_duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("queue_wait_seconds", queueWait))
		// Emit per-phase sub-timings when the handler populated them. Keys match
		// the workload materialization log attribute names so operators can
		// correlate the service-level log line with the handler-level log line
		// without reading two separate log streams.
		for k, v := range result.SubDurations {
			logAttrs = append(logAttrs, slog.Float64("sub_duration_"+k+"_seconds", v))
		}
		// Emit non-duration diagnostic signals (counts and flags such as
		// input_ready and written_rows) under a separate sub_signal_<key> prefix
		// with NO _seconds suffix, so an operator never misreads a row count or a
		// boolean flag as a wall-time measurement.
		for k, v := range result.SubSignals {
			logAttrs = append(logAttrs, slog.Float64("sub_signal_"+k, v))
		}
		logAttrs = append(logAttrs, log.WorkerID(fmt.Sprintf("%d", workerID)))
		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseReduction))
		switch status {
		case "failed", "ack_failed":
			message := "reducer execution failed"
			failureClass := reducerExecutionFailureClass(execErr)
			if status == "ack_failed" {
				failureClass = "ack_failure"
				message = "reducer ack failed"
			}
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(failureClass))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.ErrorContext(ctx, message, logAttrs...)
		case executionStatusDeferred:
			// A readiness wait the queue retries without spending the budget:
			// visible, but not an error (#7597).
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(reducerExecutionFailureClass(execErr)))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.WarnContext(ctx, "reducer execution deferred", logAttrs...)
		case "superseded":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("generation_superseded"))
			s.Logger.InfoContext(ctx, "reducer intent superseded", logAttrs...)
		case "lease_lost_before_start":
			// No handler work ran under this claim; the lease is left
			// unrenewed for the expired-lease reclaim path (#4464) rather
			// than dead-lettered, so this is an operator-visible warning, not
			// a terminal failure.
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("lease_heartbeat_failure"))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.WarnContext(ctx, "reducer claim lost its lease before handler start", logAttrs...)
		case "lease_lost_during_execution":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("execution_claim_rejected"))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.WarnContext(ctx, "reducer claim lost its lease during handler execution", logAttrs...)
		case "ack_claim_rejected":
			// The ACK path emits a separate warning with the stale-claim context.
		case ackStatusOutcomeUnknown:
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(ackStatusOutcomeUnknown), log.Err(execErr))
			s.Logger.WarnContext(ctx, "reducer batch ack outcome unknown", logAttrs...)
		case ackStatusAbandonedToLeaseExpiry:
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(ackStatusAbandonedToLeaseExpiry), log.Err(execErr))
			s.Logger.WarnContext(ctx, "reducer ack abandoned to lease expiry; claim will be reclaimed", logAttrs...)
		default:
			s.Logger.InfoContext(ctx, "reducer execution succeeded", logAttrs...)
		}
	}
}

// executionStatusDeferred is the eshu_dp_reducer_executions_total status of a
// failure whose class the queue exempts from the retry budget (#7597).
const executionStatusDeferred = "deferred"

// isDeferredFailure mirrors the queue's retry decision for a non-counting
// class: the error is Retryable and self-classifies into a class the injected
// IsDeferredFailureClass names. A producer-terminal or any other counting
// class, and a non-retryable error, stay "failed".
func (s Service) isDeferredFailure(execErr error) bool {
	return execErr != nil && s.IsDeferredFailureClass != nil && IsRetryable(execErr) &&
		s.IsDeferredFailureClass(reducerExecutionFailureClass(execErr))
}

const (
	// ackRetryAttempts is the total attempts for one ack, AckBatch or a
	// single-item Ack (first try plus retries).
	ackRetryAttempts = 5
	// defaultAckRetryBase is the first backoff between transient ack retries.
	defaultAckRetryBase = 50 * time.Millisecond
	// ackRetryMaxBackoff caps the doubling backoff.
	ackRetryMaxBackoff = 2 * time.Second
	// ackStatusAbandonedToLeaseExpiry is both the execution status and the
	// failure_class for an ack whose transient failures outlasted the retry
	// budget. The claim stays leased and is reclaimed at lease expiry.
	ackStatusAbandonedToLeaseExpiry = "ack_abandoned_to_lease_expiry"
	// ackStatusOutcomeUnknown is the execution status for an ack whose outcome
	// is not known: shutdown cut it short, or a batch ack may have committed
	// only some items. One constant keeps a typo from splitting the status
	// cardinality across the recorder, the log switch and the ack paths.
	ackStatusOutcomeUnknown = "ack_outcome_unknown"
)

// errAckAbandonedToLeaseExpiry marks an ack, batch or single-item, whose
// transient failures outlasted the retry budget. The claims stay leased and
// expire for reclaim, so the run keeps draining instead of cancelling every
// worker (#7267, #7444).
var errAckAbandonedToLeaseExpiry = errors.New("ack abandoned to lease expiry after transient failures")

// errAckRetryInterrupted marks a transient-ack retry that shutdown cut short
// while it waited to back off. It wraps the context error, so callers that test
// for cancellation still match, and it lets the single-item path tell a stopping
// process apart from an ack that failed for another reason.
var errAckRetryInterrupted = errors.New("ack retry interrupted by shutdown")

// isTransientAckError reports whether err carries a Postgres 40P01 (deadlock)
// or 40001 (serialization failure) SQLSTATE. Those are normal, retryable
// outcomes; matching the SQLState accessor keeps the reducer free of a driver
// import.
func isTransientAckError(err error) bool {
	var stateErr interface{ SQLState() string }
	if !errors.As(err, &stateErr) {
		return false
	}
	switch stateErr.SQLState() {
	case "40P01", "40001":
		return true
	}
	return false
}

// ackBatchRetryingTransient calls AckBatch, retrying transient serialization
// failures with bounded exponential backoff. Retrying is idempotent: the ack is
// keyed by claim epoch, so a retry after a reclaim matches zero rows and
// surfaces ErrExecutionClaimRejected rather than double-completing a row. A
// non-transient error is returned untouched; an exhausted budget returns
// errAckAbandonedToLeaseExpiry.
func (s Service) ackBatchRetryingTransient(
	ctx context.Context,
	sink BatchWorkSink,
	intents []Intent,
	results []Result,
) error {
	return s.retryTransientAck(ctx, len(intents), ackRetryMessages{
		retry:     "reducer batch ack hit transient failure; retrying",
		abandoned: "reducer batch ack abandoned to lease expiry",
	}, func() error {
		return sink.AckBatch(ctx, intents, results)
	})
}

// ackSingleRetryingTransient is the single-item counterpart of
// ackBatchRetryingTransient: it calls WorkSink.Ack under the same classifier,
// backoff and budget, so Workers <= 1 and the per-item concurrent path do not
// end the run on a 40P01 or 40001 (#7444).
func (s Service) ackSingleRetryingTransient(ctx context.Context, intent Intent, result Result) error {
	return s.retryTransientAck(ctx, 1, ackRetryMessages{
		retry:     "reducer ack hit transient failure; retrying",
		abandoned: "reducer ack abandoned to lease expiry",
	}, func() error {
		return s.WorkSink.Ack(ctx, intent, result)
	})
}

// ackRetryMessages carries the log text for one ack path so the shared retry
// loop keeps each path's existing wording.
type ackRetryMessages struct {
	retry     string
	abandoned string
}

// retryTransientAck runs ack up to ackRetryAttempts times, backing off between
// attempts while the error is a transient 40P01 or 40001. Each attempt is its
// own transaction, so only the ack repeats, never the handler. It returns nil
// or a non-transient error untouched, errAckRetryInterrupted (wrapping
// ctx.Err()) when shutdown interrupts a backoff, and errAckAbandonedToLeaseExpiry
// once the budget is spent.
func (s Service) retryTransientAck(ctx context.Context, items int, msgs ackRetryMessages, ack func() error) error {
	backoff := s.ackRetryBase
	if backoff <= 0 {
		backoff = defaultAckRetryBase
	}

	var err error
	for attempt := 1; attempt <= ackRetryAttempts; attempt++ {
		err = ack()
		if err == nil || !isTransientAckError(err) {
			return err
		}
		if attempt == ackRetryAttempts {
			break
		}
		s.logAckTransient(ctx, msgs.retry, "ack_transient_retry", items, attempt, err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", errAckRetryInterrupted, ctx.Err())
		case <-timer.C:
		}
		backoff = min(backoff*2, ackRetryMaxBackoff)
	}

	s.logAckTransient(ctx, msgs.abandoned, ackStatusAbandonedToLeaseExpiry, items, ackRetryAttempts, err)
	return fmt.Errorf("%w: %w", errAckAbandonedToLeaseExpiry, err)
}

// ackFailureStatus names the execution status for an ack that did not succeed.
// An ack abandoned after its transient-retry budget is reported apart from
// ack_outcome_unknown, which shutdown cancellation also uses, so a dashboard
// on the status counter can tell lease-expiry reclaim from a stopping process.
func ackFailureStatus(err error) string {
	if errors.Is(err, errAckAbandonedToLeaseExpiry) {
		return ackStatusAbandonedToLeaseExpiry
	}
	return ackStatusOutcomeUnknown
}

// BatchAckSupersededError reports a batch ACK in which the queue dropped one
// or more superseded claims before running any UPDATE, while the surviving
// claims were still acked (#8007). A lease that expires mid-handler is
// re-claimed by the same process, so one flush batch can carry both the stale
// claim and its successor for one work item: the queue commits the successor
// and rejects the batch for the dropped claim. Mapping that one error onto
// every item in the flush logs the committed successor as ack_outcome_unknown
// and drops its result, so the Service needs the dropped-claim identities to
// tell committed items from superseded ones.
//
// It unwraps to ErrExecutionClaimRejected, so every existing errors.Is branch
// (the batch acker survives instead of stopping the reducer) keeps working.
type BatchAckSupersededError struct {
	// Superseded holds the claims the queue dropped without attempting any
	// UPDATE. They never committed.
	Superseded []Intent
	// KeptCommitted reports whether every surviving claim's UPDATE matched
	// its row. False means some surviving claim was stale too, so the
	// surviving items that are not superseded stay ack_outcome_unknown
	// instead of being reported as succeeded.
	KeptCommitted bool
}

// Error names the superseded work items, so an operator reading the batch
// fence line can tell which claims the queue dropped.
func (e *BatchAckSupersededError) Error() string {
	ids := make([]string, 0, len(e.Superseded))
	for _, intent := range e.Superseded {
		ids = append(ids, intent.IntentID)
	}
	return fmt.Sprintf(
		"batch ack superseded claims [%s]: %v",
		strings.Join(ids, ", "),
		ErrExecutionClaimRejected,
	)
}

// Unwrap keeps errors.Is(err, ErrExecutionClaimRejected) true, which is the
// discriminator the batch acker uses to log and continue.
func (e *BatchAckSupersededError) Unwrap() error {
	return ErrExecutionClaimRejected
}

// Supersedes reports whether intent is one of the dropped claims. The match
// is by claim identity, not bare work item id: a flush batch can carry two
// claims of one work item, and only the older one was dropped.
func (e *BatchAckSupersededError) Supersedes(intent Intent) bool {
	for _, dropped := range e.Superseded {
		if dropped.IntentID == intent.IntentID &&
			dropped.ClaimEpoch == intent.ClaimEpoch &&
			sameBatchAckClaimedAt(dropped.ClaimedAt, intent.ClaimedAt) {
			return true
		}
	}
	return false
}

func sameBatchAckClaimedAt(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

// recordBatchAckError splits one failed batch ACK into per-item outcomes. A
// batch that carried a superseded claim alongside its successor reports the
// dropped claim as ack_claim_rejected (with its own stale-claim log line
// carrying the work item id) and each committed survivor as succeeded with
// its result; survivors that may not have committed, and every item of a
// rejection without supersession detail, stay on the previous outcome. It
// returns how many items were left unattributed, so the caller can keep the
// batch fence line for exactly those batches.
func (s Service) recordBatchAckError(ctx context.Context, pending []batchAckItem, ackErr error) int {
	var superseded *BatchAckSupersededError
	if !errors.As(ackErr, &superseded) || superseded == nil {
		status := ackFailureStatus(ackErr)
		for _, item := range pending {
			s.recordBatchAckOutcome(ctx, item, status, ackErr)
		}
		return len(pending)
	}
	unknown := 0
	for _, item := range pending {
		switch {
		case superseded.Supersedes(item.intent):
			s.recordBatchAckOutcome(ctx, item, "ack_claim_rejected", ackErr)
			s.logReducerAckClaimRejected(ctx, &item.intent, 0, ackErr)
		case superseded.KeptCommitted:
			s.recordBatchAckOutcome(ctx, item, "succeeded", nil)
		default:
			s.recordBatchAckOutcome(ctx, item, ackStatusOutcomeUnknown, ackErr)
			unknown++
		}
	}
	return unknown
}

func (s Service) logAckTransient(ctx context.Context, message, failureClass string, batchSize, attempt int, err error) {
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(ctx, message,
		log.Queue("reducer"),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
		telemetry.FailureClassAttr(failureClass),
		"batch_size", batchSize,
		"attempt", attempt,
		log.Err(err),
	)
}

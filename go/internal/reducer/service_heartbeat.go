// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

type reducerClassifiedFailure interface {
	FailureClass() string
}

func reducerExecutionFailureClass(err error) string {
	var classified reducerClassifiedFailure
	if errors.As(err, &classified) {
		if failureClass := strings.TrimSpace(classified.FailureClass()); failureClass != "" {
			return failureClass
		}
	}
	return "reducer_failure"
}

type reducerHeartbeatStop func() error

// reducerPreHeartbeatFailure marks a heartbeat failure that happened before
// Executor.Execute ever ran, so the caller can tell it apart from a failure
// during or after real handler work. No handler work ran under this claim,
// so the intent must never be routed through WorkSink.Fail (which can
// dead-letter it): the correct recovery is to leave the lease unrenewed so
// the expired-lease reclaim path (#4464) picks it back up, or a retry.
type reducerPreHeartbeatFailure struct {
	err error
}

func (e *reducerPreHeartbeatFailure) Error() string { return e.err.Error() }
func (e *reducerPreHeartbeatFailure) Unwrap() error { return e.err }

// startHeartbeat starts the reducer lease heartbeat loop for a claimed
// intent. It emits one heartbeat synchronously, before returning, so a
// worker that stalls (GC pause, slow first graph write) immediately after
// claim cannot let the lease expire before any heartbeat has landed (#4447).
// HeartbeatInterval = LeaseDuration/2 and the periodic ticker only fires
// after a full interval has elapsed, leaving that startup window open
// without this immediate pre-heartbeat.
//
// If the immediate pre-heartbeat itself fails, the returned stop function's
// error is wrapped in reducerPreHeartbeatFailure so executeWithTelemetry can
// skip Executor.Execute and WorkSink.Fail entirely (#4447 follow-up): no
// handler work has run yet, so there is nothing to execute or dead-letter.
func (s Service) startHeartbeat(
	ctx context.Context,
	intent reducercontract.Intent,
	workerID int,
) (context.Context, reducerHeartbeatStop) {
	if s.Heartbeater == nil || s.HeartbeatInterval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)

	if err := s.Heartbeater.Heartbeat(heartbeatCtx, intent); err != nil {
		heartbeatErr := fmt.Errorf("heartbeat reducer work: %w", err)
		s.recordReducerHeartbeatMissed(heartbeatCtx, intent, workerID, heartbeatErr)
		cancel()
		preFailure := &reducerPreHeartbeatFailure{err: heartbeatErr}
		return heartbeatCtx, func() error { return preFailure }
	}

	// stopping is set by the stop function immediately before it cancels
	// heartbeatCtx. A tick that returns context.Canceled while stopping is set
	// was interrupted by our own stop, not by a lost lease (#7109). It is
	// deliberately not set on parent cancellation or a failed tick's own
	// cancel(), so those still surface.
	var stopping atomic.Bool
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(s.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-heartbeatCtx.Done():
				done <- nil
				return
			case <-ticker.C:
				err := s.Heartbeater.Heartbeat(heartbeatCtx, intent)
				if err == nil {
					continue
				}
				if stopping.Load() && errors.Is(err, context.Canceled) {
					// The handler already finished and stop cancelled the
					// in-flight renewal UPDATE. The lease is fenced by the
					// ack, so drop the self-inflicted error; reporting it
					// would fail a successful item and stop the process.
					s.logReducerHeartbeatStopRace(heartbeatCtx, intent, workerID)
					done <- nil
					return
				}
				// The first real failure ends the loop. Cancelling heartbeatCtx
				// makes ctx.Done ready, but a queued ticker.C can still win the
				// next select; a follow-up tick would then run on the cancelled
				// context and either be forgiven by the stop guard above or
				// overwrite this error with context canceled, erasing the real
				// cause. Returning here makes the first failure the only one
				// that can be reported.
				heartbeatErr := fmt.Errorf("heartbeat reducer work: %w", err)
				s.recordReducerHeartbeatMissed(heartbeatCtx, intent, workerID, heartbeatErr)
				cancel()
				done <- heartbeatErr
				return
			}
		}
	}()

	var once sync.Once
	return heartbeatCtx, func() error {
		var heartbeatErr error
		once.Do(func() {
			stopping.Store(true)
			cancel()
			heartbeatErr = <-done
		})
		return heartbeatErr
	}
}

// recordReducerHeartbeatMissed logs and increments the operator-facing
// missed-heartbeat signal for a reducer lease heartbeat failure, whether it
// came from the immediate pre-heartbeat or a later periodic tick.
func (s Service) recordReducerHeartbeatMissed(
	ctx context.Context,
	intent reducercontract.Intent,
	workerID int,
	heartbeatErr error,
) {
	if s.Instruments != nil {
		s.Instruments.ReducerHeartbeatMissed.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
	}
	if s.Logger != nil {
		domainAttrs := telemetry.DomainAttrs(string(intent.Domain), firstReducerPartitionKey(intent))
		logAttrs := make([]any, 0, len(domainAttrs)+5)
		for _, attribute := range domainAttrs {
			logAttrs = append(logAttrs, attribute)
		}
		logAttrs = append(
			logAttrs,
			log.Queue("reducer"),
			log.IntentID(intent.IntentID),
			log.WorkerID(fmt.Sprintf("%d", workerID)),
			slog.Duration("heartbeat_interval", s.HeartbeatInterval),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
			telemetry.FailureClassAttr("lease_heartbeat_failure"),
			log.Err(heartbeatErr),
		)
		s.Logger.ErrorContext(ctx, "reducer lease heartbeat failed", logAttrs...)
	}
}

// logReducerHeartbeatStopRace records, at debug level, that a periodic
// heartbeat tick was interrupted by the worker's own stop and its
// context-canceled error was dropped rather than treated as a lease failure.
// It is not counted as a missed heartbeat: the lease was not lost.
func (s Service) logReducerHeartbeatStopRace(ctx context.Context, intent reducercontract.Intent, workerID int) {
	if s.Logger == nil {
		return
	}
	domainAttrs := telemetry.DomainAttrs(string(intent.Domain), firstReducerPartitionKey(intent))
	logAttrs := make([]any, 0, len(domainAttrs)+4)
	for _, attribute := range domainAttrs {
		logAttrs = append(logAttrs, attribute)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("reducer"),
		log.IntentID(intent.IntentID),
		log.WorkerID(fmt.Sprintf("%d", workerID)),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
	s.Logger.DebugContext(ctx, "reducer lease heartbeat tick cancelled by stop; ignored", logAttrs...)
}

func firstReducerPartitionKey(intent reducercontract.Intent) string {
	if len(intent.EntityKeys) == 0 {
		return ""
	}
	keys := append([]string(nil), intent.EntityKeys...)
	slices.Sort(keys)
	return keys[0]
}

// HandlerAbandonedAtShutdownFailureClass is the failure class of an intent
// whose handler never returned before shutdown arrived, so the worker stopped
// the lease heartbeat and failed the intent for retry (#8211). It is a
// counting retryable class: the item re-enters the queue under the normal
// attempt budget instead of dead-lettering on the spot.
const HandlerAbandonedAtShutdownFailureClass = "reducer_handler_abandoned_at_shutdown"

// handlerAbandonedError reports a handler that never returned before shutdown
// arrived. It self-classifies (so failIntent preserves the class on the
// retrying row and counts it on eshu_dp_reducer_retry_surge_total) and opts
// into bounded retry (so the item is retried, never dead-lettered). It wraps
// the shutdown cause so errors.Is(err, context.Canceled) still matches.
type handlerAbandonedError struct {
	intentID string
	domain   reducercontract.Domain
	elapsed  time.Duration
	cause    error
}

func (e *handlerAbandonedError) Error() string {
	return fmt.Sprintf(
		"reducer handler for intent %s (domain %s) abandoned at shutdown after %s without returning: %v",
		e.intentID, e.domain, e.elapsed, e.cause,
	)
}

func (e *handlerAbandonedError) Unwrap() error { return e.cause }

func (e *handlerAbandonedError) FailureClass() string {
	return HandlerAbandonedAtShutdownFailureClass
}

func (e *handlerAbandonedError) Retryable() bool { return true }

// HandlerTimeoutFor resolves the configured handler bound for one domain:
// the per-domain override when present, else the default. Zero means no bound
// is configured. Step (a) of #8211 records the slot without enforcing it.
func (s Service) HandlerTimeoutFor(domain reducercontract.Domain) time.Duration {
	if s.HandlerTimeoutOverrides != nil {
		if bound, ok := s.HandlerTimeoutOverrides[domain]; ok {
			return bound
		}
	}
	return s.HandlerTimeout
}

// handlerOutcome carries one Executor.Execute result across the abandonment
// boundary. When the handler panicked, panicked is true and value holds the
// panic value for the caller to re-raise; the boolean (rather than a nil test
// on value) keeps panic(nil) unambiguous.
type handlerOutcome struct {
	result   Result
	err      error
	panicked bool
	value    any
}

// executeHandler runs Executor.Execute in a goroutine and waits for either its
// return or shutdown (shutdownCtx done). A handler that ignores its context
// and never returns must not wedge the worker: when shutdown arrives first,
// executeHandler reports abandoned=true and the caller MUST stop the lease
// heartbeat before failing the intent, so no renewal can land after the
// release (see failAbandonedHandler). The abandoned goroutine keeps whatever
// lifetime the handler gives it; its late result is dropped unread (the
// channel is buffered), and any late queue mutation it attempts is rejected
// by the lease fence (#7142).
//
// There is deliberately no deadline branch yet: worst-case handler durations
// are still unmeasured, so step (a) of #8211 releases only at shutdown and
// measures durations for the later enforcing step.
//
// A handler panic must unwind on the caller's goroutine exactly as a direct
// Executor.Execute call would: harnesses such as crashreplay recover crash
// sentinels by identity on the run goroutine, and a panic stranded in the
// spawned goroutine would kill the process instead. The spawned goroutine
// therefore recovers the panic, ships the value across done, and the caller
// re-raises it. A panic that lands after abandonment is dropped with the late
// outcome: the item is already released and the process is shutting down.
//
// Residual risk: an abandoned handler that later returns may still commit
// graph writes after Fail released the item. Queue mutations are fenced, but
// graph writes are only safe when the handler's writes are idempotent
// (MERGE); a non-idempotent late write can duplicate truth the retry already
// re-committed.
func (s Service) executeHandler(
	execCtx context.Context,
	shutdownCtx context.Context,
	intent reducercontract.Intent,
) (result Result, err error, abandoned bool) {
	done := make(chan handlerOutcome, 1)
	go func() {
		completed := false
		defer func() {
			if !completed {
				done <- handlerOutcome{panicked: true, value: recover()}
			}
		}()
		result, err := s.Executor.Execute(execCtx, intent)
		completed = true
		done <- handlerOutcome{result: result, err: err}
	}()
	reraise := func(out handlerOutcome) (Result, error, bool) {
		if out.panicked {
			panic(out.value)
		}
		return out.result, out.err, false
	}
	select {
	case out := <-done:
		return reraise(out)
	case <-shutdownCtx.Done():
		// Shutdown raced a finishing handler: when its outcome is already
		// available, prefer it over abandonment. Failing an item whose
		// handler actually completed would retry work that already
		// succeeded. Only when no outcome is observable is the handler
		// genuinely still running, and only then is it abandoned.
		select {
		case out := <-done:
			return reraise(out)
		default:
		}
		return Result{}, shutdownCtx.Err(), true
	}
}

// recordReducerHandlerDuration records one completed handler execution on the
// non-enforcing measurement histogram. Abandoned handlers never return, so
// they are counted on ReducerHandlerAbandoned instead; their elapsed time is
// visible on the abandonment log line.
func (s Service) recordReducerHandlerDuration(ctx context.Context, intent reducercontract.Intent, seconds float64) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.ReducerHandlerDuration.Record(ctx, seconds, metric.WithAttributes(
		telemetry.AttrDomain(string(intent.Domain)),
	))
}

// recordReducerHandlerAbandoned counts one handler abandoned at shutdown. The
// structured log line for the abandonment is the recordReducerResult "failed"
// line the caller emits with the same error, which carries the domain, the
// intent id, and the failure class.
func (s Service) recordReducerHandlerAbandoned(ctx context.Context, intent reducercontract.Intent) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.ReducerHandlerAbandoned.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(intent.Domain)),
	))
}

// failAbandonedHandler is the per-item abandonment tail for
// executeWithTelemetry. It stops the lease heartbeat BEFORE failing the
// intent, then triages through the existing WorkSink.Fail path (retry vs
// dead_letter via failIntent) and records the standard failed outcome plus
// the abandonment counter. Fail and the records run on a detached context:
// the shutdown that triggered the abandonment already cancelled the caller's
// context, and the release UPDATE must still reach the queue. A Fail rejected
// by the lease fence is a lost claim, recorded once like a lost heartbeat
// (#7142). Any other Fail error is returned so the run stops instead of
// silently dropping the item.
func (s Service) failAbandonedHandler(
	ctx context.Context,
	intent reducercontract.Intent,
	shutdownErr error,
	start time.Time,
	execStart time.Time,
	queueWait float64,
	workerID int,
	stopHeartbeat func() error,
) error {
	elapsed := time.Since(execStart)
	duration := time.Since(start).Seconds()
	abandonErr := &handlerAbandonedError{
		intentID: intent.IntentID,
		domain:   intent.Domain,
		elapsed:  elapsed,
		cause:    shutdownErr,
	}
	var err error = abandonErr
	if stopErr := stopHeartbeat(); stopErr != nil {
		err = errors.Join(err, stopErr)
	}
	failCtx := context.WithoutCancel(ctx)
	failErr := s.WorkSink.Fail(failCtx, intent, err)
	if failErr != nil && errors.Is(failErr, ErrExecutionClaimRejected) {
		s.recordReducerResult(failCtx, intent, Result{}, duration, queueWait, "lease_lost_during_execution", workerID, failErr)
		s.recordReducerHandlerAbandoned(failCtx, intent)
		return nil
	}
	s.recordReducerResult(failCtx, intent, Result{}, duration, queueWait, "failed", workerID, err)
	s.recordReducerHandlerAbandoned(failCtx, intent)
	if failErr != nil {
		return errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
	}
	return nil
}

// failAbandonedBatchHandler is the batch abandonment tail for executeAndReport
// (service_batch.go). Same stop-heartbeat-before-Fail ordering and Fail
// triage as failAbandonedHandler; the second return is false because Fail
// already terminalized the row (or the claim was lost), so the caller owes no
// acknowledgment. A non-nil error carries a don't-care false value, matching
// the executeAndReport contract.
func (s Service) failAbandonedBatchHandler(
	ctx context.Context,
	intent reducercontract.Intent,
	shutdownErr error,
	start time.Time,
	execStart time.Time,
	queueWait float64,
	workerID int,
	stopHeartbeat func() error,
) (batchAckItem, bool, error) {
	elapsed := time.Since(execStart)
	duration := time.Since(start).Seconds()
	abandonErr := &handlerAbandonedError{
		intentID: intent.IntentID,
		domain:   intent.Domain,
		elapsed:  elapsed,
		cause:    shutdownErr,
	}
	var err error = abandonErr
	if stopErr := stopHeartbeat(); stopErr != nil {
		err = errors.Join(err, stopErr)
	}
	failCtx := context.WithoutCancel(ctx)
	failErr := s.WorkSink.Fail(failCtx, intent, err)
	if failErr != nil && errors.Is(failErr, ErrExecutionClaimRejected) {
		s.recordReducerResult(failCtx, intent, Result{}, duration, queueWait, "lease_lost_during_execution", workerID, failErr)
		s.recordReducerHandlerAbandoned(failCtx, intent)
		return batchAckItem{}, false, nil
	}
	s.recordReducerResult(failCtx, intent, Result{}, duration, queueWait, "failed", workerID, err)
	s.recordReducerHandlerAbandoned(failCtx, intent)
	if failErr != nil {
		return batchAckItem{}, false, errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
	}
	return batchAckItem{}, false, nil
}

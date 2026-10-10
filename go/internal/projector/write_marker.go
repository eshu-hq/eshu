// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// The projection write-through contract (#7389): a claimed generation records
// that it is about to write the canonical graph and content store before its
// first write, after LoadFacts. Once the marker is set the heartbeat supersede
// no longer retires the generation for a newer pending one, so the attempt
// runs to Ack; a newer delta that was diffed from the same active commit is
// then refused by the #7319 preflight instead of activating over this
// generation's overlay. A generation that wrote and still never activated (a
// dead letter, an expired lease that was replaced, or an Ack refusal) keeps
// its marker, and the git collector forces its next sync to a full snapshot.

// errWriteMarkerMissing is returned when a projection loop runs without its
// write marker. Wiring rejects this at startup; the check here keeps a nil
// marker from ever meaning "write without recording it".
var errWriteMarkerMissing = errors.New("projector write marker is required")

// DefaultWriteMarkerMaxAttempts bounds how many times MarkProjectionWriteStarted
// re-runs a marker whose lock wait timed out. With the Postgres queue's 2 s
// lock timeout it gives a busy generation row about five minutes, the same
// budget AckWhenScopeFree gives a busy scope row.
const DefaultWriteMarkerMaxAttempts = 150

// ProjectionWriteMarker records that a claimed generation is about to write
// the graph. The Postgres projector queue implements it. Every projection loop
// takes it as an explicitly wired, required dependency, never discovered by a
// type assertion, so a wrapper cannot silently drop it.
type ProjectionWriteMarker interface {
	// MarkProjectionWriteStarted sets the generation's write-start marker to
	// the latest write start, fenced on this attempt's claim. It returns an
	// error wrapping failure.ErrWorkWriteMarkerDeferred when its lock wait
	// timed out (nothing changed), failure.ErrWorkSuperseded when the
	// generation is retired, and failure.ErrWorkClaimLost when this attempt
	// lost its claim.
	MarkProjectionWriteStarted(context.Context, ScopeGenerationWork) error
}

// writeMarkerRetryableError makes a marker that could not decide fail closed:
// the work does not project, and the queue retries it.
type writeMarkerRetryableError struct{ err error }

// Error reports the underlying failure.
func (e writeMarkerRetryableError) Error() string { return e.err.Error() }

// Unwrap exposes the underlying failure.
func (e writeMarkerRetryableError) Unwrap() error { return e.err }

// Retryable marks the failure retryable for failure.IsRetryable.
func (writeMarkerRetryableError) Retryable() bool { return true }

// MarkProjectionWriteStarted runs marker before a claimed generation's first
// graph or content write (#7389). It re-runs the marker while it reports
// failure.ErrWorkWriteMarkerDeferred, up to DefaultWriteMarkerMaxAttempts, and
// relies on the caller's heartbeat to keep the lease meanwhile. onDeferred,
// when set, is called with the retry number and the deferral cause after each
// deferral. Deferrals are paced (#7907): the loop waits out
// writeMarkerDeferralBackoff before re-running, since a fence-busy deferral
// lands in milliseconds and would otherwise burn the bound in under a second.
//
// When instruments is set, each deferral increments
// eshu_dp_projector_write_marker_deferrals_total and a marker that waited
// records its wait in eshu_dp_projector_write_marker_wait_seconds; both
// carry a closed outcome (#7470). A nil instruments disables both.
//
// It returns nil when projection may write. failure.ErrWorkSuperseded and
// failure.ErrWorkClaimLost pass through unchanged: the caller drops the work
// without writing. A canceled ctx returns the context error. Any other
// failure, including exhausted retries, returns a retryable error: the caller
// must not project and routes it to the queue's Fail path.
func MarkProjectionWriteStarted(
	ctx context.Context,
	marker ProjectionWriteMarker,
	work ScopeGenerationWork,
	instruments *telemetry.Instruments,
	onDeferred func(retry int, cause string),
) (err error) {
	if marker == nil {
		return errWriteMarkerMissing
	}
	start := time.Now()
	deferrals := 0
	// stopOutcome pins the classification of a wait the loop itself ended, so
	// the counter and histogram agree even if ctx is canceled in between.
	stopOutcome := ""
	defer func() {
		if deferrals == 0 {
			return
		}
		outcome := stopOutcome
		if outcome == "" {
			outcome = writeMarkerWaitOutcome(ctx, err)
		}
		recordWriteMarkerWait(ctx, instruments, time.Since(start), outcome)
	}()
	for attempt := 1; ; attempt++ {
		err = marker.MarkProjectionWriteStarted(ctx, work)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, failure.ErrWorkSuperseded), errors.Is(err, failure.ErrWorkClaimLost):
			return err
		case ctx.Err() != nil:
			if errors.Is(err, failure.ErrWorkWriteMarkerDeferred) {
				stopOutcome = writeMarkerOutcomeShutdown
				deferrals++
				recordWriteMarkerDeferral(ctx, instruments, stopOutcome)
			}
			return fmt.Errorf("mark projection write started: %w", errors.Join(ctx.Err(), err))
		case !errors.Is(err, failure.ErrWorkWriteMarkerDeferred):
			return writeMarkerRetryableError{fmt.Errorf("mark projection write started: %w", err)}
		case attempt >= DefaultWriteMarkerMaxAttempts:
			stopOutcome = writeMarkerOutcomeGaveUp
			deferrals++
			recordWriteMarkerDeferral(ctx, instruments, stopOutcome)
			return writeMarkerRetryableError{fmt.Errorf("mark projection write started after %d attempts: %w", attempt, err)}
		}
		deferrals++
		cause := writeMarkerDeferralCause(err)
		if onDeferred != nil {
			onDeferred(attempt, cause)
		}
		if sleepErr := (*writeMarkerDeferralSleep.Load())(ctx, writeMarkerDeferralBackoff(deferrals)); sleepErr != nil {
			stopOutcome = writeMarkerOutcomeShutdown
			recordWriteMarkerDeferral(ctx, instruments, stopOutcome)
			return fmt.Errorf("mark projection write started: %w", errors.Join(ctx.Err(), err))
		}
		recordWriteMarkerDeferral(ctx, instruments, writeMarkerOutcomeRetried)
	}
}

// writeMarkerDeferralBackoff bounds.
const (
	// writeMarkerDeferralInitialBackoff is the wait after the first deferral.
	writeMarkerDeferralInitialBackoff = 10 * time.Millisecond
	// writeMarkerDeferralMaxBackoff caps the doubling wait. A full bound of
	// millisecond deferrals then waits about 29 s, inside the caller's
	// ~5 minute budget, instead of giving up in under a second.
	writeMarkerDeferralMaxBackoff = 200 * time.Millisecond
)

// writeMarkerDeferralBackoff paces consecutive deferrals: 10 ms, doubling per
// deferral, capped at 200 ms. deferral counts from 1.
func writeMarkerDeferralBackoff(deferral int) time.Duration {
	backoff := writeMarkerDeferralInitialBackoff
	for i := 1; i < deferral && backoff < writeMarkerDeferralMaxBackoff; i++ {
		backoff *= 2
	}
	if backoff > writeMarkerDeferralMaxBackoff {
		backoff = writeMarkerDeferralMaxBackoff
	}
	return backoff
}

// writeMarkerSleepFunc waits out a deferral backoff, returning ctx.Err() when
// the wait ends early.
type writeMarkerSleepFunc func(ctx context.Context, backoff time.Duration) error

// writeMarkerDeferralSleep is the loop's pacing seam. It defaults to a
// ctx-aware timer; tests swap it atomically.
var writeMarkerDeferralSleep atomic.Pointer[writeMarkerSleepFunc]

func init() {
	real := writeMarkerSleepFunc(func(ctx context.Context, backoff time.Duration) error {
		timer := time.NewTimer(backoff)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
	writeMarkerDeferralSleep.Store(&real)
}

// Closed deferral causes for the marker wait log. A fence-busy deferral wraps
// failure.ErrWorkWriteMarkerFenceBusy; anything else deferred is a generation
// row wait, the only deferral cause before #7819.
const (
	writeMarkerDeferralCauseFenceBusy     = "fence_busy"
	writeMarkerDeferralCauseGenerationRow = "generation_row"
)

// writeMarkerDeferralCause classifies a deferral error for the wait log.
func writeMarkerDeferralCause(err error) string {
	if errors.Is(err, failure.ErrWorkWriteMarkerFenceBusy) {
		return writeMarkerDeferralCauseFenceBusy
	}
	return writeMarkerDeferralCauseGenerationRow
}

// Closed outcome values for the write-marker wait metrics.
const (
	writeMarkerOutcomeRetried    = "retried"
	writeMarkerOutcomeGaveUp     = "gave_up"
	writeMarkerOutcomeShutdown   = "shutdown"
	writeMarkerOutcomeWritten    = "written"
	writeMarkerOutcomeSuperseded = "superseded"
	writeMarkerOutcomeClaimLost  = "claim_lost"
	writeMarkerOutcomeFailed     = "failed"
)

// writeMarkerWaitOutcome maps MarkProjectionWriteStarted's return to the
// terminal outcome of a wait. A deferral error means the retry bound ran out,
// mirroring how the callers classify it.
func writeMarkerWaitOutcome(ctx context.Context, err error) string {
	switch {
	case err == nil:
		return writeMarkerOutcomeWritten
	case errors.Is(err, failure.ErrWorkSuperseded):
		return writeMarkerOutcomeSuperseded
	case errors.Is(err, failure.ErrWorkClaimLost):
		return writeMarkerOutcomeClaimLost
	case ctx.Err() != nil:
		return writeMarkerOutcomeShutdown
	case errors.Is(err, failure.ErrWorkWriteMarkerDeferred):
		return writeMarkerOutcomeGaveUp
	default:
		return writeMarkerOutcomeFailed
	}
}

// recordWriteMarkerDeferral counts one deferred marker attempt. It records on
// an uncanceled context so a shutdown deferral is still exported.
func recordWriteMarkerDeferral(ctx context.Context, instruments *telemetry.Instruments, outcome string) {
	if instruments == nil {
		return
	}
	instruments.ProjectorWriteMarkerDeferrals.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	))
}

// recordWriteMarkerWait records how long a deferred marker waited before its
// terminal outcome.
func recordWriteMarkerWait(ctx context.Context, instruments *telemetry.Instruments, wait time.Duration, outcome string) {
	if instruments == nil {
		return
	}
	instruments.ProjectorWriteMarkerWaitDuration.Record(context.WithoutCancel(ctx), wait.Seconds(), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	))
}

// markProjectionWriteStarted runs MarkProjectionWriteStarted for processWork
// after LoadFacts and before Runner.Project, and reports whether it handled
// the work item. projectCtx is the heartbeat context; stopHeartbeat is checked
// first, so a supersede or claim loss the heartbeat saw wins. Superseded work
// and a lost claim are dropped with fact_count 0, since nothing was projected;
// a shutdown is recorded as canceled; any other failure is routed to Fail.
func (s Service) markProjectionWriteStarted(
	workCtx context.Context,
	projectCtx context.Context,
	work ScopeGenerationWork,
	stopHeartbeat projectorHeartbeatStop,
	start time.Time,
	workerID int,
) (bool, error) {
	err := MarkProjectionWriteStarted(projectCtx, s.WriteMarker, work, s.Instruments,
		WriteMarkerDeferredLogger(workCtx, s.Logger, work, workerID))
	if err == nil {
		return false, nil
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		if s.recordSupersededWork(workCtx, work, start, 0, heartbeatErr, workerID) ||
			s.recordClaimLostWork(workCtx, work, start, 0, heartbeatErr, "heartbeat", workerID) {
			return true, nil
		}
		err = errors.Join(err, heartbeatErr)
	}
	switch {
	case s.recordSupersededWork(workCtx, work, start, 0, err, workerID):
		return true, nil
	case s.recordClaimLostWork(workCtx, work, start, 0, err, "write_marker", workerID):
		return true, nil
	case projectorShutdownCanceled(workCtx, err):
		s.recordProjectionShutdownCanceled(workCtx, work, start, 0, err, workerID)
		return true, nil
	default:
		return true, s.failWork(workCtx, work, start, 0, err, workerID)
	}
}

// writeMarkerDeferredLogEvery spaces repeated deferral logs; with the Postgres
// queue's 2 s lock timeout this is roughly one line per minute of waiting on
// a generation row, and about one line per few seconds once paced fence-busy
// deferrals reach the backoff cap.
const writeMarkerDeferredLogEvery = 30

// WriteMarkerDeferredLogger returns an onDeferred callback for
// MarkProjectionWriteStarted that logs WARN on the first marker deferral and
// every writeMarkerDeferredLogEvery retries after, with the scope, generation,
// attempt, retry count and deferral cause, so a row held for a long time is
// visible without a line per deferral. A nil logger returns nil.
func WriteMarkerDeferredLogger(ctx context.Context, logger *slog.Logger, work ScopeGenerationWork, workerID int) func(int, string) {
	if logger == nil {
		return nil
	}
	return func(retry int, cause string) {
		if retry != 1 && retry%writeMarkerDeferredLogEvery != 0 {
			return
		}
		scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
		attrs := make([]any, 0, len(scopeAttrs)+6)
		for _, attr := range scopeAttrs {
			attrs = append(attrs, attr)
		}
		attrs = append(attrs,
			log.Queue("projector"),
			slog.Int("marker_retry", retry),
			slog.Int("attempt_count", work.AttemptCount),
			slog.String("deferral_cause", cause),
			log.WorkerID(fmt.Sprintf("%d", workerID)),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
		msg := "projector write marker waiting for busy generation row"
		if cause == writeMarkerDeferralCauseFenceBusy {
			msg = "projector write marker waiting for busy claim fence"
		}
		logger.WarnContext(context.WithoutCancel(ctx), msg, attrs...)
	}
}

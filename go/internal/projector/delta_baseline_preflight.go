// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// errDeltaBaselineFenceMissing is returned when a projection loop runs without
// its fence. Wiring rejects this at startup; the check here keeps a nil fence
// from ever meaning "unfenced".
var errDeltaBaselineFenceMissing = errors.New("projector delta baseline fence is required")

// deltaBaselineRetryableError makes a fence that could not decide fail closed:
// the work does not project, and the queue retries it.
type deltaBaselineRetryableError struct{ err error }

// Error reports the underlying failure.
func (e deltaBaselineRetryableError) Error() string { return e.err.Error() }

// Unwrap exposes the underlying failure.
func (e deltaBaselineRetryableError) Unwrap() error { return e.err }

// Retryable marks the failure retryable for failure.IsRetryable.
func (deltaBaselineRetryableError) Retryable() bool { return true }

// PreflightDeltaBaseline runs the delta-baseline fence before a claimed
// generation loads facts or writes the graph (#7319). Every projection loop
// calls it first, before its heartbeat, the large-generation semaphore, and
// LoadFacts. It takes no lock: a false refusal is safe (the collector diffs
// again from the active commit) and a false pass is caught by the Ack check.
//
// It returns nil when projection may proceed. A refusal marks the work and
// its generation superseded and returns an error wrapping
// failure.ErrWorkSuperseded, or one wrapping failure.ErrWorkClaimLost when
// this attempt lost the claim first. A read failure, a missing generation row,
// or a failed mark returns a retryable error: the caller must not project and
// routes it to the queue's Fail path. Passes are not counted here; Ack counts
// them once.
func PreflightDeltaBaseline(
	ctx context.Context,
	fence DeltaBaselineFence,
	work ScopeGenerationWork,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	if fence == nil {
		return errDeltaBaselineFenceMissing
	}
	state, err := fence.ReadDeltaBaseline(ctx, work)
	if err != nil {
		return deltaBaselineRetryableError{fmt.Errorf("read delta baseline: %w", err)}
	}
	outcome := DecideDeltaBaseline(state)
	if outcome == DeltaBaselineTargetMissing {
		return deltaBaselineRetryableError{fmt.Errorf("read delta baseline: generation %s not found",
			work.Generation.GenerationID)}
	}
	if !outcome.Refused() {
		return nil
	}
	refusal := DeltaBaselineRefusal{Phase: DeltaBaselinePhasePreflight, Outcome: outcome, State: state}
	markErr := fence.RefuseDeltaBaseline(ctx, work, refusal)
	switch {
	case errors.Is(markErr, failure.ErrWorkSuperseded):
		RecordDeltaBaselineFence(ctx, instruments, refusal.Phase, outcome)
		LogDeltaBaselineRefusal(ctx, logger, work, refusal)
		return markErr
	case errors.Is(markErr, failure.ErrWorkClaimLost):
		return markErr
	case markErr == nil:
		return deltaBaselineRetryableError{errors.New("mark delta baseline refusal: fence reported no outcome")}
	default:
		return deltaBaselineRetryableError{fmt.Errorf("mark delta baseline refusal: %w", markErr)}
	}
}

// LogDeltaBaselineRefusal logs one refusal and adds a span event to the
// active span. A preflight refusal is WARN: the graph was not touched and the
// collector re-diffs. An Ack refusal is ERROR: the generation already wrote
// its overlay, which means two claims were valid in one scope. Commit SHAs go
// in the log and the span, never in metric labels. A nil logger skips the log.
func LogDeltaBaselineRefusal(
	ctx context.Context,
	logger *slog.Logger,
	work ScopeGenerationWork,
	refusal DeltaBaselineRefusal,
) {
	ctx = context.WithoutCancel(ctx)
	activeCommit := refusal.ActiveCommitForLog()
	trace.SpanFromContext(ctx).AddEvent("projector.delta_baseline_refused", trace.WithAttributes(
		attribute.String("fence_phase", refusal.Phase),
		attribute.String("outcome", string(refusal.Outcome)),
		attribute.String("delta_baseline_commit_sha", refusal.State.BaselineCommitSHA),
		attribute.String("active_commit_sha", activeCommit),
	))
	if logger == nil {
		return
	}
	scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
	attrs := make([]any, 0, len(scopeAttrs)+9)
	for _, attr := range scopeAttrs {
		attrs = append(attrs, attr)
	}
	attrs = append(attrs,
		log.Queue("projector"),
		log.Status("superseded"),
		slog.String("fence_phase", refusal.Phase),
		slog.String("outcome", string(refusal.Outcome)),
		slog.String("delta_baseline_commit_sha", refusal.State.BaselineCommitSHA),
		slog.String("active_commit_sha", activeCommit),
		slog.String("active_generation_id", refusal.State.ActiveGenerationID),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
		telemetry.FailureClassAttr(refusal.FailureClass()),
	)
	if refusal.Phase == DeltaBaselinePhaseAck {
		logger.ErrorContext(ctx, "projector delta refused at ack after projection: baseline is not the active commit", attrs...)
		return
	}
	logger.WarnContext(ctx, "projector delta refused before projection: baseline is not the active commit", attrs...)
}

// preflightDeltaBaseline runs PreflightDeltaBaseline for processWork and
// reports whether it handled the work item. A refused delta and a lost claim
// are dropped; a fence that could not decide routes the retryable cause to
// Fail without projecting.
func (s Service) preflightDeltaBaseline(
	ctx context.Context,
	work ScopeGenerationWork,
	start time.Time,
	workerID int,
) (bool, error) {
	err := PreflightDeltaBaseline(ctx, s.DeltaBaselineFence, work, s.Instruments, s.Logger)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, failure.ErrWorkSuperseded):
		return true, nil
	case s.recordClaimLostWork(ctx, work, start, 0, err, "delta_baseline", workerID):
		return true, nil
	default:
		return true, s.failWork(ctx, work, start, 0, err, workerID)
	}
}

// IsDeltaBaselineRefusal reports whether err is a delta-baseline refusal from
// either fence phase: a superseded outcome whose failure class is one of the
// #7319 classes. Loops use it to avoid logging a refusal as "superseded by
// newer generation".
func IsDeltaBaselineRefusal(err error) bool {
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		return false
	}
	class := supersededFailureClass(err)
	return class == DeltaBaselineMismatchClass || class == DeltaBaselineMismatchAfterProjectionClass
}

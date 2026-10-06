// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (r *ActivationObligationRunner) settle(ctx context.Context, cfg ActivationObligationRunnerConfig, work ActivationObligation) {
	ctx, span := r.startSettleSpan(ctx, work)
	verdict := r.settleObligation(ctx, cfg, work)
	endSettleSpan(span, verdict)
}

// settleVerdict is how one settle ended, for its span.
type settleVerdict struct {
	outcome string
	hold    string
	failure string
	err     error
}

func (r *ActivationObligationRunner) settleObligation(ctx context.Context, cfg ActivationObligationRunnerConfig, work ActivationObligation) settleVerdict {
	if r.Instruments != nil && !work.CreatedAt.IsZero() {
		r.Instruments.ActivationObligationClaimAge.Record(ctx, time.Since(work.CreatedAt).Seconds())
	}
	result, err := r.Store.FinalizeActivation(ctx, work)
	if err == nil && result.Outcome == ActivationOutcomePhaseNotReady {
		r.recordOutcome(ctx, work, result)
		started := time.Now()
		maintainCtx, cancel := maintenanceContext(ctx, work.LeaseUntil, cfg.Lease)
		maintainErr := r.Maintainer.MaintainActivation(maintainCtx, work)
		timedOut := maintainErr != nil && ctx.Err() == nil && errors.Is(maintainCtx.Err(), context.DeadlineExceeded)
		cancel()
		var hold *ActivationHoldError
		r.recordMaintenance(ctx, time.Since(started), maintainErr)
		switch {
		case errors.Is(maintainErr, ErrActivationInapplicable):
			result, err = r.Store.RetireActivationInapplicable(ctx, work)
		case errors.As(maintainErr, &hold):
			// Held, not failed: the lease stays and the obligation is retried
			// at lease cadence; the epoch whole pass republishes the phase.
			r.recordHeld(ctx, work, hold.Reason(), maintainErr)
			return settleVerdict{outcome: ActivationOutcomePhaseNotReady, hold: hold.Reason()}
		case timedOut:
			// Cancelled before the lease ends, so no second owner runs it
			// concurrently; retried by any claimer after the lease expires.
			r.recordFailure(ctx, "maintenance_timeout", maintainErr, &work)
			return settleVerdict{outcome: ActivationOutcomePhaseNotReady, failure: "maintenance_timeout", err: maintainErr}
		case maintainErr != nil:
			// The lease stays held; the obligation is retried after it expires.
			r.recordFailure(ctx, "maintenance", maintainErr, &work)
			return settleVerdict{outcome: ActivationOutcomePhaseNotReady, failure: "maintenance", err: maintainErr}
		default:
			result, err = r.Store.FinalizeActivation(ctx, work)
		}
	}
	if err != nil {
		if errors.Is(err, ErrActivationLeaseLost) {
			r.recordOutcome(ctx, work, ActivationFinalizeResult{Outcome: activationOutcomeLeaseLost})
			return settleVerdict{outcome: activationOutcomeLeaseLost}
		}
		if errors.Is(err, ErrActivationFinalizeLockTimeout) {
			// Expected contention (an ingestion commit or Ack held the scope
			// row past the lock timeout); nothing was written and the next
			// claimer settles it. Its own reason, at Warn, and no span error.
			r.recordFailureAt(ctx, slog.LevelWarn, activationFailureFinalizeLockTimeout, err, &work)
			r.recordOutcome(ctx, work, ActivationFinalizeResult{Outcome: activationOutcomeError})
			return settleVerdict{outcome: activationOutcomeError, failure: activationFailureFinalizeLockTimeout}
		}
		r.recordFailure(ctx, "finalize", err, &work)
		r.recordOutcome(ctx, work, ActivationFinalizeResult{Outcome: activationOutcomeError})
		return settleVerdict{outcome: activationOutcomeError, failure: "finalize", err: err}
	}
	r.recordOutcome(ctx, work, result)
	return settleVerdict{outcome: result.Outcome}
}

// startSettleSpan starts the settle span. Without a tracer it installs a
// non-recording span, so nothing is ever written onto a span the caller's
// context already carries.
func (r *ActivationObligationRunner) startSettleSpan(ctx context.Context, work ActivationObligation) (context.Context, trace.Span) {
	if r.Tracer == nil {
		span := trace.SpanFromContext(context.Background())
		return trace.ContextWithSpan(ctx, span), span
	}
	return r.Tracer.Start(ctx, telemetry.SpanReducerActivationObligationSettle, trace.WithAttributes(
		attribute.String(telemetry.LogKeyScopeID, work.ScopeID),
		attribute.String(telemetry.LogKeyGenerationID, work.GenerationID),
		attribute.Int64("claim_token", work.LeaseToken),
	))
}

// endSettleSpan records how the settle ended. A hold is a designed outcome,
// not a trace error; a failed callback or finalize is.
func endSettleSpan(span trace.Span, verdict settleVerdict) {
	span.SetAttributes(attribute.String("outcome", verdict.outcome))
	if verdict.hold != "" {
		span.SetAttributes(attribute.String("hold_reason", verdict.hold))
	}
	if verdict.failure != "" {
		span.SetAttributes(attribute.String("failure_reason", verdict.failure))
	}
	if verdict.err != nil {
		span.RecordError(verdict.err)
		span.SetStatus(codes.Error, verdict.failure)
	}
	span.End()
}

// maintenanceContext bounds one maintenance callback by the obligation's
// lease (D1R-3): the callback is cancelled a margin (a fifth of the lease)
// before the lease ends, so it cannot still be running when another replica
// reclaims the obligation and starts the same maintenance. The margin also
// absorbs clock skew between the database clock that stamped lease_until and
// this process's clock. A pass that needs longer than the lease never
// finishes and is counted as maintenance_timeout: size the lease above the
// pass's p99. A zero leaseUntil (no lease information) leaves ctx unbounded.
func maintenanceContext(ctx context.Context, leaseUntil time.Time, lease time.Duration) (context.Context, context.CancelFunc) {
	if leaseUntil.IsZero() {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, leaseUntil.Add(-lease/5))
}

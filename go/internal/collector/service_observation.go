// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// StartObserveFunc starts a collector observe span around source work that has
// proven it is attempting a generation instead of reporting an idle poll.
type StartObserveFunc func(context.Context) CollectorObservation

// CollectorObservation carries the context and start time for one
// collector.observe span. Sources that implement ObservedSource return this
// value so Service can finish the same span after durable commit.
type CollectorObservation struct {
	Context   context.Context
	Span      trace.Span
	StartedAt time.Time
}

// ObservedSource lets a source delay collector.observe creation until it knows
// the poll is a real collection attempt. This avoids emitting trace spans for
// idle polls while still allowing synchronous sources to include source reads
// in the same span as durable commit.
type ObservedSource interface {
	NextObserved(context.Context, StartObserveFunc) (CollectedGeneration, bool, CollectorObservation, error)
}

// nextWithObservation wraps both Source.Next and ObservedSource.NextObserved
// errors with call-site context (wrapcheck): callers that inspect the cause
// (Run's ctx.Err()/errors.Is check) still see it through the %w chain.
func (s Service) nextWithObservation(ctx context.Context) (
	CollectedGeneration,
	bool,
	CollectorObservation,
	error,
) {
	if observed, ok := s.Source.(ObservedSource); ok {
		collected, ok, observation, err := observed.NextObserved(ctx, s.startCollectorObserve)
		if err != nil {
			return collected, ok, observation, fmt.Errorf("observed source next: %w", err)
		}
		return collected, ok, observation, nil
	}
	collected, ok, err := s.Source.Next(ctx)
	if err != nil {
		return collected, ok, CollectorObservation{}, fmt.Errorf("source next: %w", err)
	}
	return collected, ok, CollectorObservation{}, nil
}

func (s Service) startCollectorObserve(ctx context.Context) CollectorObservation {
	observeStartedAt := time.Now()
	if s.Tracer != nil {
		observedCtx, span := s.Tracer.Start(ctx, telemetry.SpanCollectorObserve)
		return CollectorObservation{
			Context:   observedCtx,
			Span:      span,
			StartedAt: observeStartedAt,
		}
	}
	return CollectorObservation{
		Context:   ctx,
		StartedAt: observeStartedAt,
	}
}

func (s Service) annotateCollectorObserve(observation CollectorObservation, collected CollectedGeneration) {
	if observation.Span == nil {
		return
	}
	observation.Span.SetAttributes(
		telemetry.AttrScopeID(collected.Scope.ScopeID),
		telemetry.AttrSourceSystem(collected.Scope.SourceSystem),
		telemetry.AttrCollectorKind(string(collected.Scope.CollectorKind)),
	)
}

func (s Service) endCollectorObserve(observation CollectorObservation, err error) {
	if observation.Span == nil {
		return
	}
	if err != nil {
		observation.Span.RecordError(err)
		observation.Span.SetStatus(codes.Error, err.Error())
	}
	observation.Span.End()
}

// cycleSkipOf reports whether err is a retryable source failure that opted
// into the cycle-skip path (CycleSkipper), and returns its description. A
// retryable error without that opt-in stays fatal.
func cycleSkipOf(err error) (CycleSkip, bool) {
	if !IsRetryable(err) {
		return CycleSkip{}, false
	}
	var skipper CycleSkipper
	if !errors.As(err, &skipper) {
		return CycleSkip{}, false
	}
	return skipper.CycleSkip(), true
}

// delay returns how long to wait before the next discovery cycle: RetryAfter,
// or fallback when the source named none.
func (c CycleSkip) delay(fallback time.Duration) time.Duration {
	if c.RetryAfter > 0 {
		return c.RetryAfter
	}
	return fallback
}

// wait waits out d through Service.Wait, or a context-aware timer when nil.
func (s Service) wait(ctx context.Context, d time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, d)
	}
	return waitForNextPoll(ctx, d)
}

// recordSkippedCycle counts one skipped discovery cycle and writes the WARN
// line an operator uses to tell a skipped cycle from a teardown.
func (s Service) recordSkippedCycle(ctx context.Context, skip CycleSkip, cause error) {
	if s.Instruments != nil && s.Instruments.CollectorDiscoverySkippedCycles != nil {
		s.Instruments.CollectorDiscoverySkippedCycles.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCollectorKind(skip.CollectorKind),
			telemetry.AttrOperation(skip.Operation),
			telemetry.AttrFailureClass(skip.FailureClass),
		))
	}
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(ctx, "collector discovery retryable; skipping cycle",
		log.CollectorKind(skip.CollectorKind),
		telemetry.FailureClassAttr(skip.FailureClass),
		log.Operation(skip.Operation),
		slog.Int("attempt", skip.Attempts),
		slog.Int("consecutive_failures", skip.Consecutive),
		slog.Float64("retry_after_seconds", skip.delay(s.PollInterval).Seconds()),
		slog.Bool("retryable", true),
		log.Err(cause),
	)
}

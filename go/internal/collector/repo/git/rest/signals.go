// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// retryEventName is the span event written once per GitHub API retry.
const retryEventName = "github.api_retry"

// Attempt outcomes, the closed value set of the outcome label on
// eshu_dp_github_api_attempts_total.
const (
	outcomeSuccess = "success"
	outcomeRetried = "retried"
	outcomeFailed  = "failed"
)

// classNone is the failure_class of a successful attempt.
const classNone = "none"

type observerKey struct{}

// observer carries the telemetry Do writes to. It rides on the context because
// discovery reaches Do through function values that take no instruments.
type observer struct {
	instruments *telemetry.Instruments
	logger      *slog.Logger
}

// WithObserver returns ctx carrying the instruments and logger Do uses. Both
// may be nil.
func WithObserver(ctx context.Context, instruments *telemetry.Instruments, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, observerKey{}, observer{instruments: instruments, logger: logger})
}

func observerFrom(ctx context.Context) observer {
	o, _ := ctx.Value(observerKey{}).(observer)
	return o
}

// recordAttempt counts one attempt of a GitHub API call. class is classNone on
// success.
func recordAttempt(ctx context.Context, operation, class, outcome string) {
	instruments := observerFrom(ctx).instruments
	if instruments == nil || instruments.GitHubAPIAttempts == nil {
		return
	}
	instruments.GitHubAPIAttempts.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, operation),
		attribute.String(telemetry.MetricDimensionFailureClass, class),
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	))
}

// recordRetry counts one retry that is about to start, marks the active span,
// and writes the structured log line. It runs after the backoff so a shutdown
// during the wait counts no retry that never ran.
func recordRetry(ctx context.Context, operation, class string, attempt, status int, wait time.Duration) {
	trace.SpanFromContext(ctx).AddEvent(retryEventName, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, operation),
		attribute.String(telemetry.MetricDimensionFailureClass, class),
		attribute.Int("github.attempt", attempt),
		attribute.Int("github.status", status),
		attribute.Float64("github.backoff_seconds", wait.Seconds()),
	))
	o := observerFrom(ctx)
	if o.instruments != nil && o.instruments.GitHubAPIRetries != nil {
		o.instruments.GitHubAPIRetries.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOperation, operation),
			attribute.String(telemetry.MetricDimensionFailureClass, class),
		))
	}
	if o.logger != nil {
		o.logger.WarnContext(ctx, "GitHub API call retrying",
			log.CollectorKind("git"),
			log.Operation(operation),
			telemetry.FailureClassAttr(class),
			slog.Int("attempt", attempt),
			slog.Int("status", status),
			slog.Float64("backoff_seconds", wait.Seconds()),
			slog.Bool("retryable", true),
		)
	}
}

// RecordConsecutiveFailures sets the consecutive-failure gauge the selector
// owns: the count after a retryable failure, or 0 after a good selection.
func RecordConsecutiveFailures(ctx context.Context, instruments *telemetry.Instruments, count int) {
	if instruments == nil || instruments.CollectorDiscoveryConsecutiveFailures == nil {
		return
	}
	instruments.CollectorDiscoveryConsecutiveFailures.Record(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrCollectorKind(collectorKind),
	))
}

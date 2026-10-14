// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Attempt outcomes, the values of the outcome label.
const (
	outcomeSuccess = "success"
	outcomeRetried = "retried"
	outcomeFailed  = "failed"
	// classNone is the failure_class of a successful attempt.
	classNone = "none"

	retryEventName = "git.command_retry"
	maxErrorLine   = 300
)

// attemptReport is everything the signals of one finished attempt are built
// from.
type attemptReport struct {
	command         Command
	limit           time.Duration
	attempt         int
	attempts        int
	elapsed         time.Duration
	err             error
	stderr          string
	class           string
	outcome         string
	willRetry       bool
	budgetExhausted bool
	cleanupErr      error
	backoff         time.Duration
}

// recordAttempt writes the metrics, the log line, and (for a retry) the span event of
// one attempt. The repository is in the log and the span event only; metric
// labels stay bounded (operation, host, failure_class, outcome).
func recordAttempt(ctx context.Context, sweep *sweepState, r attemptReport) {
	instruments, _ := ctx.Value(metricsKey{}).(*telemetry.Instruments)
	recordMetrics(ctx, instruments, r)
	var logger *slog.Logger
	if sweep != nil {
		logger = sweep.logger
	}
	switch {
	case r.outcome == outcomeSuccess && r.attempt > 1 && logger != nil:
		logger.InfoContext(ctx, fmt.Sprintf("git command recovered on attempt %d", r.attempt), r.logAttrs()...)
	case r.outcome != outcomeSuccess && logger != nil:
		logger.WarnContext(ctx, "git command attempt failed", r.logAttrs()...)
	}
}

// recordRetryStarted counts one retry and marks the active span. It runs after
// the backoff, when the next attempt is about to start, so a shutdown during
// the wait counts no retry that never ran.
func recordRetryStarted(ctx context.Context, r attemptReport) {
	trace.SpanFromContext(ctx).AddEvent(retryEventName, trace.WithAttributes(r.spanAttrs()...))
	instruments, _ := ctx.Value(metricsKey{}).(*telemetry.Instruments)
	if instruments == nil || instruments.GitCommandRetries == nil {
		return
	}
	instruments.GitCommandRetries.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, r.command.Operation),
		attribute.String(telemetry.MetricDimensionHost, r.command.Host),
		attribute.String(telemetry.MetricDimensionFailureClass, r.class),
	))
}

func recordMetrics(ctx context.Context, instruments *telemetry.Instruments, r attemptReport) {
	if instruments == nil {
		return
	}
	operation := attribute.String(telemetry.MetricDimensionOperation, r.command.Operation)
	host := attribute.String(telemetry.MetricDimensionHost, r.command.Host)
	class := attribute.String(telemetry.MetricDimensionFailureClass, r.class)
	outcome := attribute.String(telemetry.MetricDimensionOutcome, r.outcome)
	if instruments.GitCommandAttempts != nil {
		instruments.GitCommandAttempts.Add(ctx, 1, metric.WithAttributes(operation, host, class, outcome))
	}
	if instruments.GitCommandAttemptDuration != nil {
		instruments.GitCommandAttemptDuration.Record(ctx, r.elapsed.Seconds(), metric.WithAttributes(operation, outcome))
	}
	if r.budgetExhausted && instruments.GitCommandRetryBudgetExhausted != nil {
		instruments.GitCommandRetryBudgetExhausted.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionScope, telemetry.GitRetryScopeSweep),
		))
	}
}

// logAttrs are the fields of the per-attempt log line.
func (r attemptReport) logAttrs() []any {
	attrs := []any{
		log.CollectorKind("git"),
		log.Operation(r.command.Operation),
		slog.String("host", r.command.Host),
		log.RepositoryID(r.command.Scope),
		slog.Int("attempt", r.attempt),
		slog.Int("attempts", r.attempts),
		telemetry.FailureClassAttr(r.class),
		slog.Float64("deadline_seconds", r.limit.Seconds()),
		log.ElapsedSeconds(r.elapsed.Seconds()),
		slog.Bool("will_retry", r.willRetry),
		slog.Float64("backoff_seconds", r.backoff.Seconds()),
	}
	if r.err == nil {
		return attrs
	}
	if status := exitStatus(r.err); status != "" {
		attrs = append(attrs, slog.String("exit_status", status))
	}
	attrs = append(attrs, slog.String("error", firstErrorLine(r.stderr, r.err)))
	if r.budgetExhausted {
		attrs = append(attrs, slog.Bool("retry_budget_exhausted", true))
	}
	if r.cleanupErr != nil {
		attrs = append(attrs, slog.String("retry_skipped_reason", "cleanup failed: "+r.cleanupErr.Error()))
	}
	return attrs
}

// spanAttrs are the fields of the git.command_retry span event, the same as
// the log line.
func (r attemptReport) spanAttrs() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(telemetry.MetricDimensionOperation, r.command.Operation),
		attribute.String(telemetry.MetricDimensionHost, r.command.Host),
		attribute.String("git.repository_id", r.command.Scope),
		attribute.Int("git.attempt", r.attempt),
		attribute.Int("git.attempts", r.attempts),
		attribute.String(telemetry.MetricDimensionFailureClass, r.class),
		attribute.Float64("git.deadline_seconds", r.limit.Seconds()),
		attribute.Float64("git.elapsed_seconds", r.elapsed.Seconds()),
		attribute.String("git.exit_status", exitStatus(r.err)),
		attribute.String("git.error", firstErrorLine(r.stderr, r.err)),
		attribute.Bool("git.will_retry", r.willRetry),
		attribute.Float64("git.backoff_seconds", r.backoff.Seconds()),
	}
}

// exitStatus is the process outcome of a failed attempt, "exit status 128" or
// "signal: killed" for a deadline kill. It is empty when the command never
// started.
func exitStatus(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
		return exitErr.String()
	}
	return ""
}

// firstErrorLine is the first line of the underlying error. Fetch and clone
// run with --progress, so remote progress lines come first in a failed run:
// the pick is the first fatal: or error: line, else the first ssh: line, else
// the first remote: line that is not progress, else the first non-empty line,
// else the error text. It is cut at maxErrorLine bytes. stderr arrives
// redacted.
func firstErrorLine(stderr string, err error) string {
	lines := strings.FieldsFunc(stderr, func(r rune) bool { return r == '\n' || r == '\r' })
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	pick := func(match func(lower string) bool) string {
		for _, line := range lines {
			if line != "" && match(strings.ToLower(line)) {
				return line
			}
		}
		return ""
	}
	first := pick(func(l string) bool { return strings.HasPrefix(l, "fatal:") || strings.HasPrefix(l, "error:") })
	if first == "" {
		first = pick(func(l string) bool { return strings.HasPrefix(l, "ssh:") })
	}
	if first == "" {
		first = pick(func(l string) bool { return strings.HasPrefix(l, "remote:") && !remoteProgress(l) })
	}
	if first == "" {
		first = pick(func(string) bool { return true })
	}
	if first == "" && err != nil {
		first = err.Error()
	}
	if len(first) > maxErrorLine {
		first = first[:maxErrorLine]
	}
	return first
}

// remoteProgress reports a remote: line git prints for --progress, never an
// error.
func remoteProgress(lower string) bool {
	for _, prefix := range []string{
		"remote: enumerating objects", "remote: counting objects",
		"remote: compressing objects", "remote: total ", "remote: resolving deltas",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
